// Package domain holds the tenancy bounded context: entities, value objects,
// invariants, and domain events. It has no dependencies on infrastructure or
// transport, so business rules can be tested in isolation.
package domain

import "errors"

var (
	ErrInvalidName     = errors.New("name is invalid")
	ErrInvalidSlug     = errors.New("slug is invalid")
	ErrInvalidEmail    = errors.New("email is invalid")
	ErrInvalidRole     = errors.New("role is invalid")
	ErrInvalidScope    = errors.New("scope is invalid")
	ErrTenantSuspended = errors.New("tenant is suspended")
	ErrKeyRevoked      = errors.New("api key is revoked")
	ErrKeyExpired      = errors.New("api key is expired")
	ErrLastOwner       = errors.New("cannot remove the last owner of a tenant")
	ErrNotFound        = errors.New("resource not found")
	ErrAlreadyExists   = errors.New("resource already exists")
	ErrScopeNotGranted = errors.New("required scope not granted")
)
