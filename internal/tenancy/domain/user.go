package domain

import (
	"regexp"
	"strings"
	"time"
)

var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// User is a person authenticated via an external identity provider. Prism stores
// no passwords; ExternalSubject links the user to the IdP subject claim.
type User struct {
	ID              UserID
	Email           string
	DisplayName     string
	ExternalSubject string
	CreatedAt       time.Time
}

func NewUser(email, displayName, externalSubject string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !emailPattern.MatchString(email) || len(email) > 254 {
		return nil, ErrInvalidEmail
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = email
	}
	return &User{
		ID:              NewUserID(),
		Email:           email,
		DisplayName:     displayName,
		ExternalSubject: strings.TrimSpace(externalSubject),
		CreatedAt:       time.Now().UTC(),
	}, nil
}

// Membership binds a user to a tenant with a role. It is the unit of access control.
type Membership struct {
	TenantID  TenantID
	UserID    UserID
	Role      Role
	CreatedAt time.Time
}

func NewMembership(tenantID TenantID, userID UserID, role Role) Membership {
	return Membership{
		TenantID:  tenantID,
		UserID:    userID,
		Role:      role,
		CreatedAt: time.Now().UTC(),
	}
}
