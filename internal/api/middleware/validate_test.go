package middleware

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidationError(t *testing.T) {
	t.Run("formats error with field", func(t *testing.T) {
		err := ValidationError{
			Field:   "email",
			Code:    "invalid_format",
			Message: "must be a valid email",
		}
		expected := "email: must be a valid email"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("formats error without field", func(t *testing.T) {
		err := ValidationError{
			Code:    "invalid_json",
			Message: "malformed JSON",
		}
		if err.Error() != "malformed JSON" {
			t.Errorf("expected 'malformed JSON', got %q", err.Error())
		}
	})
}

func TestValidationErrors(t *testing.T) {
	t.Run("formats multiple errors", func(t *testing.T) {
		errs := ValidationErrors{
			{Field: "name", Code: "required", Message: "is required"},
			{Field: "email", Code: "invalid", Message: "invalid format"},
		}
		result := errs.Error()
		if !strings.Contains(result, "name: is required") {
			t.Errorf("expected to contain 'name: is required', got %q", result)
		}
		if !strings.Contains(result, "email: invalid format") {
			t.Errorf("expected to contain 'email: invalid format', got %q", result)
		}
	})

	t.Run("handles empty errors", func(t *testing.T) {
		errs := ValidationErrors{}
		if errs.Error() != "validation failed" {
			t.Errorf("expected 'validation failed', got %q", errs.Error())
		}
	})
}

type testRequest struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Age     int    `json:"age"`
	IsAdmin bool   `json:"is_admin"`
}

func (r *testRequest) Validate() error {
	var errs ValidationErrors

	if r.Name == "" {
		errs = append(errs, ValidationError{Field: "name", Code: "required", Message: "is required"})
	}
	if r.Email != "" && !EmailPattern.MatchString(r.Email) {
		errs = append(errs, ValidationError{Field: "email", Code: "invalid", Message: "invalid email format"})
	}
	if r.Age < 0 || r.Age > 150 {
		errs = append(errs, ValidationError{Field: "age", Code: "out_of_range", Message: "must be between 0 and 150"})
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

func TestRequestValidator_DecodeAndValidateJSON(t *testing.T) {
	rv := NewRequestValidator(DefaultRequestLimits())

	t.Run("decodes valid JSON", func(t *testing.T) {
		body := `{"name":"John Doe","email":"john@example.com","age":30}`
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))

		var target testRequest
		err := rv.DecodeAndValidateJSON(req, &target)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if target.Name != "John Doe" {
			t.Errorf("expected name 'John Doe', got %q", target.Name)
		}
		if target.Email != "john@example.com" {
			t.Errorf("expected email 'john@example.com', got %q", target.Email)
		}
		if target.Age != 30 {
			t.Errorf("expected age 30, got %d", target.Age)
		}
	})

	t.Run("rejects empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		req.Body = nil

		var target testRequest
		err := rv.DecodeAndValidateJSON(req, &target)

		if err == nil {
			t.Fatal("expected error for empty body")
		}
		var ve ValidationError
		if !errors.As(err, &ve) || ve.Code != "empty_body" {
			t.Errorf("expected empty_body error, got %v", err)
		}
	})

	t.Run("rejects body exceeding size limit", func(t *testing.T) {
		smallLimits := RequestLimits{
			MaxBodySize:  10,
			MaxJSONDepth: 20,
		}
		smallRV := NewRequestValidator(smallLimits)

		body := `{"name":"This is a very long name that exceeds the limit"}`
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))

		var target testRequest
		err := smallRV.DecodeAndValidateJSON(req, &target)

		if err == nil {
			t.Fatal("expected error for large body")
		}
		var ve ValidationError
		if !errors.As(err, &ve) || ve.Code != "body_too_large" {
			t.Errorf("expected body_too_large error, got %v", err)
		}
	})

	t.Run("rejects invalid UTF-8", func(t *testing.T) {
		// Invalid UTF-8 sequence
		body := []byte{0x80, 0x81, 0x82}
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))

		var target testRequest
		err := rv.DecodeAndValidateJSON(req, &target)

		if err == nil {
			t.Fatal("expected error for invalid UTF-8")
		}
		var ve ValidationError
		if !errors.As(err, &ve) || ve.Code != "invalid_encoding" {
			t.Errorf("expected invalid_encoding error, got %v", err)
		}
	})

	t.Run("rejects deeply nested JSON", func(t *testing.T) {
		shallowLimits := RequestLimits{
			MaxBodySize:  1024 * 1024,
			MaxJSONDepth: 3,
		}
		shallowRV := NewRequestValidator(shallowLimits)

		// Create JSON with 5 levels of nesting
		body := `{"a":{"b":{"c":{"d":{"e":"value"}}}}}`
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))

		var target map[string]interface{}
		err := shallowRV.DecodeAndValidateJSON(req, &target)

		if err == nil {
			t.Fatal("expected error for deep JSON")
		}
		var ve ValidationError
		if !errors.As(err, &ve) || ve.Code != "json_too_deep" {
			t.Errorf("expected json_too_deep error, got %v", err)
		}
	})

	t.Run("rejects malformed JSON", func(t *testing.T) {
		body := `{"name": "John", broken}`
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))

		var target testRequest
		err := rv.DecodeAndValidateJSON(req, &target)

		if err == nil {
			t.Fatal("expected error for malformed JSON")
		}
		var ve ValidationError
		if !errors.As(err, &ve) || ve.Code != "invalid_json" {
			t.Errorf("expected invalid_json error, got %v", err)
		}
	})

	t.Run("rejects unknown fields", func(t *testing.T) {
		body := `{"name":"John","unknown_field":"value"}`
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))

		var target testRequest
		err := rv.DecodeAndValidateJSON(req, &target)

		if err == nil {
			t.Fatal("expected error for unknown field")
		}
		var ve ValidationError
		if !errors.As(err, &ve) || ve.Code != "unknown_field" {
			t.Errorf("expected unknown_field error, got %v", err)
		}
	})

	t.Run("rejects type mismatches", func(t *testing.T) {
		body := `{"name":"John","age":"not-a-number"}`
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))

		var target testRequest
		err := rv.DecodeAndValidateJSON(req, &target)

		if err == nil {
			t.Fatal("expected error for type mismatch")
		}
		var ve ValidationError
		if !errors.As(err, &ve) || ve.Code != "type_error" {
			t.Errorf("expected type_error error, got %v", err)
		}
	})

	t.Run("runs custom validation", func(t *testing.T) {
		body := `{"name":"","email":"invalid-email","age":-5}`
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))

		var target testRequest
		err := rv.DecodeAndValidateJSON(req, &target)

		if err == nil {
			t.Fatal("expected validation errors")
		}
		var ves ValidationErrors
		if !errors.As(err, &ves) {
			t.Fatalf("expected ValidationErrors, got %T", err)
		}
		if len(ves) != 3 {
			t.Errorf("expected 3 validation errors, got %d", len(ves))
		}
	})
}

func TestValidateRequest(t *testing.T) {
	rv := NewRequestValidator(DefaultRequestLimits())

	t.Run("validates content type for POST", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "text/plain")

		err := rv.ValidateRequest(req)

		if err == nil {
			t.Fatal("expected error for invalid content type")
		}
		var ve ValidationError
		if !errors.As(err, &ve) || ve.Code != "invalid_content_type" {
			t.Errorf("expected invalid_content_type error, got %v", err)
		}
	})

	t.Run("accepts JSON content type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")

		err := rv.ValidateRequest(req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("accepts JSON with charset", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json; charset=utf-8")

		err := rv.ValidateRequest(req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("allows GET without content type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		err := rv.ValidateRequest(req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("rejects header injection", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-Custom", "value\r\nInjected: header")

		err := rv.ValidateRequest(req)

		if err == nil {
			t.Fatal("expected error for header injection")
		}
		var ve ValidationError
		if !errors.As(err, &ve) || ve.Code != "header_injection" {
			t.Errorf("expected header_injection error, got %v", err)
		}
	})
}

func TestValidateMiddleware(t *testing.T) {
	rv := NewRequestValidator(DefaultRequestLimits())

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("passes valid requests", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()

		rv.ValidateMiddleware(handler).ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})

	t.Run("blocks invalid requests", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "text/plain")
		rr := httptest.NewRecorder()

		rv.ValidateMiddleware(handler).ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})
}

func TestSanitizeString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "removes null bytes",
			input:    "hello\x00world",
			expected: "helloworld",
		},
		{
			name:     "removes control characters",
			input:    "hello\x01\x02world",
			expected: "helloworld",
		},
		{
			name:     "preserves newlines",
			input:    "hello\nworld",
			expected: "hello\nworld",
		},
		{
			name:     "preserves tabs",
			input:    "hello\tworld",
			expected: "hello\tworld",
		},
		{
			name:     "preserves carriage returns",
			input:    "hello\rworld",
			expected: "hello\rworld",
		},
		{
			name:     "preserves normal text",
			input:    "Hello, World! 123",
			expected: "Hello, World! 123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeString(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestValidateUUID(t *testing.T) {
	tests := []struct {
		input   string
		isValid bool
	}{
		{"550e8400-e29b-41d4-a716-446655440000", true},
		{"550E8400-E29B-41D4-A716-446655440000", true},
		{"not-a-uuid", false},
		{"550e8400-e29b-41d4-a716", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := ValidateUUID(tt.input)
			if tt.isValid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tt.isValid && err == nil {
				t.Error("expected error for invalid UUID")
			}
		})
	}
}

func TestValidateSessionID(t *testing.T) {
	tests := []struct {
		input   string
		isValid bool
	}{
		{"session-12345678", true},
		{"abc12345", true},
		{"a-b-c-d-e-f-g-h", true},
		{"ab", false}, // too short
		{"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnop", false}, // too long
		{"-invalid", false}, // starts with hyphen
		{"invalid-", false}, // ends with hyphen
		{"inva!id", false},  // invalid character
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := ValidateSessionID(tt.input)
			if tt.isValid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tt.isValid && err == nil {
				t.Error("expected error for invalid session ID")
			}
		})
	}
}

func TestValidatePath(t *testing.T) {
	tests := []struct {
		input   string
		isValid bool
	}{
		{"src/main.go", true},
		{"path/to/file.txt", true},
		{"file-name_123.json", true},
		{"../etc/passwd", false},
		{"path/../secret", false},
		{"path/with spaces", false},
		{"path/with$special", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := ValidatePath(tt.input)
			if tt.isValid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tt.isValid && err == nil {
				t.Error("expected error for invalid path")
			}
		})
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		input   string
		isValid bool
	}{
		{"user@example.com", true},
		{"user.name+tag@example.co.uk", true},
		{"user_name@example.org", true},
		{"invalid", false},
		{"@example.com", false},
		{"user@", false},
		{"user@.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := ValidateEmail(tt.input)
			if tt.isValid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tt.isValid && err == nil {
				t.Error("expected error for invalid email")
			}
		})
	}
}

func TestValidateStringLength(t *testing.T) {
	t.Run("accepts string within bounds", func(t *testing.T) {
		err := ValidateStringLength("name", "John", 1, 100)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("rejects too short string", func(t *testing.T) {
		err := ValidateStringLength("name", "ab", 5, 100)
		if err == nil {
			t.Error("expected error for short string")
		}
		var ve ValidationError
		if !errors.As(err, &ve) || ve.Code != "too_short" {
			t.Errorf("expected too_short error, got %v", err)
		}
	})

	t.Run("rejects too long string", func(t *testing.T) {
		err := ValidateStringLength("name", "abcdefghij", 1, 5)
		if err == nil {
			t.Error("expected error for long string")
		}
		var ve ValidationError
		if !errors.As(err, &ve) || ve.Code != "too_long" {
			t.Errorf("expected too_long error, got %v", err)
		}
	})
}

func TestValidateRequired(t *testing.T) {
	t.Run("accepts non-empty string", func(t *testing.T) {
		err := ValidateRequired("name", "John")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("rejects empty string", func(t *testing.T) {
		err := ValidateRequired("name", "")
		if err == nil {
			t.Error("expected error for empty string")
		}
	})

	t.Run("rejects whitespace-only string", func(t *testing.T) {
		err := ValidateRequired("name", "   ")
		if err == nil {
			t.Error("expected error for whitespace string")
		}
	})
}

func TestValidateEnum(t *testing.T) {
	allowed := []string{"low", "medium", "high"}

	t.Run("accepts valid value", func(t *testing.T) {
		err := ValidateEnum("priority", "medium", allowed)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("rejects invalid value", func(t *testing.T) {
		err := ValidateEnum("priority", "critical", allowed)
		if err == nil {
			t.Error("expected error for invalid enum value")
		}
		var ve ValidationError
		if !errors.As(err, &ve) || ve.Code != "invalid_value" {
			t.Errorf("expected invalid_value error, got %v", err)
		}
	})
}

func TestValidateRange(t *testing.T) {
	t.Run("accepts value in range", func(t *testing.T) {
		err := ValidateRange("age", 25, 0, 150)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("rejects value below range", func(t *testing.T) {
		err := ValidateRange("age", -5, 0, 150)
		if err == nil {
			t.Error("expected error for value below range")
		}
	})

	t.Run("rejects value above range", func(t *testing.T) {
		err := ValidateRange("age", 200, 0, 150)
		if err == nil {
			t.Error("expected error for value above range")
		}
	})

	t.Run("works with float64", func(t *testing.T) {
		err := ValidateRange("score", 0.5, 0.0, 1.0)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestRequestBodySizeLimit(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "read %d bytes", n)
	})

	middleware := RequestBodySizeLimit(10)

	t.Run("allows small bodies", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("small"))
		rr := httptest.NewRecorder()

		middleware(handler).ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})
}
