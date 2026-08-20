package rest

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/Talif787/prism/internal/platform/httpx"
	"github.com/Talif787/prism/internal/tenancy/app"
	"github.com/Talif787/prism/internal/tenancy/domain"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// Handler holds the dependencies for the tenancy HTTP endpoints.
type Handler struct {
	svc    *app.Service
	logger *slog.Logger
}

func NewHandler(svc *app.Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

func (h *Handler) createTenant(w http.ResponseWriter, r *http.Request) {
	var req createTenantRequest
	if !httpx.DecodeJSON(w, r, maxBodyBytes, &req) {
		return
	}
	out, err := h.svc.CreateTenant(r.Context(), app.CreateTenantInput{
		Name: req.Name, Slug: req.Slug, Plan: domain.Plan(req.Plan),
		OwnerEmail: req.OwnerEmail, OwnerName: req.OwnerName, OwnerSubject: req.OwnerSubject,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toTenantResponse(out.Tenant))
}

func (h *Handler) getTenant(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.authorizeTenant(w, r, domain.PermTenantRead)
	if !ok {
		return
	}
	t, err := h.svc.GetTenant(r.Context(), tenantID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toTenantResponse(t))
}

func (h *Handler) addMember(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.authorizeTenant(w, r, domain.PermMemberManage)
	if !ok {
		return
	}
	var req addMemberRequest
	if !httpx.DecodeJSON(w, r, maxBodyBytes, &req) {
		return
	}
	role, err := domain.ParseRole(req.Role)
	if err != nil {
		httpx.WriteProblem(w, r, http.StatusBadRequest, "invalid_role", "Invalid role", err.Error())
		return
	}
	user, err := h.svc.AddMember(r.Context(), app.AddMemberInput{
		TenantID: tenantID, Email: req.Email, DisplayName: req.DisplayName,
		Subject: req.Subject, Role: role,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, memberResponse{
		UserID: user.ID.String(), Email: user.Email,
		DisplayName: user.DisplayName, Role: string(role),
	})
}

func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.authorizeTenant(w, r, domain.PermTenantRead)
	if !ok {
		return
	}
	views, err := h.svc.ListMembers(r.Context(), tenantID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"members": toMemberResponses(views)})
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.authorizeTenant(w, r, domain.PermMemberManage)
	if !ok {
		return
	}
	userID, err := domain.ParseUserID(r.PathValue("userID"))
	if err != nil {
		httpx.WriteProblem(w, r, http.StatusBadRequest, "invalid_id", "Invalid user id", err.Error())
		return
	}
	if err := h.svc.RemoveMember(r.Context(), tenantID, userID); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) issueKey(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.authorizeTenant(w, r, domain.PermKeyManage)
	if !ok {
		return
	}
	var req issueKeyRequest
	if !httpx.DecodeJSON(w, r, maxBodyBytes, &req) {
		return
	}
	scopes, err := parseScopes(req.Scopes)
	if err != nil {
		httpx.WriteProblem(w, r, http.StatusBadRequest, "invalid_scope", "Invalid scope", err.Error())
		return
	}
	out, err := h.svc.IssueKey(r.Context(), app.IssueKeyInput{
		TenantID: tenantID, Name: req.Name, Scopes: scopes, ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	resp := issuedKeyResponse{keyResponse: toKeyResponse(out.Key), Key: out.Plaintext}
	httpx.WriteJSON(w, http.StatusCreated, resp)
}

func (h *Handler) listKeys(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.authorizeTenant(w, r, domain.PermKeyRead)
	if !ok {
		return
	}
	keys, err := h.svc.ListKeys(r.Context(), tenantID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	resp := make([]keyResponse, len(keys))
	for i, k := range keys {
		resp[i] = toKeyResponse(k)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"keys": resp})
}

func (h *Handler) rotateKey(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.authorizeTenant(w, r, domain.PermKeyManage)
	if !ok {
		return
	}
	keyID, err := domain.ParseAPIKeyID(r.PathValue("keyID"))
	if err != nil {
		httpx.WriteProblem(w, r, http.StatusBadRequest, "invalid_id", "Invalid key id", err.Error())
		return
	}
	out, err := h.svc.RotateKey(r.Context(), tenantID, keyID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	resp := issuedKeyResponse{keyResponse: toKeyResponse(out.Key), Key: out.Plaintext}
	httpx.WriteJSON(w, http.StatusCreated, resp)
}

func (h *Handler) revokeKey(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.authorizeTenant(w, r, domain.PermKeyManage)
	if !ok {
		return
	}
	keyID, err := domain.ParseAPIKeyID(r.PathValue("keyID"))
	if err != nil {
		httpx.WriteProblem(w, r, http.StatusBadRequest, "invalid_id", "Invalid key id", err.Error())
		return
	}
	if err := h.svc.RevokeKey(r.Context(), tenantID, keyID); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// verifyKey is the internal service-to-service endpoint used by the ingest and
// query gateways. It is guarded by the internal-token middleware, not user auth.
func (h *Handler) verifyKey(w http.ResponseWriter, r *http.Request) {
	var req verifyKeyRequest
	if !httpx.DecodeJSON(w, r, maxBodyBytes, &req) {
		return
	}
	scope, err := domain.ParseScope(req.Scope)
	if err != nil {
		httpx.WriteProblem(w, r, http.StatusBadRequest, "invalid_scope", "Invalid scope", err.Error())
		return
	}
	authed, err := h.svc.AuthenticateKey(r.Context(), req.Key, scope)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	scopes := make([]string, len(authed.Scopes))
	for i, s := range authed.Scopes {
		scopes[i] = string(s)
	}
	httpx.WriteJSON(w, http.StatusOK, verifyKeyResponse{
		TenantID: authed.TenantID.String(), KeyID: authed.KeyID.String(), Scopes: scopes,
	})
}

// authorizeTenant extracts the tenant id from the path, resolves the principal,
// and enforces the required permission before any handler logic runs.
func (h *Handler) authorizeTenant(w http.ResponseWriter, r *http.Request, perm domain.Permission) (domain.TenantID, bool) {
	tenantID, err := domain.ParseTenantID(r.PathValue("tenantID"))
	if err != nil {
		httpx.WriteProblem(w, r, http.StatusBadRequest, "invalid_id", "Invalid tenant id", err.Error())
		return domain.TenantID{}, false
	}
	principal, ok := principalFrom(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized", "")
		return domain.TenantID{}, false
	}
	if _, err := h.svc.Authorize(r.Context(), principal.Email, tenantID, perm); err != nil {
		h.writeAuthzError(w, r, err)
		return domain.TenantID{}, false
	}
	return tenantID, true
}

func parseScopes(in []string) ([]domain.Scope, error) {
	if len(in) == 0 {
		return nil, domain.ErrInvalidScope
	}
	out := make([]domain.Scope, 0, len(in))
	for _, s := range in {
		scope, err := domain.ParseScope(s)
		if err != nil {
			return nil, err
		}
		out = append(out, scope)
	}
	return out, nil
}

// writeError maps domain and application errors to problem responses. This is the
// single translation point between internal errors and HTTP status codes.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		httpx.WriteProblem(w, r, http.StatusNotFound, "not_found", "Not found", "")
	case errors.Is(err, domain.ErrAlreadyExists):
		httpx.WriteProblem(w, r, http.StatusConflict, "conflict", "Already exists", err.Error())
	case errors.Is(err, domain.ErrLastOwner):
		httpx.WriteProblem(w, r, http.StatusConflict, "last_owner", "Cannot remove last owner", "")
	case errors.Is(err, domain.ErrTenantSuspended):
		httpx.WriteProblem(w, r, http.StatusForbidden, "tenant_suspended", "Tenant suspended", "")
	case errors.Is(err, domain.ErrScopeNotGranted):
		httpx.WriteProblem(w, r, http.StatusForbidden, "forbidden", "Forbidden", "")
	case errors.Is(err, domain.ErrKeyRevoked), errors.Is(err, domain.ErrKeyExpired):
		httpx.WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized", "")
	case isValidationError(err):
		httpx.WriteProblem(w, r, http.StatusBadRequest, "validation_error", "Validation failed", err.Error())
	default:
		h.logger.ErrorContext(r.Context(), "unhandled error", slog.Any("error", err))
		httpx.WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
	}
}

func (h *Handler) writeAuthzError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		// Do not disclose whether the tenant exists to an unauthorized caller.
		httpx.WriteProblem(w, r, http.StatusForbidden, "forbidden", "Forbidden", "")
	case errors.Is(err, domain.ErrScopeNotGranted):
		httpx.WriteProblem(w, r, http.StatusForbidden, "forbidden", "Forbidden", "")
	default:
		h.logger.ErrorContext(r.Context(), "authorization error", slog.Any("error", err))
		httpx.WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
	}
}

func isValidationError(err error) bool {
	for _, ve := range []error{
		domain.ErrInvalidName, domain.ErrInvalidSlug, domain.ErrInvalidEmail,
		domain.ErrInvalidRole, domain.ErrInvalidScope,
	} {
		if errors.Is(err, ve) {
			return true
		}
	}
	return false
}
