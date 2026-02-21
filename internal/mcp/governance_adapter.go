package mcp

import (
	"context"
	"time"

	"github.com/adp/adp/internal/domain/governance"
)

// GovernanceEngineAdapter adapts the governance.UnifiedPolicyEngine to the mcp.UnifiedPolicyEngine interface
type GovernanceEngineAdapter struct {
	engine *governance.UnifiedPolicyEngine
}

// NewGovernanceEngineAdapter creates a new adapter wrapping the governance engine
func NewGovernanceEngineAdapter(engine *governance.UnifiedPolicyEngine) *GovernanceEngineAdapter {
	return &GovernanceEngineAdapter{engine: engine}
}

// Evaluate converts MCP types to governance types, calls the engine, and converts the result back
func (a *GovernanceEngineAdapter) Evaluate(ctx context.Context, input *UnifiedEvalInput) (*UnifiedEvalResult, error) {
	// Convert MCP input to governance input
	govInput := &governance.EvaluationInput{
		SessionID:  input.SessionID,
		TrustLevel: input.TrustLevel,
		Action: governance.ActionEvalInput{
			Type: input.Action.Type,
			Target: governance.TargetEvalInput{
				Paths:       input.Action.Target.Paths,
				Services:    input.Action.Target.Services,
				Environment: input.Action.Target.Environment,
			},
			Metadata: input.Action.Metadata,
		},
		Context: governance.ContextEvalInput{
			Environment: input.Context.Environment,
			Time:        time.Now(),
			Hour:        input.Context.Hour,
		},
		Session: governance.SessionEvalInput{
			TrustLevel: input.Session.TrustLevel,
		},
	}

	// Call the actual governance engine
	result, err := a.engine.Evaluate(ctx, govInput)
	if err != nil {
		return nil, err
	}

	// Convert governance result to MCP result
	return &UnifiedEvalResult{
		Allowed:          result.Allowed,
		RequiresApproval: result.RequiresApproval,
		DeniedReasons:    result.DeniedReasons,
		MatchedPolicies:  result.MatchedPolicies,
		Warnings:         result.Warnings,
	}, nil
}
