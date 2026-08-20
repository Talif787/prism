package domain

import "time"

// Event is a fact that has happened within the tenancy context. Events are
// published through the outbox so downstream consumers (metering, audit) react
// reliably without coupling to this context.
type Event interface {
	EventName() string
	OccurredAt() time.Time
}

type baseEvent struct{ At time.Time }

func (b baseEvent) OccurredAt() time.Time { return b.At }

type TenantCreated struct {
	baseEvent
	TenantID TenantID
	Slug     string
	Plan     Plan
}

func (TenantCreated) EventName() string { return "tenant.created" }

type MemberAdded struct {
	baseEvent
	TenantID TenantID
	UserID   UserID
	Role     Role
}

func (MemberAdded) EventName() string { return "tenant.member_added" }

type MemberRemoved struct {
	baseEvent
	TenantID TenantID
	UserID   UserID
}

func (MemberRemoved) EventName() string { return "tenant.member_removed" }

type APIKeyIssued struct {
	baseEvent
	TenantID TenantID
	KeyID    APIKeyID
	Scopes   []Scope
}

func (APIKeyIssued) EventName() string { return "apikey.issued" }

type APIKeyRevoked struct {
	baseEvent
	TenantID TenantID
	KeyID    APIKeyID
}

func (APIKeyRevoked) EventName() string { return "apikey.revoked" }

func now() baseEvent { return baseEvent{At: time.Now().UTC()} }

// Constructors keep the OccurredAt timestamp consistent across events.
func NewTenantCreated(id TenantID, slug string, plan Plan) TenantCreated {
	return TenantCreated{baseEvent: now(), TenantID: id, Slug: slug, Plan: plan}
}
func NewMemberAdded(t TenantID, u UserID, r Role) MemberAdded {
	return MemberAdded{baseEvent: now(), TenantID: t, UserID: u, Role: r}
}
func NewMemberRemoved(t TenantID, u UserID) MemberRemoved {
	return MemberRemoved{baseEvent: now(), TenantID: t, UserID: u}
}
func NewAPIKeyIssued(t TenantID, k APIKeyID, s []Scope) APIKeyIssued {
	return APIKeyIssued{baseEvent: now(), TenantID: t, KeyID: k, Scopes: s}
}
func NewAPIKeyRevoked(t TenantID, k APIKeyID) APIKeyRevoked {
	return APIKeyRevoked{baseEvent: now(), TenantID: t, KeyID: k}
}
