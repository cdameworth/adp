package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/adp/adp/internal/store"
)

func TestServerInitialization(t *testing.T) {
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)

	server := NewServerWithIO(input, output)

	if server == nil {
		t.Fatal("NewServerWithIO returned nil")
	}

	if server.tools == nil {
		t.Error("Tools map not initialized")
	}
}

func TestJSONRPCRequest_Parse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantID  interface{}
		wantErr bool
	}{
		{
			name:    "valid request",
			input:   `{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
			wantID:  float64(1), // JSON numbers are float64
			wantErr: false,
		},
		{
			name:    "string id",
			input:   `{"jsonrpc":"2.0","id":"test-id","method":"ping"}`,
			wantID:  "test-id",
			wantErr: false,
		},
		{
			name:    "invalid json",
			input:   `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req JSONRPCRequest
			err := json.Unmarshal([]byte(tt.input), &req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && req.ID != tt.wantID {
				t.Errorf("ID = %v, want %v", req.ID, tt.wantID)
			}
		})
	}
}

func TestHandleInitialize(t *testing.T) {
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)

	server := NewServerWithIO(input, output)

	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","clientInfo":{"name":"test","version":"1.0"}}`),
	}

	resp := server.handleInitialize(req)

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if resp.Error != nil {
		t.Errorf("Unexpected error: %v", resp.Error)
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if result.ProtocolVersion != ProtocolVersion {
		t.Errorf("Protocol version = %s, want %s", result.ProtocolVersion, ProtocolVersion)
	}

	if result.ServerInfo.Name != ServerName {
		t.Errorf("Server name = %s, want %s", result.ServerInfo.Name, ServerName)
	}

	if result.Capabilities.Tools == nil {
		t.Error("Tools capability not set")
	}
}

func TestHandleToolsList(t *testing.T) {
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)

	server := NewServerWithIO(input, output)

	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "tools/list",
	}

	resp := server.handleToolsList(req)

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if resp.Error != nil {
		t.Errorf("Unexpected error: %v", resp.Error)
	}

	var result ToolsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check that we have the expected tools
	expectedTools := []string{
		"adp_start_session",
		"adp_end_session",
		"adp_heartbeat",
		"adp_get_context",
		"adp_check_action",
		"adp_request_approval",
		"adp_get_approval",
		"adp_log_decision",
		"adp_prepare_commit",
		"adp_verify_commit",
	}

	toolMap := make(map[string]bool)
	for _, tool := range result.Tools {
		toolMap[tool.Name] = true
	}

	for _, expected := range expectedTools {
		if !toolMap[expected] {
			t.Errorf("Missing expected tool: %s", expected)
		}
	}
}

func TestHandlePing(t *testing.T) {
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)

	server := NewServerWithIO(input, output)

	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "ping",
	}

	resp := server.handlePing(req)

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if resp.Error != nil {
		t.Errorf("Unexpected error: %v", resp.Error)
	}

	if resp.ID != req.ID {
		t.Errorf("Response ID = %v, want %v", resp.ID, req.ID)
	}
}

func TestHandleRequest_MethodNotFound(t *testing.T) {
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)

	server := NewServerWithIO(input, output)

	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "unknown_method",
	}

	resp := server.handleRequest(context.Background(), req)

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if resp.Error == nil {
		t.Error("Expected error for unknown method")
	}

	if resp.Error.Code != ErrCodeMethodNotFound {
		t.Errorf("Error code = %d, want %d", resp.Error.Code, ErrCodeMethodNotFound)
	}
}

func TestToolHandler_StartSession(t *testing.T) {
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)

	server := NewServerWithIO(input, output)
	server.registerBuiltinTools()

	args := json.RawMessage(`{
		"organization_id": "550e8400-e29b-41d4-a716-446655440000",
		"user_id": "550e8400-e29b-41d4-a716-446655440001",
		"tool": "claude_code",
		"trust_level": 3
	}`)

	result, err := server.handleStartSession(context.Background(), args)

	if err != nil {
		t.Fatalf("handleStartSession error: %v", err)
	}

	if result.IsError {
		t.Errorf("Result indicates error: %v", result.Content)
	}

	if len(result.Content) == 0 {
		t.Error("No content in result")
	}

	// Verify the result contains session_id
	content := result.Content[0].Text
	if !strings.Contains(content, "session_id") {
		t.Error("Result should contain session_id")
	}
}

func TestToolHandler_StartSession_MissingFields(t *testing.T) {
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)

	server := NewServerWithIO(input, output)
	server.registerBuiltinTools()

	tests := []struct {
		name string
		args json.RawMessage
	}{
		{
			name: "missing organization_id",
			args: json.RawMessage(`{"user_id": "550e8400-e29b-41d4-a716-446655440001", "tool": "claude_code"}`),
		},
		{
			name: "missing user_id",
			args: json.RawMessage(`{"organization_id": "550e8400-e29b-41d4-a716-446655440000", "tool": "claude_code"}`),
		},
		{
			name: "missing tool",
			args: json.RawMessage(`{"organization_id": "550e8400-e29b-41d4-a716-446655440000", "user_id": "550e8400-e29b-41d4-a716-446655440001"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := server.handleStartSession(context.Background(), tt.args)
			if err == nil {
				t.Error("Expected error for missing field")
			}
		})
	}
}

func TestToolHandler_CheckAction(t *testing.T) {
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)

	server := NewServerWithIO(input, output)
	server.registerBuiltinTools()

	args := json.RawMessage(`{
		"session_id": "test-session-1",
		"action_type": "modify_file",
		"target": {
			"paths": ["src/main.go"]
		}
	}`)

	result, err := server.handleCheckAction(context.Background(), args)

	if err != nil {
		t.Fatalf("handleCheckAction error: %v", err)
	}

	if result.IsError {
		t.Errorf("Result indicates error: %v", result.Content)
	}

	// Check that result contains allowed field
	content := result.Content[0].Text
	if !strings.Contains(content, "allowed") {
		t.Error("Result should contain allowed field")
	}
}

func TestToolHandler_CheckAction_SensitivePath(t *testing.T) {
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)

	server := NewServerWithIO(input, output)
	server.registerBuiltinTools()

	args := json.RawMessage(`{
		"session_id": "test-session-1",
		"action_type": "modify_file",
		"target": {
			"paths": [".env"]
		}
	}`)

	result, err := server.handleCheckAction(context.Background(), args)

	if err != nil {
		t.Fatalf("handleCheckAction error: %v", err)
	}

	// Should indicate requires_approval for sensitive path
	content := result.Content[0].Text
	if !strings.Contains(content, "requires_approval") {
		t.Error("Sensitive path should require approval")
	}
}

func TestToolHandler_LogDecision(t *testing.T) {
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)

	server := NewServerWithIO(input, output)
	server.registerBuiltinTools()

	args := json.RawMessage(`{
		"session_id": "test-session-1",
		"decision_type": "code_change",
		"action": "modified src/main.go",
		"confidence": 0.9,
		"reasoning": {
			"rationale": "Fixing bug #123"
		}
	}`)

	result, err := server.handleLogDecision(context.Background(), args)

	if err != nil {
		t.Fatalf("handleLogDecision error: %v", err)
	}

	if result.IsError {
		t.Errorf("Result indicates error: %v", result.Content)
	}

	// Check that result contains decision_id
	content := result.Content[0].Text
	if !strings.Contains(content, "decision_id") {
		t.Error("Result should contain decision_id")
	}
}

func TestToolHandler_PrepareCommit(t *testing.T) {
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)

	server := NewServerWithIO(input, output)
	server.registerBuiltinTools()

	args := json.RawMessage(`{
		"session_id": "test-session-1",
		"files": ["src/main.go", "src/util.go"],
		"message": "Fix bug #123"
	}`)

	result, err := server.handlePrepareCommit(context.Background(), args)

	if err != nil {
		t.Fatalf("handlePrepareCommit error: %v", err)
	}

	if result.IsError {
		t.Errorf("Result indicates error: %v", result.Content)
	}

	// Check that result contains commit_token
	content := result.Content[0].Text
	if !strings.Contains(content, "commit_token") {
		t.Error("Result should contain commit_token")
	}
}

func TestIsSensitivePath(t *testing.T) {
	tests := []struct {
		path      string
		sensitive bool
	}{
		{".env", true},
		{".env.local", true},
		{"config/.env", true},
		{"src/main.go", false},
		{"secrets.yaml", true},
		{"secrets.json", true},
		{".aws/credentials", true},
		{".ssh/id_rsa", true},
		{"README.md", false},
		{"package.json", false},
		// Certificate and key files
		{"server.pem", true},
		{"certs/cert.pem", true},
		{"private.key", true},
		{"tls/server.key", true},
		// Non-sensitive extensions
		{"config.yaml", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := isSensitivePath(tt.path)
			if result != tt.sensitive {
				t.Errorf("isSensitivePath(%s) = %v, want %v", tt.path, result, tt.sensitive)
			}
		})
	}
}

func TestGenerateSessionID(t *testing.T) {
	id1 := generateSessionID()
	id2 := generateSessionID()

	// Check prefix
	if !strings.HasPrefix(id1, "adp_") {
		t.Errorf("Session ID should start with 'adp_': %s", id1)
	}

	// Check uniqueness
	if id1 == id2 {
		t.Error("Generated session IDs should be unique")
	}

	// Check minimum length
	if len(id1) < 10 {
		t.Errorf("Session ID too short: %s", id1)
	}
}

func TestTextResult(t *testing.T) {
	text := "Test message"
	result := textResult(text)

	if result.IsError {
		t.Error("textResult should not set IsError")
	}

	if len(result.Content) != 1 {
		t.Errorf("Expected 1 content item, got %d", len(result.Content))
	}

	if result.Content[0].Type != "text" {
		t.Errorf("Content type = %s, want text", result.Content[0].Type)
	}

	if result.Content[0].Text != text {
		t.Errorf("Content text = %s, want %s", result.Content[0].Text, text)
	}
}

func TestJSONResult(t *testing.T) {
	data := map[string]string{"key": "value"}
	result := jsonResult(data)

	if result.IsError {
		t.Error("jsonResult should not set IsError")
	}

	if len(result.Content) != 1 {
		t.Errorf("Expected 1 content item, got %d", len(result.Content))
	}

	if result.Content[0].Type != "text" {
		t.Errorf("Content type = %s, want text", result.Content[0].Type)
	}

	// Verify it's valid JSON
	var parsed map[string]string
	if err := json.Unmarshal([]byte(result.Content[0].Text), &parsed); err != nil {
		t.Errorf("jsonResult output is not valid JSON: %v", err)
	}
}

// --- Integration tests with in-memory stores ---

func TestIntegration_SessionLifecycle(t *testing.T) {
	server := newTestServerWithStores(t)
	ctx := context.Background()

	// Start session
	startResult, err := server.handleStartSession(ctx, json.RawMessage(`{
		"organization_id": "org-1",
		"user_id": "user-1",
		"tool": "claude_code",
		"trust_level": 3
	}`))
	if err != nil {
		t.Fatalf("handleStartSession failed: %v", err)
	}
	if startResult.IsError {
		t.Fatalf("startResult is error: %v", startResult.Content)
	}

	var session StartSessionResult
	json.Unmarshal([]byte(startResult.Content[0].Text), &session)
	if session.SessionID == "" {
		t.Fatal("No session ID returned")
	}

	// Heartbeat
	_, err = server.handleHeartbeat(ctx, json.RawMessage(`{"session_id":"`+session.SessionID+`"}`))
	if err != nil {
		t.Fatalf("handleHeartbeat failed: %v", err)
	}

	// Log decision
	decResult, err := server.handleLogDecision(ctx, json.RawMessage(`{
		"session_id": "`+session.SessionID+`",
		"decision_type": "code_change",
		"action": "edit main.go",
		"confidence": 0.9,
		"reasoning": {"rationale": "fix bug"}
	}`))
	if err != nil {
		t.Fatalf("handleLogDecision failed: %v", err)
	}
	var decision LogDecisionResult
	json.Unmarshal([]byte(decResult.Content[0].Text), &decision)
	if decision.DecisionID == "" {
		t.Fatal("No decision ID returned")
	}

	// End session
	_, err = server.handleEndSession(ctx, json.RawMessage(`{"session_id":"`+session.SessionID+`"}`))
	if err != nil {
		t.Fatalf("handleEndSession failed: %v", err)
	}
}

func TestIntegration_CommitLifecycle(t *testing.T) {
	server := newTestServerWithStores(t)
	ctx := context.Background()

	// Start session
	startResult, _ := server.handleStartSession(ctx, json.RawMessage(`{
		"organization_id": "org-1",
		"user_id": "user-1",
		"tool": "claude_code",
		"trust_level": 3
	}`))
	var session StartSessionResult
	json.Unmarshal([]byte(startResult.Content[0].Text), &session)

	// Prepare commit
	prepResult, err := server.handlePrepareCommit(ctx, json.RawMessage(`{
		"session_id": "`+session.SessionID+`",
		"files": ["main.go"],
		"message": "fix bug"
	}`))
	if err != nil {
		t.Fatalf("handlePrepareCommit failed: %v", err)
	}
	var commit PrepareCommitResult
	json.Unmarshal([]byte(prepResult.Content[0].Text), &commit)
	if commit.CommitToken == "" {
		t.Fatal("No commit token returned")
	}

	// Verify before marking committed - should be false
	verifyResult, _ := server.handleVerifyCommit(ctx, json.RawMessage(`{"commit_sha":"abc123"}`))
	var verify VerifyCommitResult
	json.Unmarshal([]byte(verifyResult.Content[0].Text), &verify)
	if verify.Verified {
		t.Error("Should not be verified before marking committed")
	}
}

func TestIntegration_EscalationLifecycle(t *testing.T) {
	server := newTestServerWithStores(t)
	ctx := context.Background()

	// Start session
	startResult, _ := server.handleStartSession(ctx, json.RawMessage(`{
		"organization_id": "org-1",
		"user_id": "user-1",
		"tool": "claude_code"
	}`))
	var session StartSessionResult
	json.Unmarshal([]byte(startResult.Content[0].Text), &session)

	// Request approval
	approvalResult, err := server.handleRequestApproval(ctx, json.RawMessage(`{
		"session_id": "`+session.SessionID+`",
		"action": "deploy to production",
		"reason": "requires human approval",
		"priority": "high"
	}`))
	if err != nil {
		t.Fatalf("handleRequestApproval failed: %v", err)
	}
	var approval RequestApprovalResult
	json.Unmarshal([]byte(approvalResult.Content[0].Text), &approval)
	if approval.ApprovalID == "" {
		t.Fatal("No approval ID returned")
	}
	if approval.Status != "pending" {
		t.Errorf("Status = %s, want pending", approval.Status)
	}

	// Get approval status
	getResult, err := server.handleGetApproval(ctx, json.RawMessage(`{"approval_id":"`+approval.ApprovalID+`"}`))
	if err != nil {
		t.Fatalf("handleGetApproval failed: %v", err)
	}
	var getApproval GetApprovalResult
	json.Unmarshal([]byte(getResult.Content[0].Text), &getApproval)
	if getApproval.Status != "pending" {
		t.Errorf("Get status = %s, want pending", getApproval.Status)
	}
}

func TestIntegration_GetDocs(t *testing.T) {
	server := newTestServerWithStores(t)
	ctx := context.Background()

	result, err := server.handleGetDocs(ctx, json.RawMessage(`{"category":"session_summary"}`))
	if err != nil {
		t.Fatalf("handleGetDocs failed: %v", err)
	}
	if result.IsError {
		t.Errorf("handleGetDocs returned error: %v", result.Content)
	}
}

func newTestServerWithStores(t *testing.T) *Server {
	t.Helper()
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)
	server := NewServerWithIO(input, output)

	server.SessionStore = &memSessionStore{sessions: make(map[string]*store.Session)}
	server.DecisionStore = &memDecisionStore{decisions: make(map[string]*store.DecisionRecord)}
	server.CommitStore = &memCommitStore{commits: make(map[string]*store.CommitRecord)}
	server.EscalationStore = &memEscalationStore{escalations: make(map[string]*store.EscalationRequest)}
	server.DocStore = &memDocStore{docs: make(map[string]*store.DocRecord)}

	server.registerBuiltinTools()
	return server
}

// --- In-memory store implementations for tests ---

type memSessionStore struct {
	sessions map[string]*store.Session
}

func (s *memSessionStore) Create(_ context.Context, input store.CreateSessionInput) (*store.Session, error) {
	if input.Capabilities == nil {
		input.Capabilities = []string{}
	}
	if input.Constraints == nil {
		input.Constraints = []string{}
	}
	if input.ServiceScope == nil {
		input.ServiceScope = []string{}
	}
	sess := &store.Session{
		ID: input.ID, OrganizationID: input.OrganizationID, UserID: input.UserID,
		Tool: input.Tool, TrustLevel: input.TrustLevel,
		Capabilities: input.Capabilities, Constraints: input.Constraints,
		ServiceScope: input.ServiceScope, Status: "active",
		StartedAt: time.Now(), ExpiresAt: input.ExpiresAt,
	}
	s.sessions[input.ID] = sess
	return sess, nil
}
func (s *memSessionStore) Get(_ context.Context, id string) (*store.Session, error) {
	if sess, ok := s.sessions[id]; ok {
		return sess, nil
	}
	return nil, fmt.Errorf("not found: %s", id)
}
func (s *memSessionStore) Heartbeat(_ context.Context, id string) error {
	if sess, ok := s.sessions[id]; ok {
		now := time.Now()
		sess.LastHeartbeat = &now
		return nil
	}
	return fmt.Errorf("not found: %s", id)
}
func (s *memSessionStore) End(_ context.Context, id string) error {
	if sess, ok := s.sessions[id]; ok {
		sess.Status = "ended"
		return nil
	}
	return fmt.Errorf("not found: %s", id)
}
func (s *memSessionStore) ListEnded(_ context.Context, _ string, _ int) ([]*store.Session, error) {
	return nil, nil
}

func (s *memSessionStore) ValidateToken(_ context.Context, sessionID, tokenHash string) (bool, error) {
	sess, ok := s.sessions[sessionID]
	if !ok || sess.Status != "active" {
		return false, nil
	}
	return sess.TokenHash == tokenHash, nil
}

type memDecisionStore struct {
	decisions map[string]*store.DecisionRecord
}

func (s *memDecisionStore) Create(_ context.Context, input store.CreateDecisionInput) (*store.DecisionRecord, error) {
	id := generateSessionID()
	rec := &store.DecisionRecord{
		ID: id, SessionID: input.SessionID, DecisionType: input.DecisionType,
		Action: input.Action, Confidence: input.Confidence, CreatedAt: time.Now(),
	}
	s.decisions[id] = rec
	return rec, nil
}
func (s *memDecisionStore) Get(_ context.Context, id string) (*store.DecisionRecord, error) {
	if d, ok := s.decisions[id]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("not found: %s", id)
}
func (s *memDecisionStore) GetLineage(_ context.Context, _ string, _ int) ([]*store.DecisionRecord, error) {
	return nil, nil
}
func (s *memDecisionStore) ListBySession(_ context.Context, _ string) ([]*store.DecisionRecord, error) {
	return nil, nil
}

type memCommitStore struct {
	commits map[string]*store.CommitRecord
}

func (s *memCommitStore) Prepare(_ context.Context, input store.PrepareCommitInput) (*store.CommitRecord, error) {
	token := "adp_test_" + generateSessionID()
	rec := &store.CommitRecord{
		ID: generateSessionID(), SessionID: input.SessionID, CommitToken: token,
		Files: input.Files, Message: input.Message, Status: "prepared", PreparedAt: time.Now(),
	}
	s.commits[token] = rec
	return rec, nil
}
func (s *memCommitStore) RegisterCommit(_ context.Context, token string, sha string) (*store.CommitRecord, error) {
	if c, ok := s.commits[token]; ok {
		c.CommitSHA = sha
		c.Status = "committed"
		return c, nil
	}
	return nil, fmt.Errorf("not found: %s", token)
}
func (s *memCommitStore) IsCommitVerified(_ context.Context, sha string) (bool, error) {
	for _, c := range s.commits {
		if c.CommitSHA == sha && (c.Status == "committed" || c.Status == "verified") {
			return true, nil
		}
	}
	return false, nil
}

type memEscalationStore struct {
	escalations map[string]*store.EscalationRequest
}

func (s *memEscalationStore) Create(_ context.Context, input store.CreateEscalationInput) (*store.EscalationRequest, error) {
	id := generateSessionID()
	esc := &store.EscalationRequest{
		ID: id, SessionID: input.SessionID, Action: input.Action, Reason: input.Reason,
		Status: "pending", Priority: input.Priority, RequestedAt: time.Now(), ExpiresAt: input.ExpiresAt,
	}
	s.escalations[id] = esc
	return esc, nil
}
func (s *memEscalationStore) Get(_ context.Context, id string) (*store.EscalationRequest, error) {
	if e, ok := s.escalations[id]; ok {
		return e, nil
	}
	return nil, fmt.Errorf("not found: %s", id)
}

type memDocStore struct {
	docs map[string]*store.DocRecord
}

func (s *memDocStore) Save(_ context.Context, doc store.DocRecord) error {
	s.docs[doc.ID] = &doc
	return nil
}
func (s *memDocStore) Get(_ context.Context, id string) (*store.DocRecord, error) {
	if d, ok := s.docs[id]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("not found: %s", id)
}
func (s *memDocStore) ListByCategory(_ context.Context, cat string, _ int) ([]*store.DocRecord, error) {
	var r []*store.DocRecord
	for _, d := range s.docs {
		if d.Category == cat {
			r = append(r, d)
		}
	}
	return r, nil
}
func (s *memDocStore) ListBySession(_ context.Context, sid string) ([]*store.DocRecord, error) {
	var r []*store.DocRecord
	for _, d := range s.docs {
		if d.SessionID == sid {
			r = append(r, d)
		}
	}
	return r, nil
}
func (s *memDocStore) Search(_ context.Context, _ string, _ int) ([]*store.DocRecord, error) {
	return nil, nil
}

// Benchmark tests

func BenchmarkGenerateSessionID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		generateSessionID()
	}
}

func BenchmarkIsSensitivePath(b *testing.B) {
	paths := []string{".env", "src/main.go", "secrets.yaml", "README.md"}

	for i := 0; i < b.N; i++ {
		for _, p := range paths {
			isSensitivePath(p)
		}
	}
}

func BenchmarkJSONResult(b *testing.B) {
	data := map[string]interface{}{
		"session_id":  "test-session",
		"status":      "active",
		"trust_level": 3,
	}

	for i := 0; i < b.N; i++ {
		jsonResult(data)
	}
}
