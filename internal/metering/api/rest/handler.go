// Package rest exposes the metering and billing API: usage over a period, a current
// billing-period summary with quota and cost, and invoice listing, retrieval, and
// closing. Every route requires an admin-scoped API key; the tenant scopes access.
package rest

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Talif787/prism/internal/metering/app"
	"github.com/Talif787/prism/internal/metering/domain"
	"github.com/Talif787/prism/internal/platform/httpx"
)

const scopeAdmin = "admin"

type ctxKey int

const tenantKey ctxKey = iota

type Handler struct {
	svc    *app.MeteringService
	auth   app.Authenticator
	logger *slog.Logger
}

func NewHandler(svc *app.MeteringService, auth app.Authenticator, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, auth: auth, logger: logger}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/usage", h.getUsage)
	mux.HandleFunc("GET /v1/usage/summary", h.getSummary)
	mux.HandleFunc("GET /v1/invoices", h.listInvoices)
	mux.HandleFunc("GET /v1/invoices/{id}", h.getInvoice)
	mux.HandleFunc("POST /v1/invoices/close", h.closeInvoice)
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
				httpx.WriteProblem(w, r, http.StatusForbidden, "forbidden", "Forbidden", "key lacks the admin scope")
				return
			}
			httpx.WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized", "invalid API key")
			return
		}
		if !principal.HasScope(scopeAdmin) {
			httpx.WriteProblem(w, r, http.StatusForbidden, "forbidden", "Forbidden", "key lacks the admin scope")
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

func (h *Handler) getUsage(w http.ResponseWriter, r *http.Request) {
	from, to := timeRange(r)
	usage, err := h.svc.Usage(r.Context(), tenantOf(r), from, to)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"period_start": from,
		"period_end":   to,
		"usage":        usageMap(usage),
		"total_points": domain.TotalPoints(usage),
	})
}

func (h *Handler) getSummary(w http.ResponseWriter, r *http.Request) {
	sum, err := h.svc.Summary(r.Context(), tenantOf(r), time.Now().UTC())
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"period_start": sum.PeriodStart,
		"period_end":   sum.PeriodEnd,
		"usage":        usageMap(sum.Usage),
		"total_points": sum.TotalPoints,
		"quota": map[string]any{
			"plan":      sum.Quota.Plan,
			"included":  sum.Quota.Included,
			"used":      sum.Quota.Used,
			"remaining": sum.Quota.Remaining,
			"over":      sum.Quota.Over,
		},
		"cost": map[string]any{
			"line_items": lineItemsJSON(sum.LineItems),
			"total":      sum.Cost,
			"currency":   sum.Currency,
		},
	})
}

func (h *Handler) listInvoices(w http.ResponseWriter, r *http.Request) {
	invoices, err := h.svc.ListInvoices(r.Context(), tenantOf(r))
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	out := make([]map[string]any, 0, len(invoices))
	for i := range invoices {
		out = append(out, invoiceJSON(&invoices[i]))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"invoices": out})
}

func (h *Handler) getInvoice(w http.ResponseWriter, r *http.Request) {
	inv, err := h.svc.GetInvoice(r.Context(), tenantOf(r), r.PathValue("id"))
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, invoiceJSON(inv))
}

func (h *Handler) closeInvoice(w http.ResponseWriter, r *http.Request) {
	from, to := timeRange(r)
	inv, err := h.svc.CloseInvoice(r.Context(), tenantOf(r), from, to)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, invoiceJSON(inv))
}

func (h *Handler) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidPeriod):
		httpx.WriteProblem(w, r, http.StatusBadRequest, "invalid_period", "Invalid period", "from must be before to")
	case errors.Is(err, domain.ErrNotFound):
		httpx.WriteProblem(w, r, http.StatusNotFound, "not_found", "Not found", "no such resource")
	case errors.Is(err, app.ErrUnauthorized):
		httpx.WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized", "invalid API key")
	case errors.Is(err, app.ErrForbidden):
		httpx.WriteProblem(w, r, http.StatusForbidden, "forbidden", "Forbidden", "insufficient scope")
	default:
		h.logger.ErrorContext(r.Context(), "metering request failed", slog.Any("error", err))
		httpx.WriteProblem(w, r, http.StatusInternalServerError, "internal", "Internal error", "the request could not be completed")
	}
}

// --- helpers ---

func usageMap(usage map[domain.Signal]int64) map[string]int64 {
	out := make(map[string]int64, len(domain.AllSignals))
	for _, sig := range domain.AllSignals {
		out[string(sig)] = usage[sig]
	}
	return out
}

func lineItemsJSON(items []domain.LineItem) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, li := range items {
		out = append(out, map[string]any{
			"signal":                 string(li.Signal),
			"quantity":               li.Quantity,
			"unit_price_per_million": li.UnitPricePerMillion,
			"amount":                 li.Amount,
		})
	}
	return out
}

func invoiceJSON(inv *domain.Invoice) map[string]any {
	return map[string]any{
		"id":           inv.ID,
		"tenant_id":    inv.TenantID,
		"period_start": inv.PeriodStart,
		"period_end":   inv.PeriodEnd,
		"status":       inv.Status,
		"currency":     inv.Currency,
		"total":        inv.Total,
		"line_items":   lineItemsJSON(inv.LineItems),
		"created_at":   inv.CreatedAt,
	}
}

// timeRange reads from/to as RFC3339, defaulting to the current calendar month to
// the present moment.
func timeRange(r *http.Request) (time.Time, time.Time) {
	now := time.Now().UTC()
	to := now
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = t.UTC()
		}
	}
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = t.UTC()
		}
	}
	return from, to
}

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
