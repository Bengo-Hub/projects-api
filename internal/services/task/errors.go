package task

import "errors"

// Service errors.
var (
	ErrNotFound       = errors.New("task not found")
	ErrProjectNotFound = errors.New("project not found")
)
