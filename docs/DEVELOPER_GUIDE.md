# ADP Developer Guide

**Version**: 1.4.0 | **Last Updated**: 2026-02-21 | **Status**: Alpha

Comprehensive developer documentation for the Agent Developer Portal (ADP) -- governance infrastructure for AI coding agents via the Model Context Protocol (MCP).

---

## Table of Contents

1. [What is ADP?](#what-is-adp)
2. [Why Do You Need It?](#why-do-you-need-it)
3. [Architecture Overview](#architecture-overview)
4. [Getting Started](#getting-started)
5. [MCP Tools Reference](#mcp-tools-reference)
6. [Governance Model](#governance-model)
7. [Policy Engine](#policy-engine)
8. [Deployment Options](#deployment-options)
9. [Git Enforcement](#git-enforcement)
10. [HTTP API Reference](#http-api-reference)
11. [Authentication and User Management](#authentication-and-user-management)
12. [Backstage Integration](#backstage-integration)
13. [Web Dashboard](#web-dashboard)
14. [Observability and Metrics](#observability-and-metrics)
15. [Security Model](#security-model)
16. [Multi-Tenancy](#multi-tenancy)
17. [CLI Reference](#cli-reference)
18. [Configuration Reference](#configuration-reference)
19. [Contributing](#contributing)

---

## What is ADP?

The Agent Developer Portal (ADP) is an open source governance layer for AI coding agents. It sits between AI agents (Claude Code, Cursor, Copilot, and others) and the code they modify, enforcing organizational policies and creating an audit trail of every decision an agent makes.

ADP connects to agents via the [Model Context Protocol (MCP)](https://modelcontextprotocol.io/), an open standard for tool integration with AI systems. Any MCP-compatible agent can use ADP without custom integration work.

### Core Capabilities

**Decision Audit Trail** -- Every action an agent takes is logged with the agent's reasoning, confidence score, alternatives considered, and the policy evaluation result. This creates a queryable record of what happened, why, and whether it complied with your policies.

**Policy Enforcement** -- Define rules about what agents can and cannot do. Trust levels control agent autonomy. Blast radius analysis limits the scope of changes. Time-based policies restrict when certain actions can happen. Sensitive file protection blocks access to secrets and credentials. All of this is evaluated before an agent acts.

**Human Escalation** -- When an agent proposes an action that exceeds its trust level, ADP routes the request to a human approver with priority-based expiration. The agent can poll for the result and proceed once approved.

**Documentation Engine** -- A background DocAgent runs inside the MCP server process, polling for ended sessions every 30 seconds. When a session ends, it fetches the associated decision records and generates markdown documentation (session summaries, risk reports, pattern reports) using Go templates. Optionally, set `ADP_DOC_LLM_API_KEY` to an Anthropic API key and the DocAgent will call Claude to refine template output into polished prose documentation -- this is off by default so teams that don't want to spend tokens on it can opt out. When no API key is set, template output is the final output. The intended design is that a documentation agent can optionally run alongside coding agents, keeping governance documentation current as agents work -- producing output suitable for Backstage TechDocs or other documentation systems.

---

## Why Do You Need It?

AI coding agents are writing and modifying production code with increasing autonomy. Without governance infrastructure:

- **No visibility**: You cannot see what an agent changed, why it changed it, or what alternatives it considered.
- **No policy enforcement**: An agent operating on a junior developer's machine has the same access as one operating for a staff engineer. There is no way to scope what an agent can do based on trust or context.
- **No audit trail**: When something breaks, there is no record of the reasoning chain that led to the change. You are debugging blind.
- **No escalation path**: Either the agent acts autonomously or it stops. There is no middle ground where it can propose an action and wait for human approval.
- **No compliance**: Regulated industries need evidence that code changes followed a review process. AI-generated changes are a gap in that evidence chain.

### Who Is This For?

- **Engineering teams** adopting AI coding agents who need governance without slowing down development
- **Platform teams** building internal developer platforms who want to integrate agent governance into their existing tooling (especially Backstage)
- **Security and compliance teams** who need audit trails and policy enforcement for AI-generated code changes
- **Organizations** running multiple AI agents across teams who need consistent governance standards

---

## Architecture Overview

```
┌──────────────────────────────────────────────────────────────────┐
│                        AI Coding Agents                          │
│           Claude Code  |  Cursor  |  Copilot  |  Custom          │
└──────────────────┬───────────────────────────────────────────────┘
                   │ MCP Protocol (stdio or SSE)
┌──────────────────▼───────────────────────────────────────────────┐
│                     adp-mcp (MCP Server)                         │
│                                                                  │
│   11 tools: start_session, check_action, log_decision,           │
│   request_approval, get_approval, prepare_commit,                │
│   verify_commit, get_context, get_docs, heartbeat, end_session   │
└──────────────────┬───────────────────────────────────────────────┘
                   │
┌──────────────────▼───────────────────────────────────────────────┐
│                       Core Domain                                │
│                                                                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐            │
│  │  Governance  │  │   Session    │  │    Audit     │            │
│  │   Engine     │  │  Management  │  │   Logger     │            │
│  └──────────────┘  └──────────────┘  └──────────────┘            │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐            │
│  │   Context    │  │    Docs      │  │  Escalation  │            │
│  │   Engine     │  │   Engine     │  │   Manager    │            │
│  └──────────────┘  └──────────────┘  └──────────────┘            │
└──────────────────┬───────────────────────────────────────────────┘
                   │
┌──────────────────▼───────────────────────────────────────────────┐
│                       Storage Layer                              │
│                                                                  │
│  SQLite (default, zero-config)  |  PostgreSQL (production)       │
│  Neo4j (decision graph, partial) | ClickHouse (analytics, stub)  │
│  Qdrant (vector search, stub)   | Redis (caching, planned)       │
└──────────────────────────────────────────────────────────────────┘
```

### Three Entry Points

| Binary | Purpose | When to Use |
|--------|---------|-------------|
| `adp-mcp` | MCP server for agent integration | **Primary**. Agents connect here via MCP protocol. |
| `adp-server` | HTTP REST API server | Non-MCP integrations, web dashboard, admin operations. |
| `adp-cli` | Command-line interface | Manual policy management, session inspection, debugging. |

### Enforcement Models

ADP supports three enforcement models with different trust assumptions. See [docs/ENFORCEMENT_OPTIONS.md](ENFORCEMENT_OPTIONS.md) for detailed comparison.

| Model | Trust Level | How It Works |
|-------|-------------|--------------|
| **Advisory (REST API)** | Cooperative | Agent voluntarily calls governance API. No enforcement if agent ignores it. |
| **MCP Server** | Protocol-enforced | Governance integrated into the MCP tool calls. Agent uses MCP tools for all governed actions. |
| **Gateway/Proxy** | Zero trust | All agent actions flow through ADP proxy. No bypass possible. (Future -- not yet implemented.) |

---

## Getting Started

### Prerequisites

- Go 1.25 or later
- An MCP-compatible AI agent (Claude Code, Cursor, or any MCP client)

### Build

```bash
git clone <repository-url>
cd adp

# Build the MCP server (required)
go build ./cmd/adp-mcp

# Build the HTTP API server (optional)
go build ./cmd/adp-server

# Build the CLI tool (optional)
go build ./cmd/adp-cli
```

### Configure Your Agent

Add ADP's MCP server to your agent's MCP configuration. No other setup is required -- SQLite is the default store and works without any external services.

**Claude Code** (`~/.claude/claude_desktop_config.json` or MCP settings):

```json
{
  "mcpServers": {
    "adp": {
      "command": "/absolute/path/to/adp-mcp"
    }
  }
}
```

**With PostgreSQL** (optional, for production):

```json
{
  "mcpServers": {
    "adp": {
      "command": "/absolute/path/to/adp-mcp",
      "env": {
        "ADP_STORE": "postgres",
        "ADP_DATABASE_POSTGRES_HOST": "localhost",
        "ADP_DATABASE_POSTGRES_PASSWORD": "your-password"
      }
    }
  }
}
```

### Verify

Once your agent connects, it gains access to tools prefixed with `adp_`. The typical agent workflow is:

1. `adp_start_session` -- Initialize a session with identity and trust level
2. `adp_check_action` -- Check if a proposed action is allowed
3. `adp_log_decision` -- Record the decision and reasoning
4. `adp_prepare_commit` -- Register file changes before committing
5. `adp_end_session` -- Close the session

### Run Tests

```bash
go test ./...
go vet ./...
```

---

## MCP Tools Reference

The MCP server exposes 11 tools to agents. Each tool follows a request/response pattern over the MCP protocol.

### adp_start_session

Initialize a governance session. Must be called before any other governance tool.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `organization_id` | string | No | Organization identifier |
| `user_id` | string | No | User who initiated the agent |
| `tool` | string | No | Agent tool name (e.g., "claude_code", "cursor") |
| `trust_level` | integer | No | Trust level 1-5 (default: 1) |
| `capabilities` | string[] | No | Agent capabilities list |
| `service_scope` | string[] | No | Services the agent can access |

**Response:**

```json
{
  "session_id": "uuid-here",
  "trust_level": 3,
  "capabilities": ["read", "write", "test"],
  "constraints": [
    {
      "type": "max_files_per_commit",
      "parameters": {"max_files": 50},
      "description": "Maximum 50 files per commit"
    }
  ],
  "expires_at": "2026-02-07T18:00:00Z"
}
```

**Default constraints by trust level:**

| Trust Level | Constraints |
|-------------|-------------|
| 1 (Observer) | `read_only`, `no_file_modifications` |
| 2 (Contributor) | `require_review_for_delete`, `max_files: 10` |
| 3 (Developer) | `max_files: 50` |
| 4+ (Maintainer/Admin) | None |

### adp_check_action

Evaluate a proposed action against the policy engine. Call this before executing any governed action.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `session_id` | string | Yes | Active session ID |
| `action_type` | string | Yes | Action type (e.g., "modify_file", "deploy", "delete") |
| `target` | object | Yes | What the action affects |
| `target.paths` | string[] | No | File paths affected |
| `target.services` | string[] | No | Services affected |
| `target.environment` | string | No | Target environment ("production", "staging", etc.) |
| `context` | object | No | Additional context |

**Response:**

```json
{
  "allowed": false,
  "requires_approval": true,
  "denied_reasons": [
    "access to sensitive file '.env' blocked by pattern '.env'"
  ],
  "policy_names": ["Deny Sensitive Files", "Blast Radius Limit"],
  "restrictions": ["production_environment_restrictions"]
}
```

### adp_request_approval

Request asynchronous human approval for actions that exceed the agent's trust level.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `session_id` | string | Yes | Active session ID |
| `action` | string | Yes | Description of the action |
| `action_type` | string | Yes | Action category |
| `target` | object | Yes | Files/services affected |
| `reason` | string | Yes | Why the action is needed |
| `priority` | string | No | "low", "normal", "high", "critical" |

**Response:**

```json
{
  "approval_id": "uuid-here",
  "status": "pending",
  "expires_at": "2026-02-07T10:00:00Z"
}
```

**Expiration by priority:**

| Priority | TTL |
|----------|-----|
| Critical | 15 minutes |
| High | 30 minutes |
| Normal | 1 hour |
| Low | 4 hours |

### adp_get_approval

Poll the status of an approval request.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `approval_id` | string | Yes | Approval request ID |

**Response:**

```json
{
  "status": "approved",
  "approver_comment": "Approved for hotfix deployment",
  "resolved_at": "2026-02-07T09:32:00Z"
}
```

Status values: `pending`, `approved`, `rejected`, `expired`.

### adp_log_decision

Record a decision in the audit trail. Call this after every significant action, whether it was allowed or denied.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `session_id` | string | Yes | Active session ID |
| `decision_type` | string | Yes | Decision category (e.g., "code_change", "file_create") |
| `action` | string | Yes | What was done |
| `target` | object | Yes | What was affected |
| `reasoning` | object | Yes | Why the decision was made |
| `confidence` | float | Yes | Confidence score (0.0-1.0) |
| `alternatives` | object[] | No | Other options considered |

**Response:**

```json
{
  "decision_id": "uuid-here",
  "recorded_at": "2026-02-07T09:15:00Z"
}
```

### adp_prepare_commit

Register file changes before committing so the audit trail links to the Git commit.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `session_id` | string | Yes | Active session ID |
| `files` | string[] | Yes | Files being committed |
| `message` | string | Yes | Commit message |

**Response:**

```json
{
  "commit_token": "token-here",
  "approved": true,
  "reason": ""
}
```

The `commit_token` is used by Git hooks to verify that the commit was registered with ADP.

### adp_verify_commit

Verify a commit against the audit trail after it has been made.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `commit_sha` | string | Yes | Git commit SHA |

### adp_get_context

Retrieve token-budgeted context for a task. Returns context in three priority layers.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `session_id` | string | Yes | Active session ID |
| `service_id` | string | No | Target service |
| `task` | string | No | Task description |
| `token_budget` | integer | No | Maximum tokens to return |

**Response:**

```json
{
  "essential": {
    "content": "...",
    "tokens": 500
  },
  "task_relevant": {
    "content": "...",
    "tokens": 1200
  },
  "supporting": {
    "content": "...",
    "tokens": 300
  }
}
```

### adp_get_docs

Retrieve auto-generated documentation from the documentation engine.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `category` | string | Yes | "session_summary", "risk_report", or "pattern_report" |
| `session_id` | string | No | Filter by session |
| `query` | string | No | Search query |
| `limit` | integer | No | Max results |

### adp_heartbeat

Keep a session alive. Sessions expire after 8 hours of inactivity.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `session_id` | string | Yes | Active session ID |

### adp_end_session

Close a governance session. Records session end time.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `session_id` | string | Yes | Session to close |

---

## Governance Model

### Trust Levels

ADP uses a five-level trust hierarchy that controls what actions an agent can perform autonomously.

| Level | Name | Description | Autonomy |
|-------|------|-------------|----------|
| 1 | Observer | Read-only access. Can search and analyze but cannot modify. | None |
| 2 | Contributor | Limited writes. Deletions require review. Max 10 files per action. | Propose only |
| 3 | Developer | Standard development. Max 50 files per action. Can run tests. | Execute safe |
| 4 | Maintainer | Most operations including production deploys with approval. Minimal restrictions. | Full |
| 5 | Admin | Unrestricted. Can override time-based and policy restrictions. | Full |

### Trust Level Capabilities

| Capability | L1 | L2 | L3 | L4 | L5 |
|------------|----|----|----|----|-----|
| Read files | Yes | Yes | Yes | Yes | Yes |
| Modify files | No | Yes (limited) | Yes | Yes | Yes |
| Delete files | No | With review | Yes | Yes | Yes |
| Max files per action | 3 | 5 | 10 | 25 | 100 |
| Max lines per action | 100 | 200 | 500 | 2,000 | 10,000 |
| Max services affected | 1 | 1 | 2 | 5 | 10 |
| Cost limit per action | $10 | $50 | $200 | $1,000 | $10,000 |
| Production deploy | No | No | No | With approval | Yes |
| Override time policies | No | No | No | Partial | Yes |
| Override blast radius | No | No | No | Yes | Yes |

### Graduated Autonomy

Trust levels are not static. ADP supports a graduated autonomy system where agents can be promoted based on their track record.

**Promotion requirements:**

| To Level | Sessions Required | Decisions Required | Max Escalation Rate | Min Confidence | Cooldown |
|----------|------------------|--------------------|---------------------|----------------|----------|
| 2 | 5 | 20 | 50% | 0.6 | 24 hours |
| 3 | 20 | 100 | 30% | 0.7 | 7 days |
| 4 | 50 | 500 | 15% | 0.8 | 30 days |
| 5 | 100 | 1,000 | 10% | 0.9 | 90 days |

Escalation rate = percentage of decisions that required human approval. Lower is better.

### Blast Radius Analysis

Before an action is executed, ADP calculates a risk score based on the scope of the proposed change.

**Risk factors (weighted):**

| Factor | Weight | Description |
|--------|--------|-------------|
| Files affected | 40% | Number of files modified, normalized against trust level limit |
| Lines changed | 20% | Total lines across all files |
| Services affected | 20% | Number of different services impacted |
| Critical paths | 20% | Whether the change touches security-sensitive paths |

**Risk levels:**

| Risk Level | Score Range | Action |
|------------|-------------|--------|
| Low | 0.0 - 0.3 | Allowed |
| Medium | 0.3 - 0.6 | Allowed with warnings |
| High | 0.6 - 0.8 | May require approval |
| Critical | 0.8 - 1.0 | Requires approval or denied |

**Critical path patterns** (always flagged):

- `*.env*` -- environment files
- `*secrets*` -- secret directories
- `*credentials*` -- credential files
- `**/config/production*` -- production configuration
- `**/.git/*` -- version control
- `**/migrations/*` -- database migrations
- `**/security/*` -- security code
- `**/auth/*` -- authentication code

**Protected patterns** (always denied):

- `**/.env`, `**/.env.*`
- `**/secrets.yaml`, `**/secrets.json`
- `**/*.pem`, `**/*.key`

### Escalation Workflow

When an action requires human approval:

```
Agent calls adp_check_action
    │
    ├── Allowed → Agent proceeds
    │
    └── Requires approval → Agent calls adp_request_approval
                                │
                                ├── Returns approval_id + pending status
                                │
                                ├── Agent polls adp_get_approval periodically
                                │
                                ├── Human approves/rejects via admin interface
                                │
                                └── Agent receives approved/rejected/expired
```

Escalation requests have priority-based TTLs. If no human responds within the TTL, the request expires and the agent must decide whether to skip the action or re-escalate.

---

## Policy Engine

### Policy Types

ADP's unified policy engine evaluates three types of policies in order:

1. **Builtin policies** -- Hardcoded evaluators registered at engine startup
2. **OPA/Rego policies** -- Dynamic policies loaded from the database, evaluated via Open Policy Agent
3. **Custom policies** -- Extensible webhook-based evaluation (future)

### Builtin Policies

| Policy | Description | Configuration |
|--------|-------------|---------------|
| `deny_sensitive_files` | Blocks access to `.env`, `.pem`, `.key`, `*.secret`, `credentials.*` | `patterns`: list of glob patterns |
| `blast_radius_limit` | Limits files per action based on trust level | `max_files`, `trustOverride` (trust 4+ can override) |
| `off_hours_production` | Blocks production deploys 22:00-06:00 | `start_hour`, `end_hour`, `min_trust` (default: 5) |
| `cost_limit` | Per-trust-level cost limits | `limits_by_trust`: `{1: 10, 2: 50, ...}` |
| `require_migration_approval` | Forces approval for `migrate_database`, `alter_schema` | `action_types` |
| `rate_limit_api` | Placeholder for API rate limiting | Not yet implemented |

### OPA/Rego Policies

The default policy file is at `policies/default.rego`. You can extend or replace it.

**Default rules:**

```rego
package adp.governance

default allow := false

# Reads always allowed
allow { input.action.type == "read" }
allow { input.action.type == "list" }

# Modify requires trust level 3+
allow {
    input.action.type == "modify_file"
    input.session.trust_level >= 3
    not touches_sensitive_paths
}

# Production deploy requires level 5 or triggers approval
requires_approval {
    input.action.type == "deploy"
    input.context.environment == "production"
    input.session.trust_level < 5
}
```

**Writing custom Rego policies:**

Custom policies use the `deny` rule pattern. If any `deny` rule returns a message, the action is blocked.

```rego
package policy

# Block modifications to vendor directory
deny[msg] {
    input.action.type == "modify_file"
    some path in input.action.target.paths
    contains(path, "vendor/")
    msg := "Cannot modify vendor directory"
}

# Require approval for production deployments from trust < 4
deny[msg] {
    input.action.type == "deploy"
    input.context.environment == "production"
    input.session.trust_level < 4
    msg := "Production deployments require trust level 4+"
}
```

**Policy input structure:**

```json
{
  "action": {
    "type": "modify_file",
    "target": {
      "paths": ["src/config.go", "src/main.go"],
      "services": ["api-server"],
      "environment": "staging"
    },
    "metadata": {
      "estimated_cost": 0
    }
  },
  "session": {
    "trust_level": 3
  },
  "context": {
    "environment": "staging",
    "time": "2026-02-07T14:30:00Z",
    "hour": 14
  }
}
```

### Policy Evaluation Flow

```
1. Load policies from database (cached for 5 minutes)
2. For each enabled policy:
   a. Check if agent trust level meets MinTrustLevel threshold
   b. Evaluate using appropriate engine (builtin / OPA / custom)
   c. Collect denial reasons and matched policy names
3. Evaluate base Rego policy from filesystem if configured
4. Aggregate results:
   - If any policy denies → action blocked, RequiresApproval = true
   - If all policies allow → action permitted
5. Return EvaluationResult with denied_reasons, matched_policies, warnings
```

### Time-Based Policies

ADP supports three categories of time restrictions:

**Off-hours restrictions** -- Block production deployments outside business hours (default: 22:00-06:00). Requires trust level 5 to override.

**Late night restrictions** -- Restrict destructive actions (deploy, delete, modify_database, modify_config) between 22:00-06:00. Requires trust level 4+.

**Maintenance windows** -- Block all non-admin actions during a defined maintenance window. Requires trust level 5. Created with `NewMaintenanceWindowPolicy()`.

All time policies support configurable timezones via IANA timezone database (e.g., "America/New_York").

### Policy Simulation

The `PolicySimulator` enables testing policies without deploying them:

- **Dry-run evaluation** -- Test a policy against a proposed input without affecting real sessions
- **Version comparison** -- Compare results between a draft policy and the current production policy
- **Trace execution** -- Get step-by-step evaluation traces for debugging
- **Batch simulation** -- Test multiple scenarios in a single request

---

## Deployment Options

### Option 1: Local Development (SQLite)

Zero configuration. Build the binary and point your agent at it.

```bash
go build ./cmd/adp-mcp
# Configure your agent to use /path/to/adp-mcp as an MCP server
```

SQLite database is created automatically at `~/.adp/adp.db` (or in the current directory).

### Option 2: Docker Compose

The included `docker-compose.yml` runs the full stack with PostgreSQL, Redis, Neo4j, Qdrant, and ClickHouse.

```bash
docker compose up -d
```

**Services:**

| Service | Port | Purpose |
|---------|------|---------|
| `adp-server` | 8080 | HTTP API |
| `adp-mcp` | -- (stdio) | MCP server |
| `postgres` | 5432 | Primary data store |
| `redis` | 6379 | Caching, rate limiting |
| `neo4j` | 7474, 7687 | Decision graph (partial) |
| `qdrant` | 6333, 6334 | Vector search (stub) |
| `clickhouse` | 8123, 9000 | Analytics (partial) |
| `web` | 3000 | Dashboard (skeleton) |

### Option 3: Container Images

ADP uses Chainguard base images (`cgr.dev/chainguard/static:latest`) for minimal attack surface. The Dockerfile builds three binaries in a multi-stage build:

```dockerfile
# Build stage
FROM golang:1.25 AS builder
# ... builds adp-server, adp-cli, adp-mcp

# Runtime stage
FROM cgr.dev/chainguard/static:latest
COPY --from=builder /app/adp-server /usr/local/bin/
COPY --from=builder /app/adp-cli /usr/local/bin/
COPY --from=builder /app/adp-mcp /usr/local/bin/
```

The container runs as a non-root user (`nonroot:nonroot`, UID 65532).

### Option 4: Kubernetes with Helm

Helm charts exist at `deploy/helm/adp/` but are not yet production-tested. The chart includes:

- Deployment with configurable replicas and resource limits
- Service exposing HTTP (8080) and optional metrics (9090) ports
- ConfigMap for application configuration and OPA policy injection
- Ingress with TLS support
- HorizontalPodAutoscaler based on CPU/memory
- ServiceMonitor for Prometheus integration
- Liveness and readiness probes

**Install:**

```bash
helm install adp deploy/helm/adp/ \
  --set database.postgres.host=your-postgres-host \
  --set database.postgres.password=your-password
```

**Key Helm values:**

```yaml
# deploy/helm/adp/values.yaml
replicaCount: 2

image:
  repository: adp/adp-server
  tag: latest

database:
  postgres:
    host: postgres
    port: 5432
    database: adp
    username: adp

server:
  port: 8080
  metricsPort: 9090

governance:
  defaultTrustLevel: 3
  blastRadiusLimit: 10
  enableTimePolicies: true
```

The ConfigMap template injects OPA policies from `policies/default.rego` into the running container, so policy updates can be deployed via Helm upgrades without rebuilding the image.

### Database Migrations

| Database | Location | Notes |
|----------|----------|-------|
| SQLite | Auto-migrated on startup | No manual migration needed (includes users table) |
| PostgreSQL | `migrations/postgres/` | 5 migrations (schema, indexes, constraints, functions, users) |
| Neo4j | `migrations/neo4j/` | 2 migrations (constraints, indexes) |
| ClickHouse | `migrations/clickhouse/` | 1 migration (analytics tables) |

### CI/CD Integration

CI templates exist for three platforms:

| Platform | Location | Status |
|----------|----------|--------|
| GitHub Actions | `ci-templates/github-actions/` | Template available |
| GitLab CI | `ci-templates/gitlab-ci/` | Template available |
| Azure DevOps | `ci-templates/azure-devops/` | Template available |

---

## Git Enforcement

ADP enforces governance on agent commits through Git hooks and a three-phase commit state machine. This ensures every agent-authored commit has an audit trail linking it back to a governance session.

**Zero-config setup**: The `adp-mcp` server includes an embedded HTTP sidecar (default port 8081, configurable via `ADP_HTTP_PORT`) that serves the commit endpoints git hooks need. No separate `adp-server` process is required for MCP-based workflows.

### How It Works

```
Phase 1: PREPARE              Phase 2: REGISTER             Phase 3: VERIFY
(pre-commit hook)              (post-commit hook)            (pre-push hook / CI)

Agent calls                    Post-commit hook              Pre-push hook or CI
adp_prepare_commit             sends commit SHA              calls /v1/commits/verify
  │                            to server                     for each commit
  ├─ Policy engine evaluates     │                             │
  │  files, trust, blast radius  ├─ POST /v1/commits/register  ├─ verified=true → push OK
  ├─ Sensitive file check        │  {token, sha}               └─ verified=false → push blocked
  ├─ Returns commit_token        └─ Status: committed
  └─ Status: prepared
```

### The Three Hooks

| Hook | File | Phase | Blocking? | Purpose |
|------|------|-------|-----------|---------|
| `pre-commit` | `hooks/pre-commit` | Prepare | Yes | Validates session, calls `/v1/commits/prepare`, stores commit token |
| `post-commit` | `hooks/post-commit` | Register | No | Reads token from `.git/ADP_COMMIT_TOKEN`, calls `/v1/commits/register` with commit SHA |
| `pre-push` | `hooks/pre-push` | Verify | Yes | Iterates all commits being pushed, calls `/v1/commits/verify` for each |

### Commit State Machine

```
prepared → committed → verified
    │           │
    │           └─ RegisterCommit(token, sha) sets commit_sha and committed_at
    │
    └─ Prepare(session, files, message) creates record with commit_token
```

**States:**

| State | Set By | Meaning |
|-------|--------|---------|
| `prepared` | `adp_prepare_commit` / `POST /v1/commits/prepare` | Intent registered, commit token issued |
| `committed` | `POST /v1/commits/register` | Commit SHA linked to token, `committed_at` timestamp set |
| `verified` | `POST /v1/commits/verify` (PostgreSQL only) | Explicit verification step completed |

For SQLite (zero-config local dev), `IsCommitVerified` accepts both `committed` and `verified` status. For PostgreSQL, the `Verify()` method sets `verified_at` as a separate state transition.

### Policy Engine Integration

The `adp_prepare_commit` MCP tool evaluates commits against the unified policy engine when available. This goes beyond sensitive file checks to include trust levels, blast radius limits, and time-based policies.

When the policy engine denies a commit, the prepare step returns `approved: false` with the denial reasons. The pre-commit hook blocks the commit.

When the policy engine requires approval, the prepare step returns `approved: false, requires_approval: true`. The agent must request human approval before proceeding.

### Installing Hooks

```bash
# Install hooks in the current repository
./hooks/install.sh

# Check installation status
./hooks/install.sh --check

# Force reinstall (overwrites existing)
./hooks/install.sh --force

# Uninstall
./hooks/install.sh --uninstall
```

The installer creates wrapper hooks that run ADP validation first, then execute any existing hooks (renamed to `<hook>.local`).

### Bypass Mechanism

Human developers can bypass ADP hooks for non-agent commits using a token-based system:

```bash
# One-time setup: configure a bypass token
./hooks/install.sh --configure-bypass
# Enter and confirm a secret token (stored as SHA-256 hash in .git/adp/bypass_hash)

# Use the token when committing
ADP_BYPASS_TOKEN=your-secret-token git commit -m "human commit"
```

The bypass token is verified against the stored hash at commit time. Bypass events are logged to the ADP server on a best-effort basis for audit purposes.

### Agent Git Workflow

When using `adp-mcp`, the agent must bridge MCP session credentials into the shell environment for git hooks:

1. Agent calls `adp_start_session` → receives `session_id`, `session_token`, `http_port`
2. Agent calls `adp_prepare_commit` with staged files
3. Agent runs git with env vars: `ADP_SESSION_ID=<session_id> ADP_SESSION_TOKEN=<session_token> ADP_URL=http://localhost:<http_port> git commit -m "..."`
4. Pre-commit hook calls the sidecar's `/v1/commits/prepare` endpoint
5. Post-commit hook calls `/v1/commits/register` with the commit SHA

The sidecar returns top-level JSON (`{"approved": true, "commit_token": "..."}`) without the `{"data": {...}}` envelope used by `adp-server`.

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ADP_SESSION_ID` | -- | Active ADP session ID (from `adp_start_session` response) |
| `ADP_SESSION_TOKEN` | -- | Session token (from `adp_start_session` response) |
| `ADP_URL` | `http://localhost:8081` | URL of ADP HTTP endpoint (sidecar or `adp-server`) |
| `ADP_HTTP_PORT` | `8081` | Port for the `adp-mcp` HTTP sidecar |
| `ADP_BYPASS_TOKEN` | -- | Bypass token for human commits |
| `ADP_TIMEOUT` | `10` | Request timeout in seconds |
| `ADP_MODE` | -- | Set to `standalone` to use `adp-mcp` binary directly for verification |
| `ADP_MCP_BIN` | `adp-mcp` | Path to `adp-mcp` binary (standalone mode) |

### CI/CD Verification

CI templates (`ci-templates/`) verify commits during the push pipeline. Each template calls `POST /v1/commits/verify` for every commit in the push range and fails the pipeline if any commit lacks an audit trail.

For standalone environments without a running ADP server, the pre-push hook supports `ADP_MODE=standalone` which calls the `adp-mcp` binary directly via stdin to verify commits against the local SQLite database.

### Commit Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/commits/prepare` | Register commit intent, evaluate policy, return token |
| `POST` | `/v1/commits/register` | Link commit SHA to token after commit succeeds |
| `POST` | `/v1/commits/verify` | Check if a commit SHA has a valid audit trail |

---

## HTTP API Reference

The HTTP API is served by `adp-server` and provides REST endpoints for governance operations, session management, and administration. All endpoints require JWT authentication unless otherwise noted.

**OpenAPI Specification**: The full machine-readable API spec is at [`api/openapi.yaml`](../api/openapi.yaml) (OpenAPI 3.1, 43 operations, 42 schemas). Use this for SDK generation, API testing, or importing into tools like Swagger UI or Postman.

### Governance Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/governance/check` | Evaluate action against policy engine |
| `POST` | `/v1/governance/approvals` | Create approval request |
| `GET` | `/v1/governance/approvals/{id}` | Get approval status |
| `PATCH` | `/v1/governance/approvals/{id}` | Resolve approval (approve/reject) |

### Session Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/sessions` | Create new session |
| `GET` | `/v1/sessions/{id}` | Get session details |
| `GET` | `/v1/sessions` | List sessions |
| `DELETE` | `/v1/sessions/{id}` | End session |

### Decision Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/decisions` | Log a decision |
| `GET` | `/v1/decisions/{id}` | Get decision details |
| `GET` | `/v1/decisions` | List/search decisions |

### Commit Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/commits/prepare` | Register commit intent, evaluate policy, return token |
| `POST` | `/v1/commits/register` | Link commit SHA to token after commit succeeds |
| `POST` | `/v1/commits/verify` | Check if a commit SHA has a valid audit trail |

### Documentation Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/docs` | List generated documents |
| `GET` | `/v1/docs/{id}` | Get specific document |
| `POST` | `/v1/docs/generate` | Trigger document generation |

### Authentication Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/auth/register` | Register a new user account |
| `POST` | `/v1/auth/login` | Login with email and password |
| `POST` | `/v1/auth/refresh` | Refresh an expired access token |
| `GET` | `/v1/auth/me` | Get current user profile |
| `PATCH` | `/v1/auth/me` | Update current user profile |

### Admin Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/admin/users` | List all users (admin only) |
| `GET` | `/v1/admin/users/{id}` | Get user details (admin only) |
| `PATCH` | `/v1/admin/users/{id}` | Update user role/status (admin only) |
| `DELETE` | `/v1/admin/users/{id}` | Disable a user account (admin only) |

### Health and Metrics

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check (no auth required) |
| `GET` | `/ready` | Readiness check -- verifies database connectivity (no auth required) |
| `GET` | `/metrics` | Prometheus metrics (planned) |

### Authentication

ADP supports three authentication methods, used independently or in combination:

**1. API Key Authentication** -- Set `ADP_API_KEY` to enable. Pass via `X-API-Key` header or `Authorization: Bearer <key>`.

**2. External JWT (JWKS)** -- Set `ADP_AUTH_JWKS_URL` to enable. Validates tokens against a remote JWKS endpoint, supporting enterprise identity providers (Okta, Auth0, Azure AD). The JWT must contain:
- `sub` (subject) -- User ID (required)
- `org` -- Organization identifier (optional, used for tenant resolution)
- `roles` -- Array of role strings (optional)
- Standard claims: `iss`, `aud`, `exp`, `nbf`, `iat`

**3. Local JWT (built-in auth)** -- Set `ADP_JWT_SECRET` to enable. ADP issues its own HMAC-SHA256 JWTs via the `/v1/auth/login` and `/v1/auth/refresh` endpoints. Users register via `/v1/auth/register` with email and password (bcrypt hashed). Access tokens expire in 15 minutes; refresh tokens in 7 days. See [Authentication and User Management](#authentication-and-user-management) for details.

The combined auth middleware (`internal/api/middleware/combined_auth.go`) accepts any configured method. When multiple methods are configured, a request matching any one method is authenticated.

---

## Authentication and User Management

ADP includes a built-in user management system with role-based access control (RBAC). This is optional -- you can use API key auth or external JWT (JWKS) without enabling local user management.

### Enabling Local Auth

Set `ADP_JWT_SECRET` to a strong random string (e.g., `openssl rand -hex 32`). This activates:

- User registration and login endpoints (`/v1/auth/*`)
- HMAC-SHA256 JWT token issuance
- Admin user management endpoints (`/v1/admin/*`)
- SQL-based RBAC authorization

### User Roles

| Role | Capabilities |
|------|-------------|
| `admin` | Full access: manage users, view all data, configure policies |
| `user` | Standard access: view sessions, decisions, and reports |

### First User Bootstrap

The first user to register via `POST /v1/auth/register` is automatically assigned the `admin` role. This bootstraps the system without requiring a separate setup step.

Subsequent registrations require either:
- An authenticated admin creating the account (admin sends the register request with their Bearer token)
- Open registration enabled via `ADP_OPEN_REGISTRATION=true`

The register endpoint uses optional authentication: if a Bearer token is present, the middleware parses it (enabling admin-invited registration); if absent, the request passes through (enabling first-user bootstrap).

### Registration Flow

```
POST /v1/auth/register
{
  "email": "admin@example.com",
  "password": "strong-password",
  "name": "Admin User"
}

Response:
{
  "user": {
    "id": "uuid",
    "email": "admin@example.com",
    "name": "Admin User",
    "role": "admin",
    "status": "active"
  },
  "access_token": "eyJ...",
  "refresh_token": "eyJ..."
}
```

### Login Flow

```
POST /v1/auth/login
{
  "email": "admin@example.com",
  "password": "strong-password"
}

Response:
{
  "access_token": "eyJ...",
  "refresh_token": "eyJ...",
  "token_type": "Bearer",
  "expires_in": 900
}
```

### Token Lifecycle

| Token | Lifetime | Usage |
|-------|----------|-------|
| Access token | 15 minutes | `Authorization: Bearer <token>` on API requests |
| Refresh token | 7 days | `POST /v1/auth/refresh` to get a new access token |

### Admin User Management

Admins can manage users via the `/v1/admin/users` endpoints:

- **List users**: `GET /v1/admin/users?limit=20&offset=0` -- returns `{items, total, limit, offset}`
- **Get user**: `GET /v1/admin/users/{id}` -- returns user profile with role and status
- **Update user**: `PATCH /v1/admin/users/{id}` -- change role or status
- **Disable user**: `DELETE /v1/admin/users/{id}` -- soft-deletes (sets status to disabled)

### RBAC Authorization

The SQL-based authorizer (`internal/domain/auth/sql_authorizer.go`) implements the `middleware.Authorizer` interface. It checks the user's role from the JWT claims against endpoint requirements. Admin endpoints require the `admin` role; other authenticated endpoints accept any valid user.

### Architecture

```
internal/domain/user/        -- Domain model: roles, password hashing, JWT token service
internal/domain/auth/        -- SQL-based RBAC authorizer
internal/api/handlers/auth.go    -- Auth handler (register, login, refresh, profile)
internal/api/handlers/admin.go   -- Admin handler (list/get/update/disable users)
internal/store/interfaces.go     -- UserStore interface
internal/infrastructure/database/
  sqlite_user_store.go           -- SQLite implementation
  pg_user_store.go               -- PostgreSQL implementation
migrations/postgres/
  000005_users.up.sql            -- PostgreSQL users table migration
```

### Dashboard Integration

The Next.js dashboard (`web/`) uses NextAuth.js with a credentials provider that calls `/v1/auth/login`. The dashboard API client (`web/src/lib/api.ts`) injects the access token from the NextAuth session into all API requests via `Authorization: Bearer <token>`.

**Dashboard auth env vars:**

| Variable | Description |
|----------|-------------|
| `NEXTAUTH_SECRET` | Session encryption key for NextAuth |
| `NEXTAUTH_URL` | Dashboard's public URL (e.g., `https://dashboard.example.com`) |
| `ADP_API_URL` | Backend API URL (e.g., `https://api.example.com`) |

---

## Backstage Integration

ADP includes a Backstage plugin that surfaces governance data inside your Backstage developer portal.

### Plugin Location

```
backstage-plugins/
├── plugins/
│   └── adp/
│       ├── src/
│       │   ├── plugin.ts              # Plugin registration
│       │   ├── api.ts                 # ADP API client
│       │   ├── components/
│       │   │   ├── AuditPage.tsx      # Full-page audit trail viewer
│       │   │   ├── AuditTable.tsx     # Decision records table with columns
│       │   │   ├── GovernancePage.tsx  # Policy management page
│       │   │   ├── ADPDashboard.tsx   # Overview dashboard
│       │   │   ├── SessionsPage.tsx   # Active/historical sessions
│       │   │   ├── ReportsPage.tsx    # Generated documentation
│       │   │   └── EntityADPContent/  # Entity page tab component
│       │   ├── routes.ts              # Plugin route definitions
│       │   └── index.ts              # Public exports
│       └── package.json
└── README.md
```

### Plugin Architecture

The plugin registers itself into Backstage's plugin system:

```typescript
// plugin.ts
import { createPlugin, createRoutableExtension } from '@backstage/core-plugin-api';
import { rootRouteRef } from './routes';

export const adpPlugin = createPlugin({
  id: 'adp',
  routes: {
    root: rootRouteRef,
  },
});
```

### Pages and Components

**Audit Page** -- Full-page view of the decision audit trail. Displays a table of all decisions with columns for timestamp, session, agent, action, target, confidence, policy result, and status. Supports filtering and search.

**Governance Page** -- Policy management interface. Lists active policies, their types (builtin/Rego/custom), and allows viewing policy details. Future: policy editing and simulation.

**Dashboard** -- Overview metrics including active sessions, decisions today (with trend vs. 7-day average), pending escalations, and policy health score. Includes a 30-day adoption trend chart.

**Sessions Page** -- Lists active and historical agent sessions with details on trust level, agent identity, and session duration.

**Reports Page** -- Generated documentation (session summaries, risk reports, pattern reports) from the documentation engine.

**Entity Tab** -- Embeds ADP governance data directly on Backstage entity pages, so teams can see governance status for their services without leaving the catalog.

### Integration with Other Backstage Components

**Software Catalog** -- The Entity Tab component (`EntityADPContent`) integrates with Backstage's catalog. Each service entity in the catalog can display its associated governance data (decisions, policy compliance, active sessions) on a dedicated "Governance" tab.

**TechDocs (Planned)** -- The intended design is for the documentation engine to produce output in TechDocs-compatible format, so auto-generated governance reports appear alongside service technical documentation in Backstage. Today, the doc engine generates plain markdown stored in the DocStore and viewable via the Backstage plugin's Reports Page -- but there is no automatic export to TechDocs format (no `.docs.yaml` generation, no MkDocs structure). Building this bridge is a roadmap item. In the meantime, reports are accessible through the ADP plugin UI and the `adp_get_docs` MCP tool.

**Search** -- The plugin's audit trail and documentation are searchable within Backstage's unified search, enabling teams to find governance decisions across services.

### API Client

The plugin communicates with ADP's HTTP API via a TypeScript client:

```typescript
// api.ts
export class ADPClient {
  private baseUrl: string;

  async getSessions(params?: SessionQuery): Promise<Session[]>;
  async getDecisions(params?: DecisionQuery): Promise<Decision[]>;
  async getGovernanceStatus(): Promise<GovernanceStatus>;
  async getReports(category: string): Promise<Report[]>;
}
```

### Installation

1. Install the plugin package in your Backstage app
2. Configure the ADP API URL in `app-config.yaml`:

```yaml
# app-config.yaml
adp:
  baseUrl: http://localhost:8080
```

3. Add the plugin to your Backstage app's routes:

```typescript
// App.tsx
import { ADPDashboard } from '@internal/plugin-adp';

<Route path="/adp" element={<ADPDashboard />} />
```

4. Add the entity tab to your catalog entity pages:

```typescript
// EntityPage.tsx
import { EntityADPContent } from '@internal/plugin-adp';

<EntityLayout.Route path="/governance" title="Governance">
  <EntityADPContent />
</EntityLayout.Route>
```

### How Backstage Integration Adds Value

| Backstage Component | ADP Integration | Value |
|---------------------|-----------------|-------|
| Software Catalog | Entity governance tab | See policy compliance per service |
| TechDocs | Auto-generated reports (planned) | Governance docs alongside technical docs (requires TechDocs export, not yet built) |
| Search | Decision/report search | Find governance decisions across services |
| Scaffolder | Policy templates (planned) | Bootstrap governance policies for new services |
| Kubernetes plugin | Deployment governance (planned) | Enforce policies on K8s deployments |

---

## Web Dashboard

A Next.js web dashboard exists at `web/` and is wired to the backend API with authentication.

### Current State

The dashboard provides:
- Login page with credentials authentication (via NextAuth.js → `/v1/auth/login`)
- Dashboard page with governance metric cards (active sessions, decisions today, pending escalations, policy health)
- 30-day adoption trend chart
- Authenticated API client that injects Bearer tokens from the NextAuth session
- Graceful degradation when backend is unavailable (shows placeholder dashes and informational banner)

### Technology Stack

- Next.js 15 with App Router
- React 19
- TypeScript
- Tailwind CSS
- shadcn/ui components (Radix UI primitives)
- Recharts for data visualization
- TanStack React Query for data fetching

### Running Locally

```bash
cd web
npm install
npm run dev
# Opens at http://localhost:3000
```

The dashboard calls the API at the URL configured by `ADP_API_URL` (defaults to `http://localhost:8080`).

### Setup

```bash
cd web
npm install

# Required env vars for auth
NEXTAUTH_SECRET=your-secret-key
NEXTAUTH_URL=http://localhost:3000
ADP_API_URL=http://localhost:8080

npm run dev
# Opens at http://localhost:3000
```

### Status

The dashboard has working authentication but limited page coverage. The Backstage plugin remains the recommended UI integration for teams already using Backstage. See [ROADMAP.md](../ROADMAP.md) Phase 4 for dashboard plans.

---

## Observability and Metrics

### Current State

Observability infrastructure exists but is not fully wired:

- **Prometheus**: ServiceMonitor template in Helm chart, metrics port (9090) exposed. No instrumented metrics endpoints in the Go code yet.
- **Grafana**: Dashboard configurations exist in Docker Compose for visualization. Not connected to live data.
- **ClickHouse**: Schema exists for analytics tables. Read-only reporting queries implemented. No write pipeline from the application.
- **Structured logging**: Application uses Go's `log` package with `[AUDIT]` prefix for decision records.

### What You Can Measure Today

**Decision throughput** -- Query the decision store (SQLite or PostgreSQL) for decision counts by time period, session, agent, or policy outcome.

**Policy evaluation results** -- Each decision record includes `policy_result` showing which policies were evaluated and their outcomes.

**Session activity** -- Track active sessions, session duration, heartbeat frequency, and trust level distribution.

**Escalation metrics** -- Measure approval request volume, approval/rejection rates, and time-to-resolution by priority level.

### Planned Metrics

The Helm chart's ServiceMonitor is configured for:
- `adp_decisions_total` -- Counter of decisions by type and outcome
- `adp_sessions_active` -- Gauge of active sessions
- `adp_policy_evaluations_total` -- Counter by policy name and result
- `adp_escalation_pending` -- Gauge of pending approvals
- `adp_policy_evaluation_duration` -- Histogram of evaluation latency

### Documentation Engine (DocAgent)

A background **DocAgent** runs inside the `adp-mcp` process as a goroutine. It polls for ended sessions every 30 seconds, fetches decision records, and generates markdown reports using Go templates.

**How it works:**

```
Session ends → DocAgent detects ended session (30s poll)
  → Fetches all DecisionRecords for that session
  → Checks idempotency (skips if docs already exist)
  → Analyzes: decision counts, confidence stats, policy violations, files touched
  → Renders Go templates into markdown
  → If ADP_DOC_LLM_API_KEY is set:
      → Calls Claude API to refine template draft into polished prose
      → Falls through to template output if API call fails
  → Stores DocRecords in DocStore (metadata.llm_enhanced = true/false)
```

**Report types generated:**

| Report Type | Content | Generated When |
|-------------|---------|----------------|
| Session Summary | Decision counts by type, confidence metrics (avg, min), files touched, policy violations, agent identity, trust level, duration | Always, on session end |
| Risk Report | Risk indicators (min confidence, policy violations, decision volume), denied actions, low confidence alerts | When min confidence < 0.7 OR policy violations > 0 |
| Pattern Report | Decision type distribution, session profile, high file impact warnings | When session has 5+ decisions |

**LLM refinement (optional):** When `ADP_DOC_LLM_API_KEY` is set to an Anthropic API key, the DocAgent calls Claude to refine each template-generated draft into polished technical documentation. The LLM receives a system prompt instructing it to preserve all factual data while improving readability and adding interpretive context. If the API call fails, the DocAgent falls through to template output and logs the error -- LLM refinement never blocks document generation. Set `ADP_DOC_LLM_MODEL` to override the default model (defaults to `claude-sonnet-4-5-20250929`).

| Environment Variable | Default | Description |
|----------------------|---------|-------------|
| `ADP_DOC_LLM_API_KEY` | (empty -- disabled) | Anthropic API key. When set, enables LLM refinement of generated docs. |
| `ADP_DOC_LLM_MODEL` | `claude-sonnet-4-5-20250929` | Model to use for refinement. Any Anthropic model ID works. |

**Template system:** Three embedded Go templates (`text/template`) produce markdown. Templates are hardcoded in `internal/domain/documentation/templates.go`. Template customization is not yet supported.

Reports are stored in the documentation store and accessible via the `adp_get_docs` MCP tool or the `/v1/docs` API endpoint. Each document's metadata includes an `llm_enhanced` field indicating whether LLM refinement was applied.

**TechDocs export (planned):** A TechDocs export layer that formats generated docs for Backstage TechDocs is not yet built. Today, reports are viewable through the Backstage plugin's Reports Page and the `adp_get_docs` MCP tool.

---

## Security Model

### Authentication Flow

```
HTTP Request
  → Security Headers (X-Frame-Options, CSP, HSTS)
    → CORS Middleware (configurable allowed origins)
      → Auth Middleware (API key / external JWT / local JWT)
        → Rate Limiting (token bucket per IP/user/endpoint)
          → Request Validation
            → Tenant Middleware (org resolution, permission aggregation)
              → RBAC Middleware (optional, role-permission check)
                → Handler (tenant-scoped database queries)
```

### Authentication Methods

ADP supports three authentication methods via combined auth middleware (`internal/api/middleware/combined_auth.go`):

**API Key** (`internal/api/middleware/apikey.go`): Set `ADP_API_KEY` to enable. Accepts via `X-API-Key` header or `Authorization: Bearer <key>`.

**External JWT (JWKS)**: Set `ADP_AUTH_JWKS_URL` to enable. Tokens validated against a remote JWKS endpoint. 15-minute key cache with automatic refresh. Standard claim validation: issuer, audience, expiration, not-before. 1-minute clock skew tolerance. Custom claims extracted: `sub` (user), `org` (organization), `roles`.

**Local JWT**: Set `ADP_JWT_SECRET` to enable. ADP issues HMAC-SHA256 JWTs via the auth endpoints. See [Authentication and User Management](#authentication-and-user-management).

### Security Headers

Applied to all responses via `securityHeaders` middleware in `router.go`:

| Header | Value |
|--------|-------|
| `X-Frame-Options` | `DENY` |
| `X-Content-Type-Options` | `nosniff` |
| `Content-Security-Policy` | `default-src 'self'` |
| `Strict-Transport-Security` | `max-age=31536000; includeSubDomains` (HTTPS only) |

HSTS is conditional on `X-Forwarded-Proto: https` to avoid issues in local development.

### CORS

Configurable via `ADP_CORS_ALLOWED_ORIGINS` (comma-separated list). The CORS middleware sets `Access-Control-Allow-Origin`, `Access-Control-Allow-Methods`, `Access-Control-Allow-Headers`, and `Access-Control-Allow-Credentials` headers.

### Session Token Security

MCP session tokens (`adp_start_session`) use cryptographic randomness: `adp_tok_` prefix + 32 hex bytes. Tokens are SHA-256 hashed before storage in the database (`token_hash` column in `agent_sessions`). The HTTP sidecar validates Bearer tokens against the stored hash via `SessionStore.ValidateToken()`.

### Rate Limiting

In-memory token bucket rate limiter with per-client buckets:

| Default Setting | Value |
|-----------------|-------|
| Requests per second | 10 |
| Burst size | 20 |
| Bucket cleanup interval | 5 minutes |
| Bucket TTL | 10 minutes |

Supports IP-based, user-based, and endpoint-based rate limiting. Per-endpoint overrides for hot paths (e.g., 100 RPS for governance checks).

Response headers: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`, `Retry-After`.

### MCP Server Security

- Agents connect via stdio (local process) or SSE (HTTP-based)
- No mutual authentication on the MCP channel -- agent identity is trusted at session start
- Session tokens expire after 8 hours
- Trust levels constrain what actions are possible within a session

### Readiness Check

`GET /ready` performs an actual database connectivity check (configured via `ReadinessCheck` func in `RouterConfig`). Returns `200` with component status or `503` if unhealthy. Useful for load balancer health checks and orchestrator probes.

### Known Limitations (Alpha)

- No formal security audit has been conducted
- SAML SSO is defined but not wired end-to-end
- No field-level encryption for sensitive decision data
- Neo4j authorizer for RBAC is a stub implementation
- PostgreSQL `ValidateToken()` in `PgSessionAdapter` is a stub (always returns true) -- needs PG schema migration for `token_hash` column
- See [SECURITY.md](../SECURITY.md) for the full security policy and vulnerability reporting process

---

## Multi-Tenancy

ADP supports hierarchical multi-tenancy for organizations with multiple teams and services.

### Hierarchy

```
Tenant (enterprise customer)
  └── Organizations (business units)
       └── Teams (user groups with roles and permissions)
            └── Members (users with role: owner, admin, member, viewer)
```

### Tenant Plans

| Quota | Starter | Pro | Enterprise |
|-------|---------|-----|------------|
| Organizations | 3 | 10 | 100 |
| Users per org | 25 | 100 | 1,000 |
| Concurrent sessions per user | 3 | 5 | 10 |
| Services per org | 25 | 100 | 500 |
| Policies per org | 10 | 50 | 100 |
| Storage (GB) | 10 | 100 | 1,000 |
| API calls per month | 100K | 1M | 10M |
| Audit retention (days) | 30 | 90 | 365 |

### Permissions

Permissions are aggregated from team membership. Format: `resource:action`.

**Resources:** `sessions`, `services`, `policies`, `decisions`, `reports`
**Actions:** `create`, `read`, `update`, `delete`, `approve`
**Wildcards:** `sessions:*` (all actions on sessions), `*:*` (all permissions)

**Team roles:**

| Role | Capabilities |
|------|-------------|
| Owner | Full control over team settings and membership |
| Admin | Manage team configuration and members |
| Member | Use team permissions for governance operations |
| Viewer | Read-only access to team resources |

### Tenant Context Resolution

Every authenticated API request goes through tenant context resolution:

1. Extract `tenant_id` from JWT `org` claim
2. Validate tenant status (active, not disabled/suspended)
3. Extract `organization_id` from `X-Organization-ID` header
4. Verify organization belongs to tenant
5. Aggregate user permissions from all teams in the organization
6. Compute max trust level from team memberships
7. Store resolved context for handler use

All database queries are automatically scoped to the tenant/organization boundary.

---

## CLI Reference

The `adp-cli` binary provides command-line access to ADP operations.

### Available Commands

```bash
# Session management
adp-cli session list              # List active sessions
adp-cli session get <id>          # Get session details
adp-cli session end <id>          # End a session

# Policy management
adp-cli policy list               # List enabled policies
adp-cli policy check <action>     # Test policy evaluation
adp-cli policy validate <file>    # Validate Rego policy syntax

# Decision queries
adp-cli decision list             # List recent decisions
adp-cli decision get <id>         # Get decision details
adp-cli decision export           # Export decisions (JSON/CSV)

# Reports
adp-cli report generate <type>    # Generate a report
adp-cli report list               # List generated reports

# Server management
adp-cli health                    # Check server health
adp-cli config show               # Show current configuration
```

### Configuration

The CLI reads configuration from the same sources as the server:
- `config.yaml` in the current directory
- Environment variables prefixed with `ADP_`
- Command-line flags

```bash
# Point CLI at a remote server
ADP_SERVER_URL=http://adp.internal:8080 adp-cli session list

# Or use a config file
adp-cli --config /path/to/config.yaml session list
```

---

## Configuration Reference

ADP uses [Viper](https://github.com/spf13/viper) for hierarchical configuration. Environment variables take precedence over config file values.

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ADP_STORE` | `sqlite` | Storage backend: `sqlite` or `postgres` |
| `ADP_ENVIRONMENT` | `development` | Environment: `development` or `production` |
| `ADP_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `ADP_SERVER_PORT` | `8080` | HTTP API listen port |
| `ADP_SERVER_METRICS_PORT` | `9090` | Metrics endpoint port |

### Database Configuration

**SQLite (default):**

| Variable | Default | Description |
|----------|---------|-------------|
| `ADP_DATABASE_SQLITE_PATH` | `~/.adp/adp.db` | Database file location |

**PostgreSQL:**

| Variable | Default | Description |
|----------|---------|-------------|
| `ADP_DATABASE_POSTGRES_HOST` | `localhost` | Database host |
| `ADP_DATABASE_POSTGRES_PORT` | `5432` | Database port |
| `ADP_DATABASE_POSTGRES_DATABASE` | `adp` | Database name |
| `ADP_DATABASE_POSTGRES_USERNAME` | `adp` | Database user |
| `ADP_DATABASE_POSTGRES_PASSWORD` | -- | Database password |
| `ADP_DATABASE_POSTGRES_SSLMODE` | `disable` | SSL mode |
| `ADP_DATABASE_POSTGRES_MAX_CONNECTIONS` | `25` | Max connection pool size |
| `ADP_DATABASE_POSTGRES_IDLE_CONNECTIONS` | `5` | Idle connections in pool |

**Neo4j (optional):**

| Variable | Default | Description |
|----------|---------|-------------|
| `ADP_DATABASE_NEO4J_URI` | `bolt://localhost:7687` | Neo4j connection URI |
| `ADP_DATABASE_NEO4J_USERNAME` | `neo4j` | Database user |
| `ADP_DATABASE_NEO4J_PASSWORD` | -- | Database password |

### Authentication Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `ADP_API_KEY` | -- | API key for simple authentication. Pass via `X-API-Key` header. |
| `ADP_JWT_SECRET` | -- | Secret for local HMAC-SHA256 JWT issuance. Enables `/v1/auth/*` endpoints. |
| `ADP_OPEN_REGISTRATION` | `false` | When `true`, allows public user registration without admin auth. |
| `ADP_AUTH_JWKS_URL` | -- | JWKS endpoint URL for external JWT validation |
| `ADP_AUTH_ISSUER` | -- | Expected JWT issuer (external JWKS) |
| `ADP_AUTH_AUDIENCE` | -- | Expected JWT audience (external JWKS) |
| `ADP_AUTH_CLOCK_SKEW` | `1m` | Clock skew tolerance |
| `ADP_AUTH_CACHE_REFRESH` | `15m` | JWKS cache refresh interval |
| `ADP_CORS_ALLOWED_ORIGINS` | -- | Comma-separated list of allowed CORS origins |

### Governance Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `ADP_GOVERNANCE_DEFAULT_TRUST_LEVEL` | `3` | Default agent trust level |
| `ADP_GOVERNANCE_BLAST_RADIUS_LIMIT` | `10` | Default max files per action |
| `ADP_GOVERNANCE_ENABLE_TIME_POLICIES` | `true` | Enable time-based restrictions |
| `ADP_GOVERNANCE_TIMEZONE` | `UTC` | Default timezone for time policies |

### Documentation Engine Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `ADP_DOC_LLM_API_KEY` | (empty) | Anthropic API key. When set, enables LLM refinement of auto-generated docs. |
| `ADP_DOC_LLM_MODEL` | `claude-sonnet-4-5-20250929` | Anthropic model ID for doc refinement. |

### Rate Limiting Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `ADP_RATE_LIMIT_RPS` | `10` | Requests per second |
| `ADP_RATE_LIMIT_BURST` | `20` | Burst size |
| `ADP_RATE_LIMIT_REDIS_URL` | -- | Redis URL for distributed rate limiting |

### Secrets Management

ADP supports multiple secret management backends:

| Backend | Configuration |
|---------|---------------|
| Environment variables | Default. Set `ADP_DATABASE_POSTGRES_PASSWORD`, etc. |
| HashiCorp Vault | Set `ADP_SECRETS_PROVIDER=vault` and configure Vault connection |
| AWS Secrets Manager | Set `ADP_SECRETS_PROVIDER=aws` with `ADP_AWS_REGION` |

### config.yaml Example

```yaml
server:
  port: 8080
  metricsPort: 9090

environment: production
logLevel: info

store: postgres

database:
  postgres:
    host: postgres.internal
    port: 5432
    database: adp
    username: adp
    sslmode: require
    maxConnections: 25
    idleConnections: 5

auth:
  # Option A: External JWKS (enterprise SSO)
  jwksUrl: https://auth.company.com/.well-known/jwks.json
  issuer: https://auth.company.com
  audience: adp-api
  clockSkew: 1m
  # Option B: Local JWT (built-in auth)
  # jwtSecret: your-secret-key
  # openRegistration: false

governance:
  defaultTrustLevel: 3
  blastRadiusLimit: 10
  enableTimePolicies: true
  timezone: America/New_York

rateLimit:
  requestsPerSecond: 10
  burstSize: 20
```

---

## Contributing

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the full contributor guide.

### High-Priority Areas

These packages have **zero test coverage** and are the most impactful areas to contribute:

1. `internal/domain/audit/` -- Compliance reporting, audit export (P1)
2. `internal/domain/context/` -- Context orchestration and token budgeting (P2)
3. `internal/domain/agent/` -- Agent identity and session lifecycle (P2)
4. `internal/domain/tenant/` -- Tenant isolation and permission checks (P2)
5. `internal/infrastructure/cache/` -- Caching layer (P3)

**Tested packages** (16 of 26, ~62%): `governance/`, `handlers/`, `database/`, `auth/`, `user/`, `middleware/`, `documentation/`, `mcp/`, `config/`, `api/`, `sdk/`, `cli/`, `github/`, `integration/`

### Development Workflow

```bash
# Build
go build ./cmd/adp-server && go build ./cmd/adp-cli && go build ./cmd/adp-mcp

# Test
go test ./...

# Lint
go vet ./...
go fmt ./...
```

### Commit Convention

```
<type>(<scope>): <short description>

Types: feat, fix, test, docs, refactor, chore
```

### Code Standards

- All new code must have tests (table-driven for variations)
- Doc comments on all exported types and functions
- Error wrapping: `fmt.Errorf("context: %w", err)`
- No discarding errors with `_` without documented reason

---

## Version History

| Version | Date | Changes |
|---------|------|---------|
| 1.4.0 | 2026-02-21 | Added Authentication and User Management section: local JWT auth, RBAC, user registration/login, admin user management. Updated Security Model with three auth methods (API key, external JWT, local JWT), security headers, CORS, session token security. Updated HTTP API Reference with auth/admin endpoints. Updated Web Dashboard section (now wired with NextAuth.js). Updated Configuration Reference with auth env vars. Added `/ready` readiness check. Updated OpenAPI spec references to 43 operations, 42 schemas. Fixed ADP_HTTP_PORT default to 8081. |
| 1.3.0 | 2026-02-20 | Added test coverage: governance engine (32 tests), handler integration tests (31 tests), database store expansion (21 tests). Added OpenAPI 3.1 spec at `api/openapi.yaml` (33 operations, 35 schemas). Updated contributing section with current coverage status. |
| 1.2.0 | 2026-02-11 | Added HTTP sidecar to adp-mcp for git hook integration (no separate adp-server needed). adp_start_session now returns session_token and http_port. Added ADP_HTTP_PORT env var. Documented agent git workflow. |
| 1.1.0 | 2026-02-07 | Added Git Enforcement section documenting the three-phase commit state machine (prepare/register/verify), hook installation, bypass mechanism, policy engine integration in prepare step, and CI/CD verification. Added POST /v1/commits/register endpoint. |
| 1.0.2 | 2026-02-07 | Implemented AnthropicClient for LLM doc refinement. Optional via ADP_DOC_LLM_API_KEY. Updated doc engine sections to reflect working LLM integration. Added doc engine config to Configuration Reference. |
| 1.0.1 | 2026-02-07 | Corrected documentation engine description: DocAgent is a background poller (not a live companion agent), LLM refinement is a stub, TechDocs export is not yet built. Separated actual state from intended vision. |
| 1.0.0 | 2026-02-07 | Initial comprehensive developer guide |
