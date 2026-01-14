package validation

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// ErrInvalidJSON is returned when the request body is not valid JSON.
var ErrInvalidJSON = errors.New("invalid JSON in request body")

// ErrEmptyBody is returned when the request body is empty.
var ErrEmptyBody = errors.New("request body is empty")

// BindAndValidate decodes JSON from the request body and validates it.
// This is the primary function handlers should use for request parsing.
//
// Usage:
//
//	var req CreateProjectRequest
//	if err := validation.BindAndValidate(r, &req); err != nil {
//	    // err is either ErrInvalidJSON, ErrEmptyBody, or ValidationError
//	    return err
//	}
func BindAndValidate(r *http.Request, dst any) error {
	if err := decodeJSON(r, dst); err != nil {
		return err
	}
	return Validate(dst)
}

// Validate validates a struct using the singleton validator.
// Returns nil if valid, or ValidationError if invalid.
func Validate(v any) error {
	if err := Get().Struct(v); err != nil {
		return FormatErrors(err)
	}
	return nil
}

// decodeJSON decodes JSON from the request body into dst.
func decodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return ErrEmptyBody
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields() // Strict mode: reject unknown fields

	if err := decoder.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return ErrEmptyBody
		}
		return ErrInvalidJSON
	}

	return nil
}

// IsValidationError checks if an error is a ValidationError.
func IsValidationError(err error) bool {
	_, ok := err.(ValidationError)
	return ok
}

// IsBindError checks if an error is a binding error (JSON parsing).
func IsBindError(err error) bool {
	return errors.Is(err, ErrInvalidJSON) || errors.Is(err, ErrEmptyBody)
}
