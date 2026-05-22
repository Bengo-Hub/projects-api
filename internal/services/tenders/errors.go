package tenders

import "errors"

// ErrNotFound is returned when a tender resource is not found.
var ErrNotFound = errors.New("not found")

// ValidationError represents a validation failure.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// ErrValidation creates a new validation error.
func ErrValidation(msg string) error { return ValidationError(msg) }
