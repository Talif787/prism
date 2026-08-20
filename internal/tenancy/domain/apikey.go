package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Scope is a capability granted to an API key. Keys are least-privilege: an
// ingest key cannot query, and a query key cannot ingest.
type Scope string

const (
	ScopeIngest Scope = "ingest"
	ScopeQuery  Scope = "query"
	ScopeAdmin  Scope = "admin"
)

func ParseScope(s string) (Scope, error) {
	switch Scope(s) {
	case ScopeIngest, ScopeQuery, ScopeAdmin:
		return Scope(s), nil
	default:
		return "", ErrInvalidScope
	}
}

type APIKeyStatus string

const (
	APIKeyStatusActive  APIKeyStatus = "active"
	APIKeyStatusRevoked APIKeyStatus = "revoked"
)

const (
	keyTokenPrefix = "pk"
	keyIDLen       = 8  // public identifier bytes, hex-encoded to 16 chars
	keySecretLen   = 32 // secret entropy bytes
)

// APIKey is a hashed credential. The plaintext is shown to the caller exactly
// once at creation; only the SHA-256 hash and a public prefix are persisted.
type APIKey struct {
	ID         APIKeyID
	TenantID   TenantID
	Name       string
	Prefix     string // public, e.g. "pk_1a2b3c4d5e6f7a8b"
	Hash       string // hex(sha256(plaintext))
	Scopes     []Scope
	Status     APIKeyStatus
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
}

// GeneratedKey carries the one-time plaintext alongside the persisted record.
type GeneratedKey struct {
	Key       *APIKey
	Plaintext string
}

// GenerateAPIKey mints a new key with cryptographically random material. The
// plaintext format is "pk_<id-hex>_<secret-base64url>".
func GenerateAPIKey(tenantID TenantID, name string, scopes []Scope, expiresAt *time.Time) (*GeneratedKey, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return nil, ErrInvalidName
	}
	if len(scopes) == 0 {
		return nil, ErrInvalidScope
	}
	for _, s := range scopes {
		if _, err := ParseScope(string(s)); err != nil {
			return nil, err
		}
	}

	idBytes := make([]byte, keyIDLen)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("generate key id: %w", err)
	}
	secretBytes := make([]byte, keySecretLen)
	if _, err := rand.Read(secretBytes); err != nil {
		return nil, fmt.Errorf("generate key secret: %w", err)
	}

	prefix := fmt.Sprintf("%s_%s", keyTokenPrefix, hex.EncodeToString(idBytes))
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	plaintext := fmt.Sprintf("%s_%s", prefix, secret)

	return &GeneratedKey{
		Key: &APIKey{
			ID:        NewAPIKeyID(),
			TenantID:  tenantID,
			Name:      name,
			Prefix:    prefix,
			Hash:      HashKey(plaintext),
			Scopes:    scopes,
			Status:    APIKeyStatusActive,
			CreatedAt: time.Now().UTC(),
			ExpiresAt: expiresAt,
		},
		Plaintext: plaintext,
	}, nil
}

// HashKey returns the hex SHA-256 of a plaintext key. SHA-256 is appropriate
// because keys are high-entropy random tokens, not low-entropy passwords.
func HashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// ExtractPrefix parses the public prefix from a plaintext key for indexed lookup.
func ExtractPrefix(plaintext string) (string, bool) {
	parts := strings.SplitN(plaintext, "_", 3)
	if len(parts) != 3 || parts[0] != keyTokenPrefix {
		return "", false
	}
	return parts[0] + "_" + parts[1], true
}

// Matches performs a constant-time comparison of a candidate plaintext against
// the stored hash, guarding against timing attacks.
func (k *APIKey) Matches(plaintext string) bool {
	candidate := HashKey(plaintext)
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(k.Hash)) == 1
}

// Usable validates status and expiry at the moment of authentication.
func (k *APIKey) Usable(now time.Time) error {
	if k.Status == APIKeyStatusRevoked {
		return ErrKeyRevoked
	}
	if k.ExpiresAt != nil && now.After(*k.ExpiresAt) {
		return ErrKeyExpired
	}
	return nil
}

// HasScope reports whether the key grants the given scope.
func (k *APIKey) HasScope(s Scope) bool {
	for _, granted := range k.Scopes {
		if granted == s {
			return true
		}
	}
	return false
}

func (k *APIKey) Revoke() {
	k.Status = APIKeyStatusRevoked
}
