package github

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockCommitChecker implements CommitChecker for testing
type mockCommitChecker struct {
	verified     map[string]bool
	sessions     map[string]string
	verifyError  error
	sessionError error
}

func newMockCommitChecker() *mockCommitChecker {
	return &mockCommitChecker{
		verified: make(map[string]bool),
		sessions: make(map[string]string),
	}
}

func (m *mockCommitChecker) IsCommitVerified(ctx context.Context, commitSHA string) (bool, error) {
	if m.verifyError != nil {
		return false, m.verifyError
	}
	return m.verified[commitSHA], nil
}

func (m *mockCommitChecker) GetCommitSession(ctx context.Context, commitSHA string) (string, error) {
	if m.sessionError != nil {
		return "", m.sessionError
	}
	return m.sessions[commitSHA], nil
}

func (m *mockCommitChecker) SetVerified(sha string, verified bool) {
	m.verified[sha] = verified
}

func (m *mockCommitChecker) SetSession(sha string, sessionID string) {
	m.sessions[sha] = sessionID
}

func computeSignature(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookHandler_InvalidMethod(t *testing.T) {
	app, _ := NewApp(AppConfig{
		AppID:         123456,
		PrivateKeyPEM: generateTestPrivateKey(t),
	})
	handler := NewWebhookHandler(app, newMockCommitChecker())

	req := httptest.NewRequest("GET", "/webhook", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestWebhookHandler_InvalidSignature(t *testing.T) {
	app, _ := NewApp(AppConfig{
		AppID:         123456,
		PrivateKeyPEM: generateTestPrivateKey(t),
		WebhookSecret: "test-secret",
	})
	handler := NewWebhookHandler(app, newMockCommitChecker())

	payload := []byte(`{"test": "payload"}`)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", "sha256=invalid")
	req.Header.Set("X-GitHub-Event", "ping")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestWebhookHandler_PingEvent(t *testing.T) {
	app, _ := NewApp(AppConfig{
		AppID:         123456,
		PrivateKeyPEM: generateTestPrivateKey(t),
		WebhookSecret: "test-secret",
	})
	handler := NewWebhookHandler(app, newMockCommitChecker())

	payload := []byte(`{"zen": "test"}`)
	signature := computeSignature("test-secret", payload)

	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", signature)
	req.Header.Set("X-GitHub-Event", "ping")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]string
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "pong" {
		t.Errorf("expected status 'pong', got '%s'", resp["status"])
	}
}

func TestWebhookHandler_UnknownEvent(t *testing.T) {
	app, _ := NewApp(AppConfig{
		AppID:         123456,
		PrivateKeyPEM: generateTestPrivateKey(t),
		WebhookSecret: "test-secret",
	})
	handler := NewWebhookHandler(app, newMockCommitChecker())

	payload := []byte(`{}`)
	signature := computeSignature("test-secret", payload)

	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", signature)
	req.Header.Set("X-GitHub-Event", "unknown_event")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]string
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "ignored" {
		t.Errorf("expected status 'ignored', got '%s'", resp["status"])
	}
}

func TestWebhookHandler_PushEvent_Verified(t *testing.T) {
	app, _ := NewApp(AppConfig{
		AppID:         123456,
		PrivateKeyPEM: generateTestPrivateKey(t),
		WebhookSecret: "test-secret",
	})

	checker := newMockCommitChecker()
	checker.SetVerified("abc123def456", true)
	checker.SetSession("abc123def456", "session-123")

	handler := NewWebhookHandler(app, checker)

	pushEvent := PushEvent{
		Ref:     "refs/heads/main",
		After:   "abc123def456",
		Deleted: false,
	}
	pushEvent.Repository.FullName = "owner/repo"
	pushEvent.Installation.ID = 12345
	pushEvent.Commits = append(pushEvent.Commits, struct {
		ID        string `json:"id"`
		Message   string `json:"message"`
		Timestamp string `json:"timestamp"`
		Author    struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"author"`
		Added    []string `json:"added"`
		Removed  []string `json:"removed"`
		Modified []string `json:"modified"`
	}{
		ID:      "abc123def456",
		Message: "Test commit",
	})

	payload, _ := json.Marshal(pushEvent)
	signature := computeSignature("test-secret", payload)

	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", signature)
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "processed" {
		t.Errorf("expected status 'processed', got '%v'", resp["status"])
	}
	if resp["all_verified"] != true {
		t.Errorf("expected all_verified true, got %v", resp["all_verified"])
	}
}

func TestWebhookHandler_PushEvent_Unverified(t *testing.T) {
	app, _ := NewApp(AppConfig{
		AppID:         123456,
		PrivateKeyPEM: generateTestPrivateKey(t),
		WebhookSecret: "test-secret",
	})

	checker := newMockCommitChecker()
	// Don't mark as verified

	handler := NewWebhookHandler(app, checker)

	pushEvent := PushEvent{
		Ref:     "refs/heads/main",
		After:   "unverified123",
		Deleted: false,
	}
	pushEvent.Repository.FullName = "owner/repo"
	pushEvent.Installation.ID = 12345
	pushEvent.Commits = append(pushEvent.Commits, struct {
		ID        string `json:"id"`
		Message   string `json:"message"`
		Timestamp string `json:"timestamp"`
		Author    struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"author"`
		Added    []string `json:"added"`
		Removed  []string `json:"removed"`
		Modified []string `json:"modified"`
	}{
		ID:      "unverified123",
		Message: "Unverified commit",
	})

	payload, _ := json.Marshal(pushEvent)
	signature := computeSignature("test-secret", payload)

	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", signature)
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["all_verified"] != false {
		t.Errorf("expected all_verified false, got %v", resp["all_verified"])
	}
}

func TestWebhookHandler_PushEvent_BranchDeletion(t *testing.T) {
	app, _ := NewApp(AppConfig{
		AppID:         123456,
		PrivateKeyPEM: generateTestPrivateKey(t),
		WebhookSecret: "test-secret",
	})
	handler := NewWebhookHandler(app, newMockCommitChecker())

	pushEvent := PushEvent{
		Ref:     "refs/heads/feature",
		Deleted: true,
	}
	pushEvent.Repository.FullName = "owner/repo"

	payload, _ := json.Marshal(pushEvent)
	signature := computeSignature("test-secret", payload)

	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", signature)
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "skipped" {
		t.Errorf("expected status 'skipped', got '%v'", resp["status"])
	}
}

func TestWebhookHandler_PullRequestEvent_Opened(t *testing.T) {
	app, _ := NewApp(AppConfig{
		AppID:         123456,
		PrivateKeyPEM: generateTestPrivateKey(t),
		WebhookSecret: "test-secret",
	})

	checker := newMockCommitChecker()
	checker.SetVerified("head-sha-123", true)
	checker.SetSession("head-sha-123", "session-456")

	handler := NewWebhookHandler(app, checker)

	prEvent := PullRequestEvent{
		Action: "opened",
		Number: 42,
	}
	prEvent.PullRequest.Head.SHA = "head-sha-123"
	prEvent.Repository.FullName = "owner/repo"
	prEvent.Installation.ID = 12345

	payload, _ := json.Marshal(prEvent)
	signature := computeSignature("test-secret", payload)

	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", signature)
	req.Header.Set("X-GitHub-Event", "pull_request")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestWebhookHandler_PullRequestEvent_SkippedAction(t *testing.T) {
	app, _ := NewApp(AppConfig{
		AppID:         123456,
		PrivateKeyPEM: generateTestPrivateKey(t),
		WebhookSecret: "test-secret",
	})
	handler := NewWebhookHandler(app, newMockCommitChecker())

	prEvent := PullRequestEvent{
		Action: "closed", // Not opened or synchronize
	}
	prEvent.Repository.FullName = "owner/repo"

	payload, _ := json.Marshal(prEvent)
	signature := computeSignature("test-secret", payload)

	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", signature)
	req.Header.Set("X-GitHub-Event", "pull_request")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "skipped" {
		t.Errorf("expected status 'skipped', got '%v'", resp["status"])
	}
}

func TestWebhookHandler_InstallationEvent(t *testing.T) {
	app, _ := NewApp(AppConfig{
		AppID:         123456,
		PrivateKeyPEM: generateTestPrivateKey(t),
		WebhookSecret: "test-secret",
	})
	handler := NewWebhookHandler(app, newMockCommitChecker())

	installEvent := InstallationEvent{
		Action: "created",
	}
	installEvent.Installation.ID = 12345
	installEvent.Installation.Account.Login = "test-org"
	installEvent.Repositories = []struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		FullName string `json:"full_name"`
	}{
		{ID: 1, Name: "repo1", FullName: "test-org/repo1"},
		{ID: 2, Name: "repo2", FullName: "test-org/repo2"},
	}

	payload, _ := json.Marshal(installEvent)
	signature := computeSignature("test-secret", payload)

	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", signature)
	req.Header.Set("X-GitHub-Event", "installation")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "processed" {
		t.Errorf("expected status 'processed', got '%v'", resp["status"])
	}
	if resp["action"] != "created" {
		t.Errorf("expected action 'created', got '%v'", resp["action"])
	}
	if int(resp["repositories"].(float64)) != 2 {
		t.Errorf("expected 2 repositories, got %v", resp["repositories"])
	}
}

func TestCommitVerificationResult(t *testing.T) {
	result := CommitVerificationResult{
		CommitSHA: "abc123",
		Verified:  true,
		SessionID: "session-123",
		Message:   "Test commit",
	}

	if result.CommitSHA != "abc123" {
		t.Errorf("expected commit SHA 'abc123', got '%s'", result.CommitSHA)
	}
	if !result.Verified {
		t.Error("expected verified to be true")
	}
	if result.SessionID != "session-123" {
		t.Errorf("expected session ID 'session-123', got '%s'", result.SessionID)
	}
}
