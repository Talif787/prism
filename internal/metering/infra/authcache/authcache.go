// Package authcache authenticates metering API keys. It verifies the key against
// the control plane with the "admin" scope and caches the result in Redis, so the
// common case is a single Redis GET. It mirrors the query and alerting authenticators.
package authcache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Talif787/prism/internal/metering/app"
)

const (
	scopeAdmin     = "admin"
	negativeMarker = "!"
)

type Authenticator struct {
	http             *http.Client
	verifyURL        string
	internalToken    string
	redis            *goredis.Client
	cacheTTL         time.Duration
	negativeCacheTTL time.Duration
}

type Config struct {
	VerifyURL        string
	InternalToken    string
	Timeout          time.Duration
	CacheTTL         time.Duration
	NegativeCacheTTL time.Duration
}

func New(rdb *goredis.Client, cfg Config) *Authenticator {
	return &Authenticator{
		http:             &http.Client{Timeout: cfg.Timeout},
		verifyURL:        cfg.VerifyURL,
		internalToken:    cfg.InternalToken,
		redis:            rdb,
		cacheTTL:         cfg.CacheTTL,
		negativeCacheTTL: cfg.NegativeCacheTTL,
	}
}

type cachedPrincipal struct {
	TenantID string   `json:"tenant_id"`
	Scopes   []string `json:"scopes"`
}

func (a *Authenticator) Authenticate(ctx context.Context, apiKey string) (*app.Principal, error) {
	if apiKey == "" {
		return nil, app.ErrUnauthorized
	}
	key := a.cacheKey(apiKey)

	if cached, err := a.redis.Get(ctx, key).Result(); err == nil {
		if cached == negativeMarker {
			return nil, app.ErrUnauthorized
		}
		var cp cachedPrincipal
		if json.Unmarshal([]byte(cached), &cp) == nil {
			return &app.Principal{TenantID: cp.TenantID, Scopes: cp.Scopes}, nil
		}
	}

	principal, err := a.verify(ctx, apiKey)
	if err != nil {
		if errors.Is(err, app.ErrUnauthorized) {
			a.redis.Set(ctx, key, negativeMarker, a.negativeCacheTTL)
		}
		return nil, err
	}

	if payload, mErr := json.Marshal(cachedPrincipal{TenantID: principal.TenantID, Scopes: principal.Scopes}); mErr == nil {
		a.redis.Set(ctx, key, payload, a.cacheTTL)
	}
	return principal, nil
}

func (a *Authenticator) verify(ctx context.Context, apiKey string) (*app.Principal, error) {
	body, _ := json.Marshal(map[string]string{"key": apiKey, "scope": scopeAdmin})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.verifyURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", a.internalToken)

	resp, err := a.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("verify request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var out struct {
			TenantID string   `json:"tenant_id"`
			KeyID    string   `json:"key_id"`
			Scopes   []string `json:"scopes"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nil, err
		}
		return &app.Principal{TenantID: out.TenantID, Scopes: out.Scopes}, nil
	case http.StatusForbidden:
		return nil, app.ErrForbidden
	case http.StatusUnauthorized, http.StatusNotFound:
		return nil, app.ErrUnauthorized
	default:
		return nil, fmt.Errorf("verify unexpected status %d", resp.StatusCode)
	}
}

func (a *Authenticator) cacheKey(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return "auth:mkey:" + hex.EncodeToString(sum[:])
}
