package governance

import (
	"github.com/adp/adp/internal/domain/agent"
)

// AutonomyLevel defines the degree of freedom an agent has
type AutonomyLevel int

const (
	AutonomyNone        AutonomyLevel = 0
	AutonomyProposeOnly AutonomyLevel = 1
	AutonomyExecuteSafe AutonomyLevel = 2
	AutonomyFull        AutonomyLevel = 3
)

// Constraint represents a restriction on agent actions
type Constraint struct {
	Type        string                 `json:"type"`
	Parameters  map[string]interface{} `json:"parameters"`
	Description string                 `json:"description"`
}

// AutonomyEngine determines allowed actions
type AutonomyEngine struct {
	// In a real system, this might load rules from a DB or config
}

// NewAutonomyEngine creates a new AutonomyEngine
func NewAutonomyEngine() *AutonomyEngine {
	return &AutonomyEngine{}
}

// DetermineAutonomy calculates the autonomy level for a given session
func (e *AutonomyEngine) DetermineAutonomy(session agent.AgentSession) AutonomyLevel {
	switch session.TrustLevel {
	case agent.TrustLevelObserver:
		return AutonomyNone
	case agent.TrustLevelContributor:
		return AutonomyProposeOnly
	case agent.TrustLevelDeveloper:
		return AutonomyExecuteSafe
	case agent.TrustLevelMaintainer, agent.TrustLevelAdmin:
		return AutonomyFull
	default:
		return AutonomyNone
	}
}

// CheckConstraint checks if an action violates a specific constraint
func (e *AutonomyEngine) CheckConstraint(constraint Constraint, action string, target interface{}) bool {
	// Placeholder for constraint checking logic
	// e.g., if constraint.Type == "read_only" && action == "write" -> return false
	if constraint.Type == "read_only" && (action == "write" || action == "delete" || action == "modify") {
		return false
	}
	return true
}
