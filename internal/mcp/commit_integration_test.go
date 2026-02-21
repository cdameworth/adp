package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/adp/adp/internal/infrastructure/database"
	"github.com/adp/adp/internal/store"
)

// newTestMCPServer creates an MCP server with real SQLite stores for integration testing.
func newTestMCPServer(t *testing.T) (*Server, *database.SQLiteCommitStore) {
	t.Helper()

	client, err := database.NewSQLiteClient(database.SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create SQLite client: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	sessionStore := database.NewSQLiteSessionStore(client)
	commitStore := database.NewSQLiteCommitStore(client)
	decisionStore := database.NewSQLiteDecisionStore(client)
	escalationStore := database.NewSQLiteEscalationStore(client)
	docStore := database.NewSQLiteDocStore(client)

	server := NewServerWithIO(bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	server.SessionStore = sessionStore
	server.CommitStore = commitStore
	server.DecisionStore = decisionStore
	server.EscalationStore = escalationStore
	server.DocStore = docStore

	server.registerBuiltinTools()

	return server, commitStore
}

// parseMCPResult extracts the JSON result from a CallToolResult.
func parseMCPResult(t *testing.T, result *CallToolResult) map[string]interface{} {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatal("empty tool result")
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", result.Content[0].Text)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &data); err != nil {
		t.Fatalf("failed to parse tool result JSON: %v\nraw: %s", err, result.Content[0].Text)
	}
	return data
}

func TestMCPCommitFlow_WithSQLiteStore(t *testing.T) {
	server, commitStore := newTestMCPServer(t)
	ctx := context.Background()

	// Phase 1: Prepare commit via MCP tool
	prepareArgs, _ := json.Marshal(PrepareCommitArgs{
		SessionID: "mcp-test-session",
		Files:     []string{"main.go", "util.go"},
		Message:   "mcp integration test",
	})

	result, err := server.handlePrepareCommit(ctx, prepareArgs)
	if err != nil {
		t.Fatalf("handlePrepareCommit error: %v", err)
	}
	data := parseMCPResult(t, result)

	token, _ := data["commit_token"].(string)
	if token == "" || !strings.HasPrefix(token, "adp_") {
		t.Fatalf("commit_token = %q, want adp_* prefix", token)
	}
	if approved, _ := data["approved"].(bool); !approved {
		t.Errorf("approved = false, want true for normal files")
	}

	// Phase 2: Register commit via store directly (simulating post-commit hook HTTP call).
	_, err = commitStore.RegisterCommit(ctx, token, "mcp_sha_123")
	if err != nil {
		t.Fatalf("RegisterCommit error: %v", err)
	}

	// Phase 3: Verify commit via MCP tool
	verifyArgs, _ := json.Marshal(VerifyCommitArgs{
		CommitSHA: "mcp_sha_123",
	})

	result, err = server.handleVerifyCommit(ctx, verifyArgs)
	if err != nil {
		t.Fatalf("handleVerifyCommit error: %v", err)
	}
	data = parseMCPResult(t, result)

	if verified, _ := data["verified"].(bool); !verified {
		t.Errorf("verified = false, want true after register")
	}
}

func TestMCPCommitFlow_SensitiveFileDenied(t *testing.T) {
	server, _ := newTestMCPServer(t)
	ctx := context.Background()

	prepareArgs, _ := json.Marshal(PrepareCommitArgs{
		SessionID: "mcp-test-session",
		Files:     []string{".env", "main.go"},
		Message:   "add config",
	})

	result, err := server.handlePrepareCommit(ctx, prepareArgs)
	if err != nil {
		t.Fatalf("handlePrepareCommit error: %v", err)
	}
	data := parseMCPResult(t, result)

	if approved, _ := data["approved"].(bool); approved {
		t.Errorf("approved = true, want false for sensitive files")
	}

	reason, _ := data["reason"].(string)
	if !strings.Contains(strings.ToLower(reason), "sensitive") {
		t.Errorf("reason = %q, want to contain 'sensitive'", reason)
	}

	// Token should still be returned.
	token, _ := data["commit_token"].(string)
	if token == "" {
		t.Errorf("commit_token should be returned even when not approved")
	}
}

func TestMCPCommitFlow_VerifyUnknownSHA(t *testing.T) {
	server, _ := newTestMCPServer(t)
	ctx := context.Background()

	verifyArgs, _ := json.Marshal(VerifyCommitArgs{
		CommitSHA: "unknown_sha_abc",
	})

	result, err := server.handleVerifyCommit(ctx, verifyArgs)
	if err != nil {
		t.Fatalf("handleVerifyCommit error: %v", err)
	}
	data := parseMCPResult(t, result)

	if verified, _ := data["verified"].(bool); verified {
		t.Errorf("verified = true, want false for unknown SHA")
	}
}

func TestMCPCommitFlow_PolicyEngineDenied(t *testing.T) {
	server, _ := newTestMCPServer(t)
	ctx := context.Background()

	// Wire up a mock policy engine that denies everything.
	server.UnifiedPolicyEngine = &mockDenyAllPolicyEngine{}

	prepareArgs, _ := json.Marshal(PrepareCommitArgs{
		SessionID: "mcp-test-session",
		Files:     []string{"main.go"}, // Normal file, but policy denies.
		Message:   "policy test",
	})

	result, err := server.handlePrepareCommit(ctx, prepareArgs)
	if err != nil {
		t.Fatalf("handlePrepareCommit error: %v", err)
	}
	data := parseMCPResult(t, result)

	if approved, _ := data["approved"].(bool); approved {
		t.Errorf("approved = true, want false when policy engine denies")
	}

	reason, _ := data["reason"].(string)
	if !strings.Contains(reason, "Policy denied") {
		t.Errorf("reason = %q, want to contain 'Policy denied'", reason)
	}
}

func TestMCPCommitFlow_PolicyEngineRequiresApproval(t *testing.T) {
	server, _ := newTestMCPServer(t)
	ctx := context.Background()

	// Wire up a mock policy engine that requires approval.
	server.UnifiedPolicyEngine = &mockRequireApprovalPolicyEngine{}

	prepareArgs, _ := json.Marshal(PrepareCommitArgs{
		SessionID: "mcp-test-session",
		Files:     []string{"main.go"},
		Message:   "approval test",
	})

	result, err := server.handlePrepareCommit(ctx, prepareArgs)
	if err != nil {
		t.Fatalf("handlePrepareCommit error: %v", err)
	}
	data := parseMCPResult(t, result)

	if approved, _ := data["approved"].(bool); approved {
		t.Errorf("approved = true, want false when approval required")
	}

	reason, _ := data["reason"].(string)
	if !strings.Contains(reason, "requires human approval") {
		t.Errorf("reason = %q, want to contain 'requires human approval'", reason)
	}
}

func TestMCPCommitFlow_NoStore(t *testing.T) {
	// Server with no CommitStore -- should still return a token (fallback path).
	server := NewServerWithIO(bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	server.registerBuiltinTools()
	ctx := context.Background()

	prepareArgs, _ := json.Marshal(PrepareCommitArgs{
		SessionID: "no-store-session",
		Files:     []string{"main.go"},
		Message:   "test",
	})

	result, err := server.handlePrepareCommit(ctx, prepareArgs)
	if err != nil {
		t.Fatalf("handlePrepareCommit error: %v", err)
	}
	data := parseMCPResult(t, result)

	token, _ := data["commit_token"].(string)
	if token == "" || !strings.HasPrefix(token, "adp_") {
		t.Errorf("commit_token = %q, want adp_* prefix even without store", token)
	}
}

// --- Mock policy engines ---

// mockDenyAllPolicyEngine denies every action.
type mockDenyAllPolicyEngine struct{}

func (m *mockDenyAllPolicyEngine) Evaluate(_ context.Context, _ *UnifiedEvalInput) (*UnifiedEvalResult, error) {
	return &UnifiedEvalResult{
		Allowed:         false,
		DeniedReasons:   []string{"test denial: all commits blocked"},
		MatchedPolicies: []string{"test_deny_all"},
	}, nil
}

// Verify interface compliance.
var _ UnifiedPolicyEngine = (*mockDenyAllPolicyEngine)(nil)

// mockRequireApprovalPolicyEngine allows but requires approval.
type mockRequireApprovalPolicyEngine struct{}

func (m *mockRequireApprovalPolicyEngine) Evaluate(_ context.Context, _ *UnifiedEvalInput) (*UnifiedEvalResult, error) {
	return &UnifiedEvalResult{
		Allowed:          true,
		RequiresApproval: true,
		MatchedPolicies:  []string{"test_require_approval"},
	}, nil
}

var _ UnifiedPolicyEngine = (*mockRequireApprovalPolicyEngine)(nil)

// Ensure store.CommitStore is satisfied by SQLiteCommitStore (compile-time check).
var _ store.CommitStore = (*database.SQLiteCommitStore)(nil)
