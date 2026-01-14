package validation

import (
	"reflect"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
)

// validator is a singleton instance for performance.
// The validator is thread-safe and should be reused.
var (
	once     sync.Once
	validate *validator.Validate
)

// Get returns the singleton validator instance.
func Get() *validator.Validate {
	once.Do(func() {
		validate = validator.New()

		// Use JSON tag names in error messages instead of struct field names
		validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return fld.Name
			}
			return name
		})

		// Register custom validators
		registerCustomValidators(validate)
	})
	return validate
}

// registerCustomValidators adds project-specific validation rules.
func registerCustomValidators(v *validator.Validate) {
	// Register status validator for projects
	v.RegisterValidation("project_status", validateProjectStatus)

	// Register status validator for tasks
	v.RegisterValidation("task_status", validateTaskStatus)

	// Register priority validator
	v.RegisterValidation("priority", validatePriority)
}

// Custom validator functions

func validateProjectStatus(fl validator.FieldLevel) bool {
	status := fl.Field().String()
	validStatuses := map[string]bool{
		"active":    true,
		"planning":  true,
		"on_hold":   true,
		"completed": true,
		"cancelled": true,
	}
	return validStatuses[status]
}

func validateTaskStatus(fl validator.FieldLevel) bool {
	status := fl.Field().String()
	validStatuses := map[string]bool{
		"todo":        true,
		"in_progress": true,
		"in_review":   true,
		"done":        true,
		"blocked":     true,
	}
	return validStatuses[status]
}

func validatePriority(fl validator.FieldLevel) bool {
	priority := fl.Field().String()
	validPriorities := map[string]bool{
		"low":      true,
		"medium":   true,
		"high":     true,
		"critical": true,
	}
	return validPriorities[priority]
}
