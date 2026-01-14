package response

import (
	"encoding/json"
	"net/http"

	"github.com/bengobox/projects-service/internal/shared/validation"
)

// ErrorResponse represents an API error response.
type ErrorResponse struct {
	Error   string                  `json:"error"`
	Details []validation.FieldError `json:"details,omitempty"`
}

// JSON writes a JSON response with the given status code.
func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// Error writes an error response with the given status code.
func Error(w http.ResponseWriter, status int, message string) {
	JSON(w, status, ErrorResponse{Error: message})
}

// ValidationError writes a 400 Bad Request response with validation errors.
func ValidationError(w http.ResponseWriter, err error) {
	if valErr, ok := err.(validation.ValidationError); ok {
		JSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "validation failed",
			Details: valErr.Errors,
		})
		return
	}

	// Fallback for other errors
	Error(w, http.StatusBadRequest, err.Error())
}

// BindError writes a 400 Bad Request response for JSON parsing errors.
func BindError(w http.ResponseWriter, err error) {
	switch {
	case validation.IsBindError(err):
		if err == validation.ErrEmptyBody {
			Error(w, http.StatusBadRequest, "request body is required")
		} else {
			Error(w, http.StatusBadRequest, "invalid JSON in request body")
		}
	case validation.IsValidationError(err):
		ValidationError(w, err)
	default:
		Error(w, http.StatusBadRequest, err.Error())
	}
}

// NotFound writes a 404 Not Found response.
func NotFound(w http.ResponseWriter, resource string) {
	Error(w, http.StatusNotFound, resource+" not found")
}

// Unauthorized writes a 401 Unauthorized response.
func Unauthorized(w http.ResponseWriter) {
	Error(w, http.StatusUnauthorized, "unauthorized")
}

// Forbidden writes a 403 Forbidden response.
func Forbidden(w http.ResponseWriter) {
	Error(w, http.StatusForbidden, "forbidden")
}

// InternalError writes a 500 Internal Server Error response.
func InternalError(w http.ResponseWriter) {
	Error(w, http.StatusInternalServerError, "internal server error")
}

// Created writes a 201 Created response with the given data.
func Created(w http.ResponseWriter, data any) {
	JSON(w, http.StatusCreated, data)
}

// OK writes a 200 OK response with the given data.
func OK(w http.ResponseWriter, data any) {
	JSON(w, http.StatusOK, data)
}

// NoContent writes a 204 No Content response.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}
