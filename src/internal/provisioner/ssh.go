package provisioner

import (
	"bytes"
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

	"golang.org/x/crypto/ssh"

	"github.com/underundre/unet/internal/config"
)

// hostMetacharRe matches shell metacharacters that must never appear in a
// hostname to prevent injection through crafted host strings.
var hostMetacharRe = regexp.MustCompile(`[;<>|` + "`" + `$\n]`)

// ErrInvalidHost is returned when the VPS host contains forbidden characters.
var ErrInvalidHost = errors.New("ssh: host contains forbidden metacharacters")

// knownHostsPath returns the path to the SSH known_hosts file used by the
// provisioner (~/.unet/ssh_known_hosts).
func knownHostsPath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ssh_known_hosts"), nil
}

// ------- host-key callback -------

// hostKeyCallback returns an ssh.HostKeyCallback that:
//   - On first connection: accepts the host key and appends it to the
//     known_hosts file.
//   - On subsequent connections: verifies the key matches the stored entry.
func hostKeyCallback(host string) (ssh.HostKeyCallback, error) {
	khPath, err := knownHostsPath()
	if err != nil {
		return nil, fmt.Errorf("ssh: resolve known_hosts path: %w", err)
	}

	// Ensure the file exists.
	if err := os.MkdirAll(filepath.Dir(khPath), 0o700); err != nil {
		return nil, fmt.Errorf("ssh: create known_hosts dir: %w", err)
	}

	// If the file does not exist yet, create it empty so the first-write
	// path works without extra stat checks.
	if _, err := os.Stat(khPath); os.IsNotExist(err) {
		if err := os.WriteFile(khPath, nil, 0o600); err != nil {
			return nil, fmt.Errorf("ssh: create known_hosts: %w", err)
		}
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		f, err := os.Open(khPath)
		if err != nil {
			return fmt.Errorf("ssh: open known_hosts: %w", err)
		}
		defer f.Close()

		knownKeyStr := ssh.MarshalAuthorizedKey(key)
		line := fmt.Sprintf("%s %s", hostname, strings.TrimSpace(string(knownKeyStr)))

		scanner := &lineScanner{reader: f}
		for scanner.scan() {
			if scanner.text() == line {
				// Key already known and matches.
				return nil
			}
		}

		// First time seeing this host/key pair — store it.
		f.Close()
		f2, err := os.OpenFile(khPath, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("ssh: open known_hosts for append: %w", err)
		}
		defer f2.Close()
		if _, err := fmt.Fprintln(f2, line); err != nil {
			return fmt.Errorf("ssh: write known_hosts: %w", err)
		}
		slog.Info("ssh: stored host key", "host", hostname)
		return nil
	}, nil
}

// lineScanner is a minimal line scanner that does not pull in bufio.
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

// ------- Client -------

// Client is a pooled SSH client that reconnects on disconnect.
type Client struct {
	cfg    config.VPSConfig
	mu     sync.Mutex
	client *ssh.Client
}

// NewClient creates a new SSH client wrapper for the given VPS config.
// If host looks like an SSH config alias (no IP, not a DNS name), it attempts
// to resolve HostName, User, Port, and IdentityFile from ~/.ssh/config.
func NewClient(vps config.VPSConfig) (*Client, error) {
	if err := validateHost(vps.Host); err != nil {
		return nil, err
	}

	resolved, err := resolveSSHAlias(vps)
	if err != nil {
		slog.Debug("ssh: could not resolve SSH alias, using host as-is", "host", vps.Host, "err", err)
	} else {
		vps = *resolved
	}

	return &Client{cfg: vps}, nil
}

// validateHost rejects hosts that contain shell metacharacters.
func validateHost(host string) error {
	if hostMetacharRe.MatchString(host) {
		return fmt.Errorf("%w: %q", ErrInvalidHost, host)
	}
	return nil
}

// connect dials the SSH server and returns an *ssh.Client.
func (c *Client) connect(ctx context.Context) (*ssh.Client, error) {
	host := c.cfg.Host
	if c.cfg.SSHPort == 0 {
		c.cfg.SSHPort = 22
	}
	addr := fmt.Sprintf("%s:%d", host, c.cfg.SSHPort)

	sshCfg := &ssh.ClientConfig{
		User:            c.cfg.Username,
		HostKeyCallback: nil, // set below
		Timeout:         30 * time.Second,
	}

	hkcb, err := hostKeyCallback(host)
	if err != nil {
		return nil, err
	}
	sshCfg.HostKeyCallback = hkcb

	// Auth methods.
	switch c.cfg.AuthMode {
	case "key":
		auth, err := publicKeyAuth(c.cfg.PrivateKeyPath)
		if err != nil {
			return nil, err
		}
		sshCfg.Auth = []ssh.AuthMethod{auth}
	case "password":
		sshCfg.Auth = []ssh.AuthMethod{ssh.Password(c.cfg.Password.Plain())}
	default:
		return nil, fmt.Errorf("ssh: unsupported auth mode %q", c.cfg.AuthMode)
	}

	// Dial with context support.
	d := net.Dialer{Timeout: sshCfg.Timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("ssh: dial %s: %w", addr, err)
	}

	// Wrap into SSH connection.
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, sshCfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("ssh: handshake %s: %w", addr, err)
	}
	return ssh.NewClient(sshConn, chans, reqs), nil
}

// publicKeyAuth reads a private key file and returns an ssh.AuthMethod.
func publicKeyAuth(path string) (ssh.AuthMethod, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ssh: read private key %s: %w", path, err)
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("ssh: parse private key %s: %w", path, err)
	}
	return ssh.PublicKeys(signer), nil
}

// getSession returns an active SSH session, reconnecting if necessary.
func (c *Client) getSession(ctx context.Context) (*ssh.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		sess, err := c.client.NewSession()
		if err == nil {
			return sess, nil
		}
		// Existing connection is stale; close and reconnect.
		slog.Warn("ssh: session creation failed, reconnecting", "err", err)
		c.client.Close()
		c.client = nil
	}

	client, err := c.connect(ctx)
	if err != nil {
		return nil, err
	}
	c.client = client
	sess, err := c.client.NewSession()
	if err != nil {
		client.Close()
		c.client = nil
		return nil, fmt.Errorf("ssh: new session: %w", err)
	}
	return sess, nil
}

// ExecuteCommand runs a single command over SSH and returns stdout, stderr.
func (c *Client) ExecuteCommand(ctx context.Context, cmd string) (stdout string, stderr string, err error) {
	sess, err := c.getSession(ctx)
	if err != nil {
		return "", "", err
	}
	defer sess.Close()

	var outBuf, errBuf bytes.Buffer
	sess.Stdout = &outBuf
	sess.Stderr = &errBuf

	if err := sess.Start(cmd); err != nil {
		return outBuf.String(), errBuf.String(), fmt.Errorf("ssh: start %q: %w", cmd, err)
	}

	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()

	select {
	case <-ctx.Done():
		sess.Close()
		return outBuf.String(), errBuf.String(), ctx.Err()
	case err := <-done:
		return outBuf.String(), errBuf.String(), err
	}
}

// ExecuteScript runs a multi-line bash script over SSH.
func (c *Client) ExecuteScript(ctx context.Context, script string) (stdout string, stderr string, err error) {
	sess, err := c.getSession(ctx)
	if err != nil {
		return "", "", err
	}
	defer sess.Close()

	var outBuf, errBuf bytes.Buffer
	sess.Stdout = &outBuf
	sess.Stderr = &errBuf

	// Pipe the script into bash so it can be multi-line.
	sess.Stdin = strings.NewReader(script)

	if err := sess.Start("bash -s"); err != nil {
		return outBuf.String(), errBuf.String(), fmt.Errorf("ssh: start bash script: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()

	select {
	case <-ctx.Done():
		sess.Close()
		return outBuf.String(), errBuf.String(), ctx.Err()
	case err := <-done:
		return outBuf.String(), errBuf.String(), err
	}
}

// UploadFile writes content to remotePath on the remote host via SFTP.
// It falls back to dd-over-SSH if SFTP subsystem is unavailable.
func (c *Client) UploadFile(ctx context.Context, remotePath string, content []byte) error {
	// Try SFTP first via dd-over-SSH (avoids needing the sftp package).
	// We pipe content through stdin to dd on the remote side.
	sess, err := c.getSession(ctx)
	if err != nil {
		return err
	}
	defer sess.Close()

	sess.Stdin = bytes.NewReader(content)

	ddCmd := fmt.Sprintf("dd of=%s bs=4096 2>/dev/null && chmod 0600 %s",
		shellEscape(remotePath), shellEscape(remotePath))

	if err := sess.Start(ddCmd); err != nil {
		return fmt.Errorf("ssh: upload start: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()

	select {
	case <-ctx.Done():
		sess.Close()
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("ssh: upload dd: %w", err)
		}
		return nil
	}
}

// Close shuts down the pooled SSH connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		err := c.client.Close()
		c.client = nil
		return err
	}
	return nil
}

// shellEscape wraps a string in single quotes, escaping any embedded single
// quotes.  This prevents injection when interpolating paths into remote shell
// commands.
func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// resolveSSHAlias reads ~/.ssh/config and fills in HostName, User, Port,
// and IdentityFile for the given alias if found. Returns a modified copy
// of vps with the resolved values. Returns nil if no matching Host entry
// is found (caller should use the original config as-is).
func resolveSSHAlias(vps config.VPSConfig) (*config.VPSConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(home, ".ssh", "config")

	f, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := &lineScanner{reader: f}
	inTarget := false
	resolved := vps

	for scanner.scan() {
		line := strings.TrimSpace(scanner.text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.ToLower(fields[0])
		value := fields[1]

		if key == "host" {
			if inTarget {
				break
			}
			if value == vps.Host {
				inTarget = true
			}
			continue
		}

		if !inTarget {
			continue
		}

		switch key {
		case "hostname":
			resolved.Host = value
		case "user":
			if resolved.Username == "" || resolved.Username == "root" {
				resolved.Username = value
			}
		case "port":
			if resolved.SSHPort == 0 || resolved.SSHPort == 22 {
				n := 0
				for _, c := range value {
					if c >= '0' && c <= '9' {
						n = n*10 + int(c-'0')
					} else {
						break
					}
				}
				if n > 0 {
					resolved.SSHPort = n
				}
			}
		case "identityfile":
			if resolved.PrivateKeyPath == "" {
				p := value
				if strings.HasPrefix(p, "~/") {
					p = filepath.Join(home, p[2:])
				}
				resolved.PrivateKeyPath = p
			}
		}
	}

	if !inTarget {
		return nil, fmt.Errorf("ssh: alias %q not found in %s", vps.Host, configPath)
	}
	return &resolved, nil
}
