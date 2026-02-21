package auth

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Neo4jAuthorizer implements the Authorizer interface using Neo4j
type Neo4jAuthorizer struct {
	driver neo4j.DriverWithContext
}

// NewNeo4jAuthorizer creates a new Neo4jAuthorizer
func NewNeo4jAuthorizer(driver neo4j.DriverWithContext) *Neo4jAuthorizer {
	return &Neo4jAuthorizer{
		driver: driver,
	}
}

// Authorize checks permission by querying the graph
// Query: (u:User {id: $subject})-[:HAS_ROLE]->(r:Role)-[:PERMITS]->(p:Permission {action: $action, resource: $resource})
func (a *Neo4jAuthorizer) Authorize(ctx context.Context, subject, action, resource string) (bool, error) {
	session := a.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		query := `
			MATCH (u:User {id: $subject})-[:HAS_ROLE]->(r:Role)-[:PERMITS]->(p:Permission)
			WHERE p.action = $action AND (p.resource = $resource OR p.resource = "*")
			RETURN count(p) > 0 as allowed
		`
		params := map[string]any{
			"subject":  subject,
			"action":   action,
			"resource": resource,
		}

		res, err := tx.Run(ctx, query, params)
		if err != nil {
			return false, err
		}

		if res.Next(ctx) {
			return res.Record().Values[0].(bool), nil
		}

		return false, nil
	})

	if err != nil {
		return false, fmt.Errorf("authorization query failed: %w", err)
	}

	return result.(bool), nil
}
