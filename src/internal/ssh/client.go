// Package ssh provides a pooled SSH client factory with support for key and
// password authentication. It wraps golang.org/x/crypto/ssh with connection
// pooling, idle eviction, and pre-use validation.
package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/underundre/unet/internal/config"
)

// hostMetacharRe matches shell metacharacters that must never appear in a
// hostname to prevent injection through crafted host strings.
var hostMetacharRe = regexp.MustCompile(`[;<>|` + "`" + `$\n]`)

// knownHostsMu serializes read-and-append access to the SSH known_hosts
// file to prevent corruption from concurrent pool connections.
var knownHostsMu sync.Mutex

// ErrInvalidHost is returned when the VPS host contains forbidden characters.
var ErrInvalidHost = errors.New("ssh: host contains forbidden metacharacters")

// AuthMode enumerates supported SSH authentication methods.
type AuthMode string

const (
	AuthModeKey      AuthMode = "key"
	AuthModePassword AuthMode = "password"
)

// ConnectConfig holds everything needed to establish an SSH connection.
// Constructed from VPSProfile (spec 003 data model) or VPSConfig (spec 002).
type ConnectConfig struct {
	Host           string
	Port           int
	User           string
	AuthMode       AuthMode
	PrivateKeyPath string        // Required when AuthMode == key
	Password       string        // Required when AuthMode == password
	ConnectTimeout time.Duration // Default 30s
}

// ConnectConfigFromVPSConfig converts a config.VPSConfig into a ConnectConfig.
func ConnectConfigFromVPSConfig(vps config.VPSConfig) ConnectConfig {
	return ConnectConfig{
		Host:           vps.Host,
		Port:           vps.SSHPort,
		User:           vps.Username,
		AuthMode:       AuthMode(vps.AuthMode),
		PrivateKeyPath: vps.PrivateKeyPath,
		Password:       vps.Password.Plain(),
		ConnectTimeout: 30 * time.Second,
	}
}

// Validate checks that the ConnectConfig has all required fields.
func (c ConnectConfig) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("ssh: host is required")
	}
	if err := validateHost(c.Host); err != nil {
		return err
	}
	if c.User == "" {
		return fmt.Errorf("ssh: user is required")
	}
	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("ssh: port %d out of range", c.Port)
	}
	switch c.AuthMode {
	case AuthModeKey:
		if c.PrivateKeyPath == "" {
			return fmt.Errorf("ssh: privateKeyPath required for key auth")
		}
	case AuthModePassword:
		if c.Password == "" {
			return fmt.Errorf("ssh: password required for password auth")
		}
	default:
		return fmt.Errorf("ssh: unsupported auth mode %q", c.AuthMode)
	}
	return nil
}

// Dial establishes a new SSH connection using the provided config.
// The caller is responsible for closing the returned client.
func Dial(ctx context.Context, cfg ConnectConfig) (*gossh.Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	port := cfg.Port
	if port == 0 {
		port = 22
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)

	timeout := cfg.ConnectTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	sshCfg := &gossh.ClientConfig{
		User:            cfg.User,
		HostKeyCallback: nil, // set below
		Timeout:         timeout,
	}

	hkcb, err := hostKeyCallback(cfg.Host)
	if err != nil {
		return nil, fmt.Errorf("ssh: host key callback: %w", err)
	}
	sshCfg.HostKeyCallback = hkcb

	// Auth methods.
	switch cfg.AuthMode {
	case AuthModeKey:
		auth, err := publicKeyAuth(cfg.PrivateKeyPath)
		if err != nil {
			return nil, err
		}
		sshCfg.Auth = []gossh.AuthMethod{auth}
	case AuthModePassword:
		sshCfg.Auth = []gossh.AuthMethod{gossh.Password(cfg.Password)}
	}

	// Dial with context support.
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("ssh: dial %s: %w", addr, err)
	}

	sshConn, chans, reqs, err := gossh.NewClientConn(conn, addr, sshCfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("ssh: handshake %s: %w", addr, err)
	}

	return gossh.NewClient(sshConn, chans, reqs), nil
}

// --- host key verification ---

// knownHostsPath returns the path to the SSH known_hosts file
// (~/.unet/ssh_known_hosts).
func knownHostsPath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ssh_known_hosts"), nil
}

// hostKeyCallback returns a gossh.HostKeyCallback that accepts new host keys
// (first connection) and verifies them on subsequent connections.
func hostKeyCallback(host string) (gossh.HostKeyCallback, error) {
	khPath, err := knownHostsPath()
	if err != nil {
		return nil, fmt.Errorf("resolve known_hosts path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(khPath), 0o700); err != nil {
		return nil, fmt.Errorf("create known_hosts dir: %w", err)
	}

	if _, err := os.Stat(khPath); os.IsNotExist(err) {
		if err := os.WriteFile(khPath, nil, 0o600); err != nil {
			return nil, fmt.Errorf("create known_hosts: %w", err)
		}
	}

	return func(hostname string, remote net.Addr, key gossh.PublicKey) error {
		knownHostsMu.Lock()
		defer knownHostsMu.Unlock()

		f, err := os.Open(khPath)
		if err != nil {
			return fmt.Errorf("open known_hosts: %w", err)
		}
		defer f.Close()

		knownKeyStr := gossh.MarshalAuthorizedKey(key)
		line := fmt.Sprintf("%s %s", hostname, strings.TrimSpace(string(knownKeyStr)))

		scanner := &lineScanner{reader: f}
		for scanner.scan() {
			if scanner.text() == line {
				return nil
			}
		}

		// First time seeing this host/key pair — store it.
		f.Close()
		f2, err := os.OpenFile(khPath, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("open known_hosts for append: %w", err)
		}
		defer f2.Close()
		if _, err := fmt.Fprintln(f2, line); err != nil {
			return fmt.Errorf("write known_hosts: %w", err)
		}
		slog.Info("ssh: stored host key", "host", hostname)
		return nil
	}, nil
}

// --- auth helpers ---

// publicKeyAuth reads a private key file and returns a gossh.AuthMethod.
func publicKeyAuth(path string) (gossh.AuthMethod, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ssh: read private key %s: %w", path, err)
	}
	signer, err := gossh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("ssh: parse private key %s: %w", path, err)
	}
	return gossh.PublicKeys(signer), nil
}

// --- validation ---

// validateHost rejects hosts that contain shell metacharacters.
func validateHost(host string) error {
	if hostMetacharRe.MatchString(host) {
		return fmt.Errorf("%w: %q", ErrInvalidHost, host)
	}
	return nil
}

// --- line scanner (no bufio dependency) ---

type lineScanner struct {
	reader io.Reader
	buf    string
	line   string
}

func (ls *lineScanner) scan() bool {
	for {
		if i := strings.IndexByte(ls.buf, '\n'); i >= 0 {
			ls.line = ls.buf[:i]
			ls.buf = ls.buf[i+1:]
			return true
		}
		chunk := make([]byte, 4096)
		n, err := ls.reader.Read(chunk)
		if n > 0 {
			ls.buf += string(chunk[:n])
			continue
		}
		if err != nil {
			if ls.buf != "" {
				ls.line = ls.buf
				ls.buf = ""
				return true
			}
			return false
		}
	}
}

func (ls *lineScanner) text() string { return ls.line }

// ShellEscape wraps a string in single quotes, escaping embedded single quotes.
func ShellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
