package validation

import (
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
)

// FieldError represents a single validation error for a field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError represents a collection of validation errors.
type ValidationError struct {
	Errors []FieldError `json:"errors"`
}

// Error implements the error interface.
func (v ValidationError) Error() string {
	if len(v.Errors) == 0 {
		return "validation failed"
	}
	messages := make([]string, len(v.Errors))
	for i, e := range v.Errors {
		messages[i] = fmt.Sprintf("%s: %s", e.Field, e.Message)
	}
	return strings.Join(messages, "; ")
}

// FormatErrors converts validator.ValidationErrors into a structured ValidationError.
func FormatErrors(err error) ValidationError {
	var fieldErrors []FieldError

	validationErrors, ok := err.(validator.ValidationErrors)
	if !ok {
		// Not a validation error, return generic error
		return ValidationError{
			Errors: []FieldError{
				{Field: "request", Message: err.Error()},
			},
		}
	}

	for _, e := range validationErrors {
		fieldErrors = append(fieldErrors, FieldError{
			Field:   e.Field(),
			Message: formatMessage(e),
		})
	}

	return ValidationError{Errors: fieldErrors}
}

// formatMessage creates a human-readable message for a validation error.
func formatMessage(e validator.FieldError) string {
	switch e.Tag() {
	case "required":
		return "is required"
	case "min":
		return fmt.Sprintf("must be at least %s characters", e.Param())
	case "max":
		return fmt.Sprintf("must be at most %s characters", e.Param())
	case "email":
		return "must be a valid email address"
	case "uuid":
		return "must be a valid UUID"
	case "url":
		return "must be a valid URL"
	case "oneof":
		return fmt.Sprintf("must be one of: %s", e.Param())
	case "gt":
		return fmt.Sprintf("must be greater than %s", e.Param())
	case "gte":
		return fmt.Sprintf("must be greater than or equal to %s", e.Param())
	case "lt":
		return fmt.Sprintf("must be less than %s", e.Param())
	case "lte":
		return fmt.Sprintf("must be less than or equal to %s", e.Param())
	case "project_status":
		return "must be one of: active, planning, on_hold, completed, cancelled"
	case "task_status":
		return "must be one of: todo, in_progress, in_review, done, blocked"
	case "priority":
		return "must be one of: low, medium, high, critical"
	case "datetime":
		return "must be a valid datetime"
	case "alphanum":
		return "must contain only alphanumeric characters"
	case "numeric":
		return "must be a numeric value"
	default:
		return fmt.Sprintf("failed validation: %s", e.Tag())
	}
}
