package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthCheck(t *testing.T) {
	router := NewSimpleRouter()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("Status = %v, want ok", body["status"])
	}
}

func TestReadinessCheck(t *testing.T) {
	router := NewSimpleRouter()

	req := httptest.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if body["status"] != "ready" {
		t.Errorf("Status = %v, want ready", body["status"])
	}
}

func TestAPIInfo(t *testing.T) {
	router := NewSimpleRouter()

	req := httptest.NewRequest("GET", "/v1", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if body["name"] != "ADP API" {
		t.Errorf("Name = %v, want ADP API", body["name"])
	}

	if body["version"] != "1.0.0" {
		t.Errorf("Version = %v, want 1.0.0", body["version"])
	}

	endpoints, ok := body["endpoints"].(map[string]interface{})
	if !ok {
		t.Fatal("Missing endpoints in response")
	}

	expectedEndpoints := []string{"sessions", "governance", "audit", "services", "context"}
	for _, ep := range expectedEndpoints {
		if _, exists := endpoints[ep]; !exists {
			t.Errorf("Missing endpoint: %s", ep)
		}
	}
}

func TestCORSMiddleware(t *testing.T) {
	router := NewSimpleRouter()

	req := httptest.NewRequest("OPTIONS", "/v1/sessions", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Check CORS headers
	allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	if allowOrigin != "*" {
		t.Errorf("Access-Control-Allow-Origin = %s, want *", allowOrigin)
	}

	allowMethods := resp.Header.Get("Access-Control-Allow-Methods")
	if !strings.Contains(allowMethods, "POST") {
		t.Error("Allow-Methods should include POST")
	}
	if !strings.Contains(allowMethods, "GET") {
		t.Error("Allow-Methods should include GET")
	}

	allowHeaders := resp.Header.Get("Access-Control-Allow-Headers")
	if !strings.Contains(allowHeaders, "Authorization") {
		t.Error("Allow-Headers should include Authorization")
	}

	// OPTIONS should return 200
	if resp.StatusCode != http.StatusOK {
		t.Errorf("OPTIONS status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestContextEndpoint(t *testing.T) {
	router := NewSimpleRouter()

	body := `{
		"session_id": "test-session-1",
		"task": "Fix bug in authentication module"
	}`

	req := httptest.NewRequest("POST", "/v1/context", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("Status code = %d, want %d. Body: %s", resp.StatusCode, http.StatusOK, string(bodyBytes))
	}

	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	data, ok := response["data"].(map[string]interface{})
	if !ok {
		t.Fatal("Missing data in response")
	}

	// Check context layers
	for _, layer := range []string{"essential", "task_relevant", "supporting"} {
		if _, exists := data[layer]; !exists {
			t.Errorf("Missing context layer: %s", layer)
		}
	}
}

func TestContextEndpoint_MissingSessionID(t *testing.T) {
	router := NewSimpleRouter()

	body := `{"task": "Some task"}`

	req := httptest.NewRequest("POST", "/v1/context", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestContextEndpoint_MissingTask(t *testing.T) {
	router := NewSimpleRouter()

	body := `{"session_id": "test-session"}`

	req := httptest.NewRequest("POST", "/v1/context", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestRouterConfig_WithHandlers(t *testing.T) {
	// Create mock handlers
	mockSession := &mockSessionHandler{}
	mockGovernance := &mockGovernanceHandler{}
	mockAudit := &mockAuditHandler{}
	mockService := &mockServiceHandler{}

	cfg := RouterConfig{
		SessionHandler:    mockSession,
		GovernanceHandler: mockGovernance,
		AuditHandler:      mockAudit,
		ServiceHandler:    mockService,
	}

	router := NewRouter(cfg)

	// Test that routes are registered
	tests := []struct {
		method string
		path   string
	}{
		{"POST", "/v1/sessions"},
		{"GET", "/v1/sessions"},
		{"POST", "/v1/governance/check"},
		{"POST", "/v1/audit/decisions"},
		{"POST", "/v1/services"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			// Should not return 404 (route not found)
			resp := w.Result()
			defer resp.Body.Close()

			// 405 (method not allowed) or other errors are acceptable
			// as long as the route exists
			if resp.StatusCode == http.StatusNotFound {
				t.Errorf("Route not found: %s %s", tt.method, tt.path)
			}
		})
	}
}

// Mock handlers for testing

type mockSessionHandler struct{}

func (h *mockSessionHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"data": "created"})
}
func (h *mockSessionHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"data": "session"})
}
func (h *mockSessionHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"data": "sessions"})
}
func (h *mockSessionHandler) UpdateSession(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"data": "updated"})
}
func (h *mockSessionHandler) EndSession(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"data": "ended"})
}
func (h *mockSessionHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"data": "heartbeat"})
}

type mockGovernanceHandler struct{}

func (h *mockGovernanceHandler) CheckAction(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]bool{"allowed": true})
}
func (h *mockGovernanceHandler) RequestApproval(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"id": "approval-1"})
}
func (h *mockGovernanceHandler) GetApproval(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
}
func (h *mockGovernanceHandler) ListApprovals(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode([]interface{}{})
}
func (h *mockGovernanceHandler) ListPendingApprovals(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode([]interface{}{})
}
func (h *mockGovernanceHandler) ResolveApproval(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "approved"})
}

type mockAuditHandler struct{}

func (h *mockAuditHandler) LogDecision(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"id": "decision-1"})
}
func (h *mockAuditHandler) GetDecision(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"id": "decision-1"})
}
func (h *mockAuditHandler) ListDecisions(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode([]interface{}{})
}
func (h *mockAuditHandler) GetLineage(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode([]interface{}{})
}
func (h *mockAuditHandler) PrepareCommit(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"token": "commit-token"})
}
func (h *mockAuditHandler) RegisterCommit(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "committed"})
}
func (h *mockAuditHandler) VerifyCommit(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]bool{"verified": true})
}

type mockServiceHandler struct{}

func (h *mockServiceHandler) CreateService(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"id": "service-1"})
}
func (h *mockServiceHandler) GetService(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"id": "service-1"})
}
func (h *mockServiceHandler) ListServices(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode([]interface{}{})
}
func (h *mockServiceHandler) UpdateService(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"id": "service-1"})
}
func (h *mockServiceHandler) DeleteService(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

type mockAuthHandler struct{}

func (h *mockAuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "registered"})
}
func (h *mockAuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"access_token": "token"})
}
func (h *mockAuthHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"access_token": "new-token"})
}
func (h *mockAuthHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"id": "user-1"})
}
func (h *mockAuthHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"id": "user-1"})
}
func (h *mockAuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"message": "Password changed"})
}

type mockAdminHandler struct{}

func (h *mockAdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": "new-user"})
}
func (h *mockAdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode([]interface{}{})
}
func (h *mockAdminHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"id": "user-1"})
}
func (h *mockAdminHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"id": "user-1"})
}
func (h *mockAdminHandler) DisableUser(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// Benchmark tests

func BenchmarkHealthCheck(b *testing.B) {
	router := NewSimpleRouter()

	req := httptest.NewRequest("GET", "/health", nil)

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func BenchmarkContextEndpoint(b *testing.B) {
	router := NewSimpleRouter()

	body := `{"session_id": "test-session-1", "task": "Fix bug"}`

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/v1/context", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}
