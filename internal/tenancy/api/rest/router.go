package rest

import (
	"net/http"

	"github.com/Talif787/prism/internal/platform/auth"
	"github.com/Talif787/prism/internal/platform/httpx"
)

// RouterConfig carries the cross-cutting dependencies the router needs.
type RouterConfig struct {
	Verifier      auth.TokenVerifier
	InternalToken string
}

// Register mounts the tenancy routes onto the provided mux. Console routes are
// guarded by token authentication; the internal verify route is guarded by the
// service-to-service token. Method and path patterns use the Go 1.22 mux, which
// removes the need for a third-party router.
func (h *Handler) Register(mux *http.ServeMux, cfg RouterConfig) {
	authed := func(fn http.HandlerFunc) http.Handler {
		return httpx.Chain(fn, Authenticate(cfg.Verifier))
	}
	internal := func(fn http.HandlerFunc) http.Handler {
		return httpx.Chain(fn, httpx.InternalToken(cfg.InternalToken))
	}

	// Tenant provisioning is an internal/admin operation in Phase 1 (guarded by
	// the internal token). Console self-service onboarding arrives with billing.
	mux.Handle("POST /v1/tenants", internal(h.createTenant))

	mux.Handle("GET /v1/tenants/{tenantID}", authed(h.getTenant))
	mux.Handle("GET /v1/tenants/{tenantID}/members", authed(h.listMembers))
	mux.Handle("POST /v1/tenants/{tenantID}/members", authed(h.addMember))
	mux.Handle("DELETE /v1/tenants/{tenantID}/members/{userID}", authed(h.removeMember))

	mux.Handle("GET /v1/tenants/{tenantID}/api-keys", authed(h.listKeys))
	mux.Handle("POST /v1/tenants/{tenantID}/api-keys", authed(h.issueKey))
	mux.Handle("POST /v1/tenants/{tenantID}/api-keys/{keyID}/rotate", authed(h.rotateKey))
	mux.Handle("POST /v1/tenants/{tenantID}/api-keys/{keyID}/revoke", authed(h.revokeKey))

	mux.Handle("POST /internal/v1/keys/verify", internal(h.verifyKey))
}
