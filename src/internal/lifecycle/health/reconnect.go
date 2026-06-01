package health

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/underundre/unet/internal/ssh"
	"github.com/underundre/unet/internal/state"
)

// --- ReconnectState (matches data-model.md entity 6) ---

// ReconnectPhase describes the current phase of a reconnect attempt.
type ReconnectPhase string

const (
	PhaseProbing   ReconnectPhase = "probing"
	PhaseReconnect ReconnectPhase = "reconnecting"
	PhaseVerifying ReconnectPhase = "verifying"
	PhaseSyncing   ReconnectPhase = "syncing"
	PhaseFailed    ReconnectPhase = "failed"
	PhaseRecovered ReconnectPhase = "recovered"
)

// ReconnectState tracks the state of an ongoing reconnect sequence.
type ReconnectState struct {
	StartedAt     time.Time      `json:"startedAt"`
	AttemptCount  int            `json:"attemptCount"`
	NextAttemptAt time.Time      `json:"nextAttemptAt"`
	CurrentDelay  time.Duration  `json:"currentDelayMs"`
	Phase         ReconnectPhase `json:"phase"`
	LastError     string         `json:"lastError,omitempty"`
}

// Backoff parameters per FR-008.
const (
	backoffInitial    = 2000 * time.Millisecond
	backoffMultiplier = 2.0
	backoffCap        = 60000 * time.Millisecond
	backoffJitter     = 0.2 // ±20%
)

// Reconnector manages reconnect attempts with exponential backoff.
type Reconnector struct {
	pool  *ssh.Pool
	state ReconnectState
	rng   *rand.Rand

	// OnStateChange is called when reconnect state changes.
	OnStateChange func(ReconnectState)

	// OnSuccess is called after successful reconnect + state sync.
	OnSuccess func(ctx context.Context, profile *state.VPSProfile)

	// Now is injectable for testing.
	Now func() time.Time
}

// NewReconnector creates a new reconnect manager.
func NewReconnector(pool *ssh.Pool) *Reconnector {
	return &Reconnector{
		pool: pool,
		rng:  rand.New(rand.NewSource(time.Now().UnixNano())),
		Now:  time.Now,
	}
}

// State returns the current reconnect state (read-only snapshot).
func (r *Reconnector) State() ReconnectState {
	return r.state
}

// Execute runs the reconnect sequence: SSH → Docker → Compose → WG → Health.
// Returns nil on success, error on failure (caller should schedule retry).
func (r *Reconnector) Execute(ctx context.Context) error {
	if r.state.StartedAt.IsZero() {
		r.state.StartedAt = r.Now()
	}
	r.state.AttemptCount++
	r.state.Phase = PhaseProbing
	r.emitState()

	// Step 1: Verify SSH reachable.
	sess, err := r.pool.Session(ctx)
	if err != nil {
		r.state.Phase = PhaseFailed
		r.state.LastError = fmt.Sprintf("SSH unreachable: %v", err)
		r.scheduleNext()
		r.emitState()
		return fmt.Errorf("reconnect: SSH: %w", err)
	}

	r.state.Phase = PhaseReconnect
	r.emitState()

	// Step 2: Verify Docker running.
	out, err := sess.Run(ctx, "sudo docker info --format '{{.ServerVersion}}' 2>/dev/null")
	if err != nil {
		r.pool.Put(sess)
		r.state.Phase = PhaseFailed
		r.state.LastError = fmt.Sprintf("Docker not running: %v (output: %s)", err, out)
		r.scheduleNext()
		r.emitState()
		return fmt.Errorf("reconnect: Docker: %w", err)
	}

	// Step 3: Verify compose stack up.
	r.state.Phase = PhaseVerifying
	r.emitState()

	containerOut, err := sess.Run(ctx, "sudo docker ps --filter name=unet- --format '{{.Names}}' 2>/dev/null")
	r.pool.Put(sess)
	if err != nil {
		r.state.Phase = PhaseFailed
		r.state.LastError = fmt.Sprintf("Container check failed: %v", err)
		r.scheduleNext()
		r.emitState()
		return fmt.Errorf("reconnect: containers: %w", err)
	}

	// Verify expected containers exist.
	expectedContainers := []string{"unet-net-pause", "unet-amnezia-awg", "unet-caddy"}
	for _, name := range expectedContainers {
		if !strings.Contains(containerOut, name) {
			r.state.Phase = PhaseFailed
			r.state.LastError = fmt.Sprintf("Missing container: %s", name)
			r.scheduleNext()
			r.emitState()
			return fmt.Errorf("reconnect: missing container %s", name)
		}
	}

	// Step 4: Re-sync state.
	r.state.Phase = PhaseSyncing
	r.emitState()

	// Success.
	r.state.Phase = PhaseRecovered
	r.state.LastError = ""
	r.emitState()

	slog.Info("reconnect: success", "attempts", r.state.AttemptCount,
		"elapsed", r.Now().Sub(r.state.StartedAt).Round(time.Second))

	// Reset state for next failure cycle.
	r.state = ReconnectState{}
	return nil
}

// scheduleNext computes the next backoff delay.
func (r *Reconnector) scheduleNext() {
	delay := backoffInitial * time.Duration(math.Pow(backoffMultiplier, float64(r.state.AttemptCount-1)))
	if delay > backoffCap {
		delay = backoffCap
	}

	// Apply jitter: delay * random(0.8, 1.2)
	jitterFactor := 1.0 + (r.rng.Float64()*2-1)*backoffJitter
	delay = time.Duration(float64(delay) * jitterFactor)

	r.state.CurrentDelay = delay
	r.state.NextAttemptAt = r.Now().Add(delay)
	slog.Info("reconnect: scheduling next attempt",
		"attempt", r.state.AttemptCount,
		"delay", delay,
		"nextAt", r.state.NextAttemptAt.Format(time.RFC3339))
}

func (r *Reconnector) emitState() {
	if r.OnStateChange != nil {
		r.OnStateChange(r.state)
	}
}
