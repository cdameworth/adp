package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ValidationError represents a validation failure
type ValidationError struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

func (e ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s", e.Field, e.Message)
	}
	return e.Message
}

// ValidationErrors is a collection of validation errors
type ValidationErrors []ValidationError

func (ve ValidationErrors) Error() string {
	if len(ve) == 0 {
		return "validation failed"
	}
	msgs := make([]string, len(ve))
	for i, e := range ve {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "; ")
}

// Validator is an interface that request structs can implement to validate themselves
type Validator interface {
	Validate() error
}

// RequestLimits defines size limits for incoming requests
type RequestLimits struct {
	// MaxBodySize is the maximum allowed request body size in bytes
	MaxBodySize int64
	// MaxJSONDepth is the maximum nesting depth for JSON objects
	MaxJSONDepth int
	// MaxStringLength is the maximum length for any string field
	MaxStringLength int
	// MaxArrayLength is the maximum number of elements in any array
	MaxArrayLength int
}

// DefaultRequestLimits returns sensible defaults
func DefaultRequestLimits() RequestLimits {
	return RequestLimits{
		MaxBodySize:     1 << 20, // 1 MB
		MaxJSONDepth:    20,
		MaxStringLength: 65536, // 64 KB
		MaxArrayLength:  1000,
	}
}

// RequestValidator provides comprehensive request validation
type RequestValidator struct {
	Limits RequestLimits
}

// NewRequestValidator creates a new request validator with the given limits
func NewRequestValidator(limits RequestLimits) *RequestValidator {
	return &RequestValidator{Limits: limits}
}

// ValidateRequest performs comprehensive validation on the request
func (rv *RequestValidator) ValidateRequest(r *http.Request) error {
	// Validate content type for POST/PUT/PATCH
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		contentType := r.Header.Get("Content-Type")
		if contentType != "" && !strings.HasPrefix(contentType, "application/json") {
			return ValidationError{
				Code:    "invalid_content_type",
				Message: "Content-Type must be application/json",
			}
		}
	}

	// Validate request headers
	if err := rv.validateHeaders(r); err != nil {
		return err
	}

	return nil
}

// validateHeaders checks for dangerous or malformed headers
func (rv *RequestValidator) validateHeaders(r *http.Request) error {
	// Check for header injection attempts
	for name, values := range r.Header {
		for _, value := range values {
			if strings.ContainsAny(value, "\r\n") {
				return ValidationError{
					Field:   fmt.Sprintf("Header[%s]", name),
					Code:    "header_injection",
					Message: "Header contains invalid characters",
				}
			}
		}
	}
	return nil
}

// LimitedBodyReader wraps a reader with size limits
func (rv *RequestValidator) LimitedBodyReader(r *http.Request) io.Reader {
	return io.LimitReader(r.Body, rv.Limits.MaxBodySize+1)
}

// DecodeAndValidateJSON decodes JSON request body and validates it
func (rv *RequestValidator) DecodeAndValidateJSON(r *http.Request, v interface{}) error {
	if r.Body == nil {
		return ValidationError{
			Code:    "empty_body",
			Message: "Request body is required",
		}
	}

	// Read body with size limit
	limitedReader := io.LimitReader(r.Body, rv.Limits.MaxBodySize+1)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return ValidationError{
			Code:    "read_error",
			Message: "Failed to read request body",
		}
	}

	// Check size limit
	if int64(len(body)) > rv.Limits.MaxBodySize {
		return ValidationError{
			Code:    "body_too_large",
			Message: fmt.Sprintf("Request body exceeds maximum size of %d bytes", rv.Limits.MaxBodySize),
		}
	}

	// Validate UTF-8
	if !utf8.Valid(body) {
		return ValidationError{
			Code:    "invalid_encoding",
			Message: "Request body contains invalid UTF-8",
		}
	}

	// Check JSON depth before parsing
	if err := rv.validateJSONDepth(body); err != nil {
		return err
	}

	// Decode JSON
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields() // Strict parsing

	if err := decoder.Decode(v); err != nil {
		var syntaxError *json.SyntaxError
		var unmarshalTypeError *json.UnmarshalTypeError

		switch {
		case errors.As(err, &syntaxError):
			return ValidationError{
				Code:    "invalid_json",
				Message: fmt.Sprintf("Invalid JSON at position %d", syntaxError.Offset),
			}
		case errors.As(err, &unmarshalTypeError):
			return ValidationError{
				Field:   unmarshalTypeError.Field,
				Code:    "type_error",
				Message: fmt.Sprintf("Expected %s but got %s", unmarshalTypeError.Type.String(), unmarshalTypeError.Value),
			}
		case strings.Contains(err.Error(), "unknown field"):
			return ValidationError{
				Code:    "unknown_field",
				Message: err.Error(),
			}
		default:
			return ValidationError{
				Code:    "decode_error",
				Message: "Failed to decode JSON: " + err.Error(),
			}
		}
	}

	// Check for extra data after JSON
	if decoder.More() {
		return ValidationError{
			Code:    "extra_data",
			Message: "Request body contains extra data after JSON",
		}
	}

	// If the target implements Validator, validate it
	if validator, ok := v.(Validator); ok {
		if err := validator.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// validateJSONDepth checks that JSON doesn't exceed maximum nesting depth
func (rv *RequestValidator) validateJSONDepth(data []byte) error {
	depth := 0
	maxDepth := 0

	for _, b := range data {
		switch b {
		case '{', '[':
			depth++
			if depth > maxDepth {
				maxDepth = depth
			}
			if depth > rv.Limits.MaxJSONDepth {
				return ValidationError{
					Code:    "json_too_deep",
					Message: fmt.Sprintf("JSON nesting exceeds maximum depth of %d", rv.Limits.MaxJSONDepth),
				}
			}
		case '}', ']':
			depth--
		}
	}

	return nil
}

// SanitizeString removes potentially dangerous characters from a string
func SanitizeString(s string) string {
	// Remove null bytes
	s = strings.ReplaceAll(s, "\x00", "")

	// Remove control characters except \n, \r, \t
	var builder strings.Builder
	builder.Grow(len(s))
	for _, r := range s {
		if r >= 32 || r == '\n' || r == '\r' || r == '\t' {
			builder.WriteRune(r)
		}
	}

	return builder.String()
}

// ValidateMiddleware creates middleware that validates requests before processing
func (rv *RequestValidator) ValidateMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := rv.ValidateRequest(r); err != nil {
			writeValidationError(w, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// writeValidationError writes a JSON error response for validation failures
func writeValidationError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")

	var ve ValidationError
	var ves ValidationErrors

	switch {
	case errors.As(err, &ves):
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   "validation_failed",
			"message": "Request validation failed",
			"details": ves,
		})
	case errors.As(err, &ve):
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   ve.Code,
			"message": ve.Message,
			"field":   ve.Field,
		})
	default:
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   "validation_failed",
			"message": err.Error(),
		})
	}
}

// Common validation patterns
var (
	// UUIDPattern matches valid UUIDs
	UUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

	// SessionIDPattern matches valid session IDs (alphanumeric with hyphens, 8-64 chars)
	SessionIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{6,62}[a-zA-Z0-9]$`)

	// SafePathPattern matches safe file paths (no traversal)
	SafePathPattern = regexp.MustCompile(`^[a-zA-Z0-9_./\-]+$`)

	// EmailPattern matches valid email addresses
	EmailPattern = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

	// SlugPattern matches URL-safe slugs
	SlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

// ValidateUUID validates a UUID string
func ValidateUUID(s string) error {
	if !UUIDPattern.MatchString(s) {
		return ValidationError{
			Code:    "invalid_uuid",
			Message: "Invalid UUID format",
		}
	}
	return nil
}

// ValidateSessionID validates a session ID
func ValidateSessionID(s string) error {
	if !SessionIDPattern.MatchString(s) {
		return ValidationError{
			Code:    "invalid_session_id",
			Message: "Session ID must be 8-64 alphanumeric characters with optional hyphens",
		}
	}
	return nil
}

// ValidatePath validates a file path is safe (no directory traversal)
func ValidatePath(path string) error {
	// Check for path traversal
	if strings.Contains(path, "..") {
		return ValidationError{
			Code:    "path_traversal",
			Message: "Path contains directory traversal",
		}
	}

	// Check for safe characters
	if !SafePathPattern.MatchString(path) {
		return ValidationError{
			Code:    "unsafe_path",
			Message: "Path contains unsafe characters",
		}
	}

	return nil
}

// ValidateEmail validates an email address
func ValidateEmail(s string) error {
	if !EmailPattern.MatchString(s) {
		return ValidationError{
			Code:    "invalid_email",
			Message: "Invalid email format",
		}
	}
	return nil
}

// ValidateStringLength validates a string is within length bounds
func ValidateStringLength(field, value string, minLen, maxLen int) error {
	length := len(value)
	if length < minLen {
		return ValidationError{
			Field:   field,
			Code:    "too_short",
			Message: fmt.Sprintf("Must be at least %d characters", minLen),
		}
	}
	if length > maxLen {
		return ValidationError{
			Field:   field,
			Code:    "too_long",
			Message: fmt.Sprintf("Must be at most %d characters", maxLen),
		}
	}
	return nil
}

// ValidateRequired validates a required field is not empty
func ValidateRequired(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return ValidationError{
			Field:   field,
			Code:    "required",
			Message: "This field is required",
		}
	}
	return nil
}

// ValidateEnum validates a value is one of the allowed values
func ValidateEnum(field, value string, allowed []string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return ValidationError{
		Field:   field,
		Code:    "invalid_value",
		Message: fmt.Sprintf("Must be one of: %s", strings.Join(allowed, ", ")),
	}
}

// ValidateRange validates a number is within a range
func ValidateRange[T int | int64 | float64](field string, value, minVal, maxVal T) error {
	if value < minVal || value > maxVal {
		return ValidationError{
			Field:   field,
			Code:    "out_of_range",
			Message: fmt.Sprintf("Must be between %v and %v", minVal, maxVal),
		}
	}
	return nil
}

// DecodeAndValidate is a helper for handlers to decode and validate request body
func DecodeAndValidate(w http.ResponseWriter, r *http.Request, v Validator) bool {
	rv := NewRequestValidator(DefaultRequestLimits())

	if err := rv.DecodeAndValidateJSON(r, v); err != nil {
		writeValidationError(w, err)
		return false
	}

	return true
}

// RequestBodySizeLimit middleware limits request body size
func RequestBodySizeLimit(maxSize int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxSize)
			next.ServeHTTP(w, r)
		})
	}
}
