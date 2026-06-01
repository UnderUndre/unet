package preflight

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type SSHSession interface {
	Run(ctx context.Context, cmd string) (stdout string, stderr string, err error)
	Close() error
}

type Result struct {
	TargetHost       string   `json:"target_host"`
	CheckedAt        string   `json:"checked_at"`
	Distro           string   `json:"distro"`
	DistroVersion    string   `json:"distro_version"`
	Arch             string   `json:"arch"`
	DiskFreeGB       float64  `json:"disk_free_gb"`
	RAMMB            int      `json:"ram_mb"`
	HasSudo          bool     `json:"has_sudo"`
	HasDocker        bool     `json:"has_docker"`
	DockerRunning    bool     `json:"docker_running"`
	Port443Free      bool     `json:"port_443_free"`
	Port80Free       bool     `json:"port_80_free"`
	PortWGFree       bool     `json:"port_wg_free"`
	Compatible       bool     `json:"compatible"`
	Warnings         []string `json:"warnings"`
	BlockingFailures []string `json:"blocking_failures"`
}

func Run(ctx context.Context, session SSHSession, targetHost string) (*Result, error) {
	result := &Result{
		TargetHost:       targetHost,
		CheckedAt:        time.Now().UTC().Format(time.RFC3339),
		Warnings:         []string{},
		BlockingFailures: []string{},
	}

	checkDistro(ctx, session, result)
	checkDisk(ctx, session, result)
	checkSudo(ctx, session, result)
	checkDocker(ctx, session, result)
	checkPorts(ctx, session, result)

	result.Compatible = len(result.BlockingFailures) == 0

	return result, nil
}

func runCmd(ctx context.Context, session SSHSession, cmd string) (string, error) {
	stdout, _, err := session.Run(ctx, cmd)
	return stdout, err
}

func checkDistro(ctx context.Context, session SSHSession, result *Result) {
	content, err := runCmd(ctx, session, "cat /etc/os-release")
	if err != nil {
		result.BlockingFailures = append(result.BlockingFailures,
			fmt.Sprintf("Failed to read /etc/os-release: %v", err))
		return
	}

	id, versionID := ParseOSRelease(content)
	result.Distro = id
	result.DistroVersion = versionID

	compatible, msg := CheckDistro(id, versionID)
	if !compatible {
		result.BlockingFailures = append(result.BlockingFailures, msg)
	}
}

func checkDisk(ctx context.Context, session SSHSession, result *Result) {
	output, err := runCmd(ctx, session, "df -h /")
	if err != nil {
		result.BlockingFailures = append(result.BlockingFailures,
			fmt.Sprintf("Failed to check disk space: %v", err))
		return
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		result.BlockingFailures = append(result.BlockingFailures,
			"Unexpected df output format")
		return
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		result.BlockingFailures = append(result.BlockingFailures,
			"Unexpected df output format")
		return
	}

	availStr := fields[3]
	gb, err := parseDiskSize(availStr)
	if err != nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Could not parse available disk: %v", err))
		return
	}

	result.DiskFreeGB = gb
	if gb < 2.0 {
		result.BlockingFailures = append(result.BlockingFailures,
			fmt.Sprintf("Insufficient disk space: %.1fGB available, need ≥2GB", gb))
	}
}

func parseDiskSize(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "G") {
		return strconv.ParseFloat(strings.TrimSuffix(s, "G"), 64)
	}
	if strings.HasSuffix(s, "T") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "T"), 64)
		return v * 1024, err
	}
	if strings.HasSuffix(s, "M") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "M"), 64)
		return v / 1024, err
	}
	return strconv.ParseFloat(s, 64)
}

func checkSudo(ctx context.Context, session SSHSession, result *Result) {
	_, err := runCmd(ctx, session, "sudo -n true")
	result.HasSudo = err == nil
	if !result.HasSudo {
		result.BlockingFailures = append(result.BlockingFailures,
			"Passwordless sudo is required")
	}
}

func checkDocker(ctx context.Context, session SSHSession, result *Result) {
	output, err := runCmd(ctx, session, "docker info 2>&1")
	if err != nil {
		result.HasDocker = false
		result.DockerRunning = false
		result.Warnings = append(result.Warnings,
			"Docker not found or not running. It will be installed during setup.")
		return
	}

	result.HasDocker = true
	result.DockerRunning = strings.Contains(output, "Server:")
	if !result.DockerRunning {
		result.Warnings = append(result.Warnings,
			"Docker is installed but daemon is not running")
	}
}

func checkPorts(ctx context.Context, session SSHSession, result *Result) {
	output, err := runCmd(ctx, session, "ss -tlnp 2>/dev/null || netstat -tlnp 2>/dev/null")
	if err != nil {
		result.Warnings = append(result.Warnings,
			"Could not check port availability")
		result.Port443Free = true
		result.Port80Free = true
		result.PortWGFree = true
		return
	}

	result.Port443Free = !strings.Contains(output, ":443 ")
	result.Port80Free = !strings.Contains(output, ":80 ")
	result.PortWGFree = true

	if !result.Port443Free {
		result.BlockingFailures = append(result.BlockingFailures,
			"Port 443 is already in use")
	}
	if !result.Port80Free {
		result.Warnings = append(result.Warnings,
			"Port 80 is in use (HTTP redirect may not work)")
	}
}
