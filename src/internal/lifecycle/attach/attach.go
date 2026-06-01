package attach

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/underundre/unet/internal/lifecycle/bootstrap"
	"github.com/underundre/unet/internal/lifecycle/compose"
	"github.com/underundre/unet/internal/lifecycle/detect"
	"github.com/underundre/unet/internal/ssh"
)

// AttachResult holds the outcome of an attach operation.
type AttachResult struct {
	Classification *detect.ClassifyResult `json:"classification"`
	Sync           *SyncResult            `json:"sync,omitempty"`
	ZeroDiff       bool                   `json:"zeroDiff"`    // State was already current
	Bootstrapped   bool                   `json:"bootstrapped"` // Fell through to bootstrap
	Duration       string                 `json:"duration"`
}

// AttachOpts configures the attach operation.
type AttachOpts struct {
	DaemonVersion string
	ComposeConfig compose.RenderConfig
	BootstrapOpts bootstrap.BootstrapOpts
}

// Attach orchestrates the attach sequence: classify → route → sync/bootstrap.
func Attach(ctx context.Context, pool *ssh.Pool, opts AttachOpts) (*AttachResult, error) {
	start := time.Now()
	result := &AttachResult{}
	defer func() {
		result.Duration = time.Since(start).Round(time.Second).String()
	}()

	sess, err := pool.Session(ctx)
	if err != nil {
		return nil, fmt.Errorf("attach: get SSH session: %w", err)
	}

	// Step 1: Classify VPS state.
	classification, err := detect.Classify(ctx, sess, opts.DaemonVersion, opts.ComposeConfig)
	if err != nil {
		pool.Put(sess)
		return nil, fmt.Errorf("attach: classify: %w", err)
	}
	result.Classification = classification
	pool.Put(sess)

	slog.Info("attach: VPS classified", "state", string(classification.State), "version", classification.VPSVersion)

	switch classification.State {
	case detect.StateBlank:
		// No unet installation → redirect to bootstrap.
		slog.Info("attach: blank VPS, redirecting to bootstrap")
		bootstrapResult, err := bootstrap.Bootstrap(ctx, pool, opts.BootstrapOpts)
		if err != nil {
			return nil, fmt.Errorf("attach: bootstrap redirect: %w", err)
		}
		result.Bootstrapped = true
		result.Duration = bootstrapResult.Duration
		return result, nil

	case detect.StateIncompatible:
		return result, fmt.Errorf("attach: incompatible VPS version %s (daemon: %s). Major version mismatch — manual remediation required", classification.VPSVersion, opts.DaemonVersion)

	case detect.StateOld:
		// Old but compatible — log warning, continue with sync.
		slog.Warn("attach: VPS version behind",
			"vps", classification.VPSVersion,
			"daemon", opts.DaemonVersion,
			"note", "attach in read-only mode, upgrade offer deferred to user")

	case detect.StateCurrent:
		slog.Info("attach: VPS current, syncing state")
		result.ZeroDiff = true
	}

	// Step 2: Check for compose drift (only if not blank).
	if classification.State != detect.StateBlank {
		sess2, err := pool.Session(ctx)
		if err != nil {
			return nil, fmt.Errorf("attach: get SSH session for drift check: %w", err)
		}
		drift, err := detect.CheckDrift(ctx, sess2, opts.ComposeConfig)
		pool.Put(sess2)
		if err != nil {
			slog.Warn("attach: drift check failed", "err", err)
		}
		if drift != nil {
			slog.Warn("attach: compose drift detected",
				"expected", drift.ExpectedHash,
				"actual", drift.VPSHash,
				"note", "NOT silently overwriting. User must resolve.")
		}
	}

	// Step 3: Sync state.
	sess3, err := pool.Session(ctx)
	if err != nil {
		return nil, fmt.Errorf("attach: get SSH session for sync: %w", err)
	}

	// Get connection info from pool config.
	poolCfg := pool.Config()
	syncResult, profile, err := SyncState(ctx, sess3, poolCfg.Host, poolCfg.Port, poolCfg.User, string(poolCfg.AuthMode))
	pool.Put(sess3)
	if err != nil {
		return nil, fmt.Errorf("attach: sync state: %w", err)
	}
	result.Sync = syncResult

	// Step 4: Persist profile locally.
	if err := SaveSyncedProfile(profile); err != nil {
		return nil, fmt.Errorf("attach: save profile: %w", err)
	}

	slog.Info("attach: complete",
		"peers", syncResult.PeersSynced,
		"routes", syncResult.RoutesSynced,
		"tunnel", syncResult.TunnelSynced)

	return result, nil
}
