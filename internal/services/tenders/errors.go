package tenders

import "errors"

// ErrNotFound is returned when a tender resource is not found.
var ErrNotFound = errors.New("not found")

// ErrNotYours refuses a section change by someone who is neither its assignee nor a tender
// manager, or an assignee changing more than their own work.
var ErrNotYours = errors.New("only the section's assignee or a tender manager can do this")

// ErrOwnSection refuses the assignee reviewing their own section.
var ErrOwnSection = errors.New("you cannot review a section assigned to you")

// ErrNotReviewer refuses a review by someone who is neither the section's reviewer nor a tender
// manager.
var ErrNotReviewer = errors.New("only the section's reviewer or a tender manager can review it")

// ValidationError represents a validation failure.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// ErrValidation creates a new validation error.
func ErrValidation(msg string) error { return ValidationError(msg) }

// StateError is a 409: the tender, section or document is not in a state that allows the action.
// Code names the refusal for the UI.
type StateError struct {
	Code string
	Msg  string
}

func (e *StateError) Error() string { return e.Msg }

func stateErr(code, msg string) error { return &StateError{Code: code, Msg: msg} }
