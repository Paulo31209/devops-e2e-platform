// Package httpapi exposes the notes store over HTTP, with health probes and
// Prometheus metrics.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Paulo31209/devops-e2e-platform/internal/notes"
)

const (
	maxBodyBytes   = 1 << 20
	maxTitleLength = 200
)

// API holds the HTTP handlers and their dependencies.
type API struct {
	store    *notes.Store
	logger   *slog.Logger
	version  string
	ready    atomic.Bool
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// New builds an API. It starts in the ready state.
func New(store *notes.Store, logger *slog.Logger, version string) *API {
	a := &API{
		store:    store,
		logger:   logger,
		version:  version,
		registry: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests by method, route and status code.",
		}, []string{"method", "route", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency by method and route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
	}
	a.ready.Store(true)

	a.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		a.requests,
		a.duration,
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "notes_stored",
			Help: "Number of notes currently stored.",
		}, func() float64 { return float64(store.Len()) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name:        "app_build_info",
			Help:        "Build information.",
			ConstLabels: prometheus.Labels{"version": version},
		}, func() float64 { return 1 }),
	)
	return a
}

// SetReady toggles the readiness probe, e.g. during graceful shutdown.
func (a *API) SetReady(ready bool) { a.ready.Store(ready) }

// Routes returns the instrumented HTTP handler.
func (a *API) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.healthz)
	mux.HandleFunc("GET /readyz", a.readyz)
	mux.HandleFunc("GET /version", a.versionInfo)
	mux.Handle("GET /metrics", promhttp.HandlerFor(a.registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("GET /api/v1/notes", a.listNotes)
	mux.HandleFunc("POST /api/v1/notes", a.createNote)
	mux.HandleFunc("GET /api/v1/notes/{id}", a.getNote)
	mux.HandleFunc("DELETE /api/v1/notes/{id}", a.deleteNote)
	return a.instrument(mux)
}

func (a *API) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) readyz(w http.ResponseWriter, _ *http.Request) {
	if !a.ready.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (a *API) versionInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": a.version})
}

func (a *API) listNotes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.store.List())
}

type createNoteRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (a *API) createNote(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req createNoteRequest
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	req.Title = strings.TrimSpace(req.Title)
	switch {
	case req.Title == "":
		writeError(w, http.StatusUnprocessableEntity, "title is required")
		return
	case len(req.Title) > maxTitleLength:
		writeError(w, http.StatusUnprocessableEntity, "title is too long")
		return
	}

	n := a.store.Create(req.Title, req.Body)
	w.Header().Set("Location", "/api/v1/notes/"+strconv.FormatInt(n.ID, 10))
	writeJSON(w, http.StatusCreated, n)
}

func (a *API) getNote(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	n, err := a.store.Get(id)
	if errors.Is(err, notes.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (a *API) deleteNote(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := a.store.Delete(id); errors.Is(err, notes.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return 0, false
	}
	return id, true
}

// statusRecorder captures the response status code for metrics and logs.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// instrument records Prometheus metrics and a structured access log for
// every request. The route label uses the matched pattern (not the raw path)
// to keep metric cardinality bounded.
func (a *API) instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		route := r.Pattern
		if route == "" {
			route = "unmatched"
		} else if _, path, found := strings.Cut(route, " "); found {
			route = path
		}
		elapsed := time.Since(start)

		a.requests.WithLabelValues(r.Method, route, strconv.Itoa(rec.status)).Inc()
		a.duration.WithLabelValues(r.Method, route).Observe(elapsed.Seconds())

		if route != "/healthz" && route != "/readyz" && route != "/metrics" {
			a.logger.Info("request",
				"method", r.Method,
				"route", route,
				"status", rec.status,
				"duration_ms", elapsed.Milliseconds(),
			)
		}
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
