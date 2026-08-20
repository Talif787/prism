package rest

import (
	"context"
	"net/http"
	"strings"

	"github.com/Talif787/prism/internal/platform/auth"
	"github.com/Talif787/prism/internal/platform/httpx"
)

type principalCtxKey struct{}

// Authenticate verifies the bearer token and binds the principal to the context.
// Endpoints that require a signed-in console user compose this middleware.
func Authenticate(verifier auth.TokenVerifier) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := bearerToken(r)
			if !ok {
				httpx.WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized", "missing bearer token")
				return
			}
			principal, err := verifier.Verify(r.Context(), raw)
			if err != nil {
				httpx.WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized", "invalid token")
				return
			}
			ctx := context.WithValue(r.Context(), principalCtxKey{}, principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func principalFrom(ctx context.Context) (*auth.Principal, bool) {
	p, ok := ctx.Value(principalCtxKey{}).(*auth.Principal)
	return p, ok
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	return token, token != ""
}
