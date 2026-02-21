// Neo4j Lineage Schema Migration
// Version: 000002
// Description: Complete lineage graph schema for decision tracking

// ============================================
// Node Constraints (Unique Identifiers)
// ============================================

// Decision node - primary audit record
CREATE CONSTRAINT decision_unique IF NOT EXISTS
FOR (d:Decision) REQUIRE d.id IS UNIQUE;

// Session node - agent session container
CREATE CONSTRAINT session_unique IF NOT EXISTS
FOR (s:Session) REQUIRE s.id IS UNIQUE;

// User node - human operator
CREATE CONSTRAINT user_unique IF NOT EXISTS
FOR (u:User) REQUIRE u.id IS UNIQUE;

// Service node - affected service
CREATE CONSTRAINT service_unique IF NOT EXISTS
FOR (svc:Service) REQUIRE svc.id IS UNIQUE;

// Commit node - git commit
CREATE CONSTRAINT commit_unique IF NOT EXISTS
FOR (c:Commit) REQUIRE c.sha IS UNIQUE;

// Policy node - governance policy
CREATE CONSTRAINT policy_unique IF NOT EXISTS
FOR (p:Policy) REQUIRE p.id IS UNIQUE;

// Organization node - tenant
CREATE CONSTRAINT organization_unique IF NOT EXISTS
FOR (o:Organization) REQUIRE o.id IS UNIQUE;

// Escalation node - approval request
CREATE CONSTRAINT escalation_unique IF NOT EXISTS
FOR (e:Escalation) REQUIRE e.id IS UNIQUE;

// ============================================
// Node Indexes (Query Optimization)
// ============================================

// Decision indexes
CREATE INDEX decision_session IF NOT EXISTS
FOR (d:Decision) ON (d.session_id);

CREATE INDEX decision_timestamp IF NOT EXISTS
FOR (d:Decision) ON (d.timestamp);

CREATE INDEX decision_user IF NOT EXISTS
FOR (d:Decision) ON (d.user_id);

CREATE INDEX decision_type IF NOT EXISTS
FOR (d:Decision) ON (d.decision_type);

CREATE INDEX decision_outcome IF NOT EXISTS
FOR (d:Decision) ON (d.outcome);

CREATE INDEX decision_confidence IF NOT EXISTS
FOR (d:Decision) ON (d.confidence);

// Session indexes
CREATE INDEX session_user IF NOT EXISTS
FOR (s:Session) ON (s.user_id);

CREATE INDEX session_org IF NOT EXISTS
FOR (s:Session) ON (s.organization_id);

CREATE INDEX session_status IF NOT EXISTS
FOR (s:Session) ON (s.status);

CREATE INDEX session_started IF NOT EXISTS
FOR (s:Session) ON (s.started_at);

// Service indexes
CREATE INDEX service_name IF NOT EXISTS
FOR (svc:Service) ON (svc.name);

CREATE INDEX service_owner IF NOT EXISTS
FOR (svc:Service) ON (svc.owner);

// Commit indexes
CREATE INDEX commit_timestamp IF NOT EXISTS
FOR (c:Commit) ON (c.timestamp);

CREATE INDEX commit_repo IF NOT EXISTS
FOR (c:Commit) ON (c.repository);

// Policy indexes
CREATE INDEX policy_name IF NOT EXISTS
FOR (p:Policy) ON (p.name);

CREATE INDEX policy_version IF NOT EXISTS
FOR (p:Policy) ON (p.version);

// ============================================
// Full-Text Search Indexes
// ============================================

// Search decisions by action and reasoning
CREATE FULLTEXT INDEX decision_search IF NOT EXISTS
FOR (d:Decision) ON EACH [d.action, d.reasoning, d.decision_type];

// Search services by name and description
CREATE FULLTEXT INDEX service_search IF NOT EXISTS
FOR (svc:Service) ON EACH [svc.name, svc.description];

// ============================================
// Relationship Types Documentation
// ============================================
// The following relationship types are used in the lineage graph:
//
// (Session)-[:MADE]->(Decision)
//   - A session makes decisions
//   - Properties: timestamp
//
// (Decision)-[:LED_TO]->(Decision)
//   - One decision leads to another
//   - Properties: reason, delay_ms
//
// (Decision)-[:MODIFIED]->(Service)
//   - A decision modifies a service
//   - Properties: change_type, paths
//
// (Decision)-[:RESULTED_IN]->(Commit)
//   - A decision resulted in a git commit
//   - Properties: files_changed, lines_added, lines_removed
//
// (User)-[:OPERATED]->(Session)
//   - A user operated (supervised) a session
//   - Properties: started_at
//
// (Decision)-[:EVALUATED_BY]->(Policy)
//   - A decision was evaluated by a policy
//   - Properties: result, reasons
//
// (Session)-[:BELONGS_TO]->(Organization)
//   - A session belongs to an organization
//
// (Decision)-[:TRIGGERED]->(Escalation)
//   - A decision triggered an escalation request
//   - Properties: reason
//
// (User)-[:APPROVED|REJECTED]->(Escalation)
//   - A user approved or rejected an escalation
//   - Properties: timestamp, comment

// ============================================
// Sample Queries for Reference
// ============================================

// Query: Get complete decision lineage chain
// MATCH path = (root:Decision)-[:LED_TO*0..10]->(d:Decision {id: $decision_id})
// RETURN path
// ORDER BY length(path) DESC
// LIMIT 1

// Query: Get all decisions for a session with relationships
// MATCH (s:Session {id: $session_id})-[:MADE]->(d:Decision)
// OPTIONAL MATCH (d)-[:LED_TO]->(child:Decision)
// OPTIONAL MATCH (d)-[:MODIFIED]->(svc:Service)
// OPTIONAL MATCH (d)-[:RESULTED_IN]->(c:Commit)
// RETURN d, collect(child), collect(svc), collect(c)
// ORDER BY d.timestamp

// Query: Find sessions with anomalous decision counts
// MATCH (s:Session)-[:MADE]->(d:Decision)
// WITH s, count(d) as decision_count
// WHERE decision_count > 100
// RETURN s, decision_count
// ORDER BY decision_count DESC

// Query: Get policy effectiveness metrics
// MATCH (d:Decision)-[r:EVALUATED_BY]->(p:Policy)
// WITH p, count(d) as evaluations,
//      sum(CASE WHEN r.result = 'allowed' THEN 1 ELSE 0 END) as allowed,
//      sum(CASE WHEN r.result = 'denied' THEN 1 ELSE 0 END) as denied
// RETURN p.name, evaluations, allowed, denied,
//        toFloat(denied) / evaluations as denial_rate
// ORDER BY denial_rate DESC

// Query: Trace commits back to decisions
// MATCH (c:Commit {sha: $sha})<-[:RESULTED_IN]-(d:Decision)<-[:MADE]-(s:Session)<-[:OPERATED]-(u:User)
// RETURN c, d, s, u

// Query: Find high-risk decisions (low confidence, multiple services)
// MATCH (d:Decision)-[:MODIFIED]->(svc:Service)
// WITH d, count(svc) as service_count
// WHERE d.confidence < 0.7 AND service_count > 1
// RETURN d, service_count
// ORDER BY d.confidence ASC
