package database

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/adp/adp/internal/store"
)

func newTestSQLiteClient(t *testing.T) *SQLiteClient {
	t.Helper()
	client, err := NewSQLiteClient(SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create SQLite client: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// --- Session Store Tests ---

func TestSQLiteSessionStore_CreateAndGet(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	ctx := context.Background()

	session, err := ss.Create(ctx, store.CreateSessionInput{
		ID:             "test-session-1",
		OrganizationID: "org-1",
		UserID:         "user-1",
		Tool:           "claude_code",
		TrustLevel:     3,
		Capabilities:   []string{"read", "write"},
		Constraints:    []string{"max_files_per_commit:50"},
		ServiceScope:   []string{"svc-1"},
		ExpiresAt:      time.Now().Add(8 * time.Hour),
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if session.ID != "test-session-1" {
		t.Errorf("ID = %s, want test-session-1", session.ID)
	}
	if session.Status != "active" {
		t.Errorf("Status = %s, want active", session.Status)
	}
	if session.TrustLevel != 3 {
		t.Errorf("TrustLevel = %d, want 3", session.TrustLevel)
	}

	got, err := ss.Get(ctx, "test-session-1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Tool != "claude_code" {
		t.Errorf("Tool = %s, want claude_code", got.Tool)
	}
	if len(got.Capabilities) != 2 {
		t.Errorf("Capabilities len = %d, want 2", len(got.Capabilities))
	}
}

func TestSQLiteSessionStore_Heartbeat(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID:         "hb-session",
		Tool:       "test",
		TrustLevel: 2,
		ExpiresAt:  time.Now().Add(1 * time.Hour),
	})

	if err := ss.Heartbeat(ctx, "hb-session"); err != nil {
		t.Fatalf("Heartbeat failed: %v", err)
	}

	got, _ := ss.Get(ctx, "hb-session")
	if got.LastHeartbeat == nil {
		t.Error("LastHeartbeat should be set after heartbeat")
	}
}

func TestSQLiteSessionStore_End(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID:         "end-session",
		Tool:       "test",
		TrustLevel: 2,
		ExpiresAt:  time.Now().Add(1 * time.Hour),
	})

	if err := ss.End(ctx, "end-session"); err != nil {
		t.Fatalf("End failed: %v", err)
	}

	got, _ := ss.Get(ctx, "end-session")
	if got.Status != "ended" {
		t.Errorf("Status = %s, want ended", got.Status)
	}
}

func TestSQLiteSessionStore_ListEnded(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		id := "list-session-" + string(rune('a'+i))
		ss.Create(ctx, store.CreateSessionInput{
			ID:         id,
			Tool:       "test",
			TrustLevel: 2,
			ExpiresAt:  time.Now().Add(1 * time.Hour),
		})
		ss.End(ctx, id)
	}

	sessions, err := ss.ListEnded(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListEnded failed: %v", err)
	}
	if len(sessions) != 3 {
		t.Errorf("ListEnded returned %d sessions, want 3", len(sessions))
	}
}

func TestSQLiteSessionStore_GetNotFound(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	ctx := context.Background()

	_, err := ss.Get(ctx, "nonexistent")
	if err == nil {
		t.Error("Get should fail for nonexistent session")
	}
}

// --- Decision Store Tests ---

func TestSQLiteDecisionStore_CreateAndGet(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	ds := NewSQLiteDecisionStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID:         "dec-session",
		Tool:       "test",
		TrustLevel: 3,
		ExpiresAt:  time.Now().Add(1 * time.Hour),
	})

	record, err := ds.Create(ctx, store.CreateDecisionInput{
		SessionID:    "dec-session",
		DecisionType: "code_change",
		Action:       "edit main.go",
		Target:       map[string]any{"paths": []any{"main.go"}},
		Reasoning:    map[string]any{"rationale": "fix bug"},
		Confidence:   0.9,
		Alternatives: []store.Alternative{
			{Action: "rewrite", Reason: "cleaner", Confidence: 0.7},
		},
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if record.ID == "" {
		t.Error("ID should be set")
	}
	if record.Confidence != 0.9 {
		t.Errorf("Confidence = %f, want 0.9", record.Confidence)
	}

	got, err := ds.Get(ctx, record.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.DecisionType != "code_change" {
		t.Errorf("DecisionType = %s, want code_change", got.DecisionType)
	}
	if len(got.Alternatives) != 1 {
		t.Errorf("Alternatives len = %d, want 1", len(got.Alternatives))
	}
}

func TestSQLiteDecisionStore_ListBySession(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	ds := NewSQLiteDecisionStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID:         "list-dec-session",
		Tool:       "test",
		TrustLevel: 3,
		ExpiresAt:  time.Now().Add(1 * time.Hour),
	})

	for i := 0; i < 5; i++ {
		ds.Create(ctx, store.CreateDecisionInput{
			SessionID:    "list-dec-session",
			DecisionType: "code_change",
			Action:       "edit file",
			Confidence:   0.8,
		})
	}

	records, err := ds.ListBySession(ctx, "list-dec-session")
	if err != nil {
		t.Fatalf("ListBySession failed: %v", err)
	}
	if len(records) != 5 {
		t.Errorf("ListBySession returned %d records, want 5", len(records))
	}
}

func TestSQLiteDecisionStore_GetLineage(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	ds := NewSQLiteDecisionStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID:         "lineage-session",
		Tool:       "test",
		TrustLevel: 3,
		ExpiresAt:  time.Now().Add(1 * time.Hour),
	})

	// Create a chain: grandparent -> parent -> child
	grandparent, _ := ds.Create(ctx, store.CreateDecisionInput{
		SessionID:    "lineage-session",
		DecisionType: "code_change",
		Action:       "step 1",
		Confidence:   0.8,
	})

	parent, _ := ds.Create(ctx, store.CreateDecisionInput{
		SessionID:       "lineage-session",
		DecisionType:    "code_change",
		Action:          "step 2",
		Confidence:      0.85,
		ContextSnapshot: map[string]any{"parent_decision_id": grandparent.ID},
	})

	child, _ := ds.Create(ctx, store.CreateDecisionInput{
		SessionID:       "lineage-session",
		DecisionType:    "code_change",
		Action:          "step 3",
		Confidence:      0.9,
		ContextSnapshot: map[string]any{"parent_decision_id": parent.ID},
	})

	lineage, err := ds.GetLineage(ctx, child.ID, 5)
	if err != nil {
		t.Fatalf("GetLineage failed: %v", err)
	}
	if len(lineage) != 3 {
		t.Errorf("Lineage len = %d, want 3", len(lineage))
	}
	if lineage[0].ID != child.ID {
		t.Errorf("First in lineage should be child")
	}
	if lineage[2].ID != grandparent.ID {
		t.Errorf("Last in lineage should be grandparent")
	}
}

// --- Commit Store Tests ---

func TestSQLiteCommitStore_PrepareAndVerify(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	cs := NewSQLiteCommitStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID:         "commit-session",
		Tool:       "test",
		TrustLevel: 3,
		ExpiresAt:  time.Now().Add(1 * time.Hour),
	})

	record, err := cs.Prepare(ctx, store.PrepareCommitInput{
		SessionID: "commit-session",
		Files:     []string{"main.go", "util.go"},
		Message:   "fix bug #123",
	})
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	if record.CommitToken == "" {
		t.Error("CommitToken should be set")
	}
	if record.Status != "prepared" {
		t.Errorf("Status = %s, want prepared", record.Status)
	}

	// Verify before marking committed should return false
	verified, err := cs.IsCommitVerified(ctx, "abc123")
	if err != nil {
		t.Fatalf("IsCommitVerified failed: %v", err)
	}
	if verified {
		t.Error("Should not be verified before marking committed")
	}

	// Register commit
	committed, err := cs.RegisterCommit(ctx, record.CommitToken, "abc123def456")
	if err != nil {
		t.Fatalf("RegisterCommit failed: %v", err)
	}
	if committed.CommitSHA != "abc123def456" {
		t.Errorf("CommitSHA = %s, want abc123def456", committed.CommitSHA)
	}
	if committed.Status != "committed" {
		t.Errorf("Status = %s, want committed", committed.Status)
	}

	// Now verify should return true
	verified, err = cs.IsCommitVerified(ctx, "abc123def456")
	if err != nil {
		t.Fatalf("IsCommitVerified failed: %v", err)
	}
	if !verified {
		t.Error("Should be verified after marking committed")
	}
}

func TestSQLiteCommitStore_RegisterCommitNotFound(t *testing.T) {
	client := newTestSQLiteClient(t)
	cs := NewSQLiteCommitStore(client)
	ctx := context.Background()

	_, err := cs.RegisterCommit(ctx, "nonexistent-token", "abc123")
	if err == nil {
		t.Error("RegisterCommit should fail for nonexistent token")
	}
}

func TestSQLiteCommitStore_RegisterCommitDoesNotSetVerifiedAt(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	cs := NewSQLiteCommitStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID:         "verify-session",
		Tool:       "test",
		TrustLevel: 3,
		ExpiresAt:  time.Now().Add(1 * time.Hour),
	})

	record, err := cs.Prepare(ctx, store.PrepareCommitInput{
		SessionID: "verify-session",
		Files:     []string{"main.go"},
		Message:   "test commit",
	})
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	committed, err := cs.RegisterCommit(ctx, record.CommitToken, "sha123abc")
	if err != nil {
		t.Fatalf("RegisterCommit failed: %v", err)
	}
	if committed.Status != "committed" {
		t.Errorf("Status = %s, want committed", committed.Status)
	}
	if committed.CommittedAt == nil {
		t.Error("CommittedAt should be set after RegisterCommit")
	}
	if committed.VerifiedAt != nil {
		t.Error("VerifiedAt should NOT be set after RegisterCommit (separate state)")
	}
}

func TestSQLiteCommitStore_FullLifecycle(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	cs := NewSQLiteCommitStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID:         "lifecycle-session",
		Tool:       "test",
		TrustLevel: 3,
		ExpiresAt:  time.Now().Add(1 * time.Hour),
	})

	// Phase 1: Prepare
	record, err := cs.Prepare(ctx, store.PrepareCommitInput{
		SessionID: "lifecycle-session",
		Files:     []string{"main.go", "util.go"},
		Message:   "lifecycle test",
	})
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	if record.Status != "prepared" {
		t.Errorf("Status = %s, want prepared", record.Status)
	}
	if record.Approved {
		t.Error("Should not be approved after Prepare")
	}

	// Phase 2: Register with commit SHA
	committed, err := cs.RegisterCommit(ctx, record.CommitToken, "lifecycle_sha_456")
	if err != nil {
		t.Fatalf("RegisterCommit failed: %v", err)
	}
	if committed.Status != "committed" {
		t.Errorf("Status = %s, want committed", committed.Status)
	}
	if committed.CommitSHA != "lifecycle_sha_456" {
		t.Errorf("CommitSHA = %s, want lifecycle_sha_456", committed.CommitSHA)
	}

	// Phase 3: Verify should succeed (IsCommitVerified accepts 'committed' status for SQLite)
	verified, err := cs.IsCommitVerified(ctx, "lifecycle_sha_456")
	if err != nil {
		t.Fatalf("IsCommitVerified failed: %v", err)
	}
	if !verified {
		t.Error("Should be verified after RegisterCommit (SQLite accepts committed status)")
	}

	// Verify for unknown SHA returns false
	verified, err = cs.IsCommitVerified(ctx, "unknown_sha")
	if err != nil {
		t.Fatalf("IsCommitVerified failed: %v", err)
	}
	if verified {
		t.Error("Should not be verified for unknown SHA")
	}
}

// --- Escalation Store Tests ---

func TestSQLiteEscalationStore_CreateAndGet(t *testing.T) {
	client := newTestSQLiteClient(t)
	ss := NewSQLiteSessionStore(client)
	es := NewSQLiteEscalationStore(client)
	ctx := context.Background()

	ss.Create(ctx, store.CreateSessionInput{
		ID:         "esc-session",
		Tool:       "test",
		TrustLevel: 3,
		ExpiresAt:  time.Now().Add(1 * time.Hour),
	})

	expires := time.Now().Add(30 * time.Minute)
	escalation, err := es.Create(ctx, store.CreateEscalationInput{
		SessionID:  "esc-session",
		Action:     "deploy to production",
		ActionType: "deploy",
		Target:     map[string]any{"environment": "production"},
		Reason:     "requires human approval for production deploys",
		Priority:   "high",
		ExpiresAt:  &expires,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if escalation.ID == "" {
		t.Error("ID should be set")
	}
	if escalation.Status != "pending" {
		t.Errorf("Status = %s, want pending", escalation.Status)
	}
	if escalation.Priority != "high" {
		t.Errorf("Priority = %s, want high", escalation.Priority)
	}

	got, err := es.Get(ctx, escalation.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Reason != "requires human approval for production deploys" {
		t.Errorf("Reason mismatch")
	}
}

// --- Doc Store Tests ---

func TestSQLiteDocStore_SaveAndGet(t *testing.T) {
	client := newTestSQLiteClient(t)
	ds := NewSQLiteDocStore(client)
	ctx := context.Background()

	err := ds.Save(ctx, store.DocRecord{
		ID:        "doc-1",
		SessionID: "sess-1",
		Category:  "session_summary",
		Title:     "Session Summary: sess-1",
		Content:   "# Summary\n\nThis is a test session.",
		Metadata:  map[string]any{"tool": "test"},
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := ds.Get(ctx, "doc-1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Title != "Session Summary: sess-1" {
		t.Errorf("Title = %s", got.Title)
	}
	if got.Category != "session_summary" {
		t.Errorf("Category = %s", got.Category)
	}
}

func TestSQLiteDocStore_ListByCategory(t *testing.T) {
	client := newTestSQLiteClient(t)
	ds := NewSQLiteDocStore(client)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		ds.Save(ctx, store.DocRecord{
			Category: "session_summary",
			Title:    "Summary",
			Content:  "content",
		})
	}
	ds.Save(ctx, store.DocRecord{
		Category: "risk_report",
		Title:    "Risk",
		Content:  "content",
	})

	docs, err := ds.ListByCategory(ctx, "session_summary", 10)
	if err != nil {
		t.Fatalf("ListByCategory failed: %v", err)
	}
	if len(docs) != 3 {
		t.Errorf("ListByCategory returned %d docs, want 3", len(docs))
	}
}

func TestSQLiteDocStore_ListBySession(t *testing.T) {
	client := newTestSQLiteClient(t)
	ds := NewSQLiteDocStore(client)
	ctx := context.Background()

	ds.Save(ctx, store.DocRecord{
		SessionID: "sess-x",
		Category:  "session_summary",
		Title:     "Summary",
		Content:   "content",
	})
	ds.Save(ctx, store.DocRecord{
		SessionID: "sess-x",
		Category:  "risk_report",
		Title:     "Risk",
		Content:   "content",
	})
	ds.Save(ctx, store.DocRecord{
		SessionID: "sess-y",
		Category:  "session_summary",
		Title:     "Other",
		Content:   "content",
	})

	docs, err := ds.ListBySession(ctx, "sess-x")
	if err != nil {
		t.Fatalf("ListBySession failed: %v", err)
	}
	if len(docs) != 2 {
		t.Errorf("ListBySession returned %d docs, want 2", len(docs))
	}
}

func TestSQLiteDocStore_Search(t *testing.T) {
	client := newTestSQLiteClient(t)
	ds := NewSQLiteDocStore(client)
	ctx := context.Background()

	ds.Save(ctx, store.DocRecord{
		Category: "session_summary",
		Title:    "Authentication Fix",
		Content:  "Fixed authentication flow in login module",
	})
	ds.Save(ctx, store.DocRecord{
		Category: "session_summary",
		Title:    "Database Refactor",
		Content:  "Refactored database connection pooling",
	})

	docs, err := ds.Search(ctx, "authentication", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(docs) != 1 {
		t.Errorf("Search returned %d docs, want 1", len(docs))
	}
}

func TestSQLiteDocStore_Upsert(t *testing.T) {
	client := newTestSQLiteClient(t)
	ds := NewSQLiteDocStore(client)
	ctx := context.Background()

	ds.Save(ctx, store.DocRecord{
		ID:       "upsert-doc",
		Category: "session_summary",
		Title:    "Original Title",
		Content:  "original content",
	})

	ds.Save(ctx, store.DocRecord{
		ID:       "upsert-doc",
		Category: "session_summary",
		Title:    "Updated Title",
		Content:  "updated content",
	})

	got, _ := ds.Get(ctx, "upsert-doc")
	if got.Title != "Updated Title" {
		t.Errorf("Title = %s, want Updated Title", got.Title)
	}
	if got.Content != "updated content" {
		t.Errorf("Content = %s, want updated content", got.Content)
	}
}

// --- SQLite Client Tests ---

func TestSQLiteClient_HealthCheck(t *testing.T) {
	client := newTestSQLiteClient(t)
	ctx := context.Background()

	status := client.HealthCheck(ctx)
	if !status.Healthy {
		t.Errorf("HealthCheck should be healthy: %s", status.Message)
	}
}

func TestSQLiteClient_Transaction(t *testing.T) {
	client := newTestSQLiteClient(t)
	ctx := context.Background()

	// Successful transaction
	err := client.Transaction(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO agent_sessions (id, organization_id, user_id, tool, trust_level, expires_at) VALUES (?, ?, ?, ?, ?, ?)",
			"tx-session", "org", "user", "test", 2, "2030-01-01T00:00:00Z")
		return err
	})
	if err != nil {
		t.Fatalf("Transaction failed: %v", err)
	}

	// Verify data exists
	ss := NewSQLiteSessionStore(client)
	got, err := ss.Get(ctx, "tx-session")
	if err != nil {
		t.Fatalf("Get after transaction failed: %v", err)
	}
	if got.ID != "tx-session" {
		t.Errorf("ID = %s", got.ID)
	}
}
