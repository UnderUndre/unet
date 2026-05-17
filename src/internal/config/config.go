package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

// ConfigDir returns the absolute path to the configuration directory
// (~/.unet on all platforms).
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: cannot determine home dir: %w", err)
	}
	return filepath.Join(home, ".unet"), nil
}

// ---------- Data model ----------

// VPSConfig holds VPS connection and provisioning parameters.
type VPSConfig struct {
	Host            string `json:"host,omitempty"`
	SSHPort         int    `json:"sshPort,omitempty"`
	Username        string `json:"username,omitempty"`
	AuthMode        string `json:"authMode,omitempty"` // "key" | "password"
	PrivateKeyPath  string `json:"privateKeyPath,omitempty"`
	Password        SecretString `json:"password,omitempty"`
	IsProvisioned   bool   `json:"isProvisioned,omitempty"`
	ContainerName   string `json:"containerName,omitempty"`
	ImageBuildHash  string `json:"imageBuildHash,omitempty"`
}

// ObfuscationConfig holds AmneziaWG obfuscation parameters.
type ObfuscationConfig struct {
	Jc  int `json:"Jc,omitempty"`
	Jmin int `json:"Jmin,omitempty"`
	Jmax int `json:"Jmax,omitempty"`
	S1  int `json:"S1,omitempty"`
	S2  int `json:"S2,omitempty"`
	S3  int `json:"S3,omitempty"`
	S4  int `json:"S4,omitempty"`
	H1  int `json:"H1,omitempty"`
	H2  int `json:"H2,omitempty"`
	H3  int `json:"H3,omitempty"`
	H4  int `json:"H4,omitempty"`
	I1  int `json:"I1,omitempty"`
	I2  int `json:"I2,omitempty"`
	I3  int `json:"I3,omitempty"`
	I4  int `json:"I4,omitempty"`
	I5  int `json:"I5,omitempty"`
}

// TunnelConfig holds WireGuard/AmneziaWG tunnel parameters.
type TunnelConfig struct {
	InterfaceName       string            `json:"interfaceName,omitempty"`
	Subnet              string            `json:"subnet,omitempty"`
	ServerIP            string            `json:"serverIp,omitempty"`
	LocalIP             string            `json:"localIp,omitempty"`
	ServerEndpoint      string            `json:"serverEndpoint,omitempty"`
	ServerPublicKey     string            `json:"serverPublicKey,omitempty"`
	PresharedKey        SecretString      `json:"presharedKey,omitempty"`
	PrivateKey          SecretString      `json:"privateKey,omitempty"`
	PublicKey           string            `json:"publicKey,omitempty"`
	MTU                 int               `json:"mtu,omitempty"`
	PersistentKeepalive int               `json:"persistentKeepalive,omitempty"`
	Obfuscation         ObfuscationConfig `json:"obfuscation,omitempty"`
	Status              string            `json:"status,omitempty"`
	ConnectedAt         string            `json:"connectedAt,omitempty"`
}

// CaddyAPIConfig holds the Caddy admin API connection details.
type CaddyAPIConfig struct {
	Address    string       `json:"address,omitempty"`
	TLSCert    string       `json:"tlsCert,omitempty"`
	TLSKey     SecretString `json:"tlsKey,omitempty"`
	ClientCert string       `json:"clientCert,omitempty"`
	ClientKey  SecretString `json:"clientKey,omitempty"`
}

// ExposedPort describes a single port-mapping exposed through the proxy.
type ExposedPort struct {
	ID         string `json:"id"`
	Protocol   string `json:"protocol,omitempty"`
	Internal   int    `json:"internal,omitempty"`
	HostHeader string `json:"hostHeader,omitempty"`
	Status     string `json:"status,omitempty"`
	CreatedAt  string `json:"createdAt,omitempty"`
}

// DNSConfig holds DNS provider configuration.
type DNSConfig struct {
	Provider string       `json:"provider,omitempty"`
	Token    SecretString `json:"token,omitempty"`
	Zone     string       `json:"zone,omitempty"`
}

// DaemonConfig holds daemon-level settings.
type DaemonConfig struct {
	LogLevel  string `json:"logLevel,omitempty"`
	PidFile   string `json:"pidFile,omitempty"`
	AutoStart bool   `json:"autoStart,omitempty"`
}

// RootConfig is the top-level JSON structure persisted to disk.
type RootConfig struct {
	VPS          VPSConfig     `json:"vps"`
	Tunnel       TunnelConfig  `json:"tunnel"`
	CaddyAPI     CaddyAPIConfig `json:"caddyApi"`
	ExposedPorts []ExposedPort `json:"exposedPorts"`
	DNS          DNSConfig     `json:"dns"`
	Daemon       DaemonConfig  `json:"daemon"`
	ServerMirror string        `json:"serverMirror,omitempty"`
	UIToken      SecretString  `json:"uiToken,omitempty"`
}

// ---------- Manager ----------

// Manager loads, persists, and provides thread-safe access to the
// configuration file at ~/.unet/config.json.
type Manager struct {
	mu     sync.RWMutex
	config *RootConfig
	path   string // absolute path to config.json
}

// NewManager creates a new configuration Manager.  On first run it
// creates the ~/.unet directory (mode 0700), writes a default skeleton
// config, and returns.  On subsequent runs it loads the existing file.
func NewManager() (*Manager, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	cfgPath := filepath.Join(dir, "config.json")

	// Ensure directory exists with restrictive permissions.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("config: create dir %s: %w", dir, err)
	}

	mgr := &Manager{
		path:   cfgPath,
		config: &RootConfig{},
	}

	// First-run: generate skeleton.
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		slog.Info("config: first run – creating skeleton", "dir", dir)
		mgr.config = defaultConfig()
		if err := mgr.Save(); err != nil {
			return nil, fmt.Errorf("config: write skeleton: %w", err)
		}
		return mgr, nil
	}

	// Existing config: load from disk.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", cfgPath, err)
	}
	if err := json.Unmarshal(data, mgr.config); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", cfgPath, err)
	}

	slog.Info("config: loaded", "path", cfgPath)
	return mgr, nil
}

// Get returns the live config pointer.  Callers MUST NOT modify the
// returned struct; use Update() for mutations.  This accessor provides
// raw values including secrets – reserve it for internal code that
// genuinely needs them (SSH dialer, key derivation, etc.).
func (m *Manager) Get() *RootConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// GetMasked returns a deep copy of the config with every secret-bearing
// field replaced by "****<last-4>" (per FR-011).  Use this in all API
// handler responses and anywhere the config is exposed externally.
func (m *Manager) GetMasked() *RootConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneAndMask(m.config)
}

// Update applies a mutation function to the config and atomically
// persists the result.  The callback receives a writable copy; the
// Manager's internal state is replaced only when Save succeeds.
func (m *Manager) Update(fn func(cfg *RootConfig)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Work on a copy so a failed save doesn't corrupt in-memory state.
	draft := cloneConfig(m.config)
	fn(draft)

	if err := m.saveLocked(draft); err != nil {
		return err
	}
	m.config = draft
	return nil
}

// Save persists the current in-memory config to disk using an atomic
// write (temp file + rename).
func (m *Manager) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveLocked(m.config)
}

// Path returns the absolute path to the config file.
func (m *Manager) Path() string { return m.path }

// ---------- internal helpers ----------

func (m *Manager) saveLocked(cfg *RootConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}

	dir := filepath.Dir(m.path)
	tmp, err := os.CreateTemp(dir, "config.*.tmp")
	if err != nil {
		return fmt.Errorf("config: create temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("config: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("config: sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("config: close temp: %w", err)
	}

	// Set restrictive permissions on the temp file before rename.
	if err := setFilePerm(tmpName, 0o600); err != nil {
		slog.Warn("config: could not set file permissions", "path", tmpName, "err", err)
	}

	if err := atomicRename(tmpName, m.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("config: rename %s -> %s: %w", tmpName, m.path, err)
	}

	slog.Debug("config: saved", "path", m.path)
	return nil
}

// defaultConfig returns a skeleton configuration with a fresh uiToken
// and empty exposedPorts.
func defaultConfig() *RootConfig {
	return &RootConfig{
		UIToken:      SecretString(uuid.New().String()),
		ExposedPorts: []ExposedPort{},
	}
}

// ---------- deep-copy / mask helpers ----------

// cloneConfig performs a deep copy of the RootConfig.
func cloneConfig(src *RootConfig) *RootConfig {
	dst := *src // shallow copy of value types

	// Deep copy slices.
	if src.ExposedPorts != nil {
		dst.ExposedPorts = make([]ExposedPort, len(src.ExposedPorts))
		copy(dst.ExposedPorts, src.ExposedPorts)
	}

	return &dst
}

// cloneAndMask returns a deep copy with secrets masked.
func cloneAndMask(src *RootConfig) *RootConfig {
	dst := cloneConfig(src)

	// VPS
	dst.VPS.Password = SecretString(src.VPS.Password.mask())

	// Tunnel
	dst.Tunnel.PresharedKey = SecretString(src.Tunnel.PresharedKey.mask())
	dst.Tunnel.PrivateKey = SecretString(src.Tunnel.PrivateKey.mask())

	// CaddyAPI
	dst.CaddyAPI.TLSKey = SecretString(src.CaddyAPI.TLSKey.mask())
	dst.CaddyAPI.ClientKey = SecretString(src.CaddyAPI.ClientKey.mask())

	// DNS
	dst.DNS.Token = SecretString(src.DNS.Token.mask())

	// UIToken
	dst.UIToken = SecretString(src.UIToken.mask())

	return dst
}
