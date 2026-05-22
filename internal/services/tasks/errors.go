package tasks

import "errors"

// ErrNotFound is returned when a task is not found.
var ErrNotFound = errors.New("not found")

// ErrCircularDependency is returned when adding a dependency would create a cycle.
var ErrCircularDependency = errors.New("circular dependency detected")

// ValidationError represents a validation failure.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// ErrValidation creates a new validation error.
func ErrValidation(msg string) error { return ValidationError(msg) }
