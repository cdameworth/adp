package governance

import (
	"time"

	"github.com/google/uuid"
)

// EscalationStatus represents the status of an escalation request
type EscalationStatus string

const (
	EscalationPending  EscalationStatus = "pending"
	EscalationApproved EscalationStatus = "approved"
	EscalationRejected EscalationStatus = "rejected"
	EscalationExpired  EscalationStatus = "expired"
)

// EscalationRequest represents a request for human approval
type EscalationRequest struct {
	ID          uuid.UUID        `json:"id"`
	SessionID   string           `json:"session_id"`
	Action      string           `json:"action"`
	Reason      string           `json:"reason"`
	RequestedAt time.Time        `json:"requested_at"`
	Status      EscalationStatus `json:"status"`
	ApproverID  *uuid.UUID       `json:"approver_id,omitempty"`
	Comment     string           `json:"comment,omitempty"`
}

// EscalationManager handles the lifecycle of escalation requests
type EscalationManager struct {
	requests map[uuid.UUID]*EscalationRequest
}

// NewEscalationManager creates a new EscalationManager
func NewEscalationManager() *EscalationManager {
	return &EscalationManager{
		requests: make(map[uuid.UUID]*EscalationRequest),
	}
}

// CreateRequest creates a new escalation request
func (m *EscalationManager) CreateRequest(sessionID, action, reason string) (*EscalationRequest, error) {
	req := &EscalationRequest{
		ID:          uuid.New(),
		SessionID:   sessionID,
		Action:      action,
		Reason:      reason,
		RequestedAt: time.Now(),
		Status:      EscalationPending,
	}
	m.requests[req.ID] = req
	return req, nil
}

// ApproveRequest approves a pending request
func (m *EscalationManager) ApproveRequest(id uuid.UUID, approverID uuid.UUID, comment string) error {
	req, ok := m.requests[id]
	if !ok {
		return nil // or error not found
	}
	req.Status = EscalationApproved
	req.ApproverID = &approverID
	req.Comment = comment
	return nil
}

// RejectRequest rejects a pending request
func (m *EscalationManager) RejectRequest(id uuid.UUID, approverID uuid.UUID, comment string) error {
	req, ok := m.requests[id]
	if !ok {
		return nil // or error not found
	}
	req.Status = EscalationRejected
	req.ApproverID = &approverID
	req.Comment = comment
	return nil
}
