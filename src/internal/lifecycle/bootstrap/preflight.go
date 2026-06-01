// Package bootstrap implements the idempotent VPS bootstrap engine:
// preflight checks, Docker install, compose deploy, health verification, rollback.
package bootstrap

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/underundre/unet/internal/ssh"
)

// --- Preflight ---

// CheckStatus is the outcome of a single preflight check.
type CheckStatus string

const (
	CheckPass CheckStatus = "pass"
	CheckFail CheckStatus = "fail"
)

// PreflightCheck is a single check result.
type PreflightCheck struct {
	Name    string      `json:"name"`
	Status  CheckStatus `json:"status"`
	Message string      `json:"message"`
	Output  string      `json:"output,omitempty"`
}

// PreflightResult aggregates all preflight check results.
type PreflightResult struct {
	Pass    bool             `json:"pass"`
	Checks  []PreflightCheck `json:"checks"`
}

// RunPreflight executes all Phase 1 checks over SSH. All checks are read-only
// and idempotent. Completes in < 5 seconds.
func RunPreflight(ctx context.Context, sess *ssh.Session) (*PreflightResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	result := &PreflightResult{Pass: true}

	// Check 1: Architecture (must be x86_64 or aarch64)
	result.Checks = append(result.Checks, checkArch(ctx, sess))
	// Check 2: OS (must be Ubuntu 22.04 or 24.04)
	result.Checks = append(result.Checks, checkOS(ctx, sess))
	// Check 3: Disk space (>= 2GB free on /)
	result.Checks = append(result.Checks, checkDisk(ctx, sess))
	// Check 4: Passwordless sudo
	result.Checks = append(result.Checks, checkSudo(ctx, sess))

	for _, c := range result.Checks {
		if c.Status == CheckFail {
			result.Pass = false
			break
		}
	}

	return result, nil
}

func checkArch(ctx context.Context, sess *ssh.Session) PreflightCheck {
	const name = "architecture"
	out, err := sess.Run(ctx, "uname -m")
	out = strings.TrimSpace(out)

	if err != nil {
		return PreflightCheck{
			Name:    name,
			Status:  CheckFail,
			Message: fmt.Sprintf("Failed to detect architecture: %v", err),
			Output:  out,
		}
	}

	switch out {
	case "x86_64", "aarch64":
		return PreflightCheck{Name: name, Status: CheckPass, Message: "Supported architecture", Output: out}
	default:
		return PreflightCheck{
			Name:    name,
			Status:  CheckFail,
			Message: "Unsupported architecture: " + out + ". Only x86_64 and aarch64 are supported.",
			Output:  out,
		}
	}
}

func checkOS(ctx context.Context, sess *ssh.Session) PreflightCheck {
	const name = "os"
	out, err := sess.Run(ctx, "cat /etc/os-release")
	if err != nil {
		return PreflightCheck{
			Name:    name,
			Status:  CheckFail,
			Message: fmt.Sprintf("Failed to read /etc/os-release: %v", err),
			Output:  out,
		}
	}

	// Parse ID and VERSION_ID from os-release.
	var id, versionID string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ID=") {
			id = strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
		}
		if strings.HasPrefix(line, "VERSION_ID=") {
			versionID = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
		}
	}

	if id != "ubuntu" {
		return PreflightCheck{
			Name:    name,
			Status:  CheckFail,
			Message: "Only Ubuntu 22.04 or 24.04 is supported. Detected: " + id + " " + versionID,
			Output:  out,
		}
	}

	switch versionID {
	case "22.04", "24.04":
		return PreflightCheck{Name: name, Status: CheckPass, Message: "Supported OS", Output: id + " " + versionID}
	default:
		return PreflightCheck{
			Name:    name,
			Status:  CheckFail,
			Message: "Only Ubuntu 22.04 or 24.04 is supported. Detected: Ubuntu " + versionID,
			Output:  out,
		}
	}
}

func checkDisk(ctx context.Context, sess *ssh.Session) PreflightCheck {
	const name = "disk"
	const minFreeGB = 2

	// df -BG gives output in GB blocks. Field 4 is "Available".
	out, err := sess.Run(ctx, "df -BG / | tail -1 | awk '{print $4}'")
	out = strings.TrimSpace(out)

	if err != nil {
		return PreflightCheck{
			Name:    name,
			Status:  CheckFail,
			Message: fmt.Sprintf("Failed to check disk space: %v", err),
			Output:  out,
		}
	}

	// Strip trailing 'G' if present.
	out = strings.TrimSuffix(out, "G")
	freeGB, err := strconv.Atoi(out)
	if err != nil {
		return PreflightCheck{
			Name:    name,
			Status:  CheckFail,
			Message: fmt.Sprintf("Cannot parse disk space %q: %v", out, err),
			Output:  out,
		}
	}

	if freeGB >= minFreeGB {
		return PreflightCheck{
			Name:    name,
			Status:  CheckPass,
			Message: fmt.Sprintf("Sufficient disk space: %dGB free", freeGB),
			Output:  fmt.Sprintf("%dGB", freeGB),
		}
	}

	return PreflightCheck{
		Name:    name,
		Status:  CheckFail,
		Message: fmt.Sprintf("Insufficient disk space: %dGB free, need at least %dGB", freeGB, minFreeGB),
		Output:  fmt.Sprintf("%dGB", freeGB),
	}
}

func checkSudo(ctx context.Context, sess *ssh.Session) PreflightCheck {
	const name = "sudo"

	// sudo -n true fails if password is required.
	_, err := sess.Run(ctx, "sudo -n true 2>&1")
	if err != nil {
		return PreflightCheck{
			Name:    name,
			Status:  CheckFail,
			Message: "Passwordless sudo is required. Configure NOPASSWD via: sudo visudo",
		}
	}

	return PreflightCheck{Name: name, Status: CheckPass, Message: "Passwordless sudo available"}
}
