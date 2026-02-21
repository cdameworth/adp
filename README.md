# Agent Developer Portal (ADP)

**Governance and audit infrastructure for AI coding agents, delivered via MCP.**

[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev/)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Status: Alpha](https://img.shields.io/badge/Status-Alpha-orange)]()

---

## What is ADP?

ADP is a policy engine and audit trail that sits between AI coding agents and the code they modify. It connects to agents like Claude Code, Cursor, and Copilot via the [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) and enforces governance rules on every action an agent takes. Every decision -- allowed, denied, or escalated -- is logged with the agent's reasoning and the policy outcome.

## Why does this exist?

AI agents are increasingly writing and modifying production code. Without governance, there is no visibility into what an agent changed, why it changed it, or whether the change complied with your team's policies. ADP provides:

- **Policy enforcement** -- Define rules about what agents can and cannot do (trust levels, sensitive file protection, time-based restrictions, blast radius limits).
- **Decision audit trail** -- Every agent action is logged with reasoning, confidence, and policy evaluation results.
- **Human escalation** -- Restricted actions trigger an approval workflow instead of a silent failure or an unchecked execution.

## Current Status

**This project is in alpha.** It works, the tests pass, and the core loop (start session, check policy, log decision, audit) is functional. It is not production-hardened.

### What works today

- MCP server with 11 tools for agent integration
- Unified policy engine (trust levels, blast radius analysis, time-based policies, sensitive file blocking)
- Decision audit trail with reasoning and confidence tracking
- Session management with heartbeats, expiration, and cryptographic session tokens
- Escalation workflow for human approval of restricted actions
- Documentation engine (auto-generated session summaries, risk reports, pattern reports)
- SQLite as a zero-configuration default store
- PostgreSQL as an optional production store
- HTTP API with multiple auth methods (API key, external JWT/JWKS, built-in local JWT)
- User management with RBAC (registration, login, admin user management)
- Web dashboard with authentication (Next.js + NextAuth.js, wired to backend API)
- Git hooks (pre-commit, post-commit, pre-push) with embedded HTTP sidecar
- Security headers, CORS configuration, rate limiting
- Docker Compose setup (full PostgreSQL stack and lightweight SQLite mode)
- Railway deployment support

### What is planned but not yet functional

- Vector search for semantic context retrieval (Qdrant client exists but is a stub)
- Graph-based decision lineage traversal (Neo4j writes exist, no query/read-back)
- Kubernetes/Helm deployment (directory structure exists, manifests are incomplete)
- Metrics and monitoring (Prometheus/Grafana configs exist, no working metrics endpoint)
- SAML SSO (library imported, not integrated)

## Quick Start

The fastest path is building the MCP server and pointing your agent at it. SQLite is the default store, so there is nothing else to set up.

### 1. Build

```bash
git clone <repository-url>
cd adp

go build ./cmd/adp-mcp
go build ./cmd/adp-server   # optional: HTTP API server
go build ./cmd/adp-cli      # optional: CLI tool
```

### 2. Configure your AI agent

Add the MCP server to your agent's configuration. For Claude Code, add this to your MCP settings:

```json
{
  "mcpServers": {
    "adp": {
      "command": "/absolute/path/to/adp-mcp"
    }
  }
}
```

That is it. The MCP server uses SQLite by default and requires no external services.

### 3. Verify

Once your agent connects, it will have access to 11 tools prefixed with `adp_`. The agent starts a session, checks actions against policy, and logs decisions -- all through the MCP protocol.

## MCP Tools

These are the tools exposed to AI agents via the MCP server:

| Tool | Description |
|------|-------------|
| `adp_start_session` | Initialize a governance session with agent identity and trust level |
| `adp_end_session` | Close a governance session |
| `adp_heartbeat` | Keep a session alive (sessions expire after 8 hours of inactivity) |
| `adp_get_context` | Retrieve token-budgeted context for a task (essential, task-relevant, supporting) |
| `adp_check_action` | Evaluate a proposed action against the policy engine before execution |
| `adp_request_approval` | Request async human approval for actions that exceed the agent's trust level |
| `adp_get_approval` | Poll the status of an approval request |
| `adp_log_decision` | Record a decision with reasoning, confidence, alternatives considered, and outcome |
| `adp_prepare_commit` | Register file changes before committing so the audit trail links to the commit |
| `adp_verify_commit` | Verify a commit against the audit trail |
| `adp_get_docs` | Retrieve auto-generated documentation from the documentation engine |

## Policy Engine

ADP uses a unified policy engine that combines several enforcement mechanisms:

**Trust levels** -- Agents are assigned a trust level (1-5) that determines what actions they can perform autonomously. Lower trust levels require approval for more actions.

**Sensitive file protection** -- Paths matching patterns like `.env*`, `secrets/**`, and `*.key` are blocked regardless of trust level.

**Blast radius analysis** -- The engine estimates how many files and services an action could affect and can require approval for high-impact changes.

**Time-based policies** -- Actions can be restricted based on time of day or day of week (for example, blocking production deployments at 3 AM).

**OPA/Rego rules** -- The core policy logic is defined in `policies/default.rego` and evaluated using Open Policy Agent. You can extend or replace the default rules.

## Architecture

```
┌─────────────────────────────────────────────────┐
│                   AI Agents                      │
│        (Claude Code, Cursor, Copilot, ...)       │
└────────────────────┬────────────────────────────┘
                     │ MCP Protocol (stdio)
┌────────────────────▼────────────────────────────┐
│                adp-mcp                           │
│     MCP server (11 tools) + HTTP sidecar         │
└────────────────────┬────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────┐
│              Core Domain                         │
│  ┌───────────┐ ┌───────────┐ ┌───────────┐      │
│  │Governance │ │  Session   │ │   Audit   │      │
│  │  Engine   │ │ Management │ │  Logger   │      │
│  └───────────┘ └───────────┘ └───────────┘      │
│  ┌───────────┐ ┌───────────┐                     │
│  │  Context  │ │   Docs    │                     │
│  │  Delivery │ │  Engine   │                     │
│  └───────────┘ └───────────┘                     │
└────────────────────┬────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────┐
│                  Storage                         │
│   SQLite (default)  or  PostgreSQL (production)  │
└──────────────────────────────────────────────────┘
```

The `adp-mcp` binary includes an embedded HTTP sidecar (port 8081, configurable via `ADP_HTTP_PORT`) that serves git hook endpoints. Git hooks call the sidecar for commit validation — no separate server process needed.

There is also an `adp-server` binary that exposes the full REST API with JWT authentication, intended for the web dashboard and non-MCP integrations.

## Configuration

ADP uses [Viper](https://github.com/spf13/viper) for configuration. You can use a `config.yaml` file or environment variables.

### Minimal configuration (SQLite default)

No configuration file is required. The MCP server works out of the box with SQLite.

### PostgreSQL configuration

To use PostgreSQL instead of SQLite, set these environment variables or add them to `config.yaml`:

```bash
ADP_STORE=postgres
ADP_DATABASE_POSTGRES_HOST=localhost
ADP_DATABASE_POSTGRES_PORT=5432
ADP_DATABASE_POSTGRES_DATABASE=adp
ADP_DATABASE_POSTGRES_USERNAME=adp
ADP_DATABASE_POSTGRES_PASSWORD=your-password
```

### Server configuration

```bash
ADP_SERVER_PORT=8080              # HTTP API port (adp-server only)
ADP_HTTP_PORT=8081                # HTTP sidecar port (adp-mcp, for git hooks)
ADP_LOG_LEVEL=debug               # Log verbosity
ADP_ENVIRONMENT=development       # development or production
```

### Authentication configuration

```bash
ADP_API_KEY=your-api-key          # Simple API key auth (X-API-Key header)
ADP_JWT_SECRET=your-secret        # Enable built-in local JWT auth (registration, login)
ADP_OPEN_REGISTRATION=true        # Allow public user signup (default: false)
ADP_AUTH_JWKS_URL=https://...     # External JWKS endpoint for enterprise SSO
ADP_CORS_ALLOWED_ORIGINS=https://dashboard.example.com  # Allowed CORS origins
```

### Docker Compose

#### Option A: Full stack (PostgreSQL + all services)

```bash
docker compose up -d
curl http://localhost:8080/health
```

#### Option B: Lightweight SQLite mode (recommended for greenfield)

Uses the same `~/.adp/adp.db` that `adp-mcp` writes to. No PostgreSQL needed.

```bash
docker compose -f docker-compose.sqlite.yml up -d
curl http://localhost:8080/health
```

### Greenfield Setup (MCP + Dashboard)

For a new team getting started with ADP governance locally:

```bash
# 1. Build the MCP server
go build ./cmd/adp-mcp

# 2. Configure your agent to use adp-mcp (see "Configure your AI agent" above)

# 3. Start the dashboard (reads the same SQLite DB as adp-mcp)
docker compose -f docker-compose.sqlite.yml up -d

# 4. Open http://localhost:3002 to see sessions, decisions, and approvals
```

The MCP server writes audit data to `~/.adp/adp.db`. The dashboard reads from the
same file via `adp-server` running in SQLite mode. No configuration needed.

**To use the full PostgreSQL stack instead** (if you need policies, reports, or multi-user):

```bash
# 1. Start the full stack
docker compose up -d

# 2. Point adp-mcp at PostgreSQL (the agent sets ADP_URL automatically from http_port)
ADP_STORE=postgres \
ADP_DATABASE_POSTGRES_HOST=localhost \
ADP_DATABASE_POSTGRES_PASSWORD=adp_dev_password \
./adp-mcp

# 3. Open http://localhost:3002 — telemetry now flows through PostgreSQL to the UI
```

## Development

### Prerequisites

- Go 1.25+

### Build and test

```bash
# Build all binaries
go build ./cmd/adp-server
go build ./cmd/adp-cli
go build ./cmd/adp-mcp

# Run all tests
go test ./...

# Lint
go vet ./...
```

### Project structure

```
adp/
├── cmd/
│   ├── adp-server/         # HTTP API server
│   ├── adp-cli/            # CLI tool
│   └── adp-mcp/            # MCP server (primary integration point)
├── internal/
│   ├── api/                # HTTP handlers, middleware (JWT, RBAC, API key, CORS)
│   ├── config/             # Viper configuration loading
│   ├── domain/
│   │   ├── agent/          # Agent identity and trust levels
│   │   ├── audit/          # Decision logging and trail
│   │   ├── auth/           # SQL-based RBAC authorizer
│   │   ├── context/        # Token-budgeted context delivery
│   │   ├── documentation/  # Auto-generated reports from decisions
│   │   ├── governance/     # Unified policy engine (OPA, blast radius, time)
│   │   └── user/           # User domain: roles, password hashing, JWT token service
│   ├── mcp/                # MCP protocol, tools, HTTP sidecar
│   ├── store/              # Storage interfaces (including UserStore)
│   └── infrastructure/     # Database clients (SQLite, Postgres, Neo4j, Qdrant)
├── policies/               # OPA/Rego policy files
│   └── default.rego        # Default governance rules
├── migrations/             # Database migrations (SQLite, Postgres, Neo4j)
├── hooks/                  # Git hooks (pre-commit, post-commit, pre-push)
├── docker-compose.yml
└── docs/
```

## Documentation

See [docs/DEVELOPER_GUIDE.md](docs/DEVELOPER_GUIDE.md) for comprehensive developer documentation covering:

- Architecture and design decisions
- MCP tools reference with parameters and examples
- Governance model (trust levels, blast radius, escalation workflow)
- Policy engine (builtin policies, OPA/Rego, time-based rules, simulation)
- Deployment options (local, Docker Compose, Kubernetes/Helm, container images)
- Authentication and user management (API key, JWT, local auth, RBAC)
- Backstage integration
- Security model (headers, CORS, session tokens, rate limiting)
- Multi-tenancy
- Configuration reference
- HTTP API reference

Additional documentation:
- [docs/ENFORCEMENT_OPTIONS.md](docs/ENFORCEMENT_OPTIONS.md) -- Policy enforcement models comparison
- [docs/RAILWAY_DEPLOYMENT.md](docs/RAILWAY_DEPLOYMENT.md) -- Railway deployment guide

## Roadmap

See [ROADMAP.md](ROADMAP.md) for planned features and priorities.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines and how to submit changes.

## License

Apache License 2.0. See [LICENSE](LICENSE) for the full text.
