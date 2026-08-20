package httpx

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes a success response with a consistent content type.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

// DecodeJSON strictly decodes a request body, rejecting unknown fields and
// oversized payloads to fail fast on malformed input.
func DecodeJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		WriteProblem(w, r, http.StatusBadRequest, "invalid_body", "Invalid request body", err.Error())
		return false
	}
	return true
}
