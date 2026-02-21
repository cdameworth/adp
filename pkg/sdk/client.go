// Package sdk provides a Go client for the ADP HTTP API.
//
// Usage:
//
//	client := sdk.NewClient("http://localhost:8080", sdk.WithBearerToken("jwt-token"))
//	session, err := client.CreateSession(ctx, sdk.CreateSessionRequest{...})
package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client communicates with the ADP HTTP API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithBearerToken sets the JWT bearer token for authentication.
func WithBearerToken(token string) Option {
	return func(c *Client) { c.token = token }
}

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// NewClient creates a new ADP API client.
func NewClient(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// --- Sessions ---

// CreateSessionRequest is the request body for POST /v1/sessions.
type CreateSessionRequest struct {
	OrganizationID string   `json:"organization_id"`
	UserID         string   `json:"user_id"`
	Tool           string   `json:"tool"`
	TrustLevel     int      `json:"trust_level,omitempty"`
	Capabilities   []string `json:"capabilities,omitempty"`
	ServiceScope   []string `json:"service_scope,omitempty"`
	TTLHours       int      `json:"ttl_hours,omitempty"`
}

// Session is the response from session endpoints.
type Session struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organization_id"`
	UserID         string     `json:"user_id"`
	Tool           string     `json:"tool"`
	TrustLevel     int        `json:"trust_level"`
	Capabilities   []string   `json:"capabilities"`
	Constraints    []string   `json:"constraints"`
	ServiceScope   []string   `json:"service_scope"`
	Status         string     `json:"status"`
	StartedAt      time.Time  `json:"started_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	LastHeartbeat  *time.Time `json:"last_heartbeat,omitempty"`
}

// CreateSession creates a new ADP session (POST /v1/sessions).
func (c *Client) CreateSession(ctx context.Context, req CreateSessionRequest) (*Session, error) {
	var session Session
	if err := c.post(ctx, "/v1/sessions", req, &session); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &session, nil
}

// EndSession closes an ADP session (DELETE /v1/sessions/{id}).
func (c *Client) EndSession(ctx context.Context, sessionID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/sessions/"+sessionID, nil, nil)
}

// Heartbeat sends a session heartbeat (PATCH /v1/sessions/{id}/heartbeat).
func (c *Client) Heartbeat(ctx context.Context, sessionID string) error {
	return c.do(ctx, http.MethodPatch, "/v1/sessions/"+sessionID+"/heartbeat", nil, nil)
}

// --- Governance ---

// CheckActionRequest is the request body for POST /v1/governance/check.
type CheckActionRequest struct {
	SessionID  string         `json:"session_id"`
	TrustLevel int            `json:"trust_level,omitempty"`
	ActionType string         `json:"action_type"`
	Target     map[string]any `json:"target,omitempty"`
	Context    map[string]any `json:"context,omitempty"`
}

// CheckActionResponse is the response from governance check.
type CheckActionResponse struct {
	Allowed          bool     `json:"allowed"`
	RequiresApproval bool     `json:"requires_approval,omitempty"`
	DeniedReasons    []string `json:"denied_reasons,omitempty"`
	PolicyNames      []string `json:"policy_names"`
	Warnings         []string `json:"warnings,omitempty"`
	Restrictions     []string `json:"restrictions,omitempty"`
}

// CheckAction evaluates whether an action is permitted by ADP policies (POST /v1/governance/check).
func (c *Client) CheckAction(ctx context.Context, req CheckActionRequest) (*CheckActionResponse, error) {
	var resp CheckActionResponse
	if err := c.post(ctx, "/v1/governance/check", req, &resp); err != nil {
		return nil, fmt.Errorf("check action: %w", err)
	}
	return &resp, nil
}

// --- Audit ---

// LogDecisionRequest is the request body for POST /v1/audit/decisions.
type LogDecisionRequest struct {
	SessionID       string         `json:"session_id"`
	DecisionType    string         `json:"decision_type"`
	Action          string         `json:"action"`
	Target          map[string]any `json:"target,omitempty"`
	Reasoning       map[string]any `json:"reasoning,omitempty"`
	Confidence      float64        `json:"confidence,omitempty"`
	Alternatives    []Alternative  `json:"alternatives,omitempty"`
	ContextSnapshot map[string]any `json:"context_snapshot,omitempty"`
}

// Alternative describes an alternative action considered during decision-making.
type Alternative struct {
	Action     string  `json:"action"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

// DecisionRecord is the response from decision logging.
type DecisionRecord struct {
	ID           string    `json:"id"`
	SessionID    string    `json:"session_id"`
	DecisionType string    `json:"decision_type"`
	Action       string    `json:"action"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

// LogDecision records a decision in ADP's audit trail (POST /v1/audit/decisions).
func (c *Client) LogDecision(ctx context.Context, req LogDecisionRequest) (*DecisionRecord, error) {
	var record DecisionRecord
	if err := c.post(ctx, "/v1/audit/decisions", req, &record); err != nil {
		return nil, fmt.Errorf("log decision: %w", err)
	}
	return &record, nil
}

// --- Commits ---

// PrepareCommitRequest is the request body for POST /v1/commits/prepare.
type PrepareCommitRequest struct {
	SessionID string   `json:"session_id"`
	Files     []string `json:"files"`
	Message   string   `json:"message"`
}

// PrepareCommitResponse is the response from commit preparation.
type PrepareCommitResponse struct {
	CommitToken string `json:"commit_token"`
	Approved    bool   `json:"approved"`
	Reason      string `json:"reason,omitempty"`
}

// PrepareCommit requests a commit token from ADP (POST /v1/commits/prepare).
func (c *Client) PrepareCommit(ctx context.Context, req PrepareCommitRequest) (*PrepareCommitResponse, error) {
	var resp PrepareCommitResponse
	if err := c.post(ctx, "/v1/commits/prepare", req, &resp); err != nil {
		return nil, fmt.Errorf("prepare commit: %w", err)
	}
	return &resp, nil
}

// --- HTTP helpers ---

func (c *Client) post(ctx context.Context, path string, body any, result any) error {
	return c.do(ctx, http.MethodPost, path, body, result)
}

func (c *Client) do(ctx context.Context, method, path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var apiErr ErrorResponse
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error != "" {
			return &APIError{
				StatusCode: resp.StatusCode,
				Message:    apiErr.Error,
				Code:       apiErr.Code,
			}
		}
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(respBody),
		}
	}

	// Unwrap {"data": ...} envelope
	if result != nil && len(respBody) > 0 {
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(respBody, &envelope); err != nil {
			return fmt.Errorf("unmarshal envelope: %w", err)
		}
		if len(envelope.Data) > 0 {
			if err := json.Unmarshal(envelope.Data, result); err != nil {
				return fmt.Errorf("unmarshal data: %w", err)
			}
		}
	}

	return nil
}

// ErrorResponse is ADP's error response shape.
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details any    `json:"details,omitempty"`
}

// APIError represents an HTTP error from the ADP API.
type APIError struct {
	StatusCode int
	Message    string
	Code       string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("ADP API error (HTTP %d, %s): %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("ADP API error (HTTP %d): %s", e.StatusCode, e.Message)
}
