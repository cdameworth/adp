// Package mcp provides Postgres-to-store adapter types that wrap the existing
// PostgreSQL store implementations (database.SessionStore, etc.) so they satisfy
// the database-agnostic store.* interfaces used by the MCP server.
//
// These adapters handle UUID string ↔ uuid.UUID conversion and type mapping
// between the Postgres-specific types and the portable store types.
package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/adp/adp/internal/infrastructure/database"
	"github.com/adp/adp/internal/store"
	"github.com/google/uuid"
)

// --- Session adapter ---

// PgSessionAdapter wraps database.SessionStore to satisfy store.SessionStore.
type PgSessionAdapter struct {
	pg *database.SessionStore
}

// NewPgSessionAdapter creates a new adapter.
func NewPgSessionAdapter(pg *database.SessionStore) *PgSessionAdapter {
	return &PgSessionAdapter{pg: pg}
}

func (a *PgSessionAdapter) Create(ctx context.Context, input store.CreateSessionInput) (*store.Session, error) {
	orgID, err := uuid.Parse(input.OrganizationID)
	if err != nil {
		orgID = uuid.New()
	}
	userID, err := uuid.Parse(input.UserID)
	if err != nil {
		userID = uuid.New()
	}

	var scopeUUIDs []uuid.UUID
	for _, s := range input.ServiceScope {
		if u, err := uuid.Parse(s); err == nil {
			scopeUUIDs = append(scopeUUIDs, u)
		}
	}

	pgSession, err := a.pg.Create(ctx, database.CreateSessionInput{
		ID:             input.ID,
		OrganizationID: orgID,
		UserID:         userID,
		Tool:           input.Tool,
		TrustLevel:     input.TrustLevel,
		Capabilities:   input.Capabilities,
		Constraints:    input.Constraints,
		ServiceScope:   scopeUUIDs,
		ExpiresAt:      input.ExpiresAt,
		Metadata:       database.Metadata(input.Metadata),
	})
	if err != nil {
		return nil, err
	}

	return pgSessionToStore(pgSession), nil
}

func (a *PgSessionAdapter) Get(ctx context.Context, id string) (*store.Session, error) {
	pgSession, err := a.pg.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return pgSessionToStore(pgSession), nil
}

func (a *PgSessionAdapter) Heartbeat(ctx context.Context, id string) error {
	return a.pg.Heartbeat(ctx, id)
}

func (a *PgSessionAdapter) End(ctx context.Context, id string) error {
	return a.pg.End(ctx, id)
}

func (a *PgSessionAdapter) ListEnded(ctx context.Context, afterID string, limit int) ([]*store.Session, error) {
	pgSessions, err := a.pg.List(ctx, database.SessionFilter{
		Status: "ended",
		Limit:  limit,
	})
	if err != nil {
		return nil, err
	}

	var results []*store.Session
	for _, pg := range pgSessions {
		results = append(results, pgSessionToStore(pg))
	}
	return results, nil
}

func (a *PgSessionAdapter) ValidateToken(ctx context.Context, sessionID, tokenHash string) (bool, error) {
	// PostgreSQL session store does not yet store token hashes.
	// When the PG schema is migrated to include token_hash, implement proper validation.
	// For now, skip validation in PG mode (sidecar is primarily used locally with SQLite).
	return true, nil
}

func pgSessionToStore(pg *database.Session) *store.Session {
	var scope []string
	for _, u := range pg.ServiceScope {
		scope = append(scope, u.String())
	}

	return &store.Session{
		ID:             pg.ID,
		OrganizationID: pg.OrganizationID.String(),
		UserID:         pg.UserID.String(),
		Tool:           pg.Tool,
		TrustLevel:     pg.TrustLevel,
		Capabilities:   pg.Capabilities,
		Constraints:    pg.Constraints,
		ServiceScope:   scope,
		Status:         pg.Status,
		StartedAt:      pg.StartedAt,
		ExpiresAt:      pg.ExpiresAt,
		LastHeartbeat:  pg.LastHeartbeat,
		Metadata:       map[string]any(pg.Metadata),
	}
}

// --- Decision adapter ---

// PgDecisionAdapter wraps database.DecisionStore to satisfy store.DecisionStore.
type PgDecisionAdapter struct {
	pg *database.DecisionStore
}

// NewPgDecisionAdapter creates a new adapter.
func NewPgDecisionAdapter(pg *database.DecisionStore) *PgDecisionAdapter {
	return &PgDecisionAdapter{pg: pg}
}

func (a *PgDecisionAdapter) Create(ctx context.Context, input store.CreateDecisionInput) (*store.DecisionRecord, error) {
	var pgAlts []database.Alternative
	for _, alt := range input.Alternatives {
		pgAlts = append(pgAlts, database.Alternative{
			Action:     alt.Action,
			Reason:     alt.Reason,
			Confidence: alt.Confidence,
		})
	}

	var pgPolicy *database.PolicyResult
	if input.PolicyResult != nil {
		pgPolicy = &database.PolicyResult{
			Allowed:       input.PolicyResult.Allowed,
			DeniedReasons: input.PolicyResult.DeniedReasons,
			PolicyNames:   input.PolicyResult.PolicyNames,
			EvaluatedAt:   input.PolicyResult.EvaluatedAt,
		}
	}

	pgRecord, err := a.pg.Create(ctx, database.CreateDecisionInput{
		SessionID:       input.SessionID,
		DecisionType:    input.DecisionType,
		Action:          input.Action,
		Target:          input.Target,
		Reasoning:       input.Reasoning,
		Confidence:      input.Confidence,
		Alternatives:    pgAlts,
		ContextSnapshot: input.ContextSnapshot,
		PolicyResult:    pgPolicy,
	})
	if err != nil {
		return nil, err
	}

	return pgDecisionToStore(pgRecord), nil
}

func (a *PgDecisionAdapter) Get(ctx context.Context, id string) (*store.DecisionRecord, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid decision ID: %w", err)
	}
	pgRecord, err := a.pg.Get(ctx, uid)
	if err != nil {
		return nil, err
	}
	return pgDecisionToStore(pgRecord), nil
}

func (a *PgDecisionAdapter) GetLineage(ctx context.Context, id string, depth int) ([]*store.DecisionRecord, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid decision ID: %w", err)
	}
	pgRecords, err := a.pg.GetLineage(ctx, uid, depth)
	if err != nil {
		return nil, err
	}

	var results []*store.DecisionRecord
	for _, pg := range pgRecords {
		results = append(results, pgDecisionToStore(pg))
	}
	return results, nil
}

func (a *PgDecisionAdapter) ListBySession(ctx context.Context, sessionID string) ([]*store.DecisionRecord, error) {
	pgRecords, err := a.pg.ListBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	var results []*store.DecisionRecord
	for _, pg := range pgRecords {
		results = append(results, pgDecisionToStore(pg))
	}
	return results, nil
}

func pgDecisionToStore(pg *database.DecisionRecord) *store.DecisionRecord {
	var alts []store.Alternative
	for _, a := range pg.Alternatives {
		alts = append(alts, store.Alternative{
			Action:     a.Action,
			Reason:     a.Reason,
			Confidence: a.Confidence,
		})
	}

	var pr *store.PolicyResult
	if pg.PolicyResult != nil {
		pr = &store.PolicyResult{
			Allowed:       pg.PolicyResult.Allowed,
			DeniedReasons: pg.PolicyResult.DeniedReasons,
			PolicyNames:   pg.PolicyResult.PolicyNames,
			EvaluatedAt:   pg.PolicyResult.EvaluatedAt,
		}
	}

	return &store.DecisionRecord{
		ID:              pg.ID.String(),
		SessionID:       pg.SessionID,
		DecisionType:    pg.DecisionType,
		Action:          pg.Action,
		Target:          pg.Target,
		Reasoning:       pg.Reasoning,
		Confidence:      pg.Confidence,
		Alternatives:    alts,
		ContextSnapshot: pg.ContextSnapshot,
		PolicyResult:    pr,
		Status:          pg.Status,
		Outcome:         pg.Outcome,
		CreatedAt:       pg.CreatedAt,
	}
}

// --- Commit adapter ---

// PgCommitAdapter wraps database.CommitStore to satisfy store.CommitStore.
type PgCommitAdapter struct {
	pg *database.CommitStore
}

// NewPgCommitAdapter creates a new adapter.
func NewPgCommitAdapter(pg *database.CommitStore) *PgCommitAdapter {
	return &PgCommitAdapter{pg: pg}
}

func (a *PgCommitAdapter) Prepare(ctx context.Context, input store.PrepareCommitInput) (*store.CommitRecord, error) {
	pgRecord, err := a.pg.Prepare(ctx, database.PrepareCommitInput{
		SessionID: input.SessionID,
		Files:     input.Files,
		Message:   input.Message,
	})
	if err != nil {
		return nil, err
	}
	return pgCommitToStore(pgRecord), nil
}

func (a *PgCommitAdapter) RegisterCommit(ctx context.Context, token string, sha string) (*store.CommitRecord, error) {
	pgRecord, err := a.pg.MarkCommitted(ctx, token, sha)
	if err != nil {
		return nil, err
	}
	return pgCommitToStore(pgRecord), nil
}

func (a *PgCommitAdapter) IsCommitVerified(ctx context.Context, sha string) (bool, error) {
	return a.pg.IsCommitVerified(ctx, sha)
}

func pgCommitToStore(pg *database.CommitRecord) *store.CommitRecord {
	return &store.CommitRecord{
		ID:             pg.ID.String(),
		SessionID:      pg.SessionID,
		CommitSHA:      pg.CommitSHA,
		CommitToken:    pg.CommitToken,
		Files:          pg.Files,
		Message:        pg.Message,
		Status:         pg.Status,
		Approved:       pg.Approved,
		ApprovalReason: pg.ApprovalReason,
		PreparedAt:     pg.PreparedAt,
		CommittedAt:    pg.CommittedAt,
		VerifiedAt:     pg.VerifiedAt,
	}
}

// --- Escalation adapter ---

// PgEscalationAdapter wraps database.EscalationStore to satisfy store.EscalationStore.
type PgEscalationAdapter struct {
	pg *database.EscalationStore
}

// NewPgEscalationAdapter creates a new adapter.
func NewPgEscalationAdapter(pg *database.EscalationStore) *PgEscalationAdapter {
	return &PgEscalationAdapter{pg: pg}
}

func (a *PgEscalationAdapter) Create(ctx context.Context, input store.CreateEscalationInput) (*store.EscalationRequest, error) {
	var decisionID *uuid.UUID
	if input.DecisionID != "" {
		if uid, err := uuid.Parse(input.DecisionID); err == nil {
			decisionID = &uid
		}
	}

	pgEscalation, err := a.pg.Create(ctx, database.CreateEscalationInput{
		SessionID:      input.SessionID,
		DecisionID:     decisionID,
		Action:         input.Action,
		ActionType:     input.ActionType,
		Target:         input.Target,
		Reason:         input.Reason,
		PolicyNames:    input.PolicyNames,
		ContextSummary: input.ContextSummary,
		Priority:       input.Priority,
		ExpiresAt:      input.ExpiresAt,
	})
	if err != nil {
		return nil, err
	}

	return pgEscalationToStore(pgEscalation), nil
}

func (a *PgEscalationAdapter) Get(ctx context.Context, id string) (*store.EscalationRequest, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid escalation ID: %w", err)
	}
	pgEscalation, err := a.pg.Get(ctx, uid)
	if err != nil {
		return nil, err
	}
	return pgEscalationToStore(pgEscalation), nil
}

func pgEscalationToStore(pg *database.EscalationRequest) *store.EscalationRequest {
	result := &store.EscalationRequest{
		ID:              pg.ID.String(),
		SessionID:       pg.SessionID,
		Action:          pg.Action,
		ActionType:      pg.ActionType,
		Target:          pg.Target,
		Reason:          pg.Reason,
		PolicyNames:     pg.PolicyNames,
		ContextSummary:  pg.ContextSummary,
		Status:          pg.Status,
		Priority:        pg.Priority,
		ApproverComment: pg.ApproverComment,
		RequestedAt:     pg.RequestedAt,
		ExpiresAt:       pg.ExpiresAt,
		ResolvedAt:      pg.ResolvedAt,
	}

	if pg.DecisionID != nil {
		result.DecisionID = pg.DecisionID.String()
	}
	if pg.ApproverID != nil {
		result.ApproverID = pg.ApproverID.String()
	}

	return result
}

// Compile-time interface satisfaction checks.
var (
	_ store.SessionStore    = (*PgSessionAdapter)(nil)
	_ store.DecisionStore   = (*PgDecisionAdapter)(nil)
	_ store.CommitStore     = (*PgCommitAdapter)(nil)
	_ store.EscalationStore = (*PgEscalationAdapter)(nil)
)

// timePtr is a helper to get a *time.Time from a time.Time.
func timePtr(t time.Time) *time.Time {
	return &t
}
