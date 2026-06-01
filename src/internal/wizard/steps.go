package wizard

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
)

type SSHPool interface {
	Connect(ctx context.Context, cfg SSHConfig) (SSHSession, error)
}

type SSHConfig struct {
	Host     string
	Port     int
	User     string
	AuthType string
	KeyPath  string
	Password string
}

type SSHSession interface {
	Run(ctx context.Context, cmd string) (stdout string, stderr string, err error)
	Close() error
}

type ValidationError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	IsWarning bool   `json:"is_warning"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

var peerNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func ValidatePeerName(name string) error {
	if name == "" {
		return &ValidationError{Code: "peer_name_empty", Message: "peer name is required"}
	}
	if !peerNameRe.MatchString(name) {
		return &ValidationError{
			Code:    "peer_name_invalid",
			Message: fmt.Sprintf("peer name %q must match ^[a-zA-Z0-9_-]{1,64}$", name),
		}
	}
	return nil
}

func validateStep(ctx context.Context, step WizardStep, inputs interface{}, state *WizardState, sshPool SSHPool) (WizardStep, error) {
	switch step {
	case StepWelcome:
		return StepSSH, nil

	case StepSSH:
		sshInput, ok := inputs.(*SSHInput)
		if !ok {
			return "", &ValidationError{Code: "invalid_input", Message: "expected *SSHInput"}
		}
		if err := validateSSH(ctx, sshInput, sshPool); err != nil {
			return "", err
		}
		return StepPreflight, nil

	case StepPreflight:
		return StepDomainMode, nil

	case StepDomainMode:
		mode, ok := inputs.(string)
		if !ok {
			return "", &ValidationError{Code: "invalid_input", Message: "expected string mode"}
		}
		if err := validateDomainMode(mode); err != nil {
			return "", err
		}
		return StepDomainCheck, nil

	case StepDomainCheck:
		domain, ok := inputs.(string)
		if !ok {
			return "", &ValidationError{Code: "invalid_input", Message: "expected string domain"}
		}
		if err := validateDomainCheck(domain); err != nil {
			return "", err
		}
		return StepCloudflare, nil

	case StepCloudflare:
		token, ok := inputs.(string)
		if !ok {
			return "", &ValidationError{Code: "invalid_input", Message: "expected string token"}
		}
		if err := validateCloudflare(token); err != nil {
			return "", err
		}
		return StepCreatePeer, nil

	case StepCreatePeer:
		name, ok := inputs.(string)
		if !ok {
			return "", &ValidationError{Code: "invalid_input", Message: "expected string name"}
		}
		if err := validateCreatePeer(name); err != nil {
			return "", err
		}
		return StepCommit, nil

	default:
		return "", &ValidationError{Code: "unknown_step", Message: fmt.Sprintf("no validator for step %s", step)}
	}
}

func validateSSH(ctx context.Context, input *SSHInput, sshPool SSHPool) error {
	if input.Host == "" {
		return &ValidationError{Code: "ssh_connection_refused", Message: "host is empty"}
	}
	if input.Port <= 0 || input.Port > 65535 {
		return &ValidationError{Code: "ssh_connection_refused", Message: fmt.Sprintf("invalid port %d", input.Port)}
	}

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	addr := fmt.Sprintf("%s:%d", input.Host, input.Port)
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(connectCtx, "tcp", addr)
	if err != nil {
		return &ValidationError{Code: "ssh_connection_refused", Message: fmt.Sprintf("TCP connect to %s failed: %v", addr, err)}
	}
	conn.Close()

	cfg := SSHConfig{
		Host:     input.Host,
		Port:     input.Port,
		User:     input.User,
		AuthType: input.AuthType,
		KeyPath:  input.KeyPath,
		Password: input.Password,
	}

	sess, err := sshPool.Connect(connectCtx, cfg)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "passphrase") || strings.Contains(errMsg, "decrypt") {
			return &ValidationError{Code: "ssh_passphrase_protected", Message: "SSH key is passphrase-protected"}
		}
		if strings.Contains(errMsg, "unable to authenticate") || strings.Contains(errMsg, "permission denied") {
			return &ValidationError{Code: "ssh_auth_failed", Message: fmt.Sprintf("SSH auth failed: %v", err)}
		}
		return &ValidationError{Code: "ssh_auth_failed", Message: fmt.Sprintf("SSH connection failed: %v", err)}
	}
	defer sess.Close()

	_, _, err = sess.Run(connectCtx, "sudo -n true")
	if err != nil {
		return &ValidationError{Code: "ssh_no_sudo", Message: fmt.Sprintf("user %s lacks sudo: %v", input.User, err)}
	}

	_, _, dockerErr := sess.Run(connectCtx, "docker ps")
	if dockerErr != nil {
		return &ValidationError{
			Code:      "ssh_no_docker",
			Message:   "Docker not found or not running on target host",
			IsWarning: true,
		}
	}

	return nil
}

func validateDomainMode(mode string) error {
	if mode != "byo" && mode != "nipio" {
		return &ValidationError{
			Code:    "domain_mode_invalid",
			Message: fmt.Sprintf("mode must be \"byo\" or \"nipio\", got %q", mode),
		}
	}
	return nil
}

func validateDomainCheck(domain string) error {
	if domain == "" {
		return &ValidationError{Code: "domain_empty", Message: "domain is required"}
	}
	if len(domain) > 253 {
		return &ValidationError{Code: "domain_invalid", Message: "domain exceeds 253 characters"}
	}
	invalidCharRe := regexp.MustCompile(`[^a-zA-Z0-9._-]`)
	if invalidCharRe.MatchString(domain) {
		return &ValidationError{
			Code:    "domain_invalid",
			Message: fmt.Sprintf("domain %q contains invalid characters", domain),
		}
	}
	return nil
}

func validateCloudflare(token string) error {
	if token == "" {
		return &ValidationError{Code: "cloudflare_token_empty", Message: "Cloudflare API token is required"}
	}
	return nil
}

func validateCreatePeer(name string) error {
	return ValidatePeerName(name)
}
