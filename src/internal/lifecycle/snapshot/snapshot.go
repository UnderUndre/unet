// Package snapshot manages config-only snapshots on the VPS for rollback.
package snapshot

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/underundre/unet/internal/ssh"
)

const (
	snapshotDir     = "/opt/unet/snapshots"
	maxSnapshots    = 5
	timestampFormat = "20060102-150405"
)

// SnapshotInfo describes a snapshot on the VPS.
type SnapshotInfo struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Path      string `json:"path"`
}

// Create takes a config-only snapshot on the VPS. Archives: awg0.conf,
// docker-compose.yml, compose hash, version file. Stored as tar.gz at
// /opt/unet/snapshots/<timestamp>/snapshot.tar.gz.
func Create(ctx context.Context, sess *ssh.Session) (*SnapshotInfo, error) {
	ts := time.Now().UTC().Format(timestampFormat)
	snapPath := snapshotDir + "/" + ts

	script := fmt.Sprintf(`set -euo pipefail
sudo mkdir -p %[1]s
cd %[1]s
# Collect config files (ignore missing)
sudo cp /opt/unet/docker-compose.yml . 2>/dev/null || true
sudo cp /opt/unet/.compose-hash . 2>/dev/null || true
sudo cp /opt/unet/version . 2>/dev/null || true
# AWG config from container (if running)
sudo docker cp unet-amnezia-awg:/opt/amnezia/awg/awg0.conf ./awg0.conf 2>/dev/null || true
# Create archive
sudo tar czf snapshot.tar.gz -- *.conf *.yml *.hash version 2>/dev/null || true
sudo rm -f *.conf *.yml *.hash version 2>/dev/null || true
echo "ok"
`, snapPath)

	out, _, err := sess.RunScript(ctx, script)
	if err != nil {
		return nil, fmt.Errorf("snapshot: create failed: %w\noutput: %s", err, out)
	}

	info := &SnapshotInfo{
		ID:        ts,
		Timestamp: ts,
		Path:      snapPath + "/snapshot.tar.gz",
	}

	// Prune old snapshots.
	if err := Prune(ctx, sess); err != nil {
		slog.Warn("snapshot: prune failed", "err", err)
	}

	slog.Info("snapshot: created", "id", ts)
	return info, nil
}

// Restore stops compose, extracts a snapshot, and restarts compose.
func Restore(ctx context.Context, sess *ssh.Session, snapshotID string) error {
	snapPath := snapshotDir + "/" + snapshotID + "/snapshot.tar.gz"

	script := fmt.Sprintf(`set -euo pipefail
# Verify snapshot exists
if ! sudo test -f %[1]s; then
  echo "ERROR: snapshot not found" >&2
  exit 1
fi
# Stop compose
cd /opt/unet && sudo docker compose down --remove-orphans 2>/dev/null || true
# Restore files
cd /opt/unet && sudo tar xzf %[1]s
# Restart compose
cd /opt/unet && sudo docker compose up -d
echo "ok"
`, snapPath)

	out, _, err := sess.RunScript(ctx, script)
	if err != nil {
		return fmt.Errorf("snapshot: restore failed: %w\noutput: %s", err, out)
	}

	slog.Info("snapshot: restored", "id", snapshotID)
	return nil
}

// List returns all snapshots on the VPS, sorted newest first.
func List(ctx context.Context, sess *ssh.Session) ([]SnapshotInfo, error) {
	out, err := sess.Run(ctx, "sudo ls -1d "+snapshotDir+"/*/ 2>/dev/null")
	if err != nil {
		// No snapshots directory = no snapshots.
		return nil, nil
	}

	var snaps []SnapshotInfo
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Extract timestamp from directory name.
		base := strings.TrimPrefix(line, snapshotDir+"/")
		base = strings.TrimSuffix(base, "/")
		if len(base) != len(timestampFormat) {
			continue
		}
		snaps = append(snaps, SnapshotInfo{
			ID:        base,
			Timestamp: base,
			Path:      snapshotDir + "/" + base + "/snapshot.tar.gz",
		})
	}

	// Sort newest first.
	sort.Slice(snaps, func(i, j int) bool {
		return snaps[i].ID > snaps[j].ID
	})

	return snaps, nil
}

// Prune removes oldest snapshots when count > maxSnapshots.
func Prune(ctx context.Context, sess *ssh.Session) error {
	snaps, err := List(ctx, sess)
	if err != nil {
		return err
	}

	if len(snaps) <= maxSnapshots {
		return nil
	}

	toRemove := snaps[maxSnapshots:]
	for _, s := range toRemove {
		slog.Info("snapshot: pruning", "id", s.ID)
		out, err := sess.Run(ctx, "sudo rm -rf "+snapshotDir+"/"+ssh.ShellEscape(s.ID))
		_ = out
		if err != nil {
			slog.Warn("snapshot: prune failed for", "id", s.ID, "err", err)
		}
	}

	removed := strconv.Itoa(len(toRemove))
	slog.Info("snapshot: pruned", "removed", removed)
	return nil
}
