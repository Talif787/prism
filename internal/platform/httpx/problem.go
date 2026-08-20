// Package httpx contains transport-layer HTTP helpers shared by all services:
// the server lifecycle, middleware, response helpers, and RFC 7807 error bodies.
package httpx

import (
	"encoding/json"
	"net/http"
)

// Problem is an RFC 7807 problem+json error body. Using a single, typed error
// shape keeps API error responses consistent across every endpoint.
type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Instance  string `json:"instance,omitempty"`
	Code      string `json:"code,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// WriteProblem serializes a Problem as application/problem+json.
func WriteProblem(w http.ResponseWriter, r *http.Request, status int, code, title, detail string) {
	p := Problem{
		Type:      "about:blank",
		Title:     title,
		Status:    status,
		Detail:    detail,
		Instance:  r.URL.Path,
		Code:      code,
		RequestID: RequestIDFromContext(r.Context()),
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}
