package agent

import (
	"time"

	"github.com/google/uuid"
)

// TrustLevel represents the level of trust assigned to an agent
type TrustLevel int

const (
	TrustLevelObserver    TrustLevel = 1
	TrustLevelContributor TrustLevel = 2
	TrustLevelDeveloper   TrustLevel = 3
	TrustLevelMaintainer  TrustLevel = 4
	TrustLevelAdmin       TrustLevel = 5
)

// AgentSession represents an active session for an AI agent
type AgentSession struct {
	ID             string      `json:"id"`
	OrganizationID uuid.UUID   `json:"organization_id"`
	UserID         uuid.UUID   `json:"user_id"`
	Tool           string      `json:"tool"`
	TrustLevel     TrustLevel  `json:"trust_level"`
	Capabilities   []string    `json:"capabilities"`
	Constraints    []string    `json:"constraints"`
	ServiceScope   []uuid.UUID `json:"service_scope"`
	Status         string      `json:"status"`
	StartedAt      time.Time   `json:"started_at"`
	ExpiresAt      time.Time   `json:"expires_at"`
}
