package governance

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/adp/adp/internal/sensitivepaths"
	"github.com/open-policy-agent/opa/rego"
)

// PolicyDefinition represents a policy from the database
type PolicyDefinition struct {
	ID            string
	Name          string
	Description   string
	Category      string
	Enabled       bool
	Priority      int
	PolicyType    string // "rego", "builtin", "custom"
	RegoCode      string
	BuiltinName   string
	Config        map[string]interface{}
	MinTrustLevel int
}

// PolicyStore interface for loading policies from database
type PolicyStore interface {
	ListEnabled(ctx context.Context) ([]*PolicyDefinition, error)
}

// EvaluationInput represents the input for policy evaluation
type EvaluationInput struct {
	SessionID  string           `json:"session_id"`
	UserID     string           `json:"user_id,omitempty"`
	TrustLevel int              `json:"trust_level"`
	Action     ActionEvalInput  `json:"action"`
	Context    ContextEvalInput `json:"context"`
	Session    SessionEvalInput `json:"session"`
}

// ActionEvalInput describes the action being evaluated
type ActionEvalInput struct {
	Type     string                 `json:"type"`
	Target   TargetEvalInput        `json:"target"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// TargetEvalInput describes the target of an action
type TargetEvalInput struct {
	Paths       []string `json:"paths,omitempty"`
	Services    []string `json:"services,omitempty"`
	Environment string   `json:"environment,omitempty"`
}

// ContextEvalInput provides environmental context
type ContextEvalInput struct {
	Environment string    `json:"environment,omitempty"`
	Time        time.Time `json:"time,omitempty"`
	Hour        int       `json:"hour,omitempty"`
}

// SessionEvalInput provides session context
type SessionEvalInput struct {
	TrustLevel int `json:"trust_level"`
}

// EvaluationResult represents the result of policy evaluation
type EvaluationResult struct {
	Allowed          bool     `json:"allowed"`
	RequiresApproval bool     `json:"requires_approval"`
	DeniedReasons    []string `json:"denied_reasons"`
	MatchedPolicies  []string `json:"matched_policies"`
	Warnings         []string `json:"warnings,omitempty"`
}

// UnifiedPolicyEngine combines database policies with OPA evaluation
type UnifiedPolicyEngine struct {
	policyStore     PolicyStore
	basePolicyPath  string
	builtinPolicies map[string]BuiltinEvaluator
	mu              sync.RWMutex
	cachedPolicies  []*PolicyDefinition
	cacheExpiry     time.Time
	cacheDuration   time.Duration
	// behavioralChecker backs the require_behavioral_verification builtin
	// (#20). Nil disables the check (advisory builtin no-ops).
	behavioralChecker func(commitSHA string) (bool, string)
}

// BuiltinEvaluator is a function that evaluates a built-in policy
type BuiltinEvaluator func(input *EvaluationInput, config map[string]interface{}) (allowed bool, reason string)

// NewUnifiedPolicyEngine creates a new unified policy engine
func NewUnifiedPolicyEngine(policyStore PolicyStore, basePolicyPath string) *UnifiedPolicyEngine {
	engine := &UnifiedPolicyEngine{
		policyStore:     policyStore,
		basePolicyPath:  basePolicyPath,
		builtinPolicies: make(map[string]BuiltinEvaluator),
		cacheDuration:   5 * time.Minute,
	}

	// Register built-in policy evaluators
	engine.registerBuiltinPolicies()

	return engine
}

// registerBuiltinPolicies registers all built-in policy evaluators
func (e *UnifiedPolicyEngine) registerBuiltinPolicies() {
	// Deny sensitive files. The canonical pattern set and matching semantics
	// live in internal/sensitivepaths (single source of truth, mirrored in
	// policies/default.rego). Config "patterns" replaces the default set.
	e.builtinPolicies["deny_sensitive_files"] = func(input *EvaluationInput, config map[string]interface{}) (bool, string) {
		match := sensitivepaths.Match
		if configPatterns, ok := config["patterns"].([]interface{}); ok {
			patterns := make([]string, len(configPatterns))
			for i, p := range configPatterns {
				patterns[i] = fmt.Sprintf("%v", p)
			}
			match = func(p string) (bool, string) { return sensitivepaths.MatchWith(patterns, p) }
		}

		for _, path := range input.Action.Target.Paths {
			if blocked, pattern := match(path); blocked {
				return false, fmt.Sprintf("access to sensitive file '%s' blocked by pattern '%s'", path, pattern)
			}
		}
		return true, ""
	}

	// Behavioral verification (#20): advisory feedback when an agent checks a
	// commit-finalizing action. The head SHA is only known post-commit, so
	// absence is allowed here — the merge gate enforces the requirement
	// server-side via verification.GateVerifier.
	e.builtinPolicies["require_behavioral_verification"] = func(input *EvaluationInput, config map[string]interface{}) (bool, string) {
		checker := e.behavioralChecker
		if checker == nil {
			return true, ""
		}
		switch input.Action.Type {
		case "verify_commit", "push", "merge":
		default:
			return true, ""
		}
		sha, _ := input.Action.Metadata["commit_sha"].(string)
		if sha == "" {
			return true, "" // head SHA unknown pre-commit; gate enforces later
		}
		return checker(sha)
	}

	// Blast radius limit
	e.builtinPolicies["blast_radius_limit"] = func(input *EvaluationInput, config map[string]interface{}) (bool, string) {
		maxFiles := 10
		trustOverride := 4

		if v, ok := config["max_files"].(float64); ok {
			maxFiles = int(v)
		}
		if v, ok := config["trust_level_override"].(float64); ok {
			trustOverride = int(v)
		}

		// Trust level override
		if input.TrustLevel >= trustOverride {
			return true, ""
		}

		if len(input.Action.Target.Paths) > maxFiles {
			return false, fmt.Sprintf("blast radius exceeded: %d files modified (max %d for trust level %d)",
				len(input.Action.Target.Paths), maxFiles, input.TrustLevel)
		}
		return true, ""
	}

	// Off-hours production
	e.builtinPolicies["off_hours_production"] = func(input *EvaluationInput, config map[string]interface{}) (bool, string) {
		startHour := 22
		endHour := 6
		minTrust := 5

		if v, ok := config["start_hour"].(float64); ok {
			startHour = int(v)
		}
		if v, ok := config["end_hour"].(float64); ok {
			endHour = int(v)
		}
		if v, ok := config["min_trust_level"].(float64); ok {
			minTrust = int(v)
		}

		// Only applies to production deployments
		if input.Action.Type != "deploy" || input.Action.Target.Environment != "production" {
			return true, ""
		}

		// Check trust level override
		if input.TrustLevel >= minTrust {
			return true, ""
		}

		hour := input.Context.Hour
		if hour == 0 {
			hour = time.Now().Hour()
		}

		// Check if in off-hours (handles wrap-around, e.g., 22:00 to 06:00)
		isOffHours := false
		if startHour > endHour {
			// Wraps around midnight
			isOffHours = hour >= startHour || hour < endHour
		} else {
			isOffHours = hour >= startHour && hour < endHour
		}

		if isOffHours {
			return false, fmt.Sprintf("production deployments blocked during off-hours (%d:00-%d:00) for trust level %d",
				startHour, endHour, input.TrustLevel)
		}
		return true, ""
	}

	// Cost limit
	e.builtinPolicies["cost_limit"] = func(input *EvaluationInput, config map[string]interface{}) (bool, string) {
		// Cost limits by trust level
		limits := map[int]float64{
			1: 10,
			2: 50,
			3: 200,
			4: 1000,
			5: 10000,
		}

		if configLimits, ok := config["limits_by_trust"].(map[string]interface{}); ok {
			for k, v := range configLimits {
				var level int
				fmt.Sscanf(k, "%d", &level)
				if val, ok := v.(float64); ok {
					limits[level] = val
				}
			}
		}

		// Get estimated cost from metadata
		estimatedCost := 0.0
		if cost, ok := input.Action.Metadata["estimated_cost"].(float64); ok {
			estimatedCost = cost
		}

		if estimatedCost <= 0 {
			return true, "" // No cost specified
		}

		limit, exists := limits[input.TrustLevel]
		if !exists {
			limit = 10 // Default to lowest
		}

		if estimatedCost > limit {
			return false, fmt.Sprintf("estimated cost $%.2f exceeds limit $%.2f for trust level %d",
				estimatedCost, limit, input.TrustLevel)
		}
		return true, ""
	}

	// Require migration approval
	e.builtinPolicies["require_migration_approval"] = func(input *EvaluationInput, config map[string]interface{}) (bool, string) {
		actionTypes := []string{"migrate_database", "alter_schema"}
		if configTypes, ok := config["action_types"].([]interface{}); ok {
			actionTypes = make([]string, len(configTypes))
			for i, t := range configTypes {
				actionTypes[i] = fmt.Sprintf("%v", t)
			}
		}

		for _, actionType := range actionTypes {
			if input.Action.Type == actionType {
				return false, fmt.Sprintf("action '%s' requires human approval", actionType)
			}
		}
		return true, ""
	}

	// Rate limit API calls
	e.builtinPolicies["rate_limit_api"] = func(input *EvaluationInput, config map[string]interface{}) (bool, string) {
		// This is a placeholder - actual rate limiting would need state tracking
		// For now, just return allowed
		return true, ""
	}
}

// SetBehavioralChecker installs the checker used by the
// require_behavioral_verification builtin. The checker answers: does this
// commit SHA carry a passed attestation from an independent runner?
func (e *UnifiedPolicyEngine) SetBehavioralChecker(fn func(commitSHA string) (bool, string)) {
	e.behavioralChecker = fn
}

// Evaluate evaluates all enabled policies against the input
func (e *UnifiedPolicyEngine) Evaluate(ctx context.Context, input *EvaluationInput) (*EvaluationResult, error) {
	result := &EvaluationResult{
		Allowed:         true,
		DeniedReasons:   []string{},
		MatchedPolicies: []string{},
		Warnings:        []string{},
	}

	// Load policies from database (with caching)
	policies, err := e.loadPolicies(ctx)
	if err != nil {
		// Log error but continue with base policy
		result.Warnings = append(result.Warnings, "failed to load database policies: "+err.Error())
	}

	// Evaluate each enabled policy
	for _, policy := range policies {
		if !policy.Enabled {
			continue
		}

		// Check minimum trust level
		if input.TrustLevel < policy.MinTrustLevel {
			continue // Policy doesn't apply to this trust level
		}

		var allowed bool
		var reason string

		switch policy.PolicyType {
		case "builtin":
			allowed, reason = e.evaluateBuiltin(policy, input)
		case "rego":
			allowed, reason, err = e.evaluateRego(ctx, policy, input)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("rego evaluation error for %s: %v", policy.Name, err))
				continue
			}
		case "custom":
			// Custom policies could be implemented as webhooks or other mechanisms
			continue
		}

		result.MatchedPolicies = append(result.MatchedPolicies, policy.Name)

		if !allowed {
			result.Allowed = false
			result.DeniedReasons = append(result.DeniedReasons, reason)
		}
	}

	// Also evaluate base Rego policy if it exists
	if e.basePolicyPath != "" {
		baseAllowed, err := e.evaluateBasePolicy(ctx, input)
		if err != nil {
			result.Warnings = append(result.Warnings, "base policy evaluation error: "+err.Error())
		} else if !baseAllowed {
			result.Allowed = false
			result.DeniedReasons = append(result.DeniedReasons, "denied by base policy")
			result.MatchedPolicies = append(result.MatchedPolicies, "base_policy")
		}
	}

	// If denied, check if escalation is possible
	if !result.Allowed {
		result.RequiresApproval = true
	}

	return result, nil
}

func (e *UnifiedPolicyEngine) loadPolicies(ctx context.Context) ([]*PolicyDefinition, error) {
	e.mu.RLock()
	if time.Now().Before(e.cacheExpiry) && e.cachedPolicies != nil {
		policies := e.cachedPolicies
		e.mu.RUnlock()
		return policies, nil
	}
	e.mu.RUnlock()

	// Need to refresh cache
	if e.policyStore == nil {
		return nil, nil
	}

	policies, err := e.policyStore.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}

	e.mu.Lock()
	e.cachedPolicies = policies
	e.cacheExpiry = time.Now().Add(e.cacheDuration)
	e.mu.Unlock()

	return policies, nil
}

func (e *UnifiedPolicyEngine) evaluateBuiltin(policy *PolicyDefinition, input *EvaluationInput) (bool, string) {
	evaluator, exists := e.builtinPolicies[policy.BuiltinName]
	if !exists {
		return true, "" // Unknown builtin, allow by default
	}

	return evaluator(input, policy.Config)
}

func (e *UnifiedPolicyEngine) evaluateRego(ctx context.Context, policy *PolicyDefinition, input *EvaluationInput) (bool, string, error) {
	if policy.RegoCode == "" {
		return true, "", nil
	}

	// Build Rego query
	r := rego.New(
		rego.Query("data.policy.deny"),
		rego.Module("policy.rego", policy.RegoCode),
		rego.Input(map[string]interface{}{
			"session_id":  input.SessionID,
			"trust_level": input.TrustLevel,
			"action": map[string]interface{}{
				"type": input.Action.Type,
				"target": map[string]interface{}{
					"paths":       input.Action.Target.Paths,
					"services":    input.Action.Target.Services,
					"environment": input.Action.Target.Environment,
				},
			},
			"context": map[string]interface{}{
				"environment": input.Context.Environment,
				"hour":        input.Context.Hour,
			},
			"session": map[string]interface{}{
				"trust_level": input.Session.TrustLevel,
			},
		}),
	)

	rs, err := r.Eval(ctx)
	if err != nil {
		return false, "", fmt.Errorf("rego eval: %w", err)
	}

	// Check for deny results
	for _, result := range rs {
		for _, expr := range result.Expressions {
			if set, ok := expr.Value.([]interface{}); ok && len(set) > 0 {
				// Policy denied - extract reason
				if reason, ok := set[0].(string); ok {
					return false, reason, nil
				}
				return false, fmt.Sprintf("denied by policy %s", policy.Name), nil
			}
		}
	}

	return true, "", nil
}

func (e *UnifiedPolicyEngine) evaluateBasePolicy(ctx context.Context, input *EvaluationInput) (bool, error) {
	r := rego.New(
		rego.Query("data.adp.governance.allow"),
		rego.Load([]string{e.basePolicyPath}, nil),
		rego.Input(map[string]interface{}{
			"action": map[string]interface{}{
				"type": input.Action.Type,
				"target": map[string]interface{}{
					"paths": input.Action.Target.Paths,
				},
			},
			"session": map[string]interface{}{
				"trust_level": input.TrustLevel,
			},
			"context": map[string]interface{}{
				"environment": input.Context.Environment,
			},
		}),
	)

	rs, err := r.Eval(ctx)
	if err != nil {
		return false, err
	}

	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return false, nil // No result = deny
	}

	if allowed, ok := rs[0].Expressions[0].Value.(bool); ok {
		return allowed, nil
	}

	return false, nil
}

// InvalidateCache forces a policy reload on next evaluation
func (e *UnifiedPolicyEngine) InvalidateCache() {
	e.mu.Lock()
	e.cacheExpiry = time.Time{}
	e.mu.Unlock()
}
