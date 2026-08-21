// Package rest exposes the query service over HTTP. Every route is behind API-key
// authentication; the authenticated tenant, not any request parameter, scopes
// every query. Handlers parse and clamp inputs, then delegate to the app service.
package rest

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Talif787/prism/internal/platform/httpx"
	"github.com/Talif787/prism/internal/query/app"
	"github.com/Talif787/prism/internal/query/domain"
)

const scopeQuery = "query"

type ctxKey int

const tenantKey ctxKey = iota

type Handler struct {
	svc    *app.Service
	auth   app.Authenticator
	logger *slog.Logger
}

func NewHandler(svc *app.Service, auth app.Authenticator, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, auth: auth, logger: logger}
}

// Routes returns the authenticated query router.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/metrics/names", h.metricNames)
	mux.HandleFunc("GET /v1/metrics/query", h.queryRange)
	mux.HandleFunc("GET /v1/logs", h.searchLogs)
	mux.HandleFunc("GET /v1/traces", h.listTraces)
	mux.HandleFunc("GET /v1/traces/{traceID}", h.getTrace)
	return h.authenticate(mux)
}

func (h *Handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey, ok := extractAPIKey(r)
		if !ok {
			httpx.WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized", "missing API key")
			return
		}
		principal, err := h.auth.Authenticate(r.Context(), apiKey)
		if err != nil {
			if errors.Is(err, app.ErrForbidden) {
				httpx.WriteProblem(w, r, http.StatusForbidden, "forbidden", "Forbidden", "key lacks the query scope")
				return
			}
			httpx.WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized", "invalid API key")
			return
		}
		if !principal.HasScope(scopeQuery) {
			httpx.WriteProblem(w, r, http.StatusForbidden, "forbidden", "Forbidden", "key lacks the query scope")
			return
		}
		ctx := context.WithValue(r.Context(), tenantKey, principal.TenantID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func tenantOf(r *http.Request) string {
	if v, ok := r.Context().Value(tenantKey).(string); ok {
		return v
	}
	return ""
}

func (h *Handler) metricNames(w http.ResponseWriter, r *http.Request) {
	from, to := timeRange(r)
	names, err := h.svc.MetricNames(r.Context(), tenantOf(r), from, to)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if names == nil {
		names = []string{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"names": names})
}

func (h *Handler) queryRange(w http.ResponseWriter, r *http.Request) {
	from, to := timeRange(r)
	q := domain.RangeQuery{
		Metric:  r.URL.Query().Get("name"),
		From:    from,
		To:      to,
		Step:    parseDuration(r, "step", time.Minute),
		Agg:     r.URL.Query().Get("agg"),
		GroupBy: parseCSV(r, "group_by"),
		Filters: parseFilters(r),
	}
	series, err := h.svc.QueryRange(r.Context(), tenantOf(r), q)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if series == nil {
		series = []domain.Series{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"metric": q.Metric,
		"step":   q.Step.String(),
		"agg":    q.Agg,
		"series": series,
	})
}

func (h *Handler) searchLogs(w http.ResponseWriter, r *http.Request) {
	from, to := timeRange(r)
	q := domain.LogQuery{
		From:        from,
		To:          to,
		MinSeverity: int32(parseInt(r, "min_severity", 0)),
		Contains:    r.URL.Query().Get("q"),
		Limit:       parseInt(r, "limit", 0),
	}
	logs, err := h.svc.SearchLogs(r.Context(), tenantOf(r), q)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if logs == nil {
		logs = []domain.LogEntry{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

func (h *Handler) listTraces(w http.ResponseWriter, r *http.Request) {
	from, to := timeRange(r)
	q := domain.TraceQuery{
		From:    from,
		To:      to,
		Service: r.URL.Query().Get("service"),
		Limit:   parseInt(r, "limit", 0),
	}
	spans, err := h.svc.ListTraces(r.Context(), tenantOf(r), q)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if spans == nil {
		spans = []domain.SpanEntry{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"traces": spans})
}

func (h *Handler) getTrace(w http.ResponseWriter, r *http.Request) {
	traceID := r.PathValue("traceID")
	if traceID == "" {
		httpx.WriteProblem(w, r, http.StatusBadRequest, "invalid_query", "Invalid query", "trace id is required")
		return
	}
	spans, err := h.svc.GetTrace(r.Context(), tenantOf(r), traceID)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if len(spans) == 0 {
		httpx.WriteProblem(w, r, http.StatusNotFound, "not_found", "Not found", "no spans for that trace id")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"trace_id": traceID, "spans": spans})
}

func (h *Handler) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidQuery):
		httpx.WriteProblem(w, r, http.StatusBadRequest, "invalid_query", "Invalid query", err.Error())
	case errors.Is(err, app.ErrUnauthorized):
		httpx.WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized", "invalid API key")
	case errors.Is(err, app.ErrForbidden):
		httpx.WriteProblem(w, r, http.StatusForbidden, "forbidden", "Forbidden", "insufficient scope")
	default:
		h.logger.Error("query failed", slog.Any("error", err))
		httpx.WriteProblem(w, r, http.StatusInternalServerError, "internal", "Internal error", "the query could not be completed")
	}
}

// --- parameter helpers ---

func extractAPIKey(r *http.Request) (string, bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		const prefix = "Bearer "
		if strings.HasPrefix(h, prefix) {
			if k := strings.TrimSpace(h[len(prefix):]); k != "" {
				return k, true
			}
		}
	}
	if k := strings.TrimSpace(r.Header.Get("X-Prism-Key")); k != "" {
		return k, true
	}
	return "", false
}

// timeRange reads from/to as RFC3339, defaulting to the last hour.
func timeRange(r *http.Request) (time.Time, time.Time) {
	to := time.Now().UTC()
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = t.UTC()
		}
	}
	from := to.Add(-time.Hour)
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = t.UTC()
		}
	}
	return from, to
}

func parseDuration(r *http.Request, key string, def time.Duration) time.Duration {
	if v := r.URL.Query().Get(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func parseInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func parseCSV(r *http.Request, key string) []string {
	v := r.URL.Query().Get(key)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// parseFilters reads repeated filter params of the form key=value.
func parseFilters(r *http.Request) map[string]string {
	raw := r.URL.Query()["filter"]
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for _, kv := range raw {
		if i := strings.IndexByte(kv, '='); i > 0 {
			out[kv[:i]] = kv[i+1:]
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
