package domain

import "github.com/google/uuid"

type (
	TenantID uuid.UUID
	UserID   uuid.UUID
	APIKeyID uuid.UUID
)

func NewTenantID() TenantID { return TenantID(uuid.New()) }
func NewUserID() UserID     { return UserID(uuid.New()) }
func NewAPIKeyID() APIKeyID { return APIKeyID(uuid.New()) }

func (id TenantID) String() string { return uuid.UUID(id).String() }
func (id UserID) String() string   { return uuid.UUID(id).String() }
func (id APIKeyID) String() string { return uuid.UUID(id).String() }

// MarshalText makes the id types serialize as canonical UUID strings in JSON
// (for example in outbox event payloads) rather than as raw byte arrays.
func (id TenantID) MarshalText() ([]byte, error) { return []byte(id.String()), nil }
func (id UserID) MarshalText() ([]byte, error)   { return []byte(id.String()), nil }
func (id APIKeyID) MarshalText() ([]byte, error) { return []byte(id.String()), nil }

func ParseTenantID(s string) (TenantID, error) {
	u, err := uuid.Parse(s)
	return TenantID(u), err
}

func ParseUserID(s string) (UserID, error) {
	u, err := uuid.Parse(s)
	return UserID(u), err
}

func ParseAPIKeyID(s string) (APIKeyID, error) {
	u, err := uuid.Parse(s)
	return APIKeyID(u), err
}
