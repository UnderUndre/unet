package metrics

import (
	"log/slog"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry wraps a Prometheus registry with unet-specific metrics.
// All metrics follow contracts/metrics.md naming conventions.
type Registry struct {
	reg *prometheus.Registry

	// Counters
	HealthChecksTotal *prometheus.CounterVec
	VPSOpsTotal       *prometheus.CounterVec
	LogEventsTotal    *prometheus.CounterVec

	// Gauges
	ActiveConns  prometheus.Gauge
	Uptime       prometheus.Gauge
	SSEReaders   prometheus.Gauge

	// Histograms (optional, for latency tracking)
	VPSOpDuration *prometheus.HistogramVec

	mu sync.Mutex
}

// NewRegistry creates a new metrics registry with all unet metrics pre-registered.
func NewRegistry() *Registry {
	r := &Registry{
		reg: prometheus.NewRegistry(),
	}

	// Counters
	r.HealthChecksTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "unet_health_checks_total",
		Help: "Total number of VPS health checks performed.",
	}, []string{"target", "result"})

	r.VPSOpsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "unet_vps_operations_total",
		Help: "Total number of VPS lifecycle operations.",
	}, []string{"operation", "provider", "result"})

	r.LogEventsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "unet_log_events_total",
		Help: "Total number of log events processed by the observability subsystem.",
	}, []string{"level", "component", "source"})

	// Gauges
	r.ActiveConns = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "unet_active_connections",
		Help: "Current number of active SSE/log connections.",
	})

	r.Uptime = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "unet_uptime_seconds",
		Help: "Process uptime in seconds.",
	})

	r.SSEReaders = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "unet_sse_readers",
		Help: "Current number of SSE subscribers.",
	})

	// Histograms
	r.VPSOpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "unet_vps_operation_duration_seconds",
		Help:    "Duration of VPS lifecycle operations in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"operation", "provider"})

	// Register all
	r.reg.MustRegister(
		r.HealthChecksTotal,
		r.VPSOpsTotal,
		r.LogEventsTotal,
		r.ActiveConns,
		r.Uptime,
		r.SSEReaders,
		r.VPSOpDuration,
	)

	return r
}

// PrometheusHandler returns an http.Handler that serves Prometheus metrics.
func (r *Registry) PrometheusHandler() http.Handler {
	return promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// RecordHealthCheck increments the health check counter.
func (r *Registry) RecordHealthCheck(target, result string) {
	r.HealthChecksTotal.WithLabelValues(target, result).Inc()
}

// RecordVPSOp increments the VPS operation counter.
func (r *Registry) RecordVPSOp(operation, provider, result string) {
	r.VPSOpsTotal.WithLabelValues(operation, provider, result).Inc()
}

// RecordLogEvent increments the log event counter.
func (r *Registry) RecordLogEvent(level, component, source string) {
	r.LogEventsTotal.WithLabelValues(level, component, source).Inc()
}

// ObserveVPSOpDuration records the duration of a VPS operation.
func (r *Registry) ObserveVPSOpDuration(operation, provider string, seconds float64) {
	r.VPSOpDuration.WithLabelValues(operation, provider).Observe(seconds)
}

// SetSSEReaders updates the SSE subscriber gauge.
func (r *Registry) SetSSEReaders(count int) {
	r.SSEReaders.Set(float64(count))
}

// Exposer runs a Prometheus metrics HTTP server.
type Exposer struct {
	registry   *Registry
	listenAddr string
	bearerToken string
	server     *http.Server
}

// NewExposer creates a new metrics HTTP server.
func NewExposer(registry *Registry, listenAddr, bearerToken string) *Exposer {
	return &Exposer{
		registry:    registry,
		listenAddr:  listenAddr,
		bearerToken: bearerToken,
	}
}

// Start starts the metrics HTTP server. Blocks until the server exits.
func (e *Exposer) Start() error {
	mux := http.NewServeMux()

	if e.bearerToken != "" {
		mux.Handle("/metrics", e.bearerAuth(e.registry.PrometheusHandler()))
	} else {
		mux.Handle("/metrics", e.registry.PrometheusHandler())
	}

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	e.server = &http.Server{
		Addr:    e.listenAddr,
		Handler: mux,
	}

	slog.Info("Prometheus metrics server starting", "addr", e.listenAddr)
	if err := e.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// bearerAuth wraps a handler with Bearer token authentication.
func (e *Exposer) bearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// Expect "Bearer <token>"
		if len(auth) < 7 || auth[:7] != "Bearer " {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if auth[7:] != e.bearerToken {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
