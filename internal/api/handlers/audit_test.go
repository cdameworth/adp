package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterCommit_MissingFields(t *testing.T) {
	h := &AuditHandler{} // nil stores

	tests := []struct {
		name     string
		body     string
		wantCode int
		wantBody string
	}{
		{
			name:     "empty body",
			body:     `{}`,
			wantCode: http.StatusBadRequest,
			wantBody: "commit_token is required",
		},
		{
			name:     "missing commit_sha",
			body:     `{"commit_token": "adp_abc123"}`,
			wantCode: http.StatusBadRequest,
			wantBody: "commit_sha is required",
		},
		{
			name:     "missing commit_token",
			body:     `{"commit_sha": "abc123"}`,
			wantCode: http.StatusBadRequest,
			wantBody: "commit_token is required",
		},
		{
			name:     "invalid json",
			body:     `not json`,
			wantCode: http.StatusBadRequest,
			wantBody: "Invalid request body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/commits/register", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.RegisterCommit(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", w.Code, tt.wantCode)
			}
			if !strings.Contains(w.Body.String(), tt.wantBody) {
				t.Errorf("body = %s, want to contain %s", w.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestRegisterCommit_NilStore(t *testing.T) {
	h := &AuditHandler{} // nil commitStore

	body := `{"commit_token": "adp_abc123", "commit_sha": "sha456"}`
	req := httptest.NewRequest("POST", "/v1/commits/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// With nil store, calling RegisterCommit will panic on nil pointer dereference.
	// This validates the handler requires an initialized store.
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic with nil commitStore, but no panic occurred")
		}
	}()

	h.RegisterCommit(w, req)
}

func TestIsSensitivePath(t *testing.T) {
	tests := []struct {
		path      string
		sensitive bool
	}{
		{".env", true},
		{"config/.env", true},
		{".env/config", true},
		{"src/main.go", false},
		{"src/credentials/file.go", true},
		{".ssh/id_rsa", true},
		{"README.md", false},
		{"secrets.yaml", true},
		{"secrets.json", true},
		{"config/.secrets", true},
		// Certificate and key files
		{"server.pem", true},
		{"certs/cert.pem", true},
		{"private.key", true},
		{"tls/server.key", true},
		// Non-sensitive extensions
		{"config.yaml", false},
		{"package.json", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := isSensitivePath(tt.path)
			if got != tt.sensitive {
				t.Errorf("isSensitivePath(%q) = %v, want %v", tt.path, got, tt.sensitive)
			}
		})
	}
}
