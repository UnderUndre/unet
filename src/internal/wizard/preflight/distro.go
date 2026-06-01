package preflight

import (
	"fmt"
	"strconv"
	"strings"
)

func ParseOSRelease(content string) (id, versionID string) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)

		switch key {
		case "ID":
			id = value
		case "VERSION_ID":
			versionID = value
		}
	}
	return
}

func CheckDistro(id, versionID string) (compatible bool, message string) {
	major, minor, _ := parseVersion(versionID)

	switch id {
	case "ubuntu":
		if major > 22 || (major == 22 && minor >= 4) {
			return true, ""
		}
		return false, fmt.Sprintf("Unsupported OS: %s %s. Supported: Ubuntu 22.04/24.04, Debian 12.", id, versionID)

	case "debian":
		if major >= 12 {
			return true, ""
		}
		return false, fmt.Sprintf("Unsupported OS: %s %s. Supported: Ubuntu 22.04/24.04, Debian 12.", id, versionID)

	default:
		return false, fmt.Sprintf("Unsupported OS: %s %s. Supported: Ubuntu 22.04/24.04, Debian 12.", id, versionID)
	}
}

func parseVersion(v string) (major, minor, patch int) {
	parts := strings.SplitN(v, ".", 3)
	for i, s := range parts {
		n, _ := strconv.Atoi(s)
		switch i {
		case 0:
			major = n
		case 1:
			minor = n
		case 2:
			patch = n
		}
	}
	return
}
