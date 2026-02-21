package docengine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/adp/adp/internal/store"
)

// newMockAnthropicServer creates a test HTTP server that returns a canned Anthropic API response.
func newMockAnthropicServer(t *testing.T, statusCode int, resp anthropicResponse) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("Failed to encode response: %v", err)
		}
	}))
}

// --- Template Engine Tests ---

func TestNewTemplateEngine(t *testing.T) {
	engine, err := NewTemplateEngine()
	if err != nil {
		t.Fatalf("NewTemplateEngine failed: %v", err)
	}
	if engine == nil {
		t.Fatal("Engine should not be nil")
	}
}

func TestAnalyzeSession(t *testing.T) {
	session := &store.Session{
		ID:         "test-session",
		Tool:       "claude_code",
		TrustLevel: 3,
		StartedAt:  time.Now().Add(-30 * time.Minute),
	}

	now := time.Now()
	session.LastHeartbeat = &now

	decisions := []*store.DecisionRecord{
		{
			ID:           "d1",
			DecisionType: "code_change",
			Action:       "edit main.go",
			Confidence:   0.9,
			Target:       map[string]any{"paths": []any{"main.go"}},
			Status:       "pending",
		},
		{
			ID:           "d2",
			DecisionType: "code_change",
			Action:       "edit util.go",
			Confidence:   0.6,
			Target:       map[string]any{"paths": []any{"util.go"}},
			Status:       "pending",
		},
		{
			ID:           "d3",
			DecisionType: "file_create",
			Action:       "create test.go",
			Confidence:   0.85,
			Target:       map[string]any{"paths": []any{"test.go"}},
			Status:       "pending",
			PolicyResult: &store.PolicyResult{Allowed: false, DeniedReasons: []string{"sensitive"}},
		},
	}

	analysis := AnalyzeSession(session, decisions)

	if analysis.DecisionCount != 3 {
		t.Errorf("DecisionCount = %d, want 3", analysis.DecisionCount)
	}
	if analysis.DecisionsByType["code_change"] != 2 {
		t.Errorf("code_change count = %d, want 2", analysis.DecisionsByType["code_change"])
	}
	if analysis.DecisionsByType["file_create"] != 1 {
		t.Errorf("file_create count = %d, want 1", analysis.DecisionsByType["file_create"])
	}
	if analysis.MinConfidence != 0.6 {
		t.Errorf("MinConfidence = %f, want 0.6", analysis.MinConfidence)
	}
	if analysis.PolicyViolations != 1 {
		t.Errorf("PolicyViolations = %d, want 1", analysis.PolicyViolations)
	}
	if len(analysis.FilesTouched) != 3 {
		t.Errorf("FilesTouched len = %d, want 3", len(analysis.FilesTouched))
	}

	// Average confidence: (0.9 + 0.6 + 0.85) / 3 = 0.7833...
	expectedAvg := (0.9 + 0.6 + 0.85) / 3
	if diff := analysis.AvgConfidence - expectedAvg; diff > 0.001 || diff < -0.001 {
		t.Errorf("AvgConfidence = %f, want ~%f", analysis.AvgConfidence, expectedAvg)
	}
}

func TestRenderSessionSummary(t *testing.T) {
	engine, _ := NewTemplateEngine()

	analysis := SessionAnalysis{
		SessionID:     "test-session",
		Tool:          "claude_code",
		TrustLevel:    3,
		Duration:      45 * time.Minute,
		StartedAt:     time.Now(),
		DecisionCount: 5,
		DecisionsByType: map[string]int{
			"code_change": 3,
			"file_create": 2,
		},
		AvgConfidence:    0.85,
		MinConfidence:    0.7,
		FilesTouched:     []string{"main.go", "util.go"},
		PolicyViolations: 0,
	}

	content, err := engine.RenderSessionSummary(analysis)
	if err != nil {
		t.Fatalf("RenderSessionSummary failed: %v", err)
	}
	if content == "" {
		t.Error("Content should not be empty")
	}
	if !containsStr(content, "test-session") {
		t.Error("Content should contain session ID")
	}
	if !containsStr(content, "claude_code") {
		t.Error("Content should contain tool name")
	}
	if !containsStr(content, "0.85") {
		t.Error("Content should contain avg confidence")
	}
}

func TestRenderRiskReport(t *testing.T) {
	engine, _ := NewTemplateEngine()

	analysis := SessionAnalysis{
		SessionID:        "risk-session",
		StartedAt:        time.Now(),
		DecisionCount:    10,
		MinConfidence:    0.3,
		PolicyViolations: 2,
		DeniedDecisions:  2,
	}

	content, err := engine.RenderRiskReport(analysis)
	if err != nil {
		t.Fatalf("RenderRiskReport failed: %v", err)
	}
	if !containsStr(content, "HIGH RISK") {
		t.Error("Content should contain HIGH RISK for min confidence < 0.5")
	}
	if !containsStr(content, "REVIEW REQUIRED") {
		t.Error("Content should contain REVIEW REQUIRED for policy violations")
	}
}

func TestRenderPatternReport(t *testing.T) {
	engine, _ := NewTemplateEngine()

	analysis := SessionAnalysis{
		SessionID:     "pattern-session",
		Tool:          "cursor",
		DecisionCount: 20,
		Duration:      90 * time.Minute,
		DecisionsByType: map[string]int{
			"code_change": 15,
			"file_create": 5,
		},
		AvgConfidence: 0.82,
		TrustLevel:    4,
		FilesTouched:  make([]string, 12),
	}

	content, err := engine.RenderPatternReport(analysis)
	if err != nil {
		t.Fatalf("RenderPatternReport failed: %v", err)
	}
	if !containsStr(content, "cursor") {
		t.Error("Content should contain tool name")
	}
	if !containsStr(content, "High File Impact") {
		t.Error("Content should contain High File Impact for > 10 files")
	}
}

// --- LLM Client Tests ---

func TestNoopLLMClient(t *testing.T) {
	client := &NoopLLMClient{}

	if client.IsConfigured() {
		t.Error("NoopLLMClient should not be configured")
	}

	result, err := client.GenerateDoc(context.Background(), "prompt", "draft", SessionAnalysis{})
	if err != nil {
		t.Fatalf("GenerateDoc should not error: %v", err)
	}
	if result != "" {
		t.Errorf("GenerateDoc should return empty string, got: %s", result)
	}
}

func TestNewLLMClient_NoKey(t *testing.T) {
	client := NewLLMClient("", "")
	if client.IsConfigured() {
		t.Error("Client with empty key should not be configured")
	}
}

func TestNewLLMClient_WithKey(t *testing.T) {
	client := NewLLMClient("sk-test-key", "")
	if !client.IsConfigured() {
		t.Error("Client with API key should be configured")
	}
	ac, ok := client.(*AnthropicClient)
	if !ok {
		t.Fatal("Client should be an AnthropicClient")
	}
	if ac.model != "claude-sonnet-4-5-20250929" {
		t.Errorf("Default model = %s, want claude-sonnet-4-5-20250929", ac.model)
	}
}

func TestNewLLMClient_CustomModel(t *testing.T) {
	client := NewLLMClient("sk-test-key", "claude-haiku-4-5-20251001")
	ac, ok := client.(*AnthropicClient)
	if !ok {
		t.Fatal("Client should be an AnthropicClient")
	}
	if ac.model != "claude-haiku-4-5-20251001" {
		t.Errorf("Model = %s, want claude-haiku-4-5-20251001", ac.model)
	}
}

func TestAnthropicClient_GenerateDoc_Success(t *testing.T) {
	server := newMockAnthropicServer(t, http.StatusOK, anthropicResponse{
		Content: []anthropicContentBlock{
			{Type: "text", Text: "# Refined Summary\n\nPolished documentation."},
		},
	})
	defer server.Close()

	client := &AnthropicClient{
		apiKey:  "sk-test",
		model:   "claude-sonnet-4-5-20250929",
		baseURL: server.URL,
		client:  server.Client(),
	}

	analysis := SessionAnalysis{
		SessionID:     "test-session",
		Tool:          "claude_code",
		TrustLevel:    3,
		DecisionCount: 5,
		AvgConfidence: 0.85,
		MinConfidence: 0.7,
	}

	result, err := client.GenerateDoc(context.Background(),
		"Refine this session summary.", "# Draft content", analysis)
	if err != nil {
		t.Fatalf("GenerateDoc failed: %v", err)
	}
	if result != "# Refined Summary\n\nPolished documentation." {
		t.Errorf("Unexpected result: %s", result)
	}
}

func TestAnthropicClient_GenerateDoc_APIError(t *testing.T) {
	server := newMockAnthropicServer(t, http.StatusBadRequest, anthropicResponse{
		Error: &anthropicError{
			Type:    "invalid_request_error",
			Message: "bad request",
		},
	})
	defer server.Close()

	client := &AnthropicClient{
		apiKey:  "sk-test",
		model:   "claude-sonnet-4-5-20250929",
		baseURL: server.URL,
		client:  server.Client(),
	}

	_, err := client.GenerateDoc(context.Background(), "prompt", "draft", SessionAnalysis{})
	if err == nil {
		t.Fatal("Expected error for bad request")
	}
}

func TestAnthropicClient_GenerateDoc_NotConfigured(t *testing.T) {
	client := &AnthropicClient{apiKey: ""}

	result, err := client.GenerateDoc(context.Background(), "prompt", "draft", SessionAnalysis{})
	if err != nil {
		t.Fatalf("Should not error: %v", err)
	}
	if result != "" {
		t.Errorf("Should return empty when not configured, got: %s", result)
	}
}

func TestAnthropicClient_RequestFormat(t *testing.T) {
	var capturedReq anthropicRequest
	var capturedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedReq)

		resp := anthropicResponse{
			Content: []anthropicContentBlock{{Type: "text", Text: "ok"}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := &AnthropicClient{
		apiKey:  "sk-test-key-123",
		model:   "claude-sonnet-4-5-20250929",
		baseURL: server.URL,
		client:  server.Client(),
	}

	client.GenerateDoc(context.Background(), "refine this", "# Draft", SessionAnalysis{
		SessionID:  "sess-1",
		Tool:       "cursor",
		TrustLevel: 4,
	})

	// Verify headers
	if capturedHeaders.Get("x-api-key") != "sk-test-key-123" {
		t.Errorf("x-api-key = %s, want sk-test-key-123", capturedHeaders.Get("x-api-key"))
	}
	if capturedHeaders.Get("anthropic-version") != "2023-06-01" {
		t.Errorf("anthropic-version = %s, want 2023-06-01", capturedHeaders.Get("anthropic-version"))
	}
	if capturedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %s, want application/json", capturedHeaders.Get("Content-Type"))
	}

	// Verify request body
	if capturedReq.Model != "claude-sonnet-4-5-20250929" {
		t.Errorf("Model = %s, want claude-sonnet-4-5-20250929", capturedReq.Model)
	}
	if capturedReq.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096", capturedReq.MaxTokens)
	}
	if capturedReq.System == "" {
		t.Error("System prompt should not be empty")
	}
	if len(capturedReq.Messages) != 1 {
		t.Fatalf("Messages count = %d, want 1", len(capturedReq.Messages))
	}
	if capturedReq.Messages[0].Role != "user" {
		t.Errorf("Message role = %s, want user", capturedReq.Messages[0].Role)
	}
}

// --- Doc Agent Tests ---

func TestDocAgent_ProcessSession(t *testing.T) {
	// Set up in-memory stores for testing
	sessionStore := &mockSessionStore{}
	decisionStore := &mockDecisionStore{
		decisions: map[string][]*store.DecisionRecord{
			"test-session": {
				{
					ID:           "d1",
					SessionID:    "test-session",
					DecisionType: "code_change",
					Action:       "edit main.go",
					Confidence:   0.9,
					Target:       map[string]any{"paths": []any{"main.go"}},
					Status:       "pending",
				},
				{
					ID:           "d2",
					SessionID:    "test-session",
					DecisionType: "file_create",
					Action:       "create util.go",
					Confidence:   0.4,
					Target:       map[string]any{"paths": []any{"util.go"}},
					Status:       "pending",
					PolicyResult: &store.PolicyResult{Allowed: false, DeniedReasons: []string{"restricted"}},
				},
			},
		},
	}
	docStore := &mockDocStore{docs: make(map[string]*store.DocRecord)}

	agent, err := NewDocAgent(decisionStore, sessionStore, docStore, DocAgentConfig{})
	if err != nil {
		t.Fatalf("NewDocAgent failed: %v", err)
	}

	session := &store.Session{
		ID:         "test-session",
		Tool:       "claude_code",
		TrustLevel: 3,
		StartedAt:  time.Now().Add(-1 * time.Hour),
	}

	if err := agent.ProcessSession(context.Background(), session); err != nil {
		t.Fatalf("ProcessSession failed: %v", err)
	}

	// Should have generated session_summary and risk_report (min confidence < 0.7)
	sessionDocs := docStore.ListBySessionSync("test-session")
	if len(sessionDocs) < 2 {
		t.Errorf("Expected at least 2 docs (summary + risk), got %d", len(sessionDocs))
	}

	hasCategory := func(category string) bool {
		for _, d := range sessionDocs {
			if d.Category == category {
				return true
			}
		}
		return false
	}

	if !hasCategory("session_summary") {
		t.Error("Missing session_summary doc")
	}
	if !hasCategory("risk_report") {
		t.Error("Missing risk_report doc (min confidence 0.4 < 0.7)")
	}
}

func TestDocAgent_ProcessSession_NoDocs(t *testing.T) {
	sessionStore := &mockSessionStore{}
	decisionStore := &mockDecisionStore{
		decisions: map[string][]*store.DecisionRecord{},
	}
	docStore := &mockDocStore{docs: make(map[string]*store.DocRecord)}

	agent, _ := NewDocAgent(decisionStore, sessionStore, docStore, DocAgentConfig{})

	session := &store.Session{
		ID:        "empty-session",
		Tool:      "test",
		StartedAt: time.Now(),
	}

	if err := agent.ProcessSession(context.Background(), session); err != nil {
		t.Fatalf("ProcessSession should not fail for empty session: %v", err)
	}

	docs := docStore.ListBySessionSync("empty-session")
	if len(docs) != 0 {
		t.Errorf("Should not generate docs for session with no decisions, got %d", len(docs))
	}
}

// --- Helper ---

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && findStr(s, substr) >= 0
}

func findStr(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// --- Mock Stores ---

type mockSessionStore struct{}

func (m *mockSessionStore) Create(_ context.Context, _ store.CreateSessionInput) (*store.Session, error) {
	return nil, nil
}
func (m *mockSessionStore) Get(_ context.Context, _ string) (*store.Session, error) {
	return nil, nil
}
func (m *mockSessionStore) Heartbeat(_ context.Context, _ string) error { return nil }
func (m *mockSessionStore) End(_ context.Context, _ string) error       { return nil }
func (m *mockSessionStore) ListEnded(_ context.Context, _ string, _ int) ([]*store.Session, error) {
	return nil, nil
}
func (m *mockSessionStore) ValidateToken(_ context.Context, _, _ string) (bool, error) {
	return true, nil
}

type mockDecisionStore struct {
	decisions map[string][]*store.DecisionRecord
}

func (m *mockDecisionStore) Create(_ context.Context, _ store.CreateDecisionInput) (*store.DecisionRecord, error) {
	return nil, nil
}
func (m *mockDecisionStore) Get(_ context.Context, _ string) (*store.DecisionRecord, error) {
	return nil, nil
}
func (m *mockDecisionStore) GetLineage(_ context.Context, _ string, _ int) ([]*store.DecisionRecord, error) {
	return nil, nil
}
func (m *mockDecisionStore) ListBySession(_ context.Context, sessionID string) ([]*store.DecisionRecord, error) {
	return m.decisions[sessionID], nil
}

type mockDocStore struct {
	docs map[string]*store.DocRecord
}

func (m *mockDocStore) Save(_ context.Context, doc store.DocRecord) error {
	m.docs[doc.ID] = &doc
	return nil
}
func (m *mockDocStore) Get(_ context.Context, id string) (*store.DocRecord, error) {
	if d, ok := m.docs[id]; ok {
		return d, nil
	}
	return nil, nil
}
func (m *mockDocStore) ListByCategory(_ context.Context, _ string, _ int) ([]*store.DocRecord, error) {
	return nil, nil
}
func (m *mockDocStore) ListBySession(_ context.Context, sessionID string) ([]*store.DocRecord, error) {
	var result []*store.DocRecord
	for _, d := range m.docs {
		if d.SessionID == sessionID {
			result = append(result, d)
		}
	}
	return result, nil
}
func (m *mockDocStore) Search(_ context.Context, _ string, _ int) ([]*store.DocRecord, error) {
	return nil, nil
}
func (m *mockDocStore) ListBySessionSync(sessionID string) []*store.DocRecord {
	var result []*store.DocRecord
	for _, d := range m.docs {
		if d.SessionID == sessionID {
			result = append(result, d)
		}
	}
	return result
}
