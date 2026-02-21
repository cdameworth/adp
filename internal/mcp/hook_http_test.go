package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/adp/adp/internal/infrastructure/database"
	"github.com/adp/adp/internal/store"
)

// newHookTestServer creates an httptest.Server backed by the hook HTTP handler
// with an in-memory SQLite commit store and NO session store (auth skipped).
func newHookTestServer(t *testing.T) (*httptest.Server, store.CommitStore) {
	t.Helper()

	client, err := database.NewSQLiteClient(database.SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create SQLite client: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	commitStore := database.NewSQLiteCommitStore(client)
	handler := NewHookHTTPHandler(commitStore, nil) // nil sessionStore = auth skipped

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, commitStore
}

// newHookTestServerWithSession creates a test server with a pre-configured active
// session. Returns the server and the plaintext session token for Authorization.
func newHookTestServerWithSession(t *testing.T, sessionID string) (*httptest.Server, store.CommitStore, string) {
	t.Helper()

	client, err := database.NewSQLiteClient(database.SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create SQLite client: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	commitStore := database.NewSQLiteCommitStore(client)
	sessionStore := database.NewSQLiteSessionStore(client)

	token := "adp_tok_test1234567890abcdef1234567890abcdef"
	tokenHash := sha256Hex(token)
	_, err = sessionStore.Create(context.Background(), store.CreateSessionInput{
		ID:             sessionID,
		OrganizationID: "org1",
		UserID:         "user1",
		Tool:           "test",
		TrustLevel:     2,
		ExpiresAt:      time.Now().Add(8 * time.Hour),
		TokenHash:      tokenHash,
	})
	if err != nil {
		t.Fatalf("failed to create test session: %v", err)
	}

	handler := NewHookHTTPHandler(commitStore, sessionStore)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, commitStore, token
}

// hookPostJSON sends a POST with JSON body and returns the response.
func hookPostJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s failed: %v", url, err)
	}
	return resp
}

// hookPostJSONWithAuth sends a POST with JSON body and Authorization header.
func hookPostJSONWithAuth(t *testing.T, url, body, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s failed: %v", url, err)
	}
	return resp
}

// hookParseResponse parses a top-level JSON response (no envelope).
func hookParseResponse(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	defer resp.Body.Close()
	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return data
}

func TestHookHTTP_Health(t *testing.T) {
	ts, _ := newHookTestServer(t)

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	data := hookParseResponse(t, resp)
	if status, _ := data["status"].(string); status != "ok" {
		t.Errorf("status = %q, want ok", status)
	}
}

func TestHookHTTP_PrepareHappyPath(t *testing.T) {
	ts, _ := newHookTestServer(t)

	resp := hookPostJSON(t, ts.URL+"/v1/commits/prepare", `{
		"session_id": "test-session",
		"files": ["main.go", "util.go"],
		"message": "test commit"
	}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	data := hookParseResponse(t, resp)

	// Response must have top-level fields (no "data" wrapper).
	if _, hasData := data["data"]; hasData {
		t.Fatal("response has 'data' wrapper, hook expects top-level fields")
	}

	token, _ := data["commit_token"].(string)
	if token == "" || !strings.HasPrefix(token, "adp_") {
		t.Errorf("commit_token = %q, want adp_* prefix", token)
	}
	if approved, _ := data["approved"].(bool); !approved {
		t.Errorf("approved = false, want true for normal files")
	}
}

func TestHookHTTP_PrepareWithAuth(t *testing.T) {
	sessionID := "auth-test-session"
	ts, _, token := newHookTestServerWithSession(t, sessionID)

	resp := hookPostJSONWithAuth(t, ts.URL+"/v1/commits/prepare", `{
		"session_id": "`+sessionID+`",
		"files": ["main.go"],
		"message": "auth test"
	}`, token)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	data := hookParseResponse(t, resp)
	if approved, _ := data["approved"].(bool); !approved {
		t.Errorf("approved = false, want true")
	}
}

func TestHookHTTP_PrepareUnauthorized(t *testing.T) {
	sessionID := "auth-test-session"
	ts, _, _ := newHookTestServerWithSession(t, sessionID)

	// No auth header
	resp := hookPostJSON(t, ts.URL+"/v1/commits/prepare", `{
		"session_id": "`+sessionID+`",
		"files": ["main.go"],
		"message": "no auth"
	}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no auth: status = %d, want 401", resp.StatusCode)
	}

	// Wrong token
	resp = hookPostJSONWithAuth(t, ts.URL+"/v1/commits/prepare", `{
		"session_id": "`+sessionID+`",
		"files": ["main.go"],
		"message": "bad auth"
	}`, "wrong-token")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want 401", resp.StatusCode)
	}
}

func TestHookHTTP_PrepareSensitiveFiles(t *testing.T) {
	ts, _ := newHookTestServer(t)

	resp := hookPostJSON(t, ts.URL+"/v1/commits/prepare", `{
		"session_id": "test-session",
		"files": [".env", "main.go"],
		"message": "add config"
	}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	data := hookParseResponse(t, resp)

	if approved, _ := data["approved"].(bool); approved {
		t.Errorf("approved = true, want false for sensitive files")
	}
	reason, _ := data["reason"].(string)
	if !strings.Contains(strings.ToLower(reason), "sensitive") {
		t.Errorf("reason = %q, want to contain 'sensitive'", reason)
	}
}

func TestHookHTTP_PrepareValidation(t *testing.T) {
	ts, _ := newHookTestServer(t)

	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{"missing session_id", `{"files":["main.go"]}`, http.StatusBadRequest},
		{"empty files", `{"session_id":"s1","files":[]}`, http.StatusBadRequest},
		{"bad json", `not json`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := hookPostJSON(t, ts.URL+"/v1/commits/prepare", tt.body)
			resp.Body.Close()
			if resp.StatusCode != tt.wantCode {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantCode)
			}
		})
	}
}

func TestHookHTTP_FullLifecycle(t *testing.T) {
	ts, _ := newHookTestServer(t)

	// Prepare
	resp := hookPostJSON(t, ts.URL+"/v1/commits/prepare", `{
		"session_id": "lifecycle-session",
		"files": ["main.go"],
		"message": "lifecycle test"
	}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("prepare: status = %d, want 201", resp.StatusCode)
	}
	data := hookParseResponse(t, resp)
	token, _ := data["commit_token"].(string)

	// Register
	resp = hookPostJSON(t, ts.URL+"/v1/commits/register", `{
		"commit_token": "`+token+`",
		"commit_sha": "abc123def"
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register: status = %d, want 200", resp.StatusCode)
	}
	data = hookParseResponse(t, resp)
	if status, _ := data["status"].(string); status != "committed" {
		t.Errorf("register status = %q, want committed", status)
	}

	// Verify
	resp = hookPostJSON(t, ts.URL+"/v1/commits/verify", `{
		"commit_sha": "abc123def"
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify: status = %d, want 200", resp.StatusCode)
	}
	data = hookParseResponse(t, resp)
	if verified, _ := data["verified"].(bool); !verified {
		t.Errorf("verified = false, want true")
	}

	// No "data" wrapper anywhere
	if _, hasData := data["data"]; hasData {
		t.Fatal("verify response has 'data' wrapper")
	}
}

func TestHookHTTP_RegisterUnknownToken(t *testing.T) {
	ts, _ := newHookTestServer(t)

	resp := hookPostJSON(t, ts.URL+"/v1/commits/register", `{
		"commit_token": "adp_nonexistent",
		"commit_sha": "abc123"
	}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHookHTTP_VerifyUnknownSHA(t *testing.T) {
	ts, _ := newHookTestServer(t)

	resp := hookPostJSON(t, ts.URL+"/v1/commits/verify", `{
		"commit_sha": "unknown_sha"
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	data := hookParseResponse(t, resp)
	if verified, _ := data["verified"].(bool); verified {
		t.Errorf("verified = true, want false for unknown SHA")
	}
}

func TestHookHTTP_RegisterValidation(t *testing.T) {
	ts, _ := newHookTestServer(t)

	tests := []struct {
		name string
		body string
	}{
		{"missing token", `{"commit_sha":"abc"}`},
		{"missing sha", `{"commit_token":"adp_abc"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := hookPostJSON(t, ts.URL+"/v1/commits/register", tt.body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

func TestHookHTTP_StartSessionIncludesToken(t *testing.T) {
	// Verify that adp_start_session returns a cryptographic session_token and http_port.
	client, err := database.NewSQLiteClient(database.SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create SQLite client: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	server := NewServerWithIO(strings.NewReader(""), &strings.Builder{})
	server.SessionStore = database.NewSQLiteSessionStore(client)
	server.HTTPPort = 8081
	server.registerBuiltinTools()

	args := []byte(`{"organization_id":"org1","user_id":"user1","tool":"test"}`)
	result, err := server.handleStartSession(context.Background(), args)
	if err != nil {
		t.Fatalf("handleStartSession error: %v", err)
	}

	data := parseMCPResult(t, result)

	sessionID, _ := data["session_id"].(string)
	if sessionID == "" {
		t.Fatal("session_id is empty")
	}

	token, _ := data["session_token"].(string)
	if token == "" {
		t.Fatal("session_token is empty")
	}
	if !strings.HasPrefix(token, "adp_tok_") {
		t.Errorf("session_token = %q, want adp_tok_* prefix", token)
	}
	// Token must NOT be derivable from session ID
	if token == "mcp_"+sessionID {
		t.Errorf("session_token is still the old derivable format")
	}
	// Token should be 72 chars: "adp_tok_" (8) + 64 hex chars (32 bytes)
	if len(token) != 72 {
		t.Errorf("session_token length = %d, want 72", len(token))
	}

	httpPort, _ := data["http_port"].(float64) // JSON numbers are float64
	if int(httpPort) != 8081 {
		t.Errorf("http_port = %v, want 8081", httpPort)
	}
}
