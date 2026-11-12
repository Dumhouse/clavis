// Package problem implements RFC 9457 problem details as used by every Clavis
// API error response.
package problem

import (
	"encoding/json"
	"net/http"
)

// ContentType is the RFC 9457 media type.
const ContentType = "application/problem+json"

// Problem is an RFC 9457 problem document with Clavis extensions.
type Problem struct {
	Type      string       `json:"type"`
	Title     string       `json:"title"`
	Status    int          `json:"status"`
	Detail    string       `json:"detail,omitempty"`
	Instance  string       `json:"instance,omitempty"`
	Code      string       `json:"code,omitempty"`
	RequestID string       `json:"request_id,omitempty"`
	Errors    []FieldError `json:"errors,omitempty"`
}

// FieldError describes a validation failure for a single field.
type FieldError struct {
	Field  string `json:"field"`
	Detail string `json:"detail"`
}

// Error implements error.
func (p *Problem) Error() string {
	if p.Detail != "" {
		return p.Title + ": " + p.Detail
	}
	return p.Title
}

// GetStatus lets Huma treat a Problem as an HTTP status error and serialize the
// full problem document instead of its generic error model.
func (p *Problem) GetStatus() int { return p.Status }

// ContentType forces RFC 9457 media types, as Huma's error writer expects.
func (p *Problem) ContentType(ct string) string {
	if ct == "application/json" {
		return ContentType
	}
	return ct
}

// New builds a problem with a type derived from its stable code.
func New(status int, code, title, detail string) *Problem {
	return &Problem{
		Type:   "urn:clavis:error:" + code,
		Title:  title,
		Status: status,
		Detail: detail,
		Code:   code,
	}
}

// Write serializes p to w.
func Write(w http.ResponseWriter, p *Problem) {
	if p.Status == 0 {
		p.Status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// Common constructors.

// Unauthorized is returned when no valid session or token was supplied.
func Unauthorized(detail string) *Problem {
	return New(http.StatusUnauthorized, "unauthorized", "Unauthorized", detail)
}

// Forbidden is returned when the actor lacks a required permission.
func Forbidden(detail string) *Problem {
	return New(http.StatusForbidden, "forbidden", "Forbidden", detail)
}

// NotFound is returned when a resource does not exist or is not visible.
func NotFound(detail string) *Problem {
	return New(http.StatusNotFound, "not_found", "Not found", detail)
}

// BadRequest is returned for malformed requests.
func BadRequest(detail string) *Problem {
	return New(http.StatusBadRequest, "bad_request", "Bad request", detail)
}

// Internal is returned for unexpected server failures. Detail must never
// contain secret material.
func Internal(detail string) *Problem {
	return New(http.StatusInternalServerError, "internal_error", "Internal server error", detail)
}
