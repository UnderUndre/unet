package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Server is the HTTP server for the unet daemon control API and
// static frontend assets. It binds exclusively to 127.0.0.1.
type Server struct {
	httpServer *http.Server
	port       int
	apiMux     *http.ServeMux
}

// NewServer creates a new Server that will listen on the given port.
// If port is 0 the default of 8080 is used.
func NewServer(port int) *Server {
	if port <= 0 {
		port = 8080
	}

	apiMux := http.NewServeMux()

	return &Server{
		port:   port,
		apiMux: apiMux,
	}
}

// Port returns the port the server is configured to listen on.
func (s *Server) Port() int { return s.port }

// HandleFunc registers an API handler function on the API mux.
// Pattern should follow net/http.ServeMux semantics (e.g. "GET /api/status").
func (s *Server) HandleFunc(pattern string, handler http.HandlerFunc) {
	s.apiMux.HandleFunc(pattern, handler)
}

// Handle registers an API handler on the API mux.
func (s *Server) Handle(pattern string, handler http.Handler) {
	s.apiMux.Handle(pattern, handler)
}

// Start begins serving HTTP requests. It blocks until the listener is
// ready, then returns. The server runs in the background; call Wait to
// block until it exits.
func (s *Server) Start(ctx context.Context) error {
	// Build the root handler: API mux for /api/ routes, SPA handler for everything else.
	rootHandler := s.rootHandler()

	s.httpServer = &http.Server{
		Handler:      rootHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	slog.Info("HTTP server listening", "addr", addr)

	// Serve in background.
	go func() {
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
		}
	}()

	// Wait for context cancellation to trigger graceful shutdown.
	go func() {
		<-ctx.Done()
		slog.Info("shutting down HTTP server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP server shutdown error", "error", err)
		}
	}()

	return nil
}

// rootHandler returns the top-level http.Handler that routes requests
// between the API mux and the static SPA handler.
func (s *Server) rootHandler() http.Handler {
	staticHandler := newStaticHandler()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// API routes go to the API mux.
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			s.apiMux.ServeHTTP(w, r)
			return
		}

		// Everything else is served by the SPA static handler.
		staticHandler.ServeHTTP(w, r)
	})
}
