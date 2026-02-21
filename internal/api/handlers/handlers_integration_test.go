package handlers

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

// ─── JSON parsing helpers ────────────────────────────────────────────────────

type envelope struct {
	Data json.RawMessage `json:"data"`
}

type listEnvelope struct {
	Items  json.RawMessage `json:"items"`
	Total  int             `json:"total"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

type errorEnvelope struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// ─── Test environment ────────────────────────────────────────────────────────

type testEnv struct {
	sessionStore    *database.SQLiteSessionStore
	decisionStore   *database.SQLiteDecisionStore
	commitStore     *database.SQLiteCommitStore
	escalationStore *database.SQLiteEscalationStore

	sessionHandler    *SQLiteSessionHandler
	auditHandler      *SQLiteAuditHandler
	governanceHandler *SQLiteGovernanceHandler
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	client, err := database.NewSQLiteClient(database.SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create in-memory SQLite client: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	sessionStore := database.NewSQLiteSessionStore(client)
	decisionStore := database.NewSQLiteDecisionStore(client)
	commitStore := database.NewSQLiteCommitStore(client)
	escalationStore := database.NewSQLiteEscalationStore(client)

	return &testEnv{
		sessionStore:    sessionStore,
		decisionStore:   decisionStore,
		commitStore:     commitStore,
		escalationStore: escalationStore,

		sessionHandler:    NewSQLiteSessionHandler(sessionStore),
		auditHandler:      NewSQLiteAuditHandler(decisionStore, commitStore),
		governanceHandler: NewSQLiteGovernanceHandler(escalationStore),
	}
}

// createTestSession inserts a session directly via the store so that foreign
// key constraints are satisfied for decisions, commits, and escalations.
func (e *testEnv) createTestSession(t *testing.T, id string) {
	t.Helper()
	_, err := e.sessionStore.Create(context.Background(), store.CreateSessionInput{
		ID:         id,
		Tool:       "test",
		TrustLevel: 3,
		ExpiresAt:  time.Now().Add(1 * time.Hour),
	})
	if err != nil {
		t.Fatalf("failed to create test session %q: %v", id, err)
	}
}

// ─── Session Handler Tests ──────────────────────────────────────────────────

func TestSQLiteSessionHandler_CreateSession_Success(t *testing.T) {
	env := newTestEnv(t)

	body := `{"tool":"cursor","trust_level":3}`
	req := httptest.NewRequest("POST", "/v1/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	env.sessionHandler.CreateSession(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp envelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	var session store.Session
	if err := json.Unmarshal(resp.Data, &session); err != nil {
		t.Fatalf("failed to parse session data: %v", err)
	}

	if session.ID == "" {
		t.Error("expected non-empty session ID")
	}
	if session.Tool != "cursor" {
		t.Errorf("tool = %q, want %q", session.Tool, "cursor")
	}
	if session.Status != "active" {
		t.Errorf("status = %q, want %q", session.Status, "active")
	}
	if session.TrustLevel != 3 {
		t.Errorf("trust_level = %d, want %d", session.TrustLevel, 3)
	}
}

func TestSQLiteSessionHandler_CreateSession_MissingTool(t *testing.T) {
	env := newTestEnv(t)

	body := `{}`
	req := httptest.NewRequest("POST", "/v1/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	env.sessionHandler.CreateSession(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var errResp errorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}

	if !strings.Contains(errResp.Error, "tool is required") {
		t.Errorf("error = %q, want to contain %q", errResp.Error, "tool is required")
	}
}

func TestSQLiteSessionHandler_CreateSession_InvalidTrustLevel(t *testing.T) {
	env := newTestEnv(t)

	body := `{"tool":"test","trust_level":6}`
	req := httptest.NewRequest("POST", "/v1/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	env.sessionHandler.CreateSession(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var errResp errorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}

	if !strings.Contains(errResp.Error, "trust_level must be between 1 and 5") {
		t.Errorf("error = %q, want to contain %q", errResp.Error, "trust_level must be between 1 and 5")
	}
}

func TestSQLiteSessionHandler_CreateSession_DefaultTrustLevel(t *testing.T) {
	env := newTestEnv(t)

	body := `{"tool":"test"}`
	req := httptest.NewRequest("POST", "/v1/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	env.sessionHandler.CreateSession(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp envelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	var session store.Session
	if err := json.Unmarshal(resp.Data, &session); err != nil {
		t.Fatalf("failed to parse session data: %v", err)
	}

	if session.TrustLevel != 2 {
		t.Errorf("trust_level = %d, want default %d", session.TrustLevel, 2)
	}
}

func TestSQLiteSessionHandler_GetSession_Success(t *testing.T) {
	env := newTestEnv(t)

	// Create a session via the handler first.
	createBody := `{"tool":"test","trust_level":3}`
	createReq := httptest.NewRequest("POST", "/v1/sessions", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	env.sessionHandler.CreateSession(createW, createReq)

	if createW.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", createW.Code, http.StatusCreated)
	}

	var createResp envelope
	json.Unmarshal(createW.Body.Bytes(), &createResp)
	var created store.Session
	json.Unmarshal(createResp.Data, &created)

	// GET the session.
	getReq := httptest.NewRequest("GET", "/v1/sessions/"+created.ID, nil)
	getReq.SetPathValue("id", created.ID)
	getW := httptest.NewRecorder()
	env.sessionHandler.GetSession(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d; body = %s", getW.Code, http.StatusOK, getW.Body.String())
	}

	var getResp envelope
	json.Unmarshal(getW.Body.Bytes(), &getResp)
	var fetched store.Session
	json.Unmarshal(getResp.Data, &fetched)

	if fetched.ID != created.ID {
		t.Errorf("id = %q, want %q", fetched.ID, created.ID)
	}
	if fetched.Tool != "test" {
		t.Errorf("tool = %q, want %q", fetched.Tool, "test")
	}
}

func TestSQLiteSessionHandler_GetSession_NotFound(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest("GET", "/v1/sessions/nonexistent", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()

	env.sessionHandler.GetSession(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}

	var errResp errorEnvelope
	json.Unmarshal(w.Body.Bytes(), &errResp)
	if errResp.Code != "not_found" {
		t.Errorf("code = %q, want %q", errResp.Code, "not_found")
	}
}

func TestSQLiteSessionHandler_Heartbeat_Success(t *testing.T) {
	env := newTestEnv(t)

	// Create session via the store directly so we know the ID.
	sessionID := "hb-test-session"
	env.createTestSession(t, sessionID)

	req := httptest.NewRequest("PATCH", "/v1/sessions/"+sessionID+"/heartbeat", nil)
	req.SetPathValue("id", sessionID)
	w := httptest.NewRecorder()

	env.sessionHandler.Heartbeat(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp envelope
	json.Unmarshal(w.Body.Bytes(), &resp)

	var data map[string]interface{}
	json.Unmarshal(resp.Data, &data)

	if data["session_id"] != sessionID {
		t.Errorf("session_id = %v, want %q", data["session_id"], sessionID)
	}
	if data["last_heartbeat"] == nil || data["last_heartbeat"] == "" {
		t.Error("expected non-empty last_heartbeat")
	}
}

func TestSQLiteSessionHandler_EndSession_Success(t *testing.T) {
	env := newTestEnv(t)

	sessionID := "end-test-session"
	env.createTestSession(t, sessionID)

	req := httptest.NewRequest("DELETE", "/v1/sessions/"+sessionID, nil)
	req.SetPathValue("id", sessionID)
	w := httptest.NewRecorder()

	env.sessionHandler.EndSession(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp envelope
	json.Unmarshal(w.Body.Bytes(), &resp)

	var data map[string]string
	json.Unmarshal(resp.Data, &data)

	if data["message"] != "Session ended successfully" {
		t.Errorf("message = %q, want %q", data["message"], "Session ended successfully")
	}
}

func TestSQLiteSessionHandler_ListSessions(t *testing.T) {
	env := newTestEnv(t)

	// Create 3 sessions.
	for i := 0; i < 3; i++ {
		body := `{"tool":"test","trust_level":2}`
		req := httptest.NewRequest("POST", "/v1/sessions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		env.sessionHandler.CreateSession(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create session %d: status = %d, want %d", i, w.Code, http.StatusCreated)
		}
	}

	// List sessions.
	req := httptest.NewRequest("GET", "/v1/sessions", nil)
	w := httptest.NewRecorder()
	env.sessionHandler.ListSessions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var listResp listEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}

	var items []store.Session
	if err := json.Unmarshal(listResp.Items, &items); err != nil {
		t.Fatalf("failed to parse items: %v", err)
	}

	if len(items) != 3 {
		t.Errorf("items count = %d, want %d", len(items), 3)
	}
	if listResp.Total != 3 {
		t.Errorf("total = %d, want %d", listResp.Total, 3)
	}
}

// ─── Governance Handler Tests ───────────────────────────────────────────────

func TestSQLiteGovernanceHandler_CheckAction_DefaultAllow(t *testing.T) {
	env := newTestEnv(t)

	body := `{"session_id":"test-session","action_type":"file_write"}`
	req := httptest.NewRequest("POST", "/v1/governance/check", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	env.governanceHandler.CheckAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp envelope
	json.Unmarshal(w.Body.Bytes(), &resp)

	var result CheckActionResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to parse check action result: %v", err)
	}

	if !result.Allowed {
		t.Error("expected allowed = true")
	}

	hasWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "no policy engine") {
			hasWarning = true
			break
		}
	}
	if !hasWarning {
		t.Errorf("warnings = %v, expected to contain 'no policy engine'", result.Warnings)
	}
}

func TestSQLiteGovernanceHandler_CheckAction_MissingSessionID(t *testing.T) {
	env := newTestEnv(t)

	body := `{"action_type":"file_write"}`
	req := httptest.NewRequest("POST", "/v1/governance/check", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	env.governanceHandler.CheckAction(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var errResp errorEnvelope
	json.Unmarshal(w.Body.Bytes(), &errResp)
	if !strings.Contains(errResp.Error, "session_id is required") {
		t.Errorf("error = %q, want to contain %q", errResp.Error, "session_id is required")
	}
}

func TestSQLiteGovernanceHandler_CheckAction_MissingActionType(t *testing.T) {
	env := newTestEnv(t)

	body := `{"session_id":"test-session"}`
	req := httptest.NewRequest("POST", "/v1/governance/check", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	env.governanceHandler.CheckAction(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var errResp errorEnvelope
	json.Unmarshal(w.Body.Bytes(), &errResp)
	if !strings.Contains(errResp.Error, "action_type is required") {
		t.Errorf("error = %q, want to contain %q", errResp.Error, "action_type is required")
	}
}

func TestSQLiteGovernanceHandler_RequestApproval_Success(t *testing.T) {
	env := newTestEnv(t)

	// Must create session first for FK.
	sessionID := "approval-test-session"
	env.createTestSession(t, sessionID)

	body := `{
		"session_id":"` + sessionID + `",
		"action":"deploy production",
		"reason":"critical hotfix needed"
	}`
	req := httptest.NewRequest("POST", "/v1/governance/approvals", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	env.governanceHandler.RequestApproval(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp envelope
	json.Unmarshal(w.Body.Bytes(), &resp)

	var escalation store.EscalationRequest
	if err := json.Unmarshal(resp.Data, &escalation); err != nil {
		t.Fatalf("failed to parse escalation data: %v", err)
	}

	if escalation.ID == "" {
		t.Error("expected non-empty escalation ID")
	}
	if escalation.Status != "pending" {
		t.Errorf("status = %q, want %q", escalation.Status, "pending")
	}
	if escalation.SessionID != sessionID {
		t.Errorf("session_id = %q, want %q", escalation.SessionID, sessionID)
	}
}

func TestSQLiteGovernanceHandler_RequestApproval_MissingFields(t *testing.T) {
	env := newTestEnv(t)

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "missing session_id",
			body:    `{"action":"deploy","reason":"test"}`,
			wantErr: "session_id is required",
		},
		{
			name:    "missing action",
			body:    `{"session_id":"s1","reason":"test"}`,
			wantErr: "action is required",
		},
		{
			name:    "missing reason",
			body:    `{"session_id":"s1","action":"deploy"}`,
			wantErr: "reason is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/governance/approvals", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			env.governanceHandler.RequestApproval(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
			}

			var errResp errorEnvelope
			json.Unmarshal(w.Body.Bytes(), &errResp)
			if !strings.Contains(errResp.Error, tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", errResp.Error, tt.wantErr)
			}
		})
	}
}

func TestSQLiteGovernanceHandler_GetApproval_Success(t *testing.T) {
	env := newTestEnv(t)

	sessionID := "get-approval-session"
	env.createTestSession(t, sessionID)

	// Create an approval via the handler.
	createBody := `{
		"session_id":"` + sessionID + `",
		"action":"deploy",
		"reason":"test reason"
	}`
	createReq := httptest.NewRequest("POST", "/v1/governance/approvals", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	env.governanceHandler.RequestApproval(createW, createReq)

	if createW.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", createW.Code, http.StatusCreated)
	}

	var createResp envelope
	json.Unmarshal(createW.Body.Bytes(), &createResp)
	var created store.EscalationRequest
	json.Unmarshal(createResp.Data, &created)

	// GET the approval.
	getReq := httptest.NewRequest("GET", "/v1/governance/approvals/"+created.ID, nil)
	getReq.SetPathValue("id", created.ID)
	getW := httptest.NewRecorder()
	env.governanceHandler.GetApproval(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d; body = %s", getW.Code, http.StatusOK, getW.Body.String())
	}

	var getResp envelope
	json.Unmarshal(getW.Body.Bytes(), &getResp)
	var fetched store.EscalationRequest
	json.Unmarshal(getResp.Data, &fetched)

	if fetched.ID != created.ID {
		t.Errorf("id = %q, want %q", fetched.ID, created.ID)
	}
	if fetched.Status != "pending" {
		t.Errorf("status = %q, want %q", fetched.Status, "pending")
	}
}

func TestSQLiteGovernanceHandler_ResolveApproval_Success(t *testing.T) {
	env := newTestEnv(t)

	sessionID := "resolve-approval-session"
	env.createTestSession(t, sessionID)

	// Create an approval.
	createBody := `{
		"session_id":"` + sessionID + `",
		"action":"deploy",
		"reason":"needs approval"
	}`
	createReq := httptest.NewRequest("POST", "/v1/governance/approvals", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	env.governanceHandler.RequestApproval(createW, createReq)

	if createW.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", createW.Code, http.StatusCreated)
	}

	var createResp envelope
	json.Unmarshal(createW.Body.Bytes(), &createResp)
	var created store.EscalationRequest
	json.Unmarshal(createResp.Data, &created)

	// Resolve the approval.
	resolveBody := `{"approved": true, "comment": "ok"}`
	resolveReq := httptest.NewRequest("PATCH", "/v1/governance/approvals/"+created.ID, strings.NewReader(resolveBody))
	resolveReq.Header.Set("Content-Type", "application/json")
	resolveReq.SetPathValue("id", created.ID)
	resolveW := httptest.NewRecorder()
	env.governanceHandler.ResolveApproval(resolveW, resolveReq)

	if resolveW.Code != http.StatusOK {
		t.Fatalf("resolve status = %d, want %d; body = %s", resolveW.Code, http.StatusOK, resolveW.Body.String())
	}

	var resolveResp envelope
	json.Unmarshal(resolveW.Body.Bytes(), &resolveResp)
	var resolved store.EscalationRequest
	json.Unmarshal(resolveResp.Data, &resolved)

	if resolved.Status != "approved" {
		t.Errorf("status = %q, want %q", resolved.Status, "approved")
	}
	if resolved.ApproverComment != "ok" {
		t.Errorf("approver_comment = %q, want %q", resolved.ApproverComment, "ok")
	}
}

// ─── Audit Handler Tests ───────────────────────────────────────────────────

func TestSQLiteAuditHandler_LogDecision_Success(t *testing.T) {
	env := newTestEnv(t)

	sessionID := "decision-test-session"
	env.createTestSession(t, sessionID)

	body := `{
		"session_id":"` + sessionID + `",
		"decision_type":"code_change",
		"action":"modify main.go",
		"confidence": 0.9
	}`
	req := httptest.NewRequest("POST", "/v1/audit/decisions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	env.auditHandler.LogDecision(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp envelope
	json.Unmarshal(w.Body.Bytes(), &resp)

	var decision store.DecisionRecord
	if err := json.Unmarshal(resp.Data, &decision); err != nil {
		t.Fatalf("failed to parse decision data: %v", err)
	}

	if decision.ID == "" {
		t.Error("expected non-empty decision ID")
	}
	if decision.SessionID != sessionID {
		t.Errorf("session_id = %q, want %q", decision.SessionID, sessionID)
	}
	if decision.DecisionType != "code_change" {
		t.Errorf("decision_type = %q, want %q", decision.DecisionType, "code_change")
	}
	if decision.Confidence != 0.9 {
		t.Errorf("confidence = %f, want %f", decision.Confidence, 0.9)
	}
}

func TestSQLiteAuditHandler_LogDecision_MissingFields(t *testing.T) {
	env := newTestEnv(t)

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "missing session_id",
			body:    `{"decision_type":"code_change","action":"test"}`,
			wantErr: "session_id is required",
		},
		{
			name:    "missing decision_type",
			body:    `{"session_id":"s1","action":"test"}`,
			wantErr: "decision_type is required",
		},
		{
			name:    "missing action",
			body:    `{"session_id":"s1","decision_type":"code_change"}`,
			wantErr: "action is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/audit/decisions", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			env.auditHandler.LogDecision(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
			}

			var errResp errorEnvelope
			json.Unmarshal(w.Body.Bytes(), &errResp)
			if !strings.Contains(errResp.Error, tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", errResp.Error, tt.wantErr)
			}
		})
	}
}

func TestSQLiteAuditHandler_LogDecision_InvalidConfidence(t *testing.T) {
	env := newTestEnv(t)

	body := `{
		"session_id":"s1",
		"decision_type":"code_change",
		"action":"test",
		"confidence": 1.5
	}`
	req := httptest.NewRequest("POST", "/v1/audit/decisions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	env.auditHandler.LogDecision(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}

	var errResp errorEnvelope
	json.Unmarshal(w.Body.Bytes(), &errResp)
	if !strings.Contains(errResp.Error, "confidence must be between 0 and 1") {
		t.Errorf("error = %q, want to contain %q", errResp.Error, "confidence must be between 0 and 1")
	}
}

func TestSQLiteAuditHandler_LogDecision_DefaultConfidence(t *testing.T) {
	env := newTestEnv(t)

	sessionID := "default-conf-session"
	env.createTestSession(t, sessionID)

	body := `{
		"session_id":"` + sessionID + `",
		"decision_type":"code_change",
		"action":"test"
	}`
	req := httptest.NewRequest("POST", "/v1/audit/decisions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	env.auditHandler.LogDecision(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp envelope
	json.Unmarshal(w.Body.Bytes(), &resp)

	var decision store.DecisionRecord
	json.Unmarshal(resp.Data, &decision)

	if decision.Confidence != 0.8 {
		t.Errorf("confidence = %f, want default %f", decision.Confidence, 0.8)
	}
}

func TestSQLiteAuditHandler_LogDecision_WithAlternatives(t *testing.T) {
	env := newTestEnv(t)

	sessionID := "alt-decision-session"
	env.createTestSession(t, sessionID)

	body := `{
		"session_id":"` + sessionID + `",
		"decision_type":"code_change",
		"action":"refactor auth module",
		"confidence": 0.85,
		"alternatives": [
			{"action":"rewrite auth module","reason":"cleaner but more work","confidence":0.6},
			{"action":"patch only","reason":"minimal change","confidence":0.7}
		]
	}`
	req := httptest.NewRequest("POST", "/v1/audit/decisions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	env.auditHandler.LogDecision(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp envelope
	json.Unmarshal(w.Body.Bytes(), &resp)

	var decision store.DecisionRecord
	json.Unmarshal(resp.Data, &decision)

	if len(decision.Alternatives) != 2 {
		t.Errorf("alternatives count = %d, want %d", len(decision.Alternatives), 2)
	}
	if decision.Alternatives[0].Action != "rewrite auth module" {
		t.Errorf("alternatives[0].action = %q, want %q", decision.Alternatives[0].Action, "rewrite auth module")
	}
}

func TestSQLiteAuditHandler_GetDecision_Success(t *testing.T) {
	env := newTestEnv(t)

	sessionID := "get-decision-session"
	env.createTestSession(t, sessionID)

	// Create a decision.
	createBody := `{
		"session_id":"` + sessionID + `",
		"decision_type":"code_change",
		"action":"modify main.go"
	}`
	createReq := httptest.NewRequest("POST", "/v1/audit/decisions", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	env.auditHandler.LogDecision(createW, createReq)

	if createW.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", createW.Code, http.StatusCreated)
	}

	var createResp envelope
	json.Unmarshal(createW.Body.Bytes(), &createResp)
	var created store.DecisionRecord
	json.Unmarshal(createResp.Data, &created)

	// GET the decision.
	getReq := httptest.NewRequest("GET", "/v1/audit/decisions/"+created.ID, nil)
	getReq.SetPathValue("id", created.ID)
	getW := httptest.NewRecorder()
	env.auditHandler.GetDecision(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d; body = %s", getW.Code, http.StatusOK, getW.Body.String())
	}

	var getResp envelope
	json.Unmarshal(getW.Body.Bytes(), &getResp)
	var fetched store.DecisionRecord
	json.Unmarshal(getResp.Data, &fetched)

	if fetched.ID != created.ID {
		t.Errorf("id = %q, want %q", fetched.ID, created.ID)
	}
	if fetched.Action != "modify main.go" {
		t.Errorf("action = %q, want %q", fetched.Action, "modify main.go")
	}
}

func TestSQLiteAuditHandler_ListDecisions(t *testing.T) {
	env := newTestEnv(t)

	sessionID := "list-decisions-session"
	env.createTestSession(t, sessionID)

	// Create 3 decisions.
	for i := 0; i < 3; i++ {
		body := `{
			"session_id":"` + sessionID + `",
			"decision_type":"code_change",
			"action":"action ` + string(rune('A'+i)) + `"
		}`
		req := httptest.NewRequest("POST", "/v1/audit/decisions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		env.auditHandler.LogDecision(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create decision %d: status = %d, want %d", i, w.Code, http.StatusCreated)
		}
	}

	// List decisions.
	req := httptest.NewRequest("GET", "/v1/audit/decisions", nil)
	w := httptest.NewRecorder()
	env.auditHandler.ListDecisions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var listResp listEnvelope
	json.Unmarshal(w.Body.Bytes(), &listResp)

	var items []store.DecisionRecord
	json.Unmarshal(listResp.Items, &items)

	if len(items) != 3 {
		t.Errorf("items count = %d, want %d", len(items), 3)
	}
	if listResp.Total != 3 {
		t.Errorf("total = %d, want %d", listResp.Total, 3)
	}
}

func TestSQLiteAuditHandler_PrepareCommit_Success(t *testing.T) {
	env := newTestEnv(t)

	sessionID := "commit-prep-session"
	env.createTestSession(t, sessionID)

	body := `{
		"session_id":"` + sessionID + `",
		"files":["src/main.go","src/utils.go"],
		"message":"feat: add utility functions"
	}`
	req := httptest.NewRequest("POST", "/v1/commits/prepare", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	env.auditHandler.PrepareCommit(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp envelope
	json.Unmarshal(w.Body.Bytes(), &resp)

	var data map[string]interface{}
	json.Unmarshal(resp.Data, &data)

	if data["commit_token"] == nil || data["commit_token"] == "" {
		t.Error("expected non-empty commit_token")
	}
	if data["approved"] != true {
		t.Errorf("approved = %v, want true", data["approved"])
	}
}

func TestSQLiteAuditHandler_PrepareCommit_SensitiveFiles(t *testing.T) {
	env := newTestEnv(t)

	sessionID := "commit-sensitive-session"
	env.createTestSession(t, sessionID)

	body := `{
		"session_id":"` + sessionID + `",
		"files":["src/main.go",".env"],
		"message":"feat: add config"
	}`
	req := httptest.NewRequest("POST", "/v1/commits/prepare", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	env.auditHandler.PrepareCommit(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp envelope
	json.Unmarshal(w.Body.Bytes(), &resp)

	var data map[string]interface{}
	json.Unmarshal(resp.Data, &data)

	if data["approved"] != false {
		t.Errorf("approved = %v, want false (sensitive files present)", data["approved"])
	}
}

func TestSQLiteAuditHandler_PrepareCommit_MissingFields(t *testing.T) {
	env := newTestEnv(t)

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "missing session_id",
			body:    `{"files":["main.go"],"message":"test"}`,
			wantErr: "session_id is required",
		},
		{
			name:    "empty files",
			body:    `{"session_id":"s1","files":[],"message":"test"}`,
			wantErr: "files are required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/commits/prepare", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			env.auditHandler.PrepareCommit(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
			}

			var errResp errorEnvelope
			json.Unmarshal(w.Body.Bytes(), &errResp)
			if !strings.Contains(errResp.Error, tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", errResp.Error, tt.wantErr)
			}
		})
	}
}

func TestSQLiteAuditHandler_RegisterCommit_Success(t *testing.T) {
	env := newTestEnv(t)

	sessionID := "register-commit-session"
	env.createTestSession(t, sessionID)

	// First prepare a commit.
	prepBody := `{
		"session_id":"` + sessionID + `",
		"files":["src/main.go"],
		"message":"feat: initial commit"
	}`
	prepReq := httptest.NewRequest("POST", "/v1/commits/prepare", strings.NewReader(prepBody))
	prepReq.Header.Set("Content-Type", "application/json")
	prepW := httptest.NewRecorder()
	env.auditHandler.PrepareCommit(prepW, prepReq)

	if prepW.Code != http.StatusCreated {
		t.Fatalf("prepare status = %d, want %d", prepW.Code, http.StatusCreated)
	}

	var prepResp envelope
	json.Unmarshal(prepW.Body.Bytes(), &prepResp)
	var prepData map[string]interface{}
	json.Unmarshal(prepResp.Data, &prepData)

	commitToken := prepData["commit_token"].(string)

	// Register the commit with a SHA.
	regBody := `{
		"commit_token":"` + commitToken + `",
		"commit_sha":"abc123def456"
	}`
	regReq := httptest.NewRequest("POST", "/v1/commits/register", strings.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regW := httptest.NewRecorder()
	env.auditHandler.RegisterCommit(regW, regReq)

	if regW.Code != http.StatusOK {
		t.Fatalf("register status = %d, want %d; body = %s", regW.Code, http.StatusOK, regW.Body.String())
	}

	var regResp envelope
	json.Unmarshal(regW.Body.Bytes(), &regResp)

	var commit store.CommitRecord
	json.Unmarshal(regResp.Data, &commit)

	if commit.CommitSHA != "abc123def456" {
		t.Errorf("commit_sha = %q, want %q", commit.CommitSHA, "abc123def456")
	}
	if commit.Status != "committed" {
		t.Errorf("status = %q, want %q", commit.Status, "committed")
	}
}

func TestSQLiteAuditHandler_VerifyCommit_AfterRegister(t *testing.T) {
	env := newTestEnv(t)

	sessionID := "verify-commit-session"
	env.createTestSession(t, sessionID)

	// Phase 1: Prepare
	prepBody := `{
		"session_id":"` + sessionID + `",
		"files":["src/main.go"],
		"message":"feat: full lifecycle test"
	}`
	prepReq := httptest.NewRequest("POST", "/v1/commits/prepare", strings.NewReader(prepBody))
	prepReq.Header.Set("Content-Type", "application/json")
	prepW := httptest.NewRecorder()
	env.auditHandler.PrepareCommit(prepW, prepReq)

	if prepW.Code != http.StatusCreated {
		t.Fatalf("prepare status = %d, want %d", prepW.Code, http.StatusCreated)
	}

	var prepResp envelope
	json.Unmarshal(prepW.Body.Bytes(), &prepResp)
	var prepData map[string]interface{}
	json.Unmarshal(prepResp.Data, &prepData)
	commitToken := prepData["commit_token"].(string)

	// Phase 2: Register
	commitSHA := "lifecycle_sha_789"
	regBody := `{
		"commit_token":"` + commitToken + `",
		"commit_sha":"` + commitSHA + `"
	}`
	regReq := httptest.NewRequest("POST", "/v1/commits/register", strings.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regW := httptest.NewRecorder()
	env.auditHandler.RegisterCommit(regW, regReq)

	if regW.Code != http.StatusOK {
		t.Fatalf("register status = %d, want %d; body = %s", regW.Code, http.StatusOK, regW.Body.String())
	}

	// Phase 3: Verify
	verifyBody := `{"commit_sha":"` + commitSHA + `"}`
	verifyReq := httptest.NewRequest("POST", "/v1/commits/verify", strings.NewReader(verifyBody))
	verifyReq.Header.Set("Content-Type", "application/json")
	verifyW := httptest.NewRecorder()
	env.auditHandler.VerifyCommit(verifyW, verifyReq)

	if verifyW.Code != http.StatusOK {
		t.Fatalf("verify status = %d, want %d; body = %s", verifyW.Code, http.StatusOK, verifyW.Body.String())
	}

	var verifyResp envelope
	json.Unmarshal(verifyW.Body.Bytes(), &verifyResp)

	var verifyData map[string]interface{}
	json.Unmarshal(verifyResp.Data, &verifyData)

	if verifyData["verified"] != true {
		t.Errorf("verified = %v, want true", verifyData["verified"])
	}
	if verifyData["commit_sha"] != commitSHA {
		t.Errorf("commit_sha = %v, want %q", verifyData["commit_sha"], commitSHA)
	}
	if verifyData["session_id"] != sessionID {
		t.Errorf("session_id = %v, want %q", verifyData["session_id"], sessionID)
	}
}
