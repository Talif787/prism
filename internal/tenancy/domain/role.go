package domain

// Role is a coarse RBAC role scoped to a tenant. Roles map to permissions via
// grants; the mapping lives with the role so authorization is centralized.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleEditor Role = "editor"
	RoleViewer Role = "viewer"
)

// Permission is a fine-grained capability checked at the application boundary.
type Permission string

const (
	PermTenantRead   Permission = "tenant.read"
	PermTenantManage Permission = "tenant.manage"
	PermMemberManage Permission = "member.manage"
	PermKeyManage    Permission = "key.manage"
	PermKeyRead      Permission = "key.read"
)

var rolePermissions = map[Role]map[Permission]bool{
	RoleOwner: {
		PermTenantRead: true, PermTenantManage: true,
		PermMemberManage: true, PermKeyManage: true, PermKeyRead: true,
	},
	RoleAdmin: {
		PermTenantRead: true,
		PermMemberManage: true, PermKeyManage: true, PermKeyRead: true,
	},
	RoleEditor: {PermTenantRead: true, PermKeyRead: true},
	RoleViewer: {PermTenantRead: true},
}

func ParseRole(s string) (Role, error) {
	switch Role(s) {
	case RoleOwner, RoleAdmin, RoleEditor, RoleViewer:
		return Role(s), nil
	default:
		return "", ErrInvalidRole
	}
}

// Allows reports whether the role grants the permission.
func (r Role) Allows(p Permission) bool {
	perms, ok := rolePermissions[r]
	if !ok {
		return false
	}
	return perms[p]
}
