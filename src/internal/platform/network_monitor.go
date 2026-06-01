//go:build windows

package platform

import (
	"context"
	"math/rand"
	"net"
	"time"
)

// networkMonitorImpl polls default-route reachability every ~2s.
type networkMonitorImpl struct {
	target    string
	interval  time.Duration
	failCount int
}

// NewNetworkMonitor creates a NetworkMonitor that polls reachability.
func NewNetworkMonitor() NetworkMonitor {
	return &networkMonitorImpl{
		target:   "8.8.8.8:53", // Google DNS as reachability probe target
		interval: 2 * time.Second,
	}
}

func (n *networkMonitorImpl) Watch(ctx context.Context) <-chan NetworkEvent {
	ch := make(chan NetworkEvent, 16)

	go func() {
		defer close(ch)

		wasReachable := true
		failCount := 0

		for {
			// Jitter ±500ms to avoid thundering herd (F9 from review).
			jitter := time.Duration(rand.Intn(1000)-500) * time.Millisecond
			select {
			case <-ctx.Done():
				return
			case <-time.After(n.interval + jitter):
			}

			reachable := probeReachability(n.target)

			if !reachable {
				failCount++
			} else {
				failCount = 0
			}

			// Require 2 consecutive failures before declaring reachability lost.
			if wasReachable && failCount >= 2 {
				wasReachable = false
				ch <- NetworkEvent{
					Type:      NetworkReachabilityLost,
					Timestamp: time.Now().Format(time.RFC3339),
					Details:   "default route unreachable (2 consecutive failures)",
				}
			}

			if !wasReachable && reachable {
				wasReachable = true
				failCount = 0
				ch <- NetworkEvent{
					Type:      NetworkReachabilityRestored,
					Timestamp: time.Now().Format(time.RFC3339),
					Details:   "default route reachable",
				}
			}
		}
	}()

	return ch
}

// probeReachability attempts a TCP connection with a short timeout.
func probeReachability(target string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
