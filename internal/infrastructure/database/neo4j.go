package database

import (
	"context"
	"fmt"

	"github.com/adp/adp/internal/domain/audit"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// GraphStore defines the interface for graph database operations
type GraphStore interface {
	StoreDecision(ctx context.Context, record audit.DecisionRecord) error
	Close(ctx context.Context) error
}

// Neo4jStore implements GraphStore for Neo4j
type Neo4jStore struct {
	driver neo4j.DriverWithContext
}

// NewNeo4jStore creates a new Neo4jStore
func NewNeo4jStore(uri, username, password string) (*Neo4jStore, error) {
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		return nil, fmt.Errorf("failed to create neo4j driver: %w", err)
	}

	// Verify connection
	if err := driver.VerifyConnectivity(context.Background()); err != nil {
		driver.Close(context.Background())
		return nil, fmt.Errorf("failed to connect to neo4j: %w", err)
	}

	return &Neo4jStore{
		driver: driver,
	}, nil
}

// Close closes the database connection
func (s *Neo4jStore) Close(ctx context.Context) error {
	return s.driver.Close(ctx)
}

// Driver returns the underlying Neo4j driver
func (s *Neo4jStore) Driver() neo4j.DriverWithContext {
	return s.driver
}

// StoreDecision stores a decision record and its lineage in the graph
func (s *Neo4jStore) StoreDecision(ctx context.Context, record audit.DecisionRecord) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		query := `
			MERGE (d:Decision {id: $id})
			SET d.timestamp = $timestamp,
			    d.outcome = $outcome,
			    d.policy_id = $policy_id
			
			MERGE (u:User {id: $user_id})
			MERGE (u)-[:MADE]->(d)
		`
		params := map[string]any{
			"id":        record.ID.String(),
			"timestamp": record.CreatedAt,
			"outcome":   string(record.Outcome),
			"policy_id": record.PolicyID,
			"user_id":   record.UserID,
		}

		if _, err := tx.Run(ctx, query, params); err != nil {
			return nil, err
		}
		return nil, nil
	})

	if err != nil {
		return fmt.Errorf("failed to store decision in neo4j: %w", err)
	}

	return nil
}
