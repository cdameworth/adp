package governance

import (
	"context"

	"github.com/adp/adp/internal/infrastructure/database"
)

// PolicyStoreAdapter adapts database.PolicyDefinitionStore to governance.PolicyStore
type PolicyStoreAdapter struct {
	store *database.PolicyDefinitionStore
}

// NewPolicyStoreAdapter creates a new adapter
func NewPolicyStoreAdapter(store *database.PolicyDefinitionStore) *PolicyStoreAdapter {
	return &PolicyStoreAdapter{store: store}
}

// ListEnabled returns enabled policies for the engine
func (a *PolicyStoreAdapter) ListEnabled(ctx context.Context) ([]*PolicyDefinition, error) {
	dbPolicies, err := a.store.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}

	policies := make([]*PolicyDefinition, len(dbPolicies))
	for i, p := range dbPolicies {
		policies[i] = &PolicyDefinition{
			ID:            p.ID,
			Name:          p.Name,
			Description:   p.Description,
			Category:      p.Category,
			Enabled:       p.Enabled,
			Priority:      p.Priority,
			PolicyType:    p.PolicyType,
			RegoCode:      p.RegoCode,
			BuiltinName:   p.BuiltinName,
			Config:        p.Config,
			MinTrustLevel: p.MinTrustLevel,
		}
	}

	return policies, nil
}
