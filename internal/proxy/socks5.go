package proxy

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// SOCKS5 constants (RFC 1928/1929).
const (
	socksVersion      = 0x05
	authNone          = 0x00
	authUserPass      = 0x02
	authVersionUP     = 0x01 // RFC 1929 sub-negotiation version
	cmdConnect        = 0x01
	atypIPv4          = 0x01
	atypDomain        = 0x03
	atypIPv6          = 0x04
	repSucceeded      = 0x00
	repGeneralFail    = 0x01
	repNetUnreachable = 0x03
	repHostUnreach    = 0x04
	repTTLExpired     = 0x06
	repCmdUnsupported = 0x07
)

// serveSOCKS5 runs the RFC 1928 handshake and bridges the tunnel (FR-004).
func (s *Server) serveSOCKS5(br *peekReader, conn net.Conn) {
	// --- greeting: VER NMETHODS METHODS
	hdr, err := readFull(br.br, 2)
	if err != nil || hdr[0] != socksVersion {
		return
	}
	nmethods := int(hdr[1])
	if nmethods == 0 || nmethods > 255 {
		return
	}
	methods, err := readFull(br.br, nmethods)
	if err != nil {
		return
	}
	chosen := byte(authNone)
	if s.cfg.AuthEnabled {
		chosen = authUserPass
	}
	supported := false
	for _, m := range methods {
		if m == chosen {
			supported = true
			break
		}
	}
	if !supported {
		_, _ = conn.Write([]byte{socksVersion, 0xFF}) // no acceptable methods
		return
	}
	if _, err := conn.Write([]byte{socksVersion, chosen}); err != nil {
		return
	}

	// --- RFC 1929 username/password sub-negotiation
	if s.cfg.AuthEnabled {
		if err := socksAuth(br.br, s); err != nil {
			_, _ = conn.Write([]byte{authVersionUP, 0x01})
			// No secret material in logs — reason only (FR-008.004).
			s.log.Warnf("socks5: auth failed from %s", conn.RemoteAddr())
			return
		}
		_, _ = conn.Write([]byte{authVersionUP, 0x00})
	}

	// --- request: VER CMD RSV ATYP DST.ADDR DST.PORT
	req, err := readFull(br.br, 4)
	if err != nil || req[0] != socksVersion {
		return
	}
	if req[1] != cmdConnect {
		socksReply(conn, req[1], repCmdUnsupported)
		return
	}
	host, err := parseSOCKSAddress(br.br, req[3])
	if err != nil {
		socksReply(conn, req[1], repGeneralFail)
		return
	}
	portBytes, err := readFull(br.br, 2)
	if err != nil {
		return
	}
	port := binary.BigEndian.Uint16(portBytes)
	if port == 0 {
		socksReply(conn, req[1], repGeneralFail)
		return
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.DialTimeout)
	defer cancel()
	up, err := s.dial.DialContext(ctx, "tcp", addr)
	if err != nil {
		// Target hosts carry no credentials; strip userinfo anyway for belt
		// and braces (FR-008.004).
		s.log.Warnf("socks5: dial %s: %v", redactTarget(host), err)
		socksReply(conn, req[1], mapDialError(err))
		return
	}
	defer up.Close()

	// Success: BND.ADDR unspecified (0.0.0.0:0) — egress belongs to the tunnel.
	socksReply(conn, req[1], repSucceeded)
	bridge(conn, up)
}

// socksAuth verifies the RFC 1929 username/password block.
func socksAuth(r *bufio.Reader, s *Server) error {
	ver, err := readFull(r, 1)
	if err != nil || ver[0] != authVersionUP {
		return errors.New("bad auth version")
	}
	ulen, err := readFull(r, 1)
	if err != nil || ulen[0] == 0 || ulen[0] > 255 {
		return errors.New("bad username length")
	}
	user, err := readFull(r, int(ulen[0]))
	if err != nil {
		return err
	}
	plen, err := readFull(r, 1)
	if err != nil {
		return err
	}
	pass := make([]byte, 0)
	if plen[0] > 0 {
		pass, err = readFull(r, int(plen[0]))
		if err != nil {
			return err
		}
	}
	expected := []byte(s.resolvePassword())
	if string(user) != s.cfg.Username || !constantTimeEqual(pass, expected) {
		return errors.New("credential mismatch")
	}
	return nil
}

// parseSOCKSAddress decodes DST.ADDR per ATYP. Domain targets pass through to
// the netstack dialer, which resolves via tunnel-side DNS only (FR-009).
func parseSOCKSAddress(r *bufio.Reader, atyp byte) (string, error) {
	switch atyp {
	case atypIPv4:
		b, err := readFull(r, 4)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3]), nil
	case atypDomain:
		l, err := readFull(r, 1)
		if err != nil {
			return "", err
		}
		if l[0] == 0 {
			return "", errors.New("empty domain")
		}
		name, err := readFull(r, int(l[0]))
		if err != nil {
			return "", err
		}
		return string(name), nil
	case atypIPv6:
		b, err := readFull(r, 16)
		if err != nil {
			return "", err
		}
		return net.IP(b).String(), nil
	default:
		return "", fmt.Errorf("unsupported ATYP 0x%02x", atyp)
	}
}

// mapDialError translates dial failures into SOCKS5 reply codes (§1.3).
func mapDialError(err error) byte {
	if errors.Is(err, context.DeadlineExceeded) {
		return repTTLExpired
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "no address") || strings.Contains(msg, "resolution") ||
		strings.Contains(msg, "no such host") || strings.Contains(msg, "nxdomain") ||
		strings.Contains(msg, "temporary failure in name resolution") {
		return repHostUnreach // in-tunnel resolution failed → fail closed (FR-009.002)
	}
	return repNetUnreachable
}

// socksReply writes a reply with BND.ADDR 0.0.0.0:0.
func socksReply(conn net.Conn, cmd, rep byte) {
	_, _ = conn.Write([]byte{socksVersion, rep, 0x00, atypIPv4, 0, 0, 0, 0, 0, 0})
}

// readFull reads exactly n bytes.
func readFull(r io.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// bridge copies bytes both directions until either side finishes; idle
// deadlines prevent silently-dead peers from pinning buffers (FR-007.005 iv).
func bridge(client, upstream net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		_ = copyWithDeadline(upstream, client)
		halfClose(upstream)
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		_ = copyWithDeadline(client, upstream)
		halfClose(client)
	}()
	<-done
	<-done
}

// halfClose signals "no more data" without killing the read side.
func halfClose(c net.Conn) {
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.CloseWrite()
	}
}

// copyWithDeadline enforces an idle read deadline per chunk.
func copyWithDeadline(dst, src net.Conn) error {
	buf := make([]byte, 32*1024)
	for {
		_ = src.SetReadDeadline(time.Now().Add(5 * time.Minute))
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
				errors.Is(err, io.ErrClosedPipe) || isTimeout(err) {
				return nil
			}
			return err
		}
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func constantTimeEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// redactTarget strips userinfo from a target for logging (FR-008.004).
func redactTarget(host string) string {
	if i := strings.Index(host, "@"); i >= 0 {
		return host[i+1:]
	}
	return host
}
