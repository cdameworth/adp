package store

import "context"

// SessionStore provides persistence for agent sessions.
type SessionStore interface {
	Create(ctx context.Context, input CreateSessionInput) (*Session, error)
	Get(ctx context.Context, id string) (*Session, error)
	Heartbeat(ctx context.Context, id string) error
	End(ctx context.Context, id string) error
	// ListEnded returns sessions with status "ended" that completed after the given cursor.
	// Used by the documentation agent to find sessions needing doc generation.
	ListEnded(ctx context.Context, afterID string, limit int) ([]*Session, error)
	// ValidateToken checks if the given token hash belongs to an active session.
	ValidateToken(ctx context.Context, sessionID, tokenHash string) (bool, error)
}

// DecisionStore provides persistence for agent decision records.
type DecisionStore interface {
	Create(ctx context.Context, input CreateDecisionInput) (*DecisionRecord, error)
	Get(ctx context.Context, id string) (*DecisionRecord, error)
	GetLineage(ctx context.Context, id string, depth int) ([]*DecisionRecord, error)
	ListBySession(ctx context.Context, sessionID string) ([]*DecisionRecord, error)
}

// CommitStore provides persistence for commit preparation and verification.
type CommitStore interface {
	Prepare(ctx context.Context, input PrepareCommitInput) (*CommitRecord, error)
	RegisterCommit(ctx context.Context, token string, sha string) (*CommitRecord, error)
	IsCommitVerified(ctx context.Context, sha string) (bool, error)
}

// EscalationStore provides persistence for human approval requests.
type EscalationStore interface {
	Create(ctx context.Context, input CreateEscalationInput) (*EscalationRequest, error)
	Get(ctx context.Context, id string) (*EscalationRequest, error)
}

// UserStore provides persistence for user accounts.
type UserStore interface {
	Create(ctx context.Context, input CreateUserInput) (*User, error)
	GetByID(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	Update(ctx context.Context, id string, input UpdateUserInput) (*User, error)
	List(ctx context.Context, limit, offset int) ([]*User, int, error)
	Delete(ctx context.Context, id string) error // soft-delete: sets status=disabled
}

// DocStore provides persistence for documentation artifacts.
type DocStore interface {
	Save(ctx context.Context, doc DocRecord) error
	Get(ctx context.Context, id string) (*DocRecord, error)
	ListByCategory(ctx context.Context, category string, limit int) ([]*DocRecord, error)
	ListBySession(ctx context.Context, sessionID string) ([]*DocRecord, error)
	Search(ctx context.Context, query string, limit int) ([]*DocRecord, error)
}
