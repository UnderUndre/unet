package daemon

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type sshHostEntry struct {
	Host       string `json:"host"`
	HostName   string `json:"hostName,omitempty"`
	User       string `json:"user,omitempty"`
	Port       int    `json:"port,omitempty"`
	IdentityFile string `json:"identityFile,omitempty"`
}

func HandleSSHHosts(w http.ResponseWriter, r *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "cannot determine home directory")
		return
	}

	configPath := filepath.Join(home, ".ssh", "config")
	entries, err := parseSSHConfig(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]sshHostEntry{})
			return
		}
		slog.Warn("api: failed to parse ssh config", "path", configPath, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to read SSH config")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

func parseSSHConfig(path string) ([]sshHostEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []sshHostEntry
	var current *sshHostEntry

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		key := strings.ToLower(parts[0])
		value := parts[1]

		switch key {
		case "host":
			if current != nil {
				entries = append(entries, *current)
			}
			if strings.Contains(value, "*") || strings.Contains(value, "?") {
				current = nil
				continue
			}
			current = &sshHostEntry{Host: value}
		case "hostname":
			if current != nil {
				current.HostName = value
			}
		case "user":
			if current != nil {
				current.User = value
			}
		case "port":
			if current != nil {
				port := 0
				for _, c := range value {
					if c >= '0' && c <= '9' {
						port = port*10 + int(c-'0')
					} else {
						break
					}
				}
				current.Port = port
			}
		case "identityfile":
			if current != nil {
				identityFile := value
				if strings.HasPrefix(identityFile, "~/") {
					home, _ := os.UserHomeDir()
					identityFile = filepath.Join(home, identityFile[2:])
				}
				current.IdentityFile = identityFile
			}
		}
	}

	if current != nil {
		entries = append(entries, *current)
	}

	return entries, scanner.Err()
}
