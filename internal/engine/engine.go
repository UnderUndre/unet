// Package engine bootstraps the embedded AmneziaWG device over the in-memory
// gVisor netstack (FR-001, FR-002) from a validated config.Config. The UAPI
// surface of device.Device is the single configuration entry point: keys are
// HEX there (device/noise-types.go FromHex), while TunnelConfig stores base64.
package engine

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strings"

	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"github.com/amnezia-vpn/amneziawg-go/internal/config"
	"github.com/amnezia-vpn/amneziawg-go/tun/netstack"
)

// Tunnel owns the live device + netstack handle.
type Tunnel struct {
	device *device.Device
	Net    *netstack.Net // SOCKS5/HTTP proxy (T2) and yamux dialer (T3) dial through this
}

// Start builds the netstack TUN, feeds the device via IpcSet, and brings it up.
// No privileged operations: netstack is fully user-space (SC-002).
func Start(cfg *config.Config, logLevel string) (*Tunnel, error) {
	if cfg == nil {
		return nil, fmt.Errorf("engine: nil config")
	}
	logger := device.NewLogger(parseLevel(logLevel), "unet/")

	localAddrs := make([]netip.Addr, 0, len(cfg.LocalAddresses))
	for _, p := range cfg.LocalAddresses {
		localAddrs = append(localAddrs, p.Addr())
	}

	tunDev, tnet, err := netstack.CreateNetTUN(localAddrs, cfg.DNS, cfg.MTU)
	if err != nil {
		return nil, fmt.Errorf("engine: create netstack TUN: %w", err)
	}

	dev := device.NewDevice(tunDev, conn.NewDefaultBind(), logger)
	uapi := BuildUapiConfig(cfg)
	if err := dev.IpcSet(uapi); err != nil {
		dev.Close()
		return nil, fmt.Errorf("engine: IpcSet: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, fmt.Errorf("engine: device up: %w", err)
	}
	return &Tunnel{device: dev, Net: tnet}, nil
}

// Close tears the device down (FR-007: listeners above survive and are
// managed by their owners; the device is engine-owned).
func (t *Tunnel) Close() {
	if t != nil && t.device != nil {
		t.device.Close()
	}
}

// BuildUapiConfig renders the validated config into the device UAPI text
// protocol (device/uapi.go). Keys: base64 (config) → hex (UAPI, FromHex).
// jc/jmin/jmax are emitted ONLY when present and >=1 — UAPI rejects <=0
// (zcode F4); omission = disabled.
func BuildUapiConfig(cfg *config.Config) string {
	var b strings.Builder
	w := func(format string, args ...interface{}) {
		fmt.Fprintf(&b, format+"\n", args...)
	}
	keyHex := func(k string) string {
		raw, err := base64.StdEncoding.DecodeString(k)
		if err != nil {
			return "" // validated upstream; defensive
		}
		return hex.EncodeToString(raw)
	}

	w("private_key=%s", keyHex(cfg.PrivateKey))

	o := cfg.Obfuscation
	if o.JC != nil {
		w("jc=%d", *o.JC)
	}
	if o.JMin != nil {
		w("jmin=%d", *o.JMin)
	}
	if o.JMax != nil {
		w("jmax=%d", *o.JMax)
	}
	w("s1=%d", o.S1)
	w("s2=%d", o.S2)
	w("s3=%d", o.S3)
	w("s4=%d", o.S4)
	for i, h := range []config.HeaderValue{o.H1, o.H2, o.H3, o.H4} {
		w("h%d=%s", i+1, string(h))
	}
	for i, tag := range []string{o.I1, o.I2, o.I3, o.I4, o.I5} {
		if tag != "" {
			w("i%d=%s", i+1, tag)
		}
	}

	// Peer section: blank line introduces a new peer (device/uapi.go).
	w("")
	w("public_key=%s", keyHex(cfg.Peer.PublicKey))
	if cfg.Peer.PresharedKey != "" {
		w("preshared_key=%s", keyHex(cfg.Peer.PresharedKey))
	}
	w("endpoint=%s", cfg.Peer.Endpoint)
	for _, p := range cfg.Peer.AllowedIPs {
		w("allowed_ip=%s", p.String())
	}
	if cfg.Peer.PersistentKeepalive != nil {
		w("persistent_keepalive_interval=%d", *cfg.Peer.PersistentKeepalive)
	}
	return b.String()
}

func parseLevel(s string) int {
	switch strings.ToLower(s) {
	case "debug":
		return device.LogLevelVerbose
	case "info", "":
		return device.LogLevelError // quiet by default; debug opt-in (FR-008.004)
	case "warn":
		return device.LogLevelError
	default:
		return device.LogLevelError
	}
}
