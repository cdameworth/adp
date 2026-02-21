package database

import (
	"context"
	"testing"
	"time"

	"github.com/adp/adp/internal/store"
)

// --- Session Store Extended Tests ---

func TestSQLiteSessionStore_ListWithFilters(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	ctx := context.Background()

	// Create diverse sessions
	sessions := []store.CreateSessionInput{
		{ID: "filter-1", OrganizationID: "org-a", UserID: "user-1", Tool: "claude_code", TrustLevel: 3, ExpiresAt: time.Now().Add(1 * time.Hour)},
		{ID: "filter-2", OrganizationID: "org-a", UserID: "user-2", Tool: "cursor", TrustLevel: 4, ExpiresAt: time.Now().Add(1 * time.Hour)},
		{ID: "filter-3", OrganizationID: "org-b", UserID: "user-1", Tool: "claude_code", TrustLevel: 2, ExpiresAt: time.Now().Add(1 * time.Hour)},
		{ID: "filter-4", OrganizationID: "org-b", UserID: "user-3", Tool: "copilot", TrustLevel: 5, ExpiresAt: time.Now().Add(1 * time.Hour)},
	}
	for _, s := range sessions {
		if _, err := ss.Create(ctx, s); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}
	// End one session for status filtering
	ss.End(ctx, "filter-4")

	tests := []struct {
		name    string
		filter  SQLiteSessionFilter
		wantLen int
	}{
		{"no filter", SQLiteSessionFilter{Limit: 50}, 4},
		{"by organization", SQLiteSessionFilter{OrganizationID: "org-a", Limit: 50}, 2},
		{"by tool", SQLiteSessionFilter{Tool: "claude_code", Limit: 50}, 2},
		{"by user", SQLiteSessionFilter{UserID: "user-1", Limit: 50}, 2},
		{"by status active", SQLiteSessionFilter{Status: "active", Limit: 50}, 3},
		{"by status ended", SQLiteSessionFilter{Status: "ended", Limit: 50}, 1},
		{"by min trust level", SQLiteSessionFilter{MinTrustLevel: 4, Limit: 50}, 2},
		{"by max trust level", SQLiteSessionFilter{MaxTrustLevel: 3, Limit: 50}, 2},
		{"by trust range", SQLiteSessionFilter{MinTrustLevel: 3, MaxTrustLevel: 4, Limit: 50}, 2},
		{"with limit", SQLiteSessionFilter{Limit: 2}, 2},
		{"with offset", SQLiteSessionFilter{Limit: 50, Offset: 3}, 1},
		{"combined org+tool", SQLiteSessionFilter{OrganizationID: "org-a", Tool: "claude_code", Limit: 50}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ss.List(ctx, tt.filter)
			if err != nil {
				t.Fatalf("List failed: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("List returned %d sessions, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestSQLiteSessionStore_ListDefaultLimit(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	ctx := context.Background()

	// List with zero limit should default to 50
	got, err := ss.List(ctx, SQLiteSessionFilter{Limit: 0})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	// Should succeed with no results (empty DB)
	if got != nil && len(got) != 0 {
		t.Errorf("Expected empty results, got %d", len(got))
	}
}

func TestSQLiteSessionStore_Update(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID:           "update-session",
		Tool:         "test",
		TrustLevel:   2,
		Capabilities: []string{"read"},
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	})

	// Update trust level
	newTrust := 4
	updated, err := ss.Update(ctx, "update-session", SQLiteUpdateSessionInput{
		TrustLevel: &newTrust,
	})
	if err != nil {
		t.Fatalf("Update trust level failed: %v", err)
	}
	if updated.TrustLevel != 4 {
		t.Errorf("TrustLevel = %d, want 4", updated.TrustLevel)
	}

	// Update capabilities
	updated, err = ss.Update(ctx, "update-session", SQLiteUpdateSessionInput{
		Capabilities: []string{"read", "write", "execute"},
	})
	if err != nil {
		t.Fatalf("Update capabilities failed: %v", err)
	}
	if len(updated.Capabilities) != 3 {
		t.Errorf("Capabilities len = %d, want 3", len(updated.Capabilities))
	}

	// Update constraints
	updated, err = ss.Update(ctx, "update-session", SQLiteUpdateSessionInput{
		Constraints: []string{"max_files:10"},
	})
	if err != nil {
		t.Fatalf("Update constraints failed: %v", err)
	}
	if len(updated.Constraints) != 1 {
		t.Errorf("Constraints len = %d, want 1", len(updated.Constraints))
	}

	// Update status
	updated, err = ss.Update(ctx, "update-session", SQLiteUpdateSessionInput{
		Status: "paused",
	})
	if err != nil {
		t.Fatalf("Update status failed: %v", err)
	}
	if updated.Status != "paused" {
		t.Errorf("Status = %s, want paused", updated.Status)
	}
}

func TestSQLiteSessionStore_UpdateNoChanges(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID:         "noop-session",
		Tool:       "test",
		TrustLevel: 3,
		ExpiresAt:  time.Now().Add(1 * time.Hour),
	})

	// Empty update should return current session
	got, err := ss.Update(ctx, "noop-session", SQLiteUpdateSessionInput{})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if got.TrustLevel != 3 {
		t.Errorf("TrustLevel = %d, want 3", got.TrustLevel)
	}
}

func TestSQLiteSessionStore_UpdateNotFound(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	ctx := context.Background()

	newTrust := 4
	_, err := ss.Update(ctx, "nonexistent", SQLiteUpdateSessionInput{TrustLevel: &newTrust})
	if err == nil {
		t.Error("Update should fail for nonexistent session")
	}
}

func TestSQLiteSessionStore_CreateValidation(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	ctx := context.Background()

	tests := []struct {
		name  string
		input store.CreateSessionInput
	}{
		{"empty ID", store.CreateSessionInput{Tool: "test", TrustLevel: 3, ExpiresAt: time.Now().Add(1 * time.Hour)}},
		{"empty tool", store.CreateSessionInput{ID: "x", TrustLevel: 3, ExpiresAt: time.Now().Add(1 * time.Hour)}},
		{"trust level 0", store.CreateSessionInput{ID: "x", Tool: "test", TrustLevel: 0, ExpiresAt: time.Now().Add(1 * time.Hour)}},
		{"trust level 6", store.CreateSessionInput{ID: "x", Tool: "test", TrustLevel: 6, ExpiresAt: time.Now().Add(1 * time.Hour)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ss.Create(ctx, tt.input)
			if err == nil {
				t.Error("Create should fail for invalid input")
			}
		})
	}
}

// --- Decision Store Extended Tests ---

func TestSQLiteDecisionStore_ListWithFilters(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	ds := NewSQLiteDecisionStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID: "dec-filter-sess", Tool: "test", TrustLevel: 3,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	})

	// Create varied decisions
	inputs := []store.CreateDecisionInput{
		{SessionID: "dec-filter-sess", DecisionType: "code_change", Action: "edit file", Confidence: 0.9},
		{SessionID: "dec-filter-sess", DecisionType: "code_change", Action: "add file", Confidence: 0.8},
		{SessionID: "dec-filter-sess", DecisionType: "deployment", Action: "deploy", Confidence: 0.7},
	}
	for _, input := range inputs {
		if _, err := ds.Create(ctx, input); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	tests := []struct {
		name    string
		filter  SQLiteDecisionFilter
		wantLen int
	}{
		{"no filter", SQLiteDecisionFilter{Limit: 50}, 3},
		{"by session", SQLiteDecisionFilter{SessionID: "dec-filter-sess", Limit: 50}, 3},
		{"by type", SQLiteDecisionFilter{DecisionType: "code_change", Limit: 50}, 2},
		{"by type deployment", SQLiteDecisionFilter{DecisionType: "deployment", Limit: 50}, 1},
		{"with limit", SQLiteDecisionFilter{Limit: 2}, 2},
		{"combined session+type", SQLiteDecisionFilter{SessionID: "dec-filter-sess", DecisionType: "deployment", Limit: 50}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ds.List(ctx, tt.filter)
			if err != nil {
				t.Fatalf("List failed: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("List returned %d records, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestSQLiteDecisionStore_GetNotFound(t *testing.T) {
	client := newTestSQLiteClient(t)
	ds := NewSQLiteDecisionStore(client)
	ctx := context.Background()

	_, err := ds.Get(ctx, "nonexistent-id")
	if err == nil {
		t.Error("Get should fail for nonexistent decision")
	}
}

func TestSQLiteDecisionStore_CreateWithPolicyResult(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	ds := NewSQLiteDecisionStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID: "policy-sess", Tool: "test", TrustLevel: 3,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	})

	record, err := ds.Create(ctx, store.CreateDecisionInput{
		SessionID:    "policy-sess",
		DecisionType: "code_change",
		Action:       "edit main.go",
		Confidence:   0.9,
		PolicyResult: &store.PolicyResult{
			Allowed:       true,
			DeniedReasons: []string{},
			PolicyNames:   []string{"deny_sensitive_files", "blast_radius_limit"},
			EvaluatedAt:   time.Now().Format(time.RFC3339),
		},
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := ds.Get(ctx, record.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.PolicyResult == nil {
		t.Fatal("PolicyResult should be set")
	}
	if !got.PolicyResult.Allowed {
		t.Error("PolicyResult.Allowed should be true")
	}
	if len(got.PolicyResult.PolicyNames) != 2 {
		t.Errorf("PolicyNames len = %d, want 2", len(got.PolicyResult.PolicyNames))
	}
}

// --- Escalation Store Extended Tests ---

func TestSQLiteEscalationStore_List(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	es := NewSQLiteEscalationStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID: "esc-list-sess", Tool: "test", TrustLevel: 3,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	})

	// Create escalations with different priorities
	for i, priority := range []string{"high", "normal", "critical", "low"} {
		expires := time.Now().Add(30 * time.Minute)
		es.Create(ctx, store.CreateEscalationInput{
			SessionID:  "esc-list-sess",
			Action:     "action-" + string(rune('a'+i)),
			ActionType: "deploy",
			Reason:     "test reason " + priority,
			Priority:   priority,
			ExpiresAt:  &expires,
		})
	}

	tests := []struct {
		name    string
		filter  SQLiteEscalationFilter
		wantLen int
	}{
		{"no filter", SQLiteEscalationFilter{Limit: 50}, 4},
		{"by session", SQLiteEscalationFilter{SessionID: "esc-list-sess", Limit: 50}, 4},
		{"by priority high", SQLiteEscalationFilter{Priority: "high", Limit: 50}, 1},
		{"by priority critical", SQLiteEscalationFilter{Priority: "critical", Limit: 50}, 1},
		{"by status pending", SQLiteEscalationFilter{Status: "pending", Limit: 50}, 4},
		{"with limit", SQLiteEscalationFilter{Limit: 2}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := es.List(ctx, tt.filter)
			if err != nil {
				t.Fatalf("List failed: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("List returned %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestSQLiteEscalationStore_ListPending(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	es := NewSQLiteEscalationStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID: "pending-sess", Tool: "test", TrustLevel: 3,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	})

	// Create 3 escalations
	for i := 0; i < 3; i++ {
		expires := time.Now().Add(30 * time.Minute)
		es.Create(ctx, store.CreateEscalationInput{
			SessionID: "pending-sess",
			Action:    "action",
			Reason:    "reason",
			ExpiresAt: &expires,
		})
	}

	// All 3 should be pending
	pending, err := es.ListPending(ctx, 50)
	if err != nil {
		t.Fatalf("ListPending failed: %v", err)
	}
	if len(pending) != 3 {
		t.Errorf("ListPending returned %d, want 3", len(pending))
	}

	// Resolve one
	if len(pending) > 0 {
		es.Resolve(ctx, pending[0].ID, SQLiteResolveInput{
			ApproverID: "approver-1",
			Approved:   true,
			Comment:    "approved",
		})
	}

	// Now 2 should be pending
	pending, err = es.ListPending(ctx, 50)
	if err != nil {
		t.Fatalf("ListPending failed: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("ListPending returned %d, want 2", len(pending))
	}
}

func TestSQLiteEscalationStore_Resolve(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	es := NewSQLiteEscalationStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID: "resolve-sess", Tool: "test", TrustLevel: 3,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	})

	expires := time.Now().Add(30 * time.Minute)
	escalation, _ := es.Create(ctx, store.CreateEscalationInput{
		SessionID: "resolve-sess",
		Action:    "deploy prod",
		Reason:    "production deploy",
		Priority:  "high",
		ExpiresAt: &expires,
	})

	// Approve
	resolved, err := es.Resolve(ctx, escalation.ID, SQLiteResolveInput{
		ApproverID: "admin-user",
		Approved:   true,
		Comment:    "Looks good, approved",
	})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if resolved.Status != "approved" {
		t.Errorf("Status = %s, want approved", resolved.Status)
	}
	if resolved.ApproverID != "admin-user" {
		t.Errorf("ApproverID = %s, want admin-user", resolved.ApproverID)
	}
	if resolved.ApproverComment != "Looks good, approved" {
		t.Errorf("ApproverComment = %s", resolved.ApproverComment)
	}
	if resolved.ResolvedAt == nil {
		t.Error("ResolvedAt should be set")
	}
}

func TestSQLiteEscalationStore_ResolveDeny(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	es := NewSQLiteEscalationStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID: "deny-sess", Tool: "test", TrustLevel: 3,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	})

	expires := time.Now().Add(30 * time.Minute)
	escalation, _ := es.Create(ctx, store.CreateEscalationInput{
		SessionID: "deny-sess",
		Action:    "deploy prod",
		Reason:    "risky deploy",
		ExpiresAt: &expires,
	})

	// Deny
	resolved, err := es.Resolve(ctx, escalation.ID, SQLiteResolveInput{
		ApproverID: "reviewer",
		Approved:   false,
		Comment:    "Too risky",
	})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if resolved.Status != "denied" {
		t.Errorf("Status = %s, want denied", resolved.Status)
	}
}

func TestSQLiteEscalationStore_ResolveAlreadyResolved(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	es := NewSQLiteEscalationStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID: "double-resolve-sess", Tool: "test", TrustLevel: 3,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	})

	expires := time.Now().Add(30 * time.Minute)
	escalation, _ := es.Create(ctx, store.CreateEscalationInput{
		SessionID: "double-resolve-sess",
		Action:    "deploy",
		Reason:    "test",
		ExpiresAt: &expires,
	})

	// First resolve succeeds
	es.Resolve(ctx, escalation.ID, SQLiteResolveInput{
		ApproverID: "user1", Approved: true, Comment: "ok",
	})

	// Second resolve should fail
	_, err := es.Resolve(ctx, escalation.ID, SQLiteResolveInput{
		ApproverID: "user2", Approved: false, Comment: "too late",
	})
	if err == nil {
		t.Error("Second Resolve should fail for already-resolved escalation")
	}
}

func TestSQLiteEscalationStore_ResolveNotFound(t *testing.T) {
	client := newTestSQLiteClient(t)
	es := NewSQLiteEscalationStore(client)
	ctx := context.Background()

	_, err := es.Resolve(ctx, "nonexistent", SQLiteResolveInput{
		ApproverID: "user", Approved: true, Comment: "ok",
	})
	if err == nil {
		t.Error("Resolve should fail for nonexistent escalation")
	}
}

func TestSQLiteEscalationStore_GetNotFound(t *testing.T) {
	client := newTestSQLiteClient(t)
	es := NewSQLiteEscalationStore(client)
	ctx := context.Background()

	_, err := es.Get(ctx, "nonexistent")
	if err == nil {
		t.Error("Get should fail for nonexistent escalation")
	}
}

func TestSQLiteEscalationStore_CreateValidation(t *testing.T) {
	client := newTestSQLiteClient(t)
	es := NewSQLiteEscalationStore(client)
	ctx := context.Background()

	tests := []struct {
		name  string
		input store.CreateEscalationInput
	}{
		{"empty session_id", store.CreateEscalationInput{Action: "x", Reason: "y"}},
		{"empty action", store.CreateEscalationInput{SessionID: "x", Reason: "y"}},
		{"empty reason", store.CreateEscalationInput{SessionID: "x", Action: "y"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := es.Create(ctx, tt.input)
			if err == nil {
				t.Error("Create should fail for invalid input")
			}
		})
	}
}

func TestSQLiteEscalationStore_CreateDefaults(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	es := NewSQLiteEscalationStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID: "defaults-sess", Tool: "test", TrustLevel: 3,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	})

	// Create with minimal fields
	esc, err := es.Create(ctx, store.CreateEscalationInput{
		SessionID: "defaults-sess",
		Action:    "some action",
		Reason:    "some reason",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if esc.ActionType != "unknown" {
		t.Errorf("ActionType = %s, want unknown", esc.ActionType)
	}
	if esc.Priority != "normal" {
		t.Errorf("Priority = %s, want normal", esc.Priority)
	}
	if esc.Target == nil {
		t.Error("Target should default to empty map")
	}
	if esc.PolicyNames == nil {
		t.Error("PolicyNames should default to empty slice")
	}
}

// --- Commit Store Extended Tests ---

func TestSQLiteCommitStore_GetBySHA(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	cs := NewSQLiteCommitStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID: "sha-sess", Tool: "test", TrustLevel: 3,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	})

	record, _ := cs.Prepare(ctx, store.PrepareCommitInput{
		SessionID: "sha-sess",
		Files:     []string{"main.go"},
		Message:   "test",
	})

	cs.RegisterCommit(ctx, record.CommitToken, "sha_abc_123")

	// Retrieve by SHA
	got, err := cs.GetBySHA(ctx, "sha_abc_123")
	if err != nil {
		t.Fatalf("GetBySHA failed: %v", err)
	}
	if got.CommitSHA != "sha_abc_123" {
		t.Errorf("CommitSHA = %s, want sha_abc_123", got.CommitSHA)
	}
	if got.SessionID != "sha-sess" {
		t.Errorf("SessionID = %s, want sha-sess", got.SessionID)
	}
}

func TestSQLiteCommitStore_GetBySHANotFound(t *testing.T) {
	client := newTestSQLiteClient(t)
	cs := NewSQLiteCommitStore(client)
	ctx := context.Background()

	_, err := cs.GetBySHA(ctx, "nonexistent-sha")
	if err == nil {
		t.Error("GetBySHA should fail for nonexistent SHA")
	}
}

func TestSQLiteCommitStore_Approve(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	cs := NewSQLiteCommitStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID: "approve-sess", Tool: "test", TrustLevel: 3,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	})

	record, _ := cs.Prepare(ctx, store.PrepareCommitInput{
		SessionID: "approve-sess",
		Files:     []string{"main.go"},
		Message:   "safe commit",
	})

	if record.Approved {
		t.Error("Should not be approved initially")
	}

	approved, err := cs.Approve(ctx, record.ID, "Automatic approval")
	if err != nil {
		t.Fatalf("Approve failed: %v", err)
	}
	if !approved.Approved {
		t.Error("Should be approved after Approve call")
	}
	if approved.ApprovalReason != "Automatic approval" {
		t.Errorf("ApprovalReason = %s", approved.ApprovalReason)
	}
}

func TestSQLiteCommitStore_UniqueTokens(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	cs := NewSQLiteCommitStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID: "token-sess", Tool: "test", TrustLevel: 3,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	})

	tokens := make(map[string]bool)
	for i := 0; i < 10; i++ {
		record, err := cs.Prepare(ctx, store.PrepareCommitInput{
			SessionID: "token-sess",
			Files:     []string{"main.go"},
			Message:   "commit",
		})
		if err != nil {
			t.Fatalf("Prepare %d failed: %v", i, err)
		}
		if tokens[record.CommitToken] {
			t.Fatalf("Duplicate token generated: %s", record.CommitToken)
		}
		tokens[record.CommitToken] = true
	}
}
