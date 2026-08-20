package domain

import (
	"testing"
	"time"
)

func TestGenerateAPIKey_ProducesVerifiableKey(t *testing.T) {
	tenantID := NewTenantID()
	gen, err := GenerateAPIKey(tenantID, "ci-ingest", []Scope{ScopeIngest}, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if gen.Plaintext == "" {
		t.Fatal("expected non-empty plaintext")
	}
	if !gen.Key.Matches(gen.Plaintext) {
		t.Fatal("generated key should match its own plaintext")
	}
	if gen.Key.Matches(gen.Plaintext + "tampered") {
		t.Fatal("tampered plaintext must not match")
	}
	prefix, ok := ExtractPrefix(gen.Plaintext)
	if !ok || prefix != gen.Key.Prefix {
		t.Fatalf("prefix mismatch: got %q want %q", prefix, gen.Key.Prefix)
	}
}

func TestGenerateAPIKey_Validation(t *testing.T) {
	tid := NewTenantID()
	if _, err := GenerateAPIKey(tid, "", []Scope{ScopeIngest}, nil); err == nil {
		t.Fatal("empty name should fail")
	}
	if _, err := GenerateAPIKey(tid, "k", nil, nil); err == nil {
		t.Fatal("no scopes should fail")
	}
	if _, err := GenerateAPIKey(tid, "k", []Scope{"bogus"}, nil); err == nil {
		t.Fatal("invalid scope should fail")
	}
}

func TestAPIKey_Usable(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	active := &APIKey{Status: APIKeyStatusActive}
	if err := active.Usable(now); err != nil {
		t.Fatalf("active key should be usable: %v", err)
	}
	revoked := &APIKey{Status: APIKeyStatusRevoked}
	if err := revoked.Usable(now); err != ErrKeyRevoked {
		t.Fatalf("want ErrKeyRevoked, got %v", err)
	}
	expired := &APIKey{Status: APIKeyStatusActive, ExpiresAt: &past}
	if err := expired.Usable(now); err != ErrKeyExpired {
		t.Fatalf("want ErrKeyExpired, got %v", err)
	}
	valid := &APIKey{Status: APIKeyStatusActive, ExpiresAt: &future}
	if err := valid.Usable(now); err != nil {
		t.Fatalf("future expiry should be usable: %v", err)
	}
}

func TestAPIKey_HasScope(t *testing.T) {
	k := &APIKey{Scopes: []Scope{ScopeIngest, ScopeQuery}}
	if !k.HasScope(ScopeIngest) || !k.HasScope(ScopeQuery) {
		t.Fatal("expected granted scopes")
	}
	if k.HasScope(ScopeAdmin) {
		t.Fatal("admin scope should not be granted")
	}
}
