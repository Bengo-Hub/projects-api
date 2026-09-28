package tenders

import (
	"errors"
	"fmt"
)

// ErrNotFound is returned when a tender resource is not found.
var ErrNotFound = errors.New("not found")

// ValidationError represents a validation failure.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// ErrValidation creates a new validation error.
func ErrValidation(msg string) error { return ValidationError(msg) }

// ErrTransition is returned when a tender or section cannot move to the requested status.
type ErrTransition struct{ From, To string }

func (e ErrTransition) Error() string {
	return fmt.Sprintf("cannot move from %s to %s", e.From, e.To)
}
