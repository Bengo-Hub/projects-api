package projects

import "errors"

// ErrNotFound is returned when a requested resource does not exist.
var ErrNotFound = errors.New("not found")

// ValidationError represents a validation failure.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// ErrValidation creates a new validation error with the given message.
func ErrValidation(msg string) error { return ValidationError(msg) }
