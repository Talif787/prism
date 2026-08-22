// Package rest exposes the alerting API: tenant-scoped rule CRUD and a current
// alerts listing. Every route requires an admin-scoped API key; the authenticated
// tenant scopes all access.
package rest

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Talif787/prism/internal/alerting/app"
	"github.com/Talif787/prism/internal/alerting/domain"
	"github.com/Talif787/prism/internal/platform/httpx"
)

const scopeAdmin = "admin"

type ctxKey int

const tenantKey ctxKey = iota

type Handler struct {
	svc    *app.RuleService
	auth   app.Authenticator
	logger *slog.Logger
}

func NewHandler(svc *app.RuleService, auth app.Authenticator, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, auth: auth, logger: logger}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/rules", h.createRule)
	mux.HandleFunc("GET /v1/rules", h.listRules)
	mux.HandleFunc("GET /v1/rules/{id}", h.getRule)
	mux.HandleFunc("PUT /v1/rules/{id}", h.updateRule)
	mux.HandleFunc("DELETE /v1/rules/{id}", h.deleteRule)
	mux.HandleFunc("GET /v1/alerts", h.listAlerts)
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

func (h *Handler) createRule(w http.ResponseWriter, r *http.Request) {
	var req ruleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteProblem(w, r, http.StatusBadRequest, "invalid_body", "Invalid body", "could not decode JSON")
		return
	}
	rule, err := req.toDomain()
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if err := h.svc.CreateRule(r.Context(), tenantOf(r), rule); err != nil {
		h.writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toRuleResponse(rule))
}

func (h *Handler) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.svc.ListRules(r.Context(), tenantOf(r))
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	out := make([]ruleResponse, 0, len(rules))
	for i := range rules {
		out = append(out, toRuleResponse(&rules[i]))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"rules": out})
}

func (h *Handler) getRule(w http.ResponseWriter, r *http.Request) {
	rule, err := h.svc.GetRule(r.Context(), tenantOf(r), r.PathValue("id"))
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toRuleResponse(rule))
}

func (h *Handler) updateRule(w http.ResponseWriter, r *http.Request) {
	var req ruleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteProblem(w, r, http.StatusBadRequest, "invalid_body", "Invalid body", "could not decode JSON")
		return
	}
	rule, err := req.toDomain()
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if err := h.svc.UpdateRule(r.Context(), tenantOf(r), r.PathValue("id"), rule); err != nil {
		h.writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toRuleResponse(rule))
}

func (h *Handler) deleteRule(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteRule(r.Context(), tenantOf(r), r.PathValue("id")); err != nil {
		h.writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listAlerts(w http.ResponseWriter, r *http.Request) {
	tenant := tenantOf(r)
	instances, err := h.svc.ListAlerts(r.Context(), tenant)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	rules, err := h.svc.ListRules(r.Context(), tenant)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	byID := make(map[string]*domain.Rule, len(rules))
	for i := range rules {
		byID[rules[i].ID] = &rules[i]
	}
	out := make([]instanceResponse, 0, len(instances))
	for i := range instances {
		resp := toInstanceResponse(&instances[i])
		if rule := byID[instances[i].RuleID]; rule != nil {
			resp.RuleName = rule.Name
			resp.Metric = rule.Metric
			resp.Operator = string(rule.Operator)
			resp.Threshold = rule.Threshold
			resp.Severity = rule.Severity
		}
		out = append(out, resp)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"alerts": out})
}

func (h *Handler) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidRule):
		httpx.WriteProblem(w, r, http.StatusBadRequest, "invalid_rule", "Invalid rule", err.Error())
	case errors.Is(err, domain.ErrNotFound):
		httpx.WriteProblem(w, r, http.StatusNotFound, "not_found", "Not found", "no such rule")
	case errors.Is(err, app.ErrUnauthorized):
		httpx.WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized", "invalid API key")
	case errors.Is(err, app.ErrForbidden):
		httpx.WriteProblem(w, r, http.StatusForbidden, "forbidden", "Forbidden", "insufficient scope")
	case errors.Is(err, domain.ErrRuleNameExists):
		httpx.WriteProblem(w, r, http.StatusConflict, "conflict", "Conflict", err.Error())
	default:
		h.logger.ErrorContext(r.Context(), "alerting request failed", slog.Any("error", err))
		httpx.WriteProblem(w, r, http.StatusInternalServerError, "internal", "Internal error", "the request could not be completed")
	}
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

// --- DTOs ---

type ruleRequest struct {
	Name        string            `json:"name"`
	Metric      string            `json:"metric"`
	Agg         string            `json:"agg"`
	GroupBy     []string          `json:"group_by"`
	Filters     map[string]string `json:"filters"`
	Window      string            `json:"window"`
	Operator    string            `json:"operator"`
	Threshold   float64           `json:"threshold"`
	For         string            `json:"for"`
	Interval    string            `json:"interval"`
	Severity    string            `json:"severity"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	Webhook     string            `json:"webhook"`
	Enabled     *bool             `json:"enabled"`
}

func (req ruleRequest) toDomain() (*domain.Rule, error) {
	window, err := time.ParseDuration(emptyDefault(req.Window, "1m"))
	if err != nil {
		return nil, invalid("window is not a valid duration")
	}
	forDur, err := time.ParseDuration(emptyDefault(req.For, "0s"))
	if err != nil {
		return nil, invalid("for is not a valid duration")
	}
	interval, err := time.ParseDuration(emptyDefault(req.Interval, "30s"))
	if err != nil {
		return nil, invalid("interval is not a valid duration")
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return &domain.Rule{
		Name: req.Name, Metric: req.Metric, Agg: req.Agg, GroupBy: req.GroupBy, Filters: req.Filters,
		Window: window, Operator: domain.Operator(req.Operator), Threshold: req.Threshold,
		For: forDur, Interval: interval, Severity: req.Severity, Labels: req.Labels,
		Annotations: req.Annotations, Webhook: req.Webhook, Enabled: enabled,
	}, nil
}

type ruleResponse struct {
	ID          string            `json:"id"`
	TenantID    string            `json:"tenant_id"`
	Name        string            `json:"name"`
	Metric      string            `json:"metric"`
	Agg         string            `json:"agg"`
	GroupBy     []string          `json:"group_by"`
	Filters     map[string]string `json:"filters"`
	Window      string            `json:"window"`
	Operator    string            `json:"operator"`
	Threshold   float64           `json:"threshold"`
	For         string            `json:"for"`
	Interval    string            `json:"interval"`
	Severity    string            `json:"severity"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	Webhook     string            `json:"webhook"`
	Enabled     bool              `json:"enabled"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

func toRuleResponse(r *domain.Rule) ruleResponse {
	return ruleResponse{
		ID: r.ID, TenantID: r.TenantID, Name: r.Name, Metric: r.Metric, Agg: r.Agg,
		GroupBy: orEmpty(r.GroupBy), Filters: orEmptyMap(r.Filters), Window: r.Window.String(),
		Operator: string(r.Operator), Threshold: r.Threshold, For: r.For.String(), Interval: r.Interval.String(),
		Severity: r.Severity, Labels: orEmptyMap(r.Labels), Annotations: orEmptyMap(r.Annotations),
		Webhook: r.Webhook, Enabled: r.Enabled, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

type instanceResponse struct {
	RuleID      string            `json:"rule_id"`
	RuleName    string            `json:"rule_name,omitempty"`
	Metric      string            `json:"metric,omitempty"`
	Operator    string            `json:"operator,omitempty"`
	Threshold   float64           `json:"threshold"`
	Severity    string            `json:"severity,omitempty"`
	Fingerprint string            `json:"fingerprint"`
	Labels      map[string]string `json:"labels"`
	State       string            `json:"state"`
	Value       float64           `json:"value"`
	ActiveSince time.Time         `json:"active_since"`
	FiredAt     *time.Time        `json:"fired_at,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

func toInstanceResponse(i *domain.Instance) instanceResponse {
	return instanceResponse{
		RuleID: i.RuleID, Fingerprint: i.Fingerprint, Labels: orEmptyMap(i.Labels), State: string(i.State),
		Value: i.Value, ActiveSince: i.ActiveSince, FiredAt: i.FiredAt, UpdatedAt: i.UpdatedAt,
	}
}

func invalid(detail string) error {
	return errWrap{msg: detail}
}

type errWrap struct{ msg string }

func (e errWrap) Error() string        { return "invalid rule: " + e.msg }
func (e errWrap) Is(target error) bool { return target == domain.ErrInvalidRule }

func emptyDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func orEmptyMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
