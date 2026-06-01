package detect

import (
	"fmt"
	"strconv"
	"strings"
)

// Semver represents a parsed semantic version.
type Semver struct {
	Major int
	Minor int
	Patch int
}

// ParseSemver parses a semver string (e.g., "0.3.0", "1.2.3-rc1").
// Pre-release suffixes are ignored for comparison purposes.
func ParseSemver(s string) (Semver, error) {
	s = strings.TrimSpace(s)
	// Strip pre-release suffix.
	if idx := strings.Index(s, "-"); idx >= 0 {
		s = s[:idx]
	}

	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return Semver{}, fmt.Errorf("detect: invalid semver %q", s)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return Semver{}, fmt.Errorf("detect: invalid major version in %q: %w", s, err)
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return Semver{}, fmt.Errorf("detect: invalid minor version in %q: %w", s, err)
	}

	patch := 0
	if len(parts) == 3 {
		patch, err = strconv.Atoi(parts[2])
		if err != nil {
			return Semver{}, fmt.Errorf("detect: invalid patch version in %q: %w", s, err)
		}
	}

	return Semver{Major: major, Minor: minor, Patch: patch}, nil
}

// String returns the semver as "major.minor.patch".
func (v Semver) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}
