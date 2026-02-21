package governance

import (
	"context"
	"fmt"

	"github.com/open-policy-agent/opa/rego"
)

// PolicyEngine defines the interface for policy evaluation
type PolicyEngine interface {
	Evaluate(ctx context.Context, input interface{}, query string) (bool, error)
}

// OPAEngine implements PolicyEngine using OPA Rego
type OPAEngine struct {
	policyPath string
}

// NewOPAEngine creates a new OPAEngine
func NewOPAEngine(policyPath string) *OPAEngine {
	return &OPAEngine{
		policyPath: policyPath,
	}
}

// Evaluate evaluates a Rego query against the input
func (e *OPAEngine) Evaluate(ctx context.Context, input interface{}, query string) (bool, error) {
	// In a real implementation, we would load the policy from file or string
	// For this example, we'll assume the policy is loaded or passed in a different way
	// or we construct the Rego object with the file.

	// Simplified for demonstration:
	// We would typically cache the prepared query.

	r := rego.New(
		rego.Query(query),
		rego.Load([]string{e.policyPath}, nil),
		rego.Input(input),
	)

	queryResult, err := r.Eval(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to evaluate policy: %w", err)
	}

	if len(queryResult) == 0 {
		return false, nil // Undefined result usually means deny in our default deny model
	}

	// Check if the result is true
	if allowed, ok := queryResult[0].Expressions[0].Value.(bool); ok {
		return allowed, nil
	}

	return false, nil
}
