package tunnel

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
)

// LocalTCPProxy listens on a random port bound to the tunnel's local IP
// and forwards TCP connections to the user's actual local service on
// 127.0.0.1:<userPort>.
//
// This ensures the user's service is never bound to 0.0.0.0 or exposed
// on any physical network interface — only on the WG tunnel interface.
type LocalTCPProxy struct {
	tunnelIP   string
	listenPort int
	targetAddr string // e.g. "127.0.0.1:3000"
	listener   net.Listener
	mu         sync.Mutex
	running    bool
	cancel     context.CancelFunc
	done       chan struct{}
}

// NewLocalTCPProxy creates a new TCP proxy. tunnelIP is the WG local IP.
// targetAddr is the destination (e.g. "127.0.0.1:3000").
func NewLocalTCPProxy(tunnelIP, targetAddr string) *LocalTCPProxy {
	return &LocalTCPProxy{
		tunnelIP:   tunnelIP,
		targetAddr: targetAddr,
	}
}

// Start starts listening on a random port bound to tunnelIP.
// Returns the allocated port number.
func (p *LocalTCPProxy) Start(ctx context.Context) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return p.listenPort, nil
	}

	// Listen on a random port, bound to the tunnel IP only.
	lc := net.ListenConfig{}
	addr := fmt.Sprintf("%s:0", p.tunnelIP)
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return 0, fmt.Errorf("tunnel: local proxy listen on %s: %w", addr, err)
	}

	// Extract the allocated port.
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		ln.Close()
		return 0, fmt.Errorf("tunnel: parse listen addr: %w", err)
	}
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	p.listener = ln
	p.listenPort = port
	p.running = true
	p.done = make(chan struct{})

	ctx, p.cancel = context.WithCancel(context.Background())

	go p.acceptLoop(ctx)

	slog.Info("tunnel: local TCP proxy started",
		"tunnelIP", p.tunnelIP,
		"listenPort", port,
		"target", p.targetAddr)
	return port, nil
}

func (p *LocalTCPProxy) acceptLoop(ctx context.Context) {
	defer close(p.done)
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				slog.Warn("tunnel: proxy accept error", "err", err)
				continue
			}
		}
		go p.handleConn(ctx, conn)
	}
}

func (p *LocalTCPProxy) handleConn(ctx context.Context, clientConn net.Conn) {
	defer clientConn.Close()

	dialer := net.Dialer{}
	targetConn, err := dialer.DialContext(ctx, "tcp", p.targetAddr)
	if err != nil {
		slog.Warn("tunnel: proxy dial target failed", "target", p.targetAddr, "err", err)
		return
	}
	defer targetConn.Close()

	// Bidirectional copy.
	go func() {
		io.Copy(targetConn, clientConn)
		targetConn.CloseWrite()
	}()
	io.Copy(clientConn, targetConn)
	clientConn.CloseWrite()
}

// Stop gracefully shuts down the proxy.
func (p *LocalTCPProxy) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return
	}
	p.running = false
	if p.cancel != nil {
		p.cancel()
	}
	if p.listener != nil {
		p.listener.Close()
	}
	<-p.done
	slog.Info("tunnel: local TCP proxy stopped", "port", p.listenPort)
}

// ListenPort returns the port the proxy is listening on, or 0 if not running.
func (p *LocalTCPProxy) ListenPort() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.listenPort
}
