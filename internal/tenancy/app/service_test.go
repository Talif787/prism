package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/Talif787/prism/internal/tenancy/domain"
)

// The fakes below implement the application ports with in-memory maps, letting us
// test use-case orchestration and invariants without a database.

type fakeStore struct {
	tenants     map[string]*domain.Tenant
	tenantsSlug map[string]*domain.Tenant
	users       map[string]*domain.User
	memberships map[string]domain.Membership // key: tenant|user
	keys        map[string]*domain.APIKey
	events      []domain.Event
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		tenants: map[string]*domain.Tenant{}, tenantsSlug: map[string]*domain.Tenant{},
		users: map[string]*domain.User{}, memberships: map[string]domain.Membership{},
		keys: map[string]*domain.APIKey{},
	}
}

func mkey(t domain.TenantID, u domain.UserID) string { return t.String() + "|" + u.String() }

// TenantRepository
func (f *fakeStore) Create(_ context.Context, t *domain.Tenant) error {
	f.tenants[t.ID.String()] = t
	f.tenantsSlug[t.Slug] = t
	return nil
}
func (f *fakeStore) GetByID(_ context.Context, id domain.TenantID) (*domain.Tenant, error) {
	if t, ok := f.tenants[id.String()]; ok {
		return t, nil
	}
	return nil, domain.ErrNotFound
}
func (f *fakeStore) GetBySlug(_ context.Context, slug string) (*domain.Tenant, error) {
	if t, ok := f.tenantsSlug[slug]; ok {
		return t, nil
	}
	return nil, domain.ErrNotFound
}

// UserRepository
func (f *fakeStore) Upsert(_ context.Context, u *domain.User) (*domain.User, error) {
	for _, existing := range f.users {
		if existing.Email == u.Email {
			return existing, nil
		}
	}
	f.users[u.ID.String()] = u
	return u, nil
}
func (f *fakeStore) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	for _, u := range f.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (f *fakeStore) AddMembership(_ context.Context, m domain.Membership) error {
	f.memberships[mkey(m.TenantID, m.UserID)] = m
	return nil
}
func (f *fakeStore) RemoveMembership(_ context.Context, t domain.TenantID, u domain.UserID) error {
	k := mkey(t, u)
	if _, ok := f.memberships[k]; !ok {
		return domain.ErrNotFound
	}
	delete(f.memberships, k)
	return nil
}
func (f *fakeStore) ListMemberships(_ context.Context, t domain.TenantID) ([]MembershipView, error) {
	var out []MembershipView
	for _, m := range f.memberships {
		if m.TenantID == t {
			u := f.users[m.UserID.String()]
			out = append(out, MembershipView{UserID: m.UserID, Email: u.Email, DisplayName: u.DisplayName, Role: m.Role})
		}
	}
	return out, nil
}
func (f *fakeStore) CountOwners(_ context.Context, t domain.TenantID) (int, error) {
	n := 0
	for _, m := range f.memberships {
		if m.TenantID == t && m.Role == domain.RoleOwner {
			n++
		}
	}
	return n, nil
}
func (f *fakeStore) GetRole(_ context.Context, t domain.TenantID, u domain.UserID) (domain.Role, error) {
	if m, ok := f.memberships[mkey(t, u)]; ok {
		return m.Role, nil
	}
	return "", domain.ErrNotFound
}

// APIKeyRepository
func (f *fakeStore) CreateKey(_ context.Context, k *domain.APIKey) error {
	f.keys[k.ID.String()] = k
	return nil
}
func (f *fakeStore) GetByPrefix(_ context.Context, prefix string) (*domain.APIKey, error) {
	for _, k := range f.keys {
		if k.Prefix == prefix {
			return k, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (f *fakeStore) GetKeyByID(_ context.Context, t domain.TenantID, id domain.APIKeyID) (*domain.APIKey, error) {
	if k, ok := f.keys[id.String()]; ok && k.TenantID == t {
		return k, nil
	}
	return nil, domain.ErrNotFound
}
func (f *fakeStore) ListByTenant(_ context.Context, t domain.TenantID) ([]*domain.APIKey, error) {
	var out []*domain.APIKey
	for _, k := range f.keys {
		if k.TenantID == t {
			out = append(out, k)
		}
	}
	return out, nil
}
func (f *fakeStore) UpdateStatus(_ context.Context, id domain.APIKeyID, s domain.APIKeyStatus) error {
	if k, ok := f.keys[id.String()]; ok {
		k.Status = s
		return nil
	}
	return domain.ErrNotFound
}
func (f *fakeStore) TouchLastUsed(_ context.Context, _ domain.APIKeyID) error { return nil }

// EventPublisher
func (f *fakeStore) Publish(_ context.Context, events ...domain.Event) error {
	f.events = append(f.events, events...)
	return nil
}

// keyRepoAdapter maps the interface method names to the fake's key methods, so the
// fake can satisfy APIKeyRepository without name collisions on Create/GetByID.
type keyRepoAdapter struct{ f *fakeStore }

func (a keyRepoAdapter) Create(ctx context.Context, k *domain.APIKey) error {
	return a.f.CreateKey(ctx, k)
}
func (a keyRepoAdapter) GetByPrefix(ctx context.Context, p string) (*domain.APIKey, error) {
	return a.f.GetByPrefix(ctx, p)
}
func (a keyRepoAdapter) GetByID(ctx context.Context, t domain.TenantID, id domain.APIKeyID) (*domain.APIKey, error) {
	return a.f.GetKeyByID(ctx, t, id)
}
func (a keyRepoAdapter) ListByTenant(ctx context.Context, t domain.TenantID) ([]*domain.APIKey, error) {
	return a.f.ListByTenant(ctx, t)
}
func (a keyRepoAdapter) UpdateStatus(ctx context.Context, id domain.APIKeyID, s domain.APIKeyStatus) error {
	return a.f.UpdateStatus(ctx, id, s)
}
func (a keyRepoAdapter) TouchLastUsed(ctx context.Context, id domain.APIKeyID) error {
	return a.f.TouchLastUsed(ctx, id)
}

// uowAdapter injects the key adapter into the Repositories bundle.
type uowAdapter struct{ f *fakeStore }

func (u uowAdapter) Do(_ context.Context, fn func(Repositories) error) error {
	return fn(Repositories{Tenants: u.f, Users: u.f, Keys: keyRepoAdapter(u), Events: u.f})
}

func newTestService() (*Service, *fakeStore) {
	f := newFakeStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewService(uowAdapter{f}, keyRepoAdapter{f}, logger), f
}

func TestCreateTenant_CreatesOwnerAndEmitsEvents(t *testing.T) {
	svc, store := newTestService()
	out, err := svc.CreateTenant(context.Background(), CreateTenantInput{
		Name: "Acme", Slug: "acme", Plan: domain.PlanTeam,
		OwnerEmail: "owner@acme.io", OwnerName: "Owner",
	})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if out.Tenant.Slug != "acme" {
		t.Fatalf("unexpected slug %q", out.Tenant.Slug)
	}
	owners, _ := store.CountOwners(context.Background(), out.Tenant.ID)
	if owners != 1 {
		t.Fatalf("expected 1 owner, got %d", owners)
	}
	if len(store.events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(store.events))
	}
}

func TestCreateTenant_DuplicateSlugRejected(t *testing.T) {
	svc, _ := newTestService()
	in := CreateTenantInput{Name: "Acme", Slug: "acme", OwnerEmail: "a@acme.io"}
	if _, err := svc.CreateTenant(context.Background(), in); err != nil {
		t.Fatalf("first create: %v", err)
	}
	in.OwnerEmail = "b@acme.io"
	_, err := svc.CreateTenant(context.Background(), in)
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestRemoveMember_LastOwnerRejected(t *testing.T) {
	svc, _ := newTestService()
	out, _ := svc.CreateTenant(context.Background(), CreateTenantInput{
		Name: "Acme", Slug: "acme", OwnerEmail: "owner@acme.io",
	})
	err := svc.RemoveMember(context.Background(), out.Tenant.ID, out.Owner.ID)
	if !errors.Is(err, domain.ErrLastOwner) {
		t.Fatalf("expected ErrLastOwner, got %v", err)
	}
}

func TestAuthenticateKey_HappyPathAndScope(t *testing.T) {
	svc, _ := newTestService()
	out, _ := svc.CreateTenant(context.Background(), CreateTenantInput{
		Name: "Acme", Slug: "acme", OwnerEmail: "owner@acme.io",
	})
	issued, err := svc.IssueKey(context.Background(), IssueKeyInput{
		TenantID: out.Tenant.ID, Name: "ingest", Scopes: []domain.Scope{domain.ScopeIngest},
	})
	if err != nil {
		t.Fatalf("issue key: %v", err)
	}
	authed, err := svc.AuthenticateKey(context.Background(), issued.Plaintext, domain.ScopeIngest)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if authed.TenantID != out.Tenant.ID {
		t.Fatal("tenant mismatch")
	}
	if _, err := svc.AuthenticateKey(context.Background(), issued.Plaintext, domain.ScopeQuery); !errors.Is(err, domain.ErrScopeNotGranted) {
		t.Fatalf("expected ErrScopeNotGranted, got %v", err)
	}
}

func TestAuthenticateKey_RevokedRejected(t *testing.T) {
	svc, _ := newTestService()
	out, _ := svc.CreateTenant(context.Background(), CreateTenantInput{
		Name: "Acme", Slug: "acme", OwnerEmail: "owner@acme.io",
	})
	issued, _ := svc.IssueKey(context.Background(), IssueKeyInput{
		TenantID: out.Tenant.ID, Name: "ingest", Scopes: []domain.Scope{domain.ScopeIngest},
	})
	if err := svc.RevokeKey(context.Background(), out.Tenant.ID, issued.Key.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := svc.AuthenticateKey(context.Background(), issued.Plaintext, domain.ScopeIngest); !errors.Is(err, domain.ErrKeyRevoked) {
		t.Fatalf("expected ErrKeyRevoked, got %v", err)
	}
}
