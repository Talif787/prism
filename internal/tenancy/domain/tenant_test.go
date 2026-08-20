package domain

import "testing"

func TestNewTenant_Validation(t *testing.T) {
	cases := []struct {
		name, tenantName, slug string
		wantErr                error
	}{
		{"valid", "Acme Inc", "acme", nil},
		{"empty name", "", "acme", ErrInvalidName},
		{"bad slug upper", "Acme", "Acme", ErrInvalidSlug},
		{"bad slug space", "Acme", "ac me", ErrInvalidSlug},
		{"short slug", "Acme", "a", ErrInvalidSlug},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewTenant(tc.tenantName, tc.slug, PlanFree)
			if err != tc.wantErr {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestRole_Allows(t *testing.T) {
	if !RoleOwner.Allows(PermTenantManage) {
		t.Fatal("owner should manage tenant")
	}
	if RoleViewer.Allows(PermKeyManage) {
		t.Fatal("viewer should not manage keys")
	}
	if !RoleAdmin.Allows(PermMemberManage) {
		t.Fatal("admin should manage members")
	}
	if RoleAdmin.Allows(PermTenantManage) {
		t.Fatal("admin should not manage the tenant itself")
	}
}
