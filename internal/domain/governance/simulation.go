package governance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Simulation errors
var (
	ErrSimulationFailed      = errors.New("policy simulation failed")
	ErrPolicyVersionNotFound = errors.New("policy version not found")
)

// PolicySimulator provides dry-run policy evaluation
type PolicySimulator struct {
	engine    *AdvancedPolicyEngine
	opaEngine *OPAEngine
	versions  map[string]*PolicyVersion
	mu        sync.RWMutex
}

// PolicyVersion represents a versioned policy
type PolicyVersion struct {
	ID          string    `json:"id"`
	Version     string    `json:"version"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Content     string    `json:"content"` // Rego content
	Status      string    `json:"status"`  // draft, active, deprecated
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	CreatedBy   string    `json:"created_by"`
}

// SimulationRequest represents a request for policy simulation
type SimulationRequest struct {
	// Input to evaluate
	Input PolicyInput `json:"input"`

	// Optional: specific policy version to test
	PolicyVersionID string `json:"policy_version_id,omitempty"`

	// Optional: inline policy to test
	InlinePolicy string `json:"inline_policy,omitempty"`

	// Compare with production policy
	CompareWithProduction bool `json:"compare_with_production"`

	// Include detailed trace
	IncludeTrace bool `json:"include_trace"`
}

// SimulationResult contains the results of policy simulation
type SimulationResult struct {
	// Main evaluation result
	Result *PolicyEvaluationResult `json:"result"`

	// Production comparison (if requested)
	ProductionResult *PolicyEvaluationResult `json:"production_result,omitempty"`

	// Differences from production
	Differences []PolicyDifference `json:"differences,omitempty"`

	// Execution trace (if requested)
	Trace []TraceEntry `json:"trace,omitempty"`

	// Policy version used
	PolicyVersion string `json:"policy_version,omitempty"`

	// Simulation metadata
	SimulatedAt time.Time     `json:"simulated_at"`
	Duration    time.Duration `json:"duration"`

	// Warnings about the simulation
	Warnings []string `json:"warnings,omitempty"`
}

// PolicyDifference represents a difference between policy evaluations
type PolicyDifference struct {
	Type        string      `json:"type"` // "allowed_changed", "denial_added", "denial_removed", "warning_changed"
	Description string      `json:"description"`
	OldValue    interface{} `json:"old_value,omitempty"`
	NewValue    interface{} `json:"new_value,omitempty"`
}

// TraceEntry represents a step in policy evaluation
type TraceEntry struct {
	Timestamp  time.Time     `json:"timestamp"`
	PolicyName string        `json:"policy_name"`
	Action     string        `json:"action"`
	Input      interface{}   `json:"input,omitempty"`
	Output     interface{}   `json:"output,omitempty"`
	Duration   time.Duration `json:"duration"`
}

// NewPolicySimulator creates a new policy simulator
func NewPolicySimulator(engine *AdvancedPolicyEngine, opaEngine *OPAEngine) *PolicySimulator {
	return &PolicySimulator{
		engine:    engine,
		opaEngine: opaEngine,
		versions:  make(map[string]*PolicyVersion),
	}
}

// Simulate performs a dry-run policy evaluation
func (s *PolicySimulator) Simulate(ctx context.Context, req SimulationRequest) (*SimulationResult, error) {
	startTime := time.Now()

	result := &SimulationResult{
		SimulatedAt: startTime,
		Warnings:    make([]string, 0),
		Trace:       make([]TraceEntry, 0),
	}

	// Evaluate with current/specified policy
	evalResult, err := s.evaluateWithTrace(ctx, req.Input, req.IncludeTrace, result)
	if err != nil {
		return nil, err
	}
	result.Result = evalResult

	// Compare with production if requested
	if req.CompareWithProduction && req.PolicyVersionID != "" {
		prodResult, err := s.engine.Evaluate(ctx, req.Input)
		if err == nil {
			result.ProductionResult = prodResult
			result.Differences = s.computeDifferences(prodResult, evalResult)
		}
	}

	result.Duration = time.Since(startTime)
	return result, nil
}

func (s *PolicySimulator) evaluateWithTrace(ctx context.Context, input PolicyInput, includeTrace bool, result *SimulationResult) (*PolicyEvaluationResult, error) {
	if !includeTrace {
		return s.engine.Evaluate(ctx, input)
	}

	// Initialize trace result (will be populated during evaluation)
	_ = &PolicyEvaluationResult{
		Allowed:       true,
		DeniedReasons: make([]string, 0),
		Warnings:      make([]string, 0),
		Metadata:      make(map[string]interface{}),
		EvaluatedAt:   time.Now(),
	}

	// Trace time policies
	traceStart := time.Now()
	result.Trace = append(result.Trace, TraceEntry{
		Timestamp:  traceStart,
		PolicyName: "time_policies",
		Action:     "evaluate",
		Input:      map[string]interface{}{"time": input.Context.Time, "environment": input.Context.Environment},
	})

	// Evaluate (simplified trace - actual implementation would hook deeper)
	fullResult, err := s.engine.Evaluate(ctx, input)
	if err != nil {
		return nil, err
	}

	result.Trace = append(result.Trace, TraceEntry{
		Timestamp:  time.Now(),
		PolicyName: "full_evaluation",
		Action:     "complete",
		Output:     fullResult,
		Duration:   time.Since(traceStart),
	})

	return fullResult, nil
}

func (s *PolicySimulator) computeDifferences(old, new *PolicyEvaluationResult) []PolicyDifference {
	differences := make([]PolicyDifference, 0)

	// Check allowed status
	if old.Allowed != new.Allowed {
		differences = append(differences, PolicyDifference{
			Type:        "allowed_changed",
			Description: "Policy decision changed",
			OldValue:    old.Allowed,
			NewValue:    new.Allowed,
		})
	}

	// Check denied reasons
	oldReasons := make(map[string]bool)
	for _, r := range old.DeniedReasons {
		oldReasons[r] = true
	}

	for _, r := range new.DeniedReasons {
		if !oldReasons[r] {
			differences = append(differences, PolicyDifference{
				Type:        "denial_added",
				Description: "New denial reason added",
				NewValue:    r,
			})
		}
	}

	newReasons := make(map[string]bool)
	for _, r := range new.DeniedReasons {
		newReasons[r] = true
	}

	for _, r := range old.DeniedReasons {
		if !newReasons[r] {
			differences = append(differences, PolicyDifference{
				Type:        "denial_removed",
				Description: "Denial reason removed",
				OldValue:    r,
			})
		}
	}

	return differences
}

// RegisterPolicyVersion registers a new policy version
func (s *PolicySimulator) RegisterPolicyVersion(version *PolicyVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.versions[version.ID] = version
	return nil
}

// GetPolicyVersion retrieves a policy version
func (s *PolicySimulator) GetPolicyVersion(id string) (*PolicyVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	version, ok := s.versions[id]
	if !ok {
		return nil, ErrPolicyVersionNotFound
	}
	return version, nil
}

// ListPolicyVersions returns all registered policy versions
func (s *PolicySimulator) ListPolicyVersions() []*PolicyVersion {
	s.mu.RLock()
	defer s.mu.RUnlock()
	versions := make([]*PolicyVersion, 0, len(s.versions))
	for _, v := range s.versions {
		versions = append(versions, v)
	}
	return versions
}

// BatchSimulate performs simulation on multiple inputs
func (s *PolicySimulator) BatchSimulate(ctx context.Context, inputs []PolicyInput) ([]*SimulationResult, error) {
	results := make([]*SimulationResult, len(inputs))
	for i, input := range inputs {
		result, err := s.Simulate(ctx, SimulationRequest{Input: input})
		if err != nil {
			return nil, err
		}
		results[i] = result
	}
	return results, nil
}

// GraduatedAutonomyManager manages the graduated autonomy workflow
type GraduatedAutonomyManager struct {
	engine           *AdvancedPolicyEngine
	trustProgression TrustProgression
	mu               sync.RWMutex
}

// TrustProgression defines the criteria for trust level advancement
type TrustProgression struct {
	Levels []TrustLevelRequirements `json:"levels"`
}

// TrustLevelRequirements defines what's needed to reach a trust level
type TrustLevelRequirements struct {
	Level                int           `json:"level"`
	Name                 string        `json:"name"`
	MinSessionsCompleted int           `json:"min_sessions_completed"`
	MinDecisions         int           `json:"min_decisions"`
	MaxEscalationRate    float64       `json:"max_escalation_rate"` // 0.0-1.0
	MinConfidenceAvg     float64       `json:"min_confidence_avg"`  // 0.0-1.0
	RequiresApproval     bool          `json:"requires_approval"`
	CooldownPeriod       time.Duration `json:"cooldown_period"`
	AllowedActions       []string      `json:"allowed_actions"`
	RestrictedActions    []string      `json:"restricted_actions"`
}

// DefaultTrustProgression returns the default trust progression
func DefaultTrustProgression() TrustProgression {
	return TrustProgression{
		Levels: []TrustLevelRequirements{
			{
				Level:                1,
				Name:                 "Observer",
				MinSessionsCompleted: 0,
				MinDecisions:         0,
				MaxEscalationRate:    1.0,
				MinConfidenceAvg:     0.0,
				RequiresApproval:     false,
				AllowedActions:       []string{"read", "search", "analyze"},
				RestrictedActions:    []string{"write", "delete", "deploy", "modify"},
			},
			{
				Level:                2,
				Name:                 "Contributor",
				MinSessionsCompleted: 5,
				MinDecisions:         20,
				MaxEscalationRate:    0.5,
				MinConfidenceAvg:     0.6,
				RequiresApproval:     true,
				CooldownPeriod:       24 * time.Hour,
				AllowedActions:       []string{"read", "search", "analyze", "write_safe"},
				RestrictedActions:    []string{"delete", "deploy", "modify_config"},
			},
			{
				Level:                3,
				Name:                 "Developer",
				MinSessionsCompleted: 20,
				MinDecisions:         100,
				MaxEscalationRate:    0.3,
				MinConfidenceAvg:     0.7,
				RequiresApproval:     true,
				CooldownPeriod:       7 * 24 * time.Hour,
				AllowedActions:       []string{"read", "search", "analyze", "write", "test"},
				RestrictedActions:    []string{"deploy_production", "delete_critical"},
			},
			{
				Level:                4,
				Name:                 "Maintainer",
				MinSessionsCompleted: 50,
				MinDecisions:         500,
				MaxEscalationRate:    0.15,
				MinConfidenceAvg:     0.8,
				RequiresApproval:     true,
				CooldownPeriod:       30 * 24 * time.Hour,
				AllowedActions:       []string{"*"},
				RestrictedActions:    []string{"admin"},
			},
			{
				Level:                5,
				Name:                 "Admin",
				MinSessionsCompleted: 100,
				MinDecisions:         1000,
				MaxEscalationRate:    0.1,
				MinConfidenceAvg:     0.9,
				RequiresApproval:     true,
				CooldownPeriod:       90 * 24 * time.Hour,
				AllowedActions:       []string{"*"},
				RestrictedActions:    []string{},
			},
		},
	}
}

// NewGraduatedAutonomyManager creates a new graduated autonomy manager
func NewGraduatedAutonomyManager(engine *AdvancedPolicyEngine, progression *TrustProgression) *GraduatedAutonomyManager {
	if progression == nil {
		defaultProg := DefaultTrustProgression()
		progression = &defaultProg
	}
	return &GraduatedAutonomyManager{
		engine:           engine,
		trustProgression: *progression,
	}
}

// AgentMetrics represents an agent's performance metrics
type AgentMetrics struct {
	SessionsCompleted int       `json:"sessions_completed"`
	TotalDecisions    int       `json:"total_decisions"`
	EscalationCount   int       `json:"escalation_count"`
	AverageConfidence float64   `json:"average_confidence"`
	LastLevelChange   time.Time `json:"last_level_change"`
	CurrentLevel      int       `json:"current_level"`
}

// TrustEvaluation represents the result of trust level evaluation
type TrustEvaluation struct {
	CurrentLevel          int                     `json:"current_level"`
	RecommendedLevel      int                     `json:"recommended_level"`
	EligibleForPromotion  bool                    `json:"eligible_for_promotion"`
	MissingRequirements   []string                `json:"missing_requirements,omitempty"`
	NextLevelRequirements *TrustLevelRequirements `json:"next_level_requirements,omitempty"`
	Metrics               *AgentMetrics           `json:"metrics"`
	EvaluatedAt           time.Time               `json:"evaluated_at"`
}

// EvaluateTrustLevel evaluates an agent's eligibility for trust level changes
func (m *GraduatedAutonomyManager) EvaluateTrustLevel(ctx context.Context, metrics *AgentMetrics) (*TrustEvaluation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	eval := &TrustEvaluation{
		CurrentLevel:         metrics.CurrentLevel,
		RecommendedLevel:     metrics.CurrentLevel,
		EligibleForPromotion: false,
		MissingRequirements:  make([]string, 0),
		Metrics:              metrics,
		EvaluatedAt:          time.Now(),
	}

	// Find current and next level requirements
	var nextReqs *TrustLevelRequirements
	for i, level := range m.trustProgression.Levels {
		if level.Level == metrics.CurrentLevel {
			if i+1 < len(m.trustProgression.Levels) {
				nextReqs = &m.trustProgression.Levels[i+1]
			}
			break
		}
	}

	if nextReqs == nil {
		// Already at max level
		return eval, nil
	}

	eval.NextLevelRequirements = nextReqs

	// Check cooldown
	if !metrics.LastLevelChange.IsZero() {
		cooldownEnd := metrics.LastLevelChange.Add(nextReqs.CooldownPeriod)
		if time.Now().Before(cooldownEnd) {
			eval.MissingRequirements = append(eval.MissingRequirements,
				fmt.Sprintf("cooldown period not elapsed (ends %s)", cooldownEnd.Format(time.RFC3339)))
			return eval, nil
		}
	}

	// Check sessions
	if metrics.SessionsCompleted < nextReqs.MinSessionsCompleted {
		eval.MissingRequirements = append(eval.MissingRequirements,
			fmt.Sprintf("need %d sessions (have %d)", nextReqs.MinSessionsCompleted, metrics.SessionsCompleted))
	}

	// Check decisions
	if metrics.TotalDecisions < nextReqs.MinDecisions {
		eval.MissingRequirements = append(eval.MissingRequirements,
			fmt.Sprintf("need %d decisions (have %d)", nextReqs.MinDecisions, metrics.TotalDecisions))
	}

	// Check escalation rate
	if metrics.TotalDecisions > 0 {
		escalationRate := float64(metrics.EscalationCount) / float64(metrics.TotalDecisions)
		if escalationRate > nextReqs.MaxEscalationRate {
			eval.MissingRequirements = append(eval.MissingRequirements,
				fmt.Sprintf("escalation rate %.2f exceeds max %.2f", escalationRate, nextReqs.MaxEscalationRate))
		}
	}

	// Check confidence
	if metrics.AverageConfidence < nextReqs.MinConfidenceAvg {
		eval.MissingRequirements = append(eval.MissingRequirements,
			fmt.Sprintf("confidence %.2f below min %.2f", metrics.AverageConfidence, nextReqs.MinConfidenceAvg))
	}

	// Determine eligibility
	if len(eval.MissingRequirements) == 0 {
		eval.EligibleForPromotion = true
		eval.RecommendedLevel = nextReqs.Level
	}

	return eval, nil
}

// GetLevelRequirements returns requirements for a specific level
func (m *GraduatedAutonomyManager) GetLevelRequirements(level int) (*TrustLevelRequirements, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, req := range m.trustProgression.Levels {
		if req.Level == level {
			return &req, nil
		}
	}
	return nil, fmt.Errorf("trust level %d not found", level)
}

// IsActionAllowed checks if an action is allowed at a trust level
func (m *GraduatedAutonomyManager) IsActionAllowed(level int, action string) (bool, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, req := range m.trustProgression.Levels {
		if req.Level == level {
			// Check restricted first
			for _, restricted := range req.RestrictedActions {
				if matchAction(action, restricted) {
					return false, fmt.Sprintf("action '%s' is restricted at trust level %d (%s)", action, level, req.Name)
				}
			}
			// Check allowed
			for _, allowed := range req.AllowedActions {
				if matchAction(action, allowed) {
					return true, ""
				}
			}
			return false, fmt.Sprintf("action '%s' not in allowed list for trust level %d (%s)", action, level, req.Name)
		}
	}
	return false, fmt.Sprintf("trust level %d not found", level)
}

// matchAction checks if an action matches a pattern (* is wildcard)
func matchAction(action, pattern string) bool {
	if pattern == "*" {
		return true
	}
	return action == pattern
}

// SerializeToJSON serializes the progression config to JSON
func (m *GraduatedAutonomyManager) SerializeToJSON() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return json.Marshal(m.trustProgression)
}

// UpdateProgression updates the trust progression configuration
func (m *GraduatedAutonomyManager) UpdateProgression(progression TrustProgression) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trustProgression = progression
}
