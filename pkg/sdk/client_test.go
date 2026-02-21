package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// envelope wraps a response in ADP's {"data": ...} format.
func envelope(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return []byte(`{"data":` + string(data) + `}`)
}

func TestCreateSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-jwt" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected content-type: %s", r.Header.Get("Content-Type"))
		}

		var req CreateSessionRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Tool != "claude-code" {
			t.Errorf("expected tool 'claude-code', got %q", req.Tool)
		}
		if req.UserID != "factory" {
			t.Errorf("expected user_id 'factory', got %q", req.UserID)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write(envelope(t, Session{
			ID:     "sess-abc",
			Tool:   "claude-code",
			UserID: "factory",
			Status: "active",
		}))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithBearerToken("test-jwt"))
	sess, err := client.CreateSession(context.Background(), CreateSessionRequest{
		Tool:   "claude-code",
		UserID: "factory",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess.ID != "sess-abc" {
		t.Errorf("expected ID 'sess-abc', got %q", sess.ID)
	}
	if sess.Status != "active" {
		t.Errorf("expected status 'active', got %q", sess.Status)
	}
}

func TestEndSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/sess-abc" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodDelete {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, WithBearerToken("test-jwt"))
	err := client.EndSession(context.Background(), "sess-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHeartbeat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/sess-abc/heartbeat" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, WithBearerToken("test-jwt"))
	err := client.Heartbeat(context.Background(), "sess-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckAction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/governance/check" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var req CheckActionRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.SessionID != "sess-abc" {
			t.Errorf("expected session_id 'sess-abc', got %q", req.SessionID)
		}
		if req.ActionType != "file_write" {
			t.Errorf("expected action_type 'file_write', got %q", req.ActionType)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(envelope(t, CheckActionResponse{
			Allowed:     true,
			PolicyNames: []string{"default-allow"},
		}))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithBearerToken("test-jwt"))
	resp, err := client.CheckAction(context.Background(), CheckActionRequest{
		SessionID:  "sess-abc",
		ActionType: "file_write",
		Target:     map[string]any{"path": "/src/main.go"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Allowed {
		t.Error("expected Allowed=true")
	}
	if len(resp.PolicyNames) != 1 || resp.PolicyNames[0] != "default-allow" {
		t.Errorf("unexpected policy names: %v", resp.PolicyNames)
	}
}

func TestCheckActionDenied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(envelope(t, CheckActionResponse{
			Allowed:       false,
			DeniedReasons: []string{"production environment restricted"},
			PolicyNames:   []string{"env-protection"},
		}))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithBearerToken("test-jwt"))
	resp, err := client.CheckAction(context.Background(), CheckActionRequest{
		SessionID:  "sess-abc",
		ActionType: "deploy",
		Context:    map[string]any{"environment": "production"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Allowed {
		t.Error("expected Allowed=false")
	}
	if len(resp.DeniedReasons) != 1 {
		t.Errorf("expected 1 denied reason, got %d", len(resp.DeniedReasons))
	}
}

func TestLogDecision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audit/decisions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var req LogDecisionRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.SessionID != "sess-abc" {
			t.Errorf("expected session_id 'sess-abc', got %q", req.SessionID)
		}
		if req.DecisionType != "implementation" {
			t.Errorf("expected decision_type 'implementation', got %q", req.DecisionType)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write(envelope(t, DecisionRecord{
			ID:           "dec-123",
			SessionID:    "sess-abc",
			DecisionType: "implementation",
			Action:       "create_file",
			Status:       "recorded",
		}))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithBearerToken("test-jwt"))
	record, err := client.LogDecision(context.Background(), LogDecisionRequest{
		SessionID:    "sess-abc",
		DecisionType: "implementation",
		Action:       "create_file",
		Confidence:   0.95,
		Alternatives: []Alternative{
			{Action: "modify_file", Reason: "less invasive", Confidence: 0.7},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if record.ID != "dec-123" {
		t.Errorf("expected ID 'dec-123', got %q", record.ID)
	}
	if record.Status != "recorded" {
		t.Errorf("expected status 'recorded', got %q", record.Status)
	}
}

func TestPrepareCommit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/commits/prepare" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var req PrepareCommitRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.SessionID != "sess-abc" {
			t.Errorf("expected session_id 'sess-abc', got %q", req.SessionID)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(envelope(t, PrepareCommitResponse{
			CommitToken: "tok-xyz",
			Approved:    true,
		}))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithBearerToken("test-jwt"))
	resp, err := client.PrepareCommit(context.Background(), PrepareCommitRequest{
		SessionID: "sess-abc",
		Files:     []string{"main.go", "go.mod"},
		Message:   "feat: add new feature",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.CommitToken != "tok-xyz" {
		t.Errorf("expected token 'tok-xyz', got %q", resp.CommitToken)
	}
	if !resp.Approved {
		t.Error("expected Approved=true")
	}
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "insufficient permissions",
			Code:  "FORBIDDEN",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, WithBearerToken("bad-token"))
	_, err := client.CreateSession(context.Background(), CreateSessionRequest{
		Tool: "claude-code",
	})
	if err == nil {
		t.Fatal("expected error for 403")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", apiErr.StatusCode)
	}
	if apiErr.Code != "FORBIDDEN" {
		t.Errorf("expected code 'FORBIDDEN', got %q", apiErr.Code)
	}
}

func TestAPIErrorPlainText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, err := client.CheckAction(context.Background(), CheckActionRequest{
		SessionID:  "sess-abc",
		ActionType: "test",
	})
	if err == nil {
		t.Fatal("expected error for 500")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", apiErr.StatusCode)
	}
}

func TestNoToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("expected no authorization header when no token set")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(envelope(t, Session{ID: "sess-anon", Status: "active"}))
	}))
	defer server.Close()

	client := NewClient(server.URL) // no token
	sess, err := client.CreateSession(context.Background(), CreateSessionRequest{
		Tool: "claude-code",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess.ID != "sess-anon" {
		t.Errorf("expected 'sess-anon', got %q", sess.ID)
	}
}

func TestWithHTTPClient(t *testing.T) {
	customClient := &http.Client{}
	client := NewClient("http://example.com", WithHTTPClient(customClient))
	if client.httpClient != customClient {
		t.Error("expected custom HTTP client to be set")
	}
}

func TestEnvelopeUnwrap(t *testing.T) {
	// Verify the client correctly unwraps {"data": ...} envelope
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Respond with nested envelope
		w.Write([]byte(`{"data":{"id":"sess-nested","status":"active","tool":"test"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	sess, err := client.CreateSession(context.Background(), CreateSessionRequest{Tool: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess.ID != "sess-nested" {
		t.Errorf("expected 'sess-nested', got %q", sess.ID)
	}
}
