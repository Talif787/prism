// Package rest is the HTTP presentation layer for the tenancy context. It maps
// requests to application use cases and domain results to JSON responses. It owns
// no business logic; validation here is limited to request shape.
package rest

import (
	"time"

	"github.com/Talif787/prism/internal/tenancy/app"
	"github.com/Talif787/prism/internal/tenancy/domain"
)

type createTenantRequest struct {
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Plan         string `json:"plan"`
	OwnerEmail   string `json:"owner_email"`
	OwnerName    string `json:"owner_name"`
	OwnerSubject string `json:"owner_subject"`
}

type tenantResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Plan      string    `json:"plan"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func toTenantResponse(t *domain.Tenant) tenantResponse {
	return tenantResponse{
		ID: t.ID.String(), Name: t.Name, Slug: t.Slug,
		Plan: string(t.Plan), Status: string(t.Status), CreatedAt: t.CreatedAt,
	}
}

type addMemberRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Subject     string `json:"subject"`
	Role        string `json:"role"`
}

type memberResponse struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

func toMemberResponses(views []app.MembershipView) []memberResponse {
	out := make([]memberResponse, len(views))
	for i, v := range views {
		out[i] = memberResponse{
			UserID: v.UserID.String(), Email: v.Email,
			DisplayName: v.DisplayName, Role: string(v.Role),
		}
	}
	return out
}

type issueKeyRequest struct {
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type keyResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// issuedKeyResponse includes the one-time plaintext, returned only at creation.
type issuedKeyResponse struct {
	keyResponse
	Key string `json:"key"`
}

func toKeyResponse(k *domain.APIKey) keyResponse {
	scopes := make([]string, len(k.Scopes))
	for i, s := range k.Scopes {
		scopes[i] = string(s)
	}
	return keyResponse{
		ID: k.ID.String(), Name: k.Name, Prefix: k.Prefix, Scopes: scopes,
		Status: string(k.Status), CreatedAt: k.CreatedAt,
		ExpiresAt: k.ExpiresAt, LastUsedAt: k.LastUsedAt,
	}
}

type verifyKeyRequest struct {
	Key   string `json:"key"`
	Scope string `json:"scope"`
}

type verifyKeyResponse struct {
	TenantID string   `json:"tenant_id"`
	KeyID    string   `json:"key_id"`
	Scopes   []string `json:"scopes"`
}
