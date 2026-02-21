package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adp/adp/internal/api"
	"github.com/adp/adp/internal/api/handlers"
	"github.com/adp/adp/internal/infrastructure/database"
)

// newTestServer creates an httptest.Server backed by in-memory SQLite
// with real handlers wired to real stores. No auth middleware.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	client, err := database.NewSQLiteClient(database.SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create SQLite client: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	sessionStore := database.NewSQLiteSessionStore(client)
	escalationStore := database.NewSQLiteEscalationStore(client)
	decisionStore := database.NewSQLiteDecisionStore(client)
	commitStore := database.NewSQLiteCommitStore(client)

	router := api.NewRouter(api.RouterConfig{
		SessionHandler:    handlers.NewSQLiteSessionHandler(sessionStore),
		GovernanceHandler: handlers.NewSQLiteGovernanceHandler(escalationStore),
		AuditHandler:      handlers.NewSQLiteAuditHandler(decisionStore, commitStore),
	})

	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return ts
}

// postJSON makes a POST request with JSON body and returns the response.
func postJSON(t *testing.T, url string, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s failed: %v", url, err)
	}
	return resp
}

// parseData decodes the response body and returns the "data" field as a map.
// ADP handlers wrap responses in {"data": {...}}.
func parseData(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	defer resp.Body.Close()
	var envelope map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	data, ok := envelope["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("response missing 'data' field, got: %v", envelope)
	}
	return data
}

// parseError decodes the response body and returns the "error" field.
func parseError(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	var envelope map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	errMsg, _ := envelope["error"].(string)
	return errMsg
}

func TestCommitFlow_HappyPath(t *testing.T) {
	ts := newTestServer(t)

	// Phase 1: Prepare
	resp := postJSON(t, ts.URL+"/v1/commits/prepare", `{
		"session_id": "test-session",
		"files": ["main.go", "util.go"],
		"message": "fix bug #123"
	}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Prepare: status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	data := parseData(t, resp)

	token, _ := data["commit_token"].(string)
	if token == "" || !strings.HasPrefix(token, "adp_") {
		t.Fatalf("Prepare: commit_token = %q, want adp_* prefix", token)
	}
	if approved, _ := data["approved"].(bool); !approved {
		t.Errorf("Prepare: approved = false, want true for normal files")
	}

	// Phase 2: Register
	resp = postJSON(t, ts.URL+"/v1/commits/register", `{
		"commit_token": "`+token+`",
		"commit_sha": "abc123def456"
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Register: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	data = parseData(t, resp)

	if status, _ := data["status"].(string); status != "committed" {
		t.Errorf("Register: status = %q, want committed", status)
	}
	if sha, _ := data["commit_sha"].(string); sha != "abc123def456" {
		t.Errorf("Register: commit_sha = %q, want abc123def456", sha)
	}

	// Phase 3: Verify
	resp = postJSON(t, ts.URL+"/v1/commits/verify", `{
		"commit_sha": "abc123def456"
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Verify: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	data = parseData(t, resp)

	if verified, _ := data["verified"].(bool); !verified {
		t.Errorf("Verify: verified = false, want true after register")
	}
}

func TestCommitFlow_SensitiveFilesDenied(t *testing.T) {
	ts := newTestServer(t)

	resp := postJSON(t, ts.URL+"/v1/commits/prepare", `{
		"session_id": "test-session",
		"files": [".env", "main.go"],
		"message": "add config"
	}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	data := parseData(t, resp)

	if approved, _ := data["approved"].(bool); approved {
		t.Errorf("approved = true, want false for sensitive files")
	}

	reason, _ := data["reason"].(string)
	if !strings.Contains(strings.ToLower(reason), "sensitive") {
		t.Errorf("reason = %q, want to contain 'sensitive'", reason)
	}

	// Token should still be returned (record exists in DB).
	token, _ := data["commit_token"].(string)
	if token == "" {
		t.Errorf("commit_token should be returned even when not approved")
	}
}

func TestCommitFlow_RegisterUnknownToken(t *testing.T) {
	ts := newTestServer(t)

	resp := postJSON(t, ts.URL+"/v1/commits/register", `{
		"commit_token": "adp_nonexistent_token",
		"commit_sha": "abc123"
	}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestCommitFlow_VerifyUnregisteredSHA(t *testing.T) {
	ts := newTestServer(t)

	resp := postJSON(t, ts.URL+"/v1/commits/verify", `{
		"commit_sha": "never_registered_sha"
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	data := parseData(t, resp)

	if verified, _ := data["verified"].(bool); verified {
		t.Errorf("verified = true, want false for unregistered SHA")
	}
}

func TestCommitFlow_VerifyBeforeRegister(t *testing.T) {
	ts := newTestServer(t)

	// Prepare a commit (creates a record but no SHA link).
	resp := postJSON(t, ts.URL+"/v1/commits/prepare", `{
		"session_id": "test-session",
		"files": ["main.go"],
		"message": "test"
	}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Prepare: status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	resp.Body.Close()

	// Verify an arbitrary SHA that was never registered.
	resp = postJSON(t, ts.URL+"/v1/commits/verify", `{
		"commit_sha": "some_unlinked_sha"
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Verify: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	data := parseData(t, resp)

	if verified, _ := data["verified"].(bool); verified {
		t.Errorf("verified = true, want false (SHA never registered)")
	}
}

func TestCommitFlow_PrepareValidation(t *testing.T) {
	ts := newTestServer(t)

	tests := []struct {
		name     string
		body     string
		wantCode int
		wantErr  string
	}{
		{
			name:     "missing session_id",
			body:     `{"files": ["main.go"], "message": "test"}`,
			wantCode: http.StatusBadRequest,
			wantErr:  "session_id is required",
		},
		{
			name:     "empty files",
			body:     `{"session_id": "s1", "files": [], "message": "test"}`,
			wantCode: http.StatusBadRequest,
			wantErr:  "files are required",
		},
		{
			name:     "invalid json",
			body:     `not json`,
			wantCode: http.StatusBadRequest,
			wantErr:  "Invalid request body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := postJSON(t, ts.URL+"/v1/commits/prepare", tt.body)
			if resp.StatusCode != tt.wantCode {
				resp.Body.Close()
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantCode)
			}
			errMsg := parseError(t, resp)
			if !strings.Contains(errMsg, tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", errMsg, tt.wantErr)
			}
		})
	}
}

func TestCommitFlow_RegisterValidation(t *testing.T) {
	ts := newTestServer(t)

	tests := []struct {
		name     string
		body     string
		wantCode int
		wantErr  string
	}{
		{
			name:     "missing commit_token",
			body:     `{"commit_sha": "abc123"}`,
			wantCode: http.StatusBadRequest,
			wantErr:  "commit_token is required",
		},
		{
			name:     "missing commit_sha",
			body:     `{"commit_token": "adp_abc123"}`,
			wantCode: http.StatusBadRequest,
			wantErr:  "commit_sha is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := postJSON(t, ts.URL+"/v1/commits/register", tt.body)
			if resp.StatusCode != tt.wantCode {
				resp.Body.Close()
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantCode)
			}
			errMsg := parseError(t, resp)
			if !strings.Contains(errMsg, tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", errMsg, tt.wantErr)
			}
		})
	}
}
