// Package store defines database-agnostic types and interfaces for ADP data persistence.
// These types use basic Go types (string for UUIDs, []string for arrays) to avoid
// coupling to any specific database driver (e.g., lib/pq, uuid.UUID).
package store

import "time"

// Session represents an agent session.
type Session struct {
	ID             string         `json:"id"`
	OrganizationID string         `json:"organization_id"`
	UserID         string         `json:"user_id"`
	Tool           string         `json:"tool"`
	TrustLevel     int            `json:"trust_level"`
	Capabilities   []string       `json:"capabilities"`
	Constraints    []string       `json:"constraints"`
	ServiceScope   []string       `json:"service_scope"`
	Status         string         `json:"status"`
	StartedAt      time.Time      `json:"started_at"`
	ExpiresAt      time.Time      `json:"expires_at"`
	LastHeartbeat  *time.Time     `json:"last_heartbeat,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	TokenHash      string         `json:"token_hash,omitempty"`
}

// CreateSessionInput contains the input for creating a session.
type CreateSessionInput struct {
	ID             string
	OrganizationID string
	UserID         string
	Tool           string
	TrustLevel     int
	Capabilities   []string
	Constraints    []string
	ServiceScope   []string
	ExpiresAt      time.Time
	Metadata       map[string]any
	TokenHash      string
}

// DecisionRecord represents a decision made by an agent.
type DecisionRecord struct {
	ID              string         `json:"id"`
	SessionID       string         `json:"session_id"`
	DecisionType    string         `json:"decision_type"`
	Action          string         `json:"action"`
	Target          map[string]any `json:"target"`
	Reasoning       map[string]any `json:"reasoning"`
	Confidence      float64        `json:"confidence"`
	Alternatives    []Alternative  `json:"alternatives"`
	ContextSnapshot map[string]any `json:"context_snapshot"`
	PolicyResult    *PolicyResult  `json:"policy_result,omitempty"`
	Status          string         `json:"status"`
	Outcome         map[string]any `json:"outcome,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
}

// Alternative represents an alternative action considered by the agent.
type Alternative struct {
	Action     string  `json:"action"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

// PolicyResult represents the result of policy evaluation.
type PolicyResult struct {
	Allowed       bool     `json:"allowed"`
	DeniedReasons []string `json:"denied_reasons,omitempty"`
	PolicyNames   []string `json:"policy_names"`
	EvaluatedAt   string   `json:"evaluated_at"`
}

// CreateDecisionInput contains the input for creating a decision record.
type CreateDecisionInput struct {
	SessionID       string
	DecisionType    string
	Action          string
	Target          map[string]any
	Reasoning       map[string]any
	Confidence      float64
	Alternatives    []Alternative
	ContextSnapshot map[string]any
	PolicyResult    *PolicyResult
}

// CommitRecord represents a prepared or verified commit.
type CommitRecord struct {
	ID             string     `json:"id"`
	SessionID      string     `json:"session_id"`
	CommitSHA      string     `json:"commit_sha,omitempty"`
	CommitToken    string     `json:"commit_token"`
	Files          []string   `json:"files"`
	Message        string     `json:"message,omitempty"`
	Status         string     `json:"status"`
	Approved       bool       `json:"approved"`
	ApprovalReason string     `json:"approval_reason,omitempty"`
	PreparedAt     time.Time  `json:"prepared_at"`
	CommittedAt    *time.Time `json:"committed_at,omitempty"`
	VerifiedAt     *time.Time `json:"verified_at,omitempty"`
}

// PrepareCommitInput contains the input for preparing a commit.
type PrepareCommitInput struct {
	SessionID string
	Files     []string
	Message   string
}

// EscalationRequest represents a request for human approval.
type EscalationRequest struct {
	ID              string         `json:"id"`
	SessionID       string         `json:"session_id"`
	DecisionID      string         `json:"decision_id,omitempty"`
	Action          string         `json:"action"`
	ActionType      string         `json:"action_type"`
	Target          map[string]any `json:"target"`
	Reason          string         `json:"reason"`
	PolicyNames     []string       `json:"policy_names"`
	ContextSummary  map[string]any `json:"context_summary"`
	Status          string         `json:"status"`
	Priority        string         `json:"priority"`
	ApproverID      string         `json:"approver_id,omitempty"`
	ApproverComment string         `json:"approver_comment,omitempty"`
	RequestedAt     time.Time      `json:"requested_at"`
	ExpiresAt       *time.Time     `json:"expires_at,omitempty"`
	ResolvedAt      *time.Time     `json:"resolved_at,omitempty"`
}

// CreateEscalationInput contains the input for creating an escalation request.
type CreateEscalationInput struct {
	SessionID      string
	DecisionID     string
	Action         string
	ActionType     string
	Target         map[string]any
	Reason         string
	PolicyNames    []string
	ContextSummary map[string]any
	Priority       string
	ExpiresAt      *time.Time
}

// User represents a user account.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CreateUserInput contains the input for creating a user.
type CreateUserInput struct {
	Email        string
	Name         string
	PasswordHash string
	Role         string // "admin" or "user"
}

// UpdateUserInput contains optional fields for updating a user.
type UpdateUserInput struct {
	Name         *string
	Role         *string
	Status       *string
	PasswordHash *string
}

// DocRecord represents a documentation artifact generated by the doc agent.
type DocRecord struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id,omitempty"`
	Category  string         `json:"category"`
	Title     string         `json:"title"`
	Content   string         `json:"content"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}
