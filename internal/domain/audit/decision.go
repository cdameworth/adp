package audit

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// DecisionRecord represents a recorded decision made by an agent
type DecisionRecord struct {
	ID              uuid.UUID       `json:"id"`
	SessionID       string          `json:"session_id"`
	DecisionType    string          `json:"decision_type"`
	Action          string          `json:"action"`
	Target          json.RawMessage `json:"target"`
	Reasoning       json.RawMessage `json:"reasoning"`
	Confidence      float64         `json:"confidence"`
	Alternatives    json.RawMessage `json:"alternatives"`
	ContextSnapshot json.RawMessage `json:"context_snapshot"`
	PolicyResult    json.RawMessage `json:"policy_result"`
	PolicyID        string          `json:"policy_id"`
	UserID          string          `json:"user_id"`
	Status          string          `json:"status"`
	Outcome         json.RawMessage `json:"outcome"`
	CreatedAt       time.Time       `json:"created_at"`
}
