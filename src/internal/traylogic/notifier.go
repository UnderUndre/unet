package traylogic

import (
	"log/slog"
	"sync"
	"time"

	"github.com/underundre/unet/internal/platform"
)

// notificationThrottle prevents notification spam.
// Max 1 notification per event type per 60s (FR-005).
type notificationThrottle struct {
	mu      sync.Mutex
	lastSent map[string]time.Time
	cooldown time.Duration
}

// newNotificationThrottle creates a throttle with 60s cooldown.
func newNotificationThrottle() *notificationThrottle {
	return &notificationThrottle{
		lastSent: make(map[string]time.Time),
		cooldown: 60 * time.Second,
	}
}

// Send dispatches a notification if cooldown has elapsed for this event type.
func (t *notificationThrottle) Send(notifier platform.Notifier, eventType, title, body string, severity platform.Severity) {
	t.mu.Lock()
	defer t.mu.Unlock()

	last, ok := t.lastSent[eventType]
	if ok && time.Since(last) < t.cooldown {
		slog.Debug("tray: notification throttled", "event", eventType)
		return
	}

	t.lastSent[eventType] = time.Now()

	if err := notifier.Send(title, body, severity); err != nil {
		slog.Warn("tray: notification send failed", "error", err)
	}
}

// throttledNotifier wraps platform.Notifier with per-event-type throttling.
type throttledNotifier struct {
	notifier platform.Notifier
	throttle *notificationThrottle
}

func newThrottledNotifier(n platform.Notifier) *throttledNotifier {
	return &throttledNotifier{
		notifier: n,
		throttle: newNotificationThrottle(),
	}
}

func (t *throttledNotifier) Send(title, body string, severity platform.Severity) error {
	// Use title as event type key for throttling.
	t.throttle.Send(t.notifier, title, title, body, severity)
	return nil
}
