package observability

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Checker reports whether a dependency is currently usable.
type Checker interface {
	Name() string
	Check(ctx context.Context) error
}

// CheckerFunc adapts a function to the Checker interface.
type CheckerFunc struct {
	CheckerName string
	Fn          func(ctx context.Context) error
}

func (c CheckerFunc) Name() string                    { return c.CheckerName }
func (c CheckerFunc) Check(ctx context.Context) error { return c.Fn(ctx) }

// Health aggregates readiness checks. Liveness is intentionally trivial: the
// process is alive if it can serve the endpoint. Readiness reflects dependencies.
type Health struct {
	mu       sync.RWMutex
	checkers []Checker
	timeout  time.Duration
}

func NewHealth(timeout time.Duration) *Health {
	return &Health{timeout: timeout}
}

func (h *Health) Register(c Checker) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkers = append(h.checkers, c)
}

// LiveHandler always returns 200 while the process can serve requests.
func (h *Health) LiveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
	}
}

// ReadyHandler returns 200 only when every registered dependency is healthy.
func (h *Health) ReadyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.mu.RLock()
		checkers := make([]Checker, len(h.checkers))
		copy(checkers, h.checkers)
		h.mu.RUnlock()

		results := make(map[string]string, len(checkers))
		status := http.StatusOK
		for _, c := range checkers {
			ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
			err := c.Check(ctx)
			cancel()
			if err != nil {
				results[c.Name()] = "unhealthy: " + err.Error()
				status = http.StatusServiceUnavailable
				continue
			}
			results[c.Name()] = "healthy"
		}
		body := map[string]any{"status": "ready", "checks": results}
		if status != http.StatusOK {
			body["status"] = "not_ready"
		}
		writeJSON(w, status, body)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
