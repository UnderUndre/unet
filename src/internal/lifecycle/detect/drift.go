package detect

import (
	"context"
	"fmt"
	"strings"

	"github.com/underundre/unet/internal/lifecycle/compose"
	"github.com/underundre/unet/internal/ssh"
)

// DriftResolution describes how a compose drift was resolved.
type DriftResolution string

const (
	DriftNone      DriftResolution = "none"      // No drift
	DriftDetected  DriftResolution = "detected"  // Drift found, awaiting user decision
	DriftMerged    DriftResolution = "merged"    // User chose merge: canonical overwrites VPS
	DriftRefused   DriftResolution = "refused"   // User refused: attach in read-only mode
)

// DriftDetail holds the result of a drift check with diff details.
type DriftDetail struct {
	Resolution   DriftResolution `json:"resolution"`
	ExpectedHash string          `json:"expectedHash"`
	VPSHash      string          `json:"vpsHash"`
	Diff         string          `json:"diff,omitempty"`
}

// CheckDrift reads the compose file on the VPS and compares its hash to the
// canonical hash from embedded templates. Returns nil if no drift.
func CheckDrift(ctx context.Context, sess *ssh.Session, cfg compose.RenderConfig) (*DriftDetail, error) {
	// Render canonical compose.
	canonical, err := compose.Render(cfg)
	if err != nil {
		return nil, fmt.Errorf("detect: render canonical: %w", err)
	}
	expectedHash := compose.Hash(canonical)

	// Read VPS compose file.
	vpsCompose, err := sess.Run(ctx, "sudo cat /opt/unet/docker-compose.yml 2>/dev/null")
	if err != nil {
		return nil, fmt.Errorf("detect: read VPS compose: %w", err)
	}
	if strings.TrimSpace(vpsCompose) == "" {
		// No compose file on VPS — not drift, just missing.
		return nil, nil
	}

	vpsHash := compose.Hash([]byte(vpsCompose))
	if vpsHash == expectedHash {
		return nil, nil // No drift.
	}

	// Drift detected — compute unified diff.
	diff := computeDiff(string(canonical), vpsCompose)

	return &DriftDetail{
		Resolution:   DriftDetected,
		ExpectedHash: expectedHash,
		VPSHash:      vpsHash,
		Diff:         diff,
	}, nil
}

// ResolveDriftMerge overwrites the VPS compose file with the canonical version.
// User explicitly chose merge — this is NOT automatic.
func ResolveDriftMerge(ctx context.Context, sess *ssh.Session, cfg compose.RenderConfig) error {
	canonical, err := compose.Render(cfg)
	if err != nil {
		return fmt.Errorf("detect: render canonical: %w", err)
	}

	uploadScript := fmt.Sprintf(`cat > /tmp/docker-compose.yml.unet << 'UNET_COMPOSE_EOF'
%s
UNET_COMPOSE_EOF
sudo mv /tmp/docker-compose.yml.unet /opt/unet/docker-compose.yml
echo '%s' | sudo tee /opt/unet/.compose-hash > /dev/null
`, string(canonical), compose.Hash(canonical))

	if out, _, err := sess.RunScript(ctx, uploadScript); err != nil {
		return fmt.Errorf("detect: merge overwrite failed: %w\noutput: %s", err, out)
	}

	return nil
}

// computeDiff produces a simple unified-style diff between canonical and VPS compose.
func computeDiff(canonical, vps string) string {
	canonicalLines := strings.Split(canonical, "\n")
	vpsLines := strings.Split(vps, "\n")

	var diff strings.Builder
	maxLen := len(canonicalLines)
	if len(vpsLines) > maxLen {
		maxLen = len(vpsLines)
	}

	for i := 0; i < maxLen; i++ {
		var cLine, vLine string
		if i < len(canonicalLines) {
			cLine = canonicalLines[i]
		}
		if i < len(vpsLines) {
			vLine = vpsLines[i]
		}

		if cLine != vLine {
			if cLine != "" {
				diff.WriteString(fmt.Sprintf("- %s\n", cLine))
			}
			if vLine != "" {
				diff.WriteString(fmt.Sprintf("+ %s\n", vLine))
			}
		}
	}

	return diff.String()
}
