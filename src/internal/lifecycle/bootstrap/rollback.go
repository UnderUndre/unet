package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/underundre/unet/internal/ssh"
)

// Rollback cleans up after a failed bootstrap. Stops containers, removes
// /opt/unet artifacts. Docker itself is left installed (useful dependency).
// No rollback is attempted for Phase 1 (preflight) failures since no
// mutations were made.
func Rollback(ctx context.Context, sess *ssh.Session) error {
	slog.Warn("bootstrap: executing rollback")

	var errs []error

	// Stop compose stack.
	if out, err := sess.Run(ctx, "cd /opt/unet && sudo docker compose down --remove-orphans 2>&1"); err != nil {
		errs = append(errs, fmt.Errorf("docker compose down: %w (output: %s)", err, out))
	}

	// Remove /opt/unet contents (but not the directory itself).
	if out, err := sess.Run(ctx, "sudo rm -rf /opt/unet/* 2>&1"); err != nil {
		errs = append(errs, fmt.Errorf("rm /opt/unet/*: %w (output: %s)", err, out))
	}

	if len(errs) > 0 {
		return fmt.Errorf("rollback partially failed: %v", errs)
	}

	slog.Info("bootstrap: rollback complete")
	return nil
}
