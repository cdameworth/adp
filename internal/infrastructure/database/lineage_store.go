package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Lineage errors
var (
	ErrLineageNotFound     = errors.New("lineage not found")
	ErrInvalidLineageInput = errors.New("invalid lineage input")
	ErrLineageStoreFailed  = errors.New("failed to store lineage")
)

// LineageStore handles decision lineage operations in Neo4j
type LineageStore struct {
	driver neo4j.DriverWithContext
}

// NewLineageStore creates a new lineage store
func NewLineageStore(driver neo4j.DriverWithContext) *LineageStore {
	return &LineageStore{driver: driver}
}

// DecisionNode represents a decision in the lineage graph
type DecisionNode struct {
	ID           uuid.UUID              `json:"id"`
	SessionID    string                 `json:"session_id"`
	UserID       string                 `json:"user_id"`
	DecisionType string                 `json:"decision_type"`
	Action       string                 `json:"action"`
	Target       map[string]interface{} `json:"target"`
	Reasoning    ReasoningTrace         `json:"reasoning"`
	Confidence   float64                `json:"confidence"`
	Outcome      string                 `json:"outcome"`
	PolicyID     string                 `json:"policy_id,omitempty"`
	Timestamp    time.Time              `json:"timestamp"`
	ServiceID    string                 `json:"service_id,omitempty"`
	Environment  string                 `json:"environment,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// ReasoningTrace captures the reasoning behind a decision
type ReasoningTrace struct {
	Steps        []ReasoningStep        `json:"steps"`
	Inputs       map[string]interface{} `json:"inputs"`
	Constraints  []string               `json:"constraints"`
	Assumptions  []string               `json:"assumptions"`
	Alternatives []AlternativeAction    `json:"alternatives"`
	FinalReason  string                 `json:"final_reason"`
}

// ReasoningStep represents a single step in the reasoning chain
type ReasoningStep struct {
	Order       int                    `json:"order"`
	Type        string                 `json:"type"` // analysis, comparison, validation, decision
	Description string                 `json:"description"`
	Evidence    map[string]interface{} `json:"evidence,omitempty"`
	Confidence  float64                `json:"confidence"`
}

// AlternativeAction represents an alternative that was considered
type AlternativeAction struct {
	Action      string  `json:"action"`
	Reason      string  `json:"reason"`
	Confidence  float64 `json:"confidence"`
	WhyRejected string  `json:"why_rejected"`
}

// SessionNode represents a session in the lineage graph
type SessionNode struct {
	ID             string     `json:"id"`
	UserID         string     `json:"user_id"`
	OrganizationID string     `json:"organization_id"`
	AgentTool      string     `json:"agent_tool"`
	TrustLevel     int        `json:"trust_level"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	Status         string     `json:"status"`
}

// ServiceNode represents a service in the lineage graph
type ServiceNode struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Owner string   `json:"owner"`
	Team  string   `json:"team"`
	Tier  string   `json:"tier"`
	Tags  []string `json:"tags"`
}

// UserNode represents a user in the lineage graph
type UserNode struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// PolicyNode represents a policy in the lineage graph
type PolicyNode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// CommitNode represents a commit in the lineage graph
type CommitNode struct {
	SHA        string    `json:"sha"`
	Message    string    `json:"message"`
	Author     string    `json:"author"`
	Repository string    `json:"repository"`
	Branch     string    `json:"branch"`
	Timestamp  time.Time `json:"timestamp"`
}

// LineageRelationship types
const (
	RelMade        = "MADE"         // (Session)-[:MADE]->(Decision)
	RelLedTo       = "LED_TO"       // (Decision)-[:LED_TO]->(Decision)
	RelModified    = "MODIFIED"     // (Decision)-[:MODIFIED]->(Service)
	RelResultedIn  = "RESULTED_IN"  // (Decision)-[:RESULTED_IN]->(Commit)
	RelOperated    = "OPERATED"     // (User)-[:OPERATED]->(Session)
	RelEvaluatedBy = "EVALUATED_BY" // (Decision)-[:EVALUATED_BY]->(Policy)
	RelBelongsTo   = "BELONGS_TO"   // (Session)-[:BELONGS_TO]->(Organization)
)

// StoreDecisionInput contains input for storing a decision with lineage
type StoreDecisionInput struct {
	Decision   DecisionNode
	ParentID   *uuid.UUID // Parent decision if part of a chain
	SessionID  string
	ServiceIDs []string
	PolicyID   string
	CommitSHA  string
}

// StoreDecision stores a decision node with its relationships
func (s *LineageStore) StoreDecision(ctx context.Context, input StoreDecisionInput) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		// Create decision node
		reasoningJSON, _ := json.Marshal(input.Decision.Reasoning)
		targetJSON, _ := json.Marshal(input.Decision.Target)
		metadataJSON, _ := json.Marshal(input.Decision.Metadata)

		query := `
			MERGE (d:Decision {id: $id})
			SET d.session_id = $session_id,
			    d.user_id = $user_id,
			    d.decision_type = $decision_type,
			    d.action = $action,
			    d.target = $target,
			    d.reasoning = $reasoning,
			    d.confidence = $confidence,
			    d.outcome = $outcome,
			    d.policy_id = $policy_id,
			    d.timestamp = $timestamp,
			    d.service_id = $service_id,
			    d.environment = $environment,
			    d.metadata = $metadata
		`
		params := map[string]any{
			"id":            input.Decision.ID.String(),
			"session_id":    input.Decision.SessionID,
			"user_id":       input.Decision.UserID,
			"decision_type": input.Decision.DecisionType,
			"action":        input.Decision.Action,
			"target":        string(targetJSON),
			"reasoning":     string(reasoningJSON),
			"confidence":    input.Decision.Confidence,
			"outcome":       input.Decision.Outcome,
			"policy_id":     input.Decision.PolicyID,
			"timestamp":     input.Decision.Timestamp,
			"service_id":    input.Decision.ServiceID,
			"environment":   input.Decision.Environment,
			"metadata":      string(metadataJSON),
		}

		if _, err := tx.Run(ctx, query, params); err != nil {
			return nil, fmt.Errorf("failed to create decision node: %w", err)
		}

		// Create session relationship
		if input.SessionID != "" {
			sessionQuery := `
				MATCH (d:Decision {id: $decision_id})
				MERGE (s:Session {id: $session_id})
				MERGE (s)-[:MADE]->(d)
			`
			if _, err := tx.Run(ctx, sessionQuery, map[string]any{
				"decision_id": input.Decision.ID.String(),
				"session_id":  input.SessionID,
			}); err != nil {
				return nil, fmt.Errorf("failed to create session relationship: %w", err)
			}
		}

		// Create user relationship
		if input.Decision.UserID != "" {
			userQuery := `
				MATCH (d:Decision {id: $decision_id})
				MERGE (u:User {id: $user_id})
				MERGE (u)-[:OPERATED]->(s:Session {id: $session_id})-[:MADE]->(d)
			`
			if _, err := tx.Run(ctx, userQuery, map[string]any{
				"decision_id": input.Decision.ID.String(),
				"user_id":     input.Decision.UserID,
				"session_id":  input.SessionID,
			}); err != nil {
				// Don't fail if this relationship can't be created
			}
		}

		// Create parent relationship (decision chain)
		if input.ParentID != nil {
			parentQuery := `
				MATCH (d:Decision {id: $decision_id})
				MATCH (p:Decision {id: $parent_id})
				MERGE (p)-[:LED_TO]->(d)
			`
			if _, err := tx.Run(ctx, parentQuery, map[string]any{
				"decision_id": input.Decision.ID.String(),
				"parent_id":   input.ParentID.String(),
			}); err != nil {
				return nil, fmt.Errorf("failed to create parent relationship: %w", err)
			}
		}

		// Create service relationships
		for _, serviceID := range input.ServiceIDs {
			serviceQuery := `
				MATCH (d:Decision {id: $decision_id})
				MERGE (svc:Service {id: $service_id})
				MERGE (d)-[:MODIFIED]->(svc)
			`
			if _, err := tx.Run(ctx, serviceQuery, map[string]any{
				"decision_id": input.Decision.ID.String(),
				"service_id":  serviceID,
			}); err != nil {
				// Don't fail on service relationship
			}
		}

		// Create policy relationship
		if input.PolicyID != "" {
			policyQuery := `
				MATCH (d:Decision {id: $decision_id})
				MERGE (p:Policy {id: $policy_id})
				MERGE (d)-[:EVALUATED_BY]->(p)
			`
			if _, err := tx.Run(ctx, policyQuery, map[string]any{
				"decision_id": input.Decision.ID.String(),
				"policy_id":   input.PolicyID,
			}); err != nil {
				// Don't fail on policy relationship
			}
		}

		// Create commit relationship
		if input.CommitSHA != "" {
			commitQuery := `
				MATCH (d:Decision {id: $decision_id})
				MERGE (c:Commit {sha: $commit_sha})
				MERGE (d)-[:RESULTED_IN]->(c)
			`
			if _, err := tx.Run(ctx, commitQuery, map[string]any{
				"decision_id": input.Decision.ID.String(),
				"commit_sha":  input.CommitSHA,
			}); err != nil {
				// Don't fail on commit relationship
			}
		}

		return nil, nil
	})

	return err
}

// GetDecisionLineage retrieves the lineage chain for a decision
func (s *LineageStore) GetDecisionLineage(ctx context.Context, decisionID uuid.UUID, depth int) (*LineageChain, error) {
	if depth <= 0 {
		depth = 10
	}

	session := s.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		// Get the decision and its ancestors
		query := `
			MATCH path = (root:Decision)-[:LED_TO*0..` + fmt.Sprintf("%d", depth) + `]->(d:Decision {id: $decision_id})
			RETURN nodes(path) as decisions, relationships(path) as relations
			ORDER BY length(path) DESC
			LIMIT 1
		`

		records, err := tx.Run(ctx, query, map[string]any{
			"decision_id": decisionID.String(),
		})
		if err != nil {
			return nil, err
		}

		chain := &LineageChain{
			Decisions:     make([]DecisionNode, 0),
			Relationships: make([]LineageEdge, 0),
		}

		for records.Next(ctx) {
			record := records.Record()

			// Parse decisions
			decisionsVal, _ := record.Get("decisions")
			if decisions, ok := decisionsVal.([]any); ok {
				for _, d := range decisions {
					if node, ok := d.(neo4j.Node); ok {
						decision := parseDecisionNode(node.Props)
						chain.Decisions = append(chain.Decisions, decision)
					}
				}
			}

			// Parse relationships
			relationsVal, _ := record.Get("relations")
			if relations, ok := relationsVal.([]any); ok {
				for _, r := range relations {
					if rel, ok := r.(neo4j.Relationship); ok {
						edge := LineageEdge{
							Type: rel.Type,
						}
						chain.Relationships = append(chain.Relationships, edge)
					}
				}
			}
		}

		// Also get descendants
		descendantQuery := `
			MATCH path = (d:Decision {id: $decision_id})-[:LED_TO*1..` + fmt.Sprintf("%d", depth) + `]->(child:Decision)
			RETURN nodes(path) as decisions
		`
		descRecords, err := tx.Run(ctx, descendantQuery, map[string]any{
			"decision_id": decisionID.String(),
		})
		if err == nil {
			for descRecords.Next(ctx) {
				record := descRecords.Record()
				decisionsVal, _ := record.Get("decisions")
				if decisions, ok := decisionsVal.([]any); ok {
					for _, d := range decisions {
						if node, ok := d.(neo4j.Node); ok {
							decision := parseDecisionNode(node.Props)
							// Avoid duplicates
							found := false
							for _, existing := range chain.Decisions {
								if existing.ID == decision.ID {
									found = true
									break
								}
							}
							if !found {
								chain.Decisions = append(chain.Decisions, decision)
							}
						}
					}
				}
			}
		}

		return chain, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to get lineage: %w", err)
	}

	return result.(*LineageChain), nil
}

// LineageChain represents a chain of decisions
type LineageChain struct {
	Decisions     []DecisionNode `json:"decisions"`
	Relationships []LineageEdge  `json:"relationships"`
	TotalDepth    int            `json:"total_depth"`
}

// LineageEdge represents a relationship between decisions
type LineageEdge struct {
	Type       string                 `json:"type"`
	FromID     string                 `json:"from_id"`
	ToID       string                 `json:"to_id"`
	Properties map[string]interface{} `json:"properties,omitempty"`
}

// GetSessionLineage retrieves all decisions and their relationships for a session
func (s *LineageStore) GetSessionLineage(ctx context.Context, sessionID string) (*SessionLineage, error) {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		query := `
			MATCH (s:Session {id: $session_id})-[:MADE]->(d:Decision)
			OPTIONAL MATCH (d)-[:LED_TO]->(child:Decision)
			OPTIONAL MATCH (d)-[:MODIFIED]->(svc:Service)
			OPTIONAL MATCH (d)-[:RESULTED_IN]->(c:Commit)
			OPTIONAL MATCH (d)-[:EVALUATED_BY]->(p:Policy)
			RETURN d, collect(DISTINCT child) as children,
			       collect(DISTINCT svc) as services,
			       collect(DISTINCT c) as commits,
			       collect(DISTINCT p) as policies
			ORDER BY d.timestamp
		`

		records, err := tx.Run(ctx, query, map[string]any{
			"session_id": sessionID,
		})
		if err != nil {
			return nil, err
		}

		lineage := &SessionLineage{
			SessionID: sessionID,
			Decisions: make([]DecisionNode, 0),
			Services:  make([]ServiceNode, 0),
			Commits:   make([]CommitNode, 0),
			Policies:  make([]PolicyNode, 0),
			Edges:     make([]LineageEdge, 0),
		}

		serviceSet := make(map[string]bool)
		commitSet := make(map[string]bool)
		policySet := make(map[string]bool)

		for records.Next(ctx) {
			record := records.Record()

			// Parse decision
			dVal, _ := record.Get("d")
			if node, ok := dVal.(neo4j.Node); ok {
				decision := parseDecisionNode(node.Props)
				lineage.Decisions = append(lineage.Decisions, decision)

				// Add edges for children
				childrenVal, _ := record.Get("children")
				if children, ok := childrenVal.([]any); ok {
					for _, c := range children {
						if childNode, ok := c.(neo4j.Node); ok {
							childID := getStringProp(childNode.Props, "id")
							if childID != "" {
								lineage.Edges = append(lineage.Edges, LineageEdge{
									Type:   RelLedTo,
									FromID: decision.ID.String(),
									ToID:   childID,
								})
							}
						}
					}
				}

				// Collect services
				servicesVal, _ := record.Get("services")
				if services, ok := servicesVal.([]any); ok {
					for _, svc := range services {
						if svcNode, ok := svc.(neo4j.Node); ok {
							svcID := getStringProp(svcNode.Props, "id")
							if svcID != "" && !serviceSet[svcID] {
								serviceSet[svcID] = true
								lineage.Services = append(lineage.Services, ServiceNode{
									ID:   svcID,
									Name: getStringProp(svcNode.Props, "name"),
								})
								lineage.Edges = append(lineage.Edges, LineageEdge{
									Type:   RelModified,
									FromID: decision.ID.String(),
									ToID:   svcID,
								})
							}
						}
					}
				}

				// Collect commits
				commitsVal, _ := record.Get("commits")
				if commits, ok := commitsVal.([]any); ok {
					for _, c := range commits {
						if commitNode, ok := c.(neo4j.Node); ok {
							sha := getStringProp(commitNode.Props, "sha")
							if sha != "" && !commitSet[sha] {
								commitSet[sha] = true
								lineage.Commits = append(lineage.Commits, CommitNode{
									SHA:     sha,
									Message: getStringProp(commitNode.Props, "message"),
								})
								lineage.Edges = append(lineage.Edges, LineageEdge{
									Type:   RelResultedIn,
									FromID: decision.ID.String(),
									ToID:   sha,
								})
							}
						}
					}
				}

				// Collect policies
				policiesVal, _ := record.Get("policies")
				if policies, ok := policiesVal.([]any); ok {
					for _, p := range policies {
						if policyNode, ok := p.(neo4j.Node); ok {
							policyID := getStringProp(policyNode.Props, "id")
							if policyID != "" && !policySet[policyID] {
								policySet[policyID] = true
								lineage.Policies = append(lineage.Policies, PolicyNode{
									ID:   policyID,
									Name: getStringProp(policyNode.Props, "name"),
								})
								lineage.Edges = append(lineage.Edges, LineageEdge{
									Type:   RelEvaluatedBy,
									FromID: decision.ID.String(),
									ToID:   policyID,
								})
							}
						}
					}
				}
			}
		}

		return lineage, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to get session lineage: %w", err)
	}

	return result.(*SessionLineage), nil
}

// SessionLineage represents the complete lineage for a session
type SessionLineage struct {
	SessionID string         `json:"session_id"`
	Decisions []DecisionNode `json:"decisions"`
	Services  []ServiceNode  `json:"services"`
	Commits   []CommitNode   `json:"commits"`
	Policies  []PolicyNode   `json:"policies"`
	Edges     []LineageEdge  `json:"edges"`
}

// QueryLineage performs a custom Cypher query for lineage
func (s *LineageStore) QueryLineage(ctx context.Context, query string, params map[string]interface{}) ([]map[string]interface{}, error) {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		records, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}

		results := make([]map[string]interface{}, 0)
		for records.Next(ctx) {
			record := records.Record()
			row := make(map[string]interface{})
			for _, key := range record.Keys {
				val, _ := record.Get(key)
				row[key] = val
			}
			results = append(results, row)
		}

		return results, nil
	})

	if err != nil {
		return nil, err
	}

	return result.([]map[string]interface{}), nil
}

// FindAnomalies queries for anomalous decision patterns
func (s *LineageStore) FindAnomalies(ctx context.Context, thresholds AnomalyThresholds) ([]AnomalyResult, error) {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		anomalies := make([]AnomalyResult, 0)

		// Find decisions with low confidence
		if thresholds.MinConfidence > 0 {
			query := `
				MATCH (d:Decision)
				WHERE d.confidence < $min_confidence
				RETURN d, 'low_confidence' as anomaly_type
				ORDER BY d.timestamp DESC
				LIMIT 100
			`
			records, err := tx.Run(ctx, query, map[string]any{
				"min_confidence": thresholds.MinConfidence,
			})
			if err == nil {
				for records.Next(ctx) {
					record := records.Record()
					dVal, _ := record.Get("d")
					if node, ok := dVal.(neo4j.Node); ok {
						decision := parseDecisionNode(node.Props)
						anomalies = append(anomalies, AnomalyResult{
							Type:        "low_confidence",
							DecisionID:  decision.ID.String(),
							Description: fmt.Sprintf("Decision confidence %.2f below threshold %.2f", decision.Confidence, thresholds.MinConfidence),
							Severity:    "warning",
							Timestamp:   decision.Timestamp,
						})
					}
				}
			}
		}

		// Find sessions with high decision volume
		if thresholds.MaxDecisionsPerSession > 0 {
			query := `
				MATCH (s:Session)-[:MADE]->(d:Decision)
				WITH s, count(d) as decision_count
				WHERE decision_count > $max_decisions
				RETURN s.id as session_id, decision_count, 'high_volume' as anomaly_type
			`
			records, err := tx.Run(ctx, query, map[string]any{
				"max_decisions": thresholds.MaxDecisionsPerSession,
			})
			if err == nil {
				for records.Next(ctx) {
					record := records.Record()
					sessionID, _ := record.Get("session_id")
					count, _ := record.Get("decision_count")
					anomalies = append(anomalies, AnomalyResult{
						Type:        "high_volume",
						SessionID:   sessionID.(string),
						Description: fmt.Sprintf("Session has %d decisions, exceeding threshold of %d", count.(int64), thresholds.MaxDecisionsPerSession),
						Severity:    "info",
					})
				}
			}
		}

		return anomalies, nil
	})

	if err != nil {
		return nil, err
	}

	return result.([]AnomalyResult), nil
}

// AnomalyThresholds defines thresholds for anomaly detection
type AnomalyThresholds struct {
	MinConfidence          float64 `json:"min_confidence"`
	MaxDecisionsPerSession int     `json:"max_decisions_per_session"`
	MaxServicesPerDecision int     `json:"max_services_per_decision"`
	MaxChainDepth          int     `json:"max_chain_depth"`
}

// AnomalyResult represents a detected anomaly
type AnomalyResult struct {
	Type        string    `json:"type"`
	DecisionID  string    `json:"decision_id,omitempty"`
	SessionID   string    `json:"session_id,omitempty"`
	Description string    `json:"description"`
	Severity    string    `json:"severity"` // info, warning, critical
	Timestamp   time.Time `json:"timestamp,omitempty"`
}

// ExportVisualizationData exports lineage data for visualization
func (s *LineageStore) ExportVisualizationData(ctx context.Context, sessionID string) (*VisualizationData, error) {
	lineage, err := s.GetSessionLineage(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	viz := &VisualizationData{
		Nodes: make([]VizNode, 0),
		Edges: make([]VizEdge, 0),
	}

	// Add decision nodes
	for _, d := range lineage.Decisions {
		viz.Nodes = append(viz.Nodes, VizNode{
			ID:    d.ID.String(),
			Type:  "decision",
			Label: d.Action,
			Data: map[string]interface{}{
				"decision_type": d.DecisionType,
				"confidence":    d.Confidence,
				"outcome":       d.Outcome,
				"timestamp":     d.Timestamp,
			},
		})
	}

	// Add service nodes
	for _, svc := range lineage.Services {
		viz.Nodes = append(viz.Nodes, VizNode{
			ID:    svc.ID,
			Type:  "service",
			Label: svc.Name,
			Data: map[string]interface{}{
				"owner": svc.Owner,
				"tier":  svc.Tier,
			},
		})
	}

	// Add commit nodes
	for _, c := range lineage.Commits {
		viz.Nodes = append(viz.Nodes, VizNode{
			ID:    c.SHA,
			Type:  "commit",
			Label: c.SHA[:7], // Short SHA
			Data: map[string]interface{}{
				"message": c.Message,
				"author":  c.Author,
			},
		})
	}

	// Add edges
	for _, e := range lineage.Edges {
		viz.Edges = append(viz.Edges, VizEdge{
			Source: e.FromID,
			Target: e.ToID,
			Type:   e.Type,
		})
	}

	return viz, nil
}

// VisualizationData contains data formatted for visualization
type VisualizationData struct {
	Nodes []VizNode `json:"nodes"`
	Edges []VizEdge `json:"edges"`
}

// VizNode represents a node for visualization
type VizNode struct {
	ID    string                 `json:"id"`
	Type  string                 `json:"type"`
	Label string                 `json:"label"`
	Data  map[string]interface{} `json:"data,omitempty"`
}

// VizEdge represents an edge for visualization
type VizEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

// Helper functions

func parseDecisionNode(props map[string]any) DecisionNode {
	id, _ := uuid.Parse(getStringProp(props, "id"))
	timestamp, _ := props["timestamp"].(time.Time)
	confidence, _ := props["confidence"].(float64)

	var reasoning ReasoningTrace
	if reasoningStr, ok := props["reasoning"].(string); ok {
		json.Unmarshal([]byte(reasoningStr), &reasoning)
	}

	var target map[string]interface{}
	if targetStr, ok := props["target"].(string); ok {
		json.Unmarshal([]byte(targetStr), &target)
	}

	return DecisionNode{
		ID:           id,
		SessionID:    getStringProp(props, "session_id"),
		UserID:       getStringProp(props, "user_id"),
		DecisionType: getStringProp(props, "decision_type"),
		Action:       getStringProp(props, "action"),
		Target:       target,
		Reasoning:    reasoning,
		Confidence:   confidence,
		Outcome:      getStringProp(props, "outcome"),
		PolicyID:     getStringProp(props, "policy_id"),
		Timestamp:    timestamp,
		ServiceID:    getStringProp(props, "service_id"),
		Environment:  getStringProp(props, "environment"),
	}
}

func getStringProp(props map[string]any, key string) string {
	if v, ok := props[key].(string); ok {
		return v
	}
	return ""
}
