package governance

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Policy evaluation errors
var (
	ErrPolicyViolation   = errors.New("policy violation")
	ErrTimeRestriction   = errors.New("time-based restriction")
	ErrFinancialLimit    = errors.New("financial limit exceeded")
	ErrInsufficientTrust = errors.New("insufficient trust level")
	ErrPolicyNotFound    = errors.New("policy not found")
)

// PolicyResult represents the result of policy evaluation
type PolicyEvaluationResult struct {
	Allowed       bool                   `json:"allowed"`
	PolicyName    string                 `json:"policy_name"`
	DeniedReasons []string               `json:"denied_reasons,omitempty"`
	Warnings      []string               `json:"warnings,omitempty"`
	RequiredLevel AutonomyLevel          `json:"required_level"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	EvaluatedAt   time.Time              `json:"evaluated_at"`
}

// PolicyInput represents input for policy evaluation
type PolicyInput struct {
	SessionID  string       `json:"session_id"`
	UserID     string       `json:"user_id"`
	TrustLevel int          `json:"trust_level"`
	Action     ActionInput  `json:"action"`
	Context    ContextInput `json:"context"`
	Session    SessionInput `json:"session"`
}

// ActionInput describes the action being evaluated
type ActionInput struct {
	Type             string                 `json:"type"`
	Target           TargetInput            `json:"target"`
	EstimatedCost    float64                `json:"estimated_cost,omitempty"`
	EstimatedTime    time.Duration          `json:"estimated_time,omitempty"`
	RequiresApproval bool                   `json:"requires_approval,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// TargetInput describes the target of an action
type TargetInput struct {
	Paths       []string               `json:"paths,omitempty"`
	Services    []string               `json:"services,omitempty"`
	Environment string                 `json:"environment,omitempty"`
	Resources   []string               `json:"resources,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ContextInput provides environmental context
type ContextInput struct {
	Environment   string    `json:"environment"`
	Time          time.Time `json:"time"`
	Timezone      string    `json:"timezone,omitempty"`
	BusinessHours bool      `json:"business_hours"`
	OnCall        bool      `json:"on_call,omitempty"`
}

// SessionInput provides session context
type SessionInput struct {
	TrustLevel   int       `json:"trust_level"`
	CostLimit    float64   `json:"cost_limit,omitempty"`
	ServiceScope []string  `json:"service_scope,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	ActionsCount int       `json:"actions_count"`
}

// AdvancedPolicyEngine provides comprehensive policy evaluation
type AdvancedPolicyEngine struct {
	timePolicies      []*TimePolicy
	financialPolicies []*FinancialPolicy
	customPolicies    []*CustomPolicy
	blastAnalyzer     *BlastRadiusAnalyzer
	config            *AdvancedPolicyConfig
	mu                sync.RWMutex
}

// AdvancedPolicyConfig contains engine configuration
type AdvancedPolicyConfig struct {
	EnableTimePolicies      bool   `json:"enable_time_policies"`
	EnableFinancialPolicies bool   `json:"enable_financial_policies"`
	EnableBlastRadius       bool   `json:"enable_blast_radius"`
	DefaultTimezone         string `json:"default_timezone"`
	BusinessHoursStart      int    `json:"business_hours_start"` // 0-23
	BusinessHoursEnd        int    `json:"business_hours_end"`   // 0-23
	WeekendRestrictions     bool   `json:"weekend_restrictions"`
}

// DefaultAdvancedPolicyConfig returns default configuration
func DefaultAdvancedPolicyConfig() *AdvancedPolicyConfig {
	return &AdvancedPolicyConfig{
		EnableTimePolicies:      true,
		EnableFinancialPolicies: true,
		EnableBlastRadius:       true,
		DefaultTimezone:         "UTC",
		BusinessHoursStart:      9,
		BusinessHoursEnd:        17,
		WeekendRestrictions:     true,
	}
}

// NewAdvancedPolicyEngine creates a new advanced policy engine
func NewAdvancedPolicyEngine(config *AdvancedPolicyConfig) *AdvancedPolicyEngine {
	if config == nil {
		config = DefaultAdvancedPolicyConfig()
	}
	return &AdvancedPolicyEngine{
		timePolicies:      make([]*TimePolicy, 0),
		financialPolicies: make([]*FinancialPolicy, 0),
		customPolicies:    make([]*CustomPolicy, 0),
		blastAnalyzer:     NewBlastRadiusAnalyzer(nil),
		config:            config,
	}
}

// Evaluate evaluates all policies against the input
func (e *AdvancedPolicyEngine) Evaluate(ctx context.Context, input PolicyInput) (*PolicyEvaluationResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := &PolicyEvaluationResult{
		Allowed:       true,
		DeniedReasons: make([]string, 0),
		Warnings:      make([]string, 0),
		Metadata:      make(map[string]interface{}),
		EvaluatedAt:   time.Now(),
	}

	// Evaluate time-based policies
	if e.config.EnableTimePolicies {
		if err := e.evaluateTimePolicies(ctx, input, result); err != nil {
			return result, nil // Return result with denial
		}
	}

	// Evaluate financial policies
	if e.config.EnableFinancialPolicies {
		if err := e.evaluateFinancialPolicies(ctx, input, result); err != nil {
			return result, nil
		}
	}

	// Evaluate blast radius
	if e.config.EnableBlastRadius && len(input.Action.Target.Paths) > 0 {
		if err := e.evaluateBlastRadius(ctx, input, result); err != nil {
			return result, nil
		}
	}

	// Evaluate custom policies
	for _, policy := range e.customPolicies {
		if err := policy.Evaluate(ctx, input, result); err != nil {
			return result, nil
		}
	}

	return result, nil
}

func (e *AdvancedPolicyEngine) evaluateTimePolicies(ctx context.Context, input PolicyInput, result *PolicyEvaluationResult) error {
	evalTime := input.Context.Time
	if evalTime.IsZero() {
		evalTime = time.Now()
	}

	// Load timezone
	loc, err := time.LoadLocation(e.config.DefaultTimezone)
	if err != nil {
		loc = time.UTC
	}
	localTime := evalTime.In(loc)

	hour := localTime.Hour()
	weekday := localTime.Weekday()

	// Check business hours for production
	if input.Context.Environment == "production" {
		// Production deploy outside business hours
		if input.Action.Type == "deploy" {
			isBusinessHours := hour >= e.config.BusinessHoursStart && hour < e.config.BusinessHoursEnd
			isWeekend := weekday == time.Saturday || weekday == time.Sunday

			if !isBusinessHours || (e.config.WeekendRestrictions && isWeekend) {
				if input.TrustLevel < 5 {
					result.Allowed = false
					result.PolicyName = "time_restriction_production"
					result.DeniedReasons = append(result.DeniedReasons,
						fmt.Sprintf("production deployments outside business hours (22:00-06:00 or weekends) require trust level 5, current: %d", input.TrustLevel))
					return ErrTimeRestriction
				}
				result.Warnings = append(result.Warnings, "deploying to production outside business hours")
			}
		}
	}

	// Late night restrictions for non-admin
	if (hour >= 22 || hour < 6) && input.TrustLevel < 4 {
		dangerousActions := map[string]bool{
			"deploy":          true,
			"delete":          true,
			"modify_database": true,
			"modify_config":   true,
		}
		if dangerousActions[input.Action.Type] {
			result.Allowed = false
			result.PolicyName = "time_restriction_off_hours"
			result.DeniedReasons = append(result.DeniedReasons,
				fmt.Sprintf("action '%s' between 22:00-06:00 requires trust level 4+, current: %d", input.Action.Type, input.TrustLevel))
			return ErrTimeRestriction
		}
	}

	// Evaluate registered time policies
	for _, policy := range e.timePolicies {
		if !policy.Evaluate(localTime, input) {
			result.Allowed = false
			result.PolicyName = policy.Name
			result.DeniedReasons = append(result.DeniedReasons, policy.DenialMessage)
			return ErrTimeRestriction
		}
	}

	return nil
}

func (e *AdvancedPolicyEngine) evaluateFinancialPolicies(ctx context.Context, input PolicyInput, result *PolicyEvaluationResult) error {
	// Check if action has cost
	if input.Action.EstimatedCost <= 0 {
		return nil
	}

	// Check session cost limit
	if input.Session.CostLimit > 0 && input.Action.EstimatedCost > input.Session.CostLimit {
		result.Allowed = false
		result.PolicyName = "financial_limit_session"
		result.DeniedReasons = append(result.DeniedReasons,
			fmt.Sprintf("estimated cost $%.2f exceeds session limit $%.2f", input.Action.EstimatedCost, input.Session.CostLimit))
		return ErrFinancialLimit
	}

	// Trust-level based cost limits
	costLimits := map[int]float64{
		1: 10.0,    // Observer: $10
		2: 50.0,    // Contributor: $50
		3: 200.0,   // Developer: $200
		4: 1000.0,  // Maintainer: $1000
		5: 10000.0, // Admin: $10000
	}

	if limit, ok := costLimits[input.TrustLevel]; ok {
		if input.Action.EstimatedCost > limit {
			result.Allowed = false
			result.PolicyName = "financial_limit_trust"
			result.DeniedReasons = append(result.DeniedReasons,
				fmt.Sprintf("estimated cost $%.2f exceeds limit $%.2f for trust level %d", input.Action.EstimatedCost, limit, input.TrustLevel))
			return ErrFinancialLimit
		}
	}

	// Evaluate registered financial policies
	for _, policy := range e.financialPolicies {
		if !policy.Evaluate(input) {
			result.Allowed = false
			result.PolicyName = policy.Name
			result.DeniedReasons = append(result.DeniedReasons, policy.DenialMessage)
			return ErrFinancialLimit
		}
	}

	// Add cost warning if approaching limit
	if limit, ok := costLimits[input.TrustLevel]; ok {
		if input.Action.EstimatedCost > limit*0.8 {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("cost $%.2f is approaching limit $%.2f", input.Action.EstimatedCost, limit))
		}
	}

	return nil
}

func (e *AdvancedPolicyEngine) evaluateBlastRadius(ctx context.Context, input PolicyInput, result *PolicyEvaluationResult) error {
	scope := ActionScope{
		AffectedPaths: input.Action.Target.Paths,
		Services:      input.Action.Target.Services,
		ActionType:    input.Action.Type,
		TrustLevel:    input.TrustLevel,
	}

	blastResult, err := e.blastAnalyzer.Analyze(ctx, scope)
	if err != nil {
		return err
	}

	result.Metadata["blast_radius"] = blastResult

	if !blastResult.Allowed {
		result.Allowed = false
		result.PolicyName = "blast_radius"
		result.DeniedReasons = append(result.DeniedReasons, blastResult.DenialReason)
		return ErrPolicyViolation
	}

	result.Warnings = append(result.Warnings, blastResult.Warnings...)
	return nil
}

// RegisterTimePolicy adds a time-based policy
func (e *AdvancedPolicyEngine) RegisterTimePolicy(policy *TimePolicy) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.timePolicies = append(e.timePolicies, policy)
}

// RegisterFinancialPolicy adds a financial policy
func (e *AdvancedPolicyEngine) RegisterFinancialPolicy(policy *FinancialPolicy) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.financialPolicies = append(e.financialPolicies, policy)
}

// RegisterCustomPolicy adds a custom policy
func (e *AdvancedPolicyEngine) RegisterCustomPolicy(policy *CustomPolicy) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.customPolicies = append(e.customPolicies, policy)
}

// TimePolicy represents a time-based policy
type TimePolicy struct {
	Name          string
	Description   string
	DenialMessage string
	Condition     TimePolicyCondition
}

// TimePolicyCondition is the evaluation function for time policies
type TimePolicyCondition func(t time.Time, input PolicyInput) bool

// Evaluate evaluates the time policy
func (p *TimePolicy) Evaluate(t time.Time, input PolicyInput) bool {
	return p.Condition(t, input)
}

// FinancialPolicy represents a financial policy
type FinancialPolicy struct {
	Name          string
	Description   string
	DenialMessage string
	Condition     FinancialPolicyCondition
}

// FinancialPolicyCondition is the evaluation function for financial policies
type FinancialPolicyCondition func(input PolicyInput) bool

// Evaluate evaluates the financial policy
func (p *FinancialPolicy) Evaluate(input PolicyInput) bool {
	return p.Condition(input)
}

// CustomPolicy represents a custom policy
type CustomPolicy struct {
	Name        string
	Description string
	Priority    int
	Evaluator   CustomPolicyEvaluator
}

// CustomPolicyEvaluator is the evaluation function for custom policies
type CustomPolicyEvaluator func(ctx context.Context, input PolicyInput, result *PolicyEvaluationResult) error

// Evaluate evaluates the custom policy
func (p *CustomPolicy) Evaluate(ctx context.Context, input PolicyInput, result *PolicyEvaluationResult) error {
	return p.Evaluator(ctx, input, result)
}

// Common pre-built policies

// NewMaintenanceWindowPolicy creates a policy that restricts actions during maintenance windows
func NewMaintenanceWindowPolicy(windows []MaintenanceWindow) *TimePolicy {
	return &TimePolicy{
		Name:          "maintenance_window",
		Description:   "Restricts actions during maintenance windows",
		DenialMessage: "action blocked during maintenance window",
		Condition: func(t time.Time, input PolicyInput) bool {
			for _, window := range windows {
				if t.After(window.Start) && t.Before(window.End) {
					// During maintenance window - only allow admin
					return input.TrustLevel >= 5
				}
			}
			return true
		},
	}
}

// MaintenanceWindow represents a scheduled maintenance window
type MaintenanceWindow struct {
	Start time.Time
	End   time.Time
	Name  string
}

// NewMonthlyBudgetPolicy creates a policy that enforces monthly budget limits
func NewMonthlyBudgetPolicy(monthlyLimit float64, getCurrentSpend func() float64) *FinancialPolicy {
	return &FinancialPolicy{
		Name:          "monthly_budget",
		Description:   "Enforces monthly budget limits",
		DenialMessage: fmt.Sprintf("monthly budget of $%.2f would be exceeded", monthlyLimit),
		Condition: func(input PolicyInput) bool {
			currentSpend := getCurrentSpend()
			return currentSpend+input.Action.EstimatedCost <= monthlyLimit
		},
	}
}

// NewServiceScopePolicy creates a policy that restricts actions to allowed services
func NewServiceScopePolicy() *CustomPolicy {
	return &CustomPolicy{
		Name:        "service_scope",
		Description: "Restricts actions to services in session scope",
		Priority:    100,
		Evaluator: func(ctx context.Context, input PolicyInput, result *PolicyEvaluationResult) error {
			if len(input.Session.ServiceScope) == 0 {
				return nil // No scope restriction
			}

			allowedServices := make(map[string]bool)
			for _, s := range input.Session.ServiceScope {
				allowedServices[s] = true
			}

			for _, target := range input.Action.Target.Services {
				if !allowedServices[target] {
					result.Allowed = false
					result.PolicyName = "service_scope"
					result.DeniedReasons = append(result.DeniedReasons,
						fmt.Sprintf("service '%s' is not in session scope", target))
					return ErrPolicyViolation
				}
			}

			return nil
		},
	}
}

// NewEnvironmentGatePolicy creates a policy that requires higher trust for production
func NewEnvironmentGatePolicy() *CustomPolicy {
	return &CustomPolicy{
		Name:        "environment_gate",
		Description: "Requires higher trust levels for sensitive environments",
		Priority:    90,
		Evaluator: func(ctx context.Context, input PolicyInput, result *PolicyEvaluationResult) error {
			envRequirements := map[string]int{
				"production":  4,
				"staging":     3,
				"development": 1,
				"test":        1,
			}

			requiredLevel, ok := envRequirements[input.Context.Environment]
			if !ok {
				requiredLevel = 3 // Default to developer level
			}

			if input.TrustLevel < requiredLevel {
				result.Allowed = false
				result.PolicyName = "environment_gate"
				result.DeniedReasons = append(result.DeniedReasons,
					fmt.Sprintf("environment '%s' requires trust level %d, current: %d",
						input.Context.Environment, requiredLevel, input.TrustLevel))
				return ErrInsufficientTrust
			}

			return nil
		},
	}
}
