package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_ProjectStatus(t *testing.T) {
	type testStruct struct {
		Status string `validate:"project_status"`
	}

	tests := []struct {
		name    string
		status  string
		wantErr bool
	}{
		{name: "valid active", status: "active", wantErr: false},
		{name: "valid planning", status: "planning", wantErr: false},
		{name: "valid on_hold", status: "on_hold", wantErr: false},
		{name: "valid completed", status: "completed", wantErr: false},
		{name: "valid cancelled", status: "cancelled", wantErr: false},
		{name: "invalid status", status: "invalid", wantErr: true},
		{name: "empty status", status: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&testStruct{Status: tt.status})
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidate_TaskStatus(t *testing.T) {
	type testStruct struct {
		Status string `validate:"task_status"`
	}

	tests := []struct {
		name    string
		status  string
		wantErr bool
	}{
		{name: "valid todo", status: "todo", wantErr: false},
		{name: "valid in_progress", status: "in_progress", wantErr: false},
		{name: "valid in_review", status: "in_review", wantErr: false},
		{name: "valid done", status: "done", wantErr: false},
		{name: "valid blocked", status: "blocked", wantErr: false},
		{name: "invalid status", status: "invalid", wantErr: true},
		{name: "empty status", status: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&testStruct{Status: tt.status})
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidate_Priority(t *testing.T) {
	type testStruct struct {
		Priority string `validate:"priority"`
	}

	tests := []struct {
		name     string
		priority string
		wantErr  bool
	}{
		{name: "valid low", priority: "low", wantErr: false},
		{name: "valid medium", priority: "medium", wantErr: false},
		{name: "valid high", priority: "high", wantErr: false},
		{name: "valid critical", priority: "critical", wantErr: false},
		{name: "invalid priority", priority: "urgent", wantErr: true},
		{name: "empty priority", priority: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&testStruct{Priority: tt.priority})
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidate_Required(t *testing.T) {
	type testStruct struct {
		Name string `json:"name" validate:"required"`
	}

	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "valid name", value: "Test", wantErr: false},
		{name: "empty name", value: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&testStruct{Name: tt.value})
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidate_MinMax(t *testing.T) {
	type testStruct struct {
		Name string `json:"name" validate:"min=2,max=10"`
	}

	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "valid length", value: "Test", wantErr: false},
		{name: "min length", value: "AB", wantErr: false},
		{name: "max length", value: "1234567890", wantErr: false},
		{name: "too short", value: "A", wantErr: true},
		{name: "too long", value: "12345678901", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&testStruct{Name: tt.value})
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatErrors(t *testing.T) {
	type testStruct struct {
		Name  string `json:"name" validate:"required"`
		Email string `json:"email" validate:"required,email"`
	}

	// Test with validation errors
	err := Validate(&testStruct{Name: "", Email: "invalid"})
	require.Error(t, err)

	valErr, ok := err.(ValidationError)
	require.True(t, ok, "expected ValidationError")

	// Should have at least 2 errors (name required, email invalid)
	assert.GreaterOrEqual(t, len(valErr.Errors), 2)

	// Check that field names use JSON tags
	fieldNames := make(map[string]bool)
	for _, e := range valErr.Errors {
		fieldNames[e.Field] = true
	}
	assert.True(t, fieldNames["name"], "should have 'name' field error")
	assert.True(t, fieldNames["email"], "should have 'email' field error")
}

func TestFormatErrors_Messages(t *testing.T) {
	type testStruct struct {
		Status string `json:"status" validate:"project_status"`
	}

	err := Validate(&testStruct{Status: "invalid"})
	require.Error(t, err)

	valErr, ok := err.(ValidationError)
	require.True(t, ok)
	require.Len(t, valErr.Errors, 1)

	// Check custom message for project_status
	assert.Contains(t, valErr.Errors[0].Message, "active")
}

func TestGet_Singleton(t *testing.T) {
	// Get validator twice and verify it's the same instance
	v1 := Get()
	v2 := Get()

	assert.Same(t, v1, v2, "Get() should return the same validator instance")
}
