package provisioner

import (
	"fmt"
	"strings"
)

// FirewallConfig holds parameters for UFW rule provisioning.
type FirewallConfig struct {
	// AWGPort is the AmneziaWG listen port to open over UDP.
	AWGPort int
	// ManualDNS is true when DNS mode is "manual" (port 80/tcp needed
	// for ACME HTTP-01 challenge).
	ManualDNS bool
}

// UFWRule represents a single UFW allow rule with a descriptive comment.
type UFWRule struct {
	Port     int
	Protocol string // "udp" or "tcp"
	Comment  string
}

// UFWRules returns the list of firewall rules that should be opened when
// UFW is active on the VPS. The caller is responsible for detecting UFW
// state and executing these commands over SSH.
//
// Rules per research.md §6:
//   - <AWGPort>/udp  — AmneziaWG handshake
//   - 443/tcp        — Caddy HTTPS
//   - 80/tcp         — Caddy ACME HTTP-01 (manual DNS mode only)
func UFWRules(cfg FirewallConfig) []UFWRule {
	rules := []UFWRule{
		{Port: cfg.AWGPort, Protocol: "udp", Comment: "unet AmneziaWG"},
		{Port: 443, Protocol: "tcp", Comment: "unet Caddy HTTPS"},
	}
	if cfg.ManualDNS {
		rules = append(rules, UFWRule{
			Port:     80,
			Protocol: "tcp",
			Comment:  "unet Caddy ACME HTTP-01",
		})
	}
	return rules
}

// UFWAllowCommand returns the shell command string for a single UFW rule.
func UFWAllowCommand(r UFWRule) string {
	return fmt.Sprintf("ufw allow %d/%s comment '%s'", r.Port, r.Protocol, r.Comment)
}

// UFWAllowScript returns a multi-line shell script that opens all required
// UFW rules. It is intended to be executed over SSH after confirming that
// UFW is installed and active.
func UFWAllowScript(cfg FirewallConfig) string {
	rules := UFWRules(cfg)
	lines := make([]string, 0, len(rules)+2)
	lines = append(lines, "#!/bin/bash")
	lines = append(lines, "set -e")
	for _, r := range rules {
		lines = append(lines, UFWAllowCommand(r))
	}
	return strings.Join(lines, "\n")
}

// DetectUFWCommands returns the SSH commands needed to detect whether UFW
// is installed and active on the remote host.
//
// Returns two commands:
//  1. "command -v ufw" — exits 0 if ufw is on PATH, non-zero otherwise.
//  2. "ufw status"     — output contains "Status: active" when UFW is enabled.
//
// The caller should run these over SSH and check results before running
// UFWAllowScript. If the first command fails (ufw not installed), skip
// firewall provisioning entirely (graceful skip per T008d).
func DetectUFWCommands() (checkInstalled, checkStatus string) {
	return "command -v ufw", "ufw status"
}

// IsUFWActive parses the output of "ufw status" and returns true if the
// firewall is active.
func IsUFWActive(ufwStatusOutput string) bool {
	return strings.Contains(ufwStatusOutput, "Status: active")
}
