// Package auth verifies console session tokens. Two strategies are provided and
// selected by configuration: a dev HS256 verifier for local development and an
// OIDC verifier for real environments. Both satisfy the same TokenVerifier port,
// so the transport layer is identical everywhere (strategy pattern, justified by
// a genuine environment-driven difference).
package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
)

// Principal is the authenticated identity extracted from a verified token.
type Principal struct {
	Subject string
	Email   string
	Name    string
}

// TokenVerifier validates a raw bearer token and returns its principal.
type TokenVerifier interface {
	Verify(ctx context.Context, rawToken string) (*Principal, error)
}

var ErrInvalidToken = errors.New("invalid token")

// DevVerifier validates HS256 tokens with a shared secret. It is only permitted
// outside production, enforced by configuration validation.
type DevVerifier struct {
	secret   []byte
	audience string
}

func NewDevVerifier(secret, audience string) *DevVerifier {
	return &DevVerifier{secret: []byte(secret), audience: audience}
}

func (v *DevVerifier) Verify(_ context.Context, raw string) (*Principal, error) {
	claims := jwt.MapClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
	)
	_, err := parser.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) {
		return v.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	return principalFromClaims(claims)
}

// OIDCVerifier validates RS256 ID tokens against a provider's JWKS.
type OIDCVerifier struct {
	verifier *oidc.IDTokenVerifier
}

func NewOIDCVerifier(ctx context.Context, issuer, audience string) (*OIDCVerifier, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("discover oidc provider: %w", err)
	}
	return &OIDCVerifier{verifier: provider.Verifier(&oidc.Config{ClientID: audience})}, nil
}

func (v *OIDCVerifier) Verify(ctx context.Context, raw string) (*Principal, error) {
	tok, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := tok.Claims(&claims); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	return &Principal{Subject: tok.Subject, Email: claims.Email, Name: claims.Name}, nil
}

func principalFromClaims(claims jwt.MapClaims) (*Principal, error) {
	sub, _ := claims["sub"].(string)
	email, _ := claims["email"].(string)
	name, _ := claims["name"].(string)
	if sub == "" && email == "" {
		return nil, fmt.Errorf("%w: token missing subject and email", ErrInvalidToken)
	}
	return &Principal{Subject: sub, Email: email, Name: name}, nil
}
