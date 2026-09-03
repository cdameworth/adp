# Proctor

**Provenance, policy, and proof for AI coding agents — delivered over MCP.**

> Formerly ADP (Agent Developer Portal). See [REBRAND.md](REBRAND.md) for the rename plan.

---

## The problem

AI agents are writing and modifying production code. Three questions follow every agent-authored change, and today most teams can't answer any of them:

1. **What did the agent decide, and why?** (provenance)
2. **Was it allowed to do that?** (policy)
3. **Does the change actually work?** (proof)

Access logs answer none of these. An MCP gateway can tell you a tool was called; it cannot tell you what the agent *reasoned*, which *policy* evaluated the action, which *human* approved it, or whether the resulting commit was *verified*. Proctor exists to close that gap.

## What Proctor is

Proctor is governance infrastructure that sits between AI coding agents (Claude Code, Cursor, Copilot — anything that speaks MCP) and your codebase, with enforcement at the git boundary:

- **Provenance — the decision ledger.** Every agent decision is logged with its reasoning, confidence score, alternatives considered, and policy outcome — and linked to the commit it produced. The audit object is the *decision*, not the tool call.
- **Policy — enforcement with graduated trust.** Agents carry trust levels (1–5). OPA/Rego policies, sensitive-file protection, blast-radius limits, and time-based rules decide what happens autonomously, what gets denied, and what escalates to a human. Deny-by-default.
- **Proof — the merge gate.** A server-side verify-batch endpoint and forge integration (GitHub Action) make "no ungoverned commit merges" a *required check*, not a convention. A reconciliation backstop scans history and flags any commit that bypassed governance. Behavioral verification (attested build/test evidence as a second merge gate) is in progress — see issues #20 and #21.
- **Accountability — documentation for humans.** A documentation engine auto-generates session summaries, risk reports, and pattern reports from decision records, so engineering leads, security, and compliance can see what agents did without reading diffs.

## Trust model (read this before deploying)

Governance tools live or die on honest enforcement boundaries. Proctor's:

| Layer | Mechanism | Trust assumption |
|---|---|---|
| MCP server (11 tools) | Agents call `adp_check_action` before acting; decisions logged | Cooperative — an agent could act without asking |
| Git hooks | pre-commit / post-commit / pre-push validate against the session audit trail | Local enforcement; token-verified, *audited* bypass |
| Merge gate | `POST /v1/commits/verify-batch` as a required forge check | **Non-bypassable** — server-side, enforced by branch protection |
| Reconciliation | Scans commit history, flags ungoverned commits as findings | Backstop — detects anything that slipped through |

**Known boundary:** Proctor governs what reaches git. An agent with unrestricted shell and network access could exfiltrate data without committing. Run agents in a sandboxed environment (e.g. Bedrock AgentCore — see `integrations/agentcore/`) for egress control; Proctor then guarantees the code boundary. See [docs/ENFORCEMENT_OPTIONS.md](docs/ENFORCEMENT_OPTIONS.md).

## Quick start

The MCP server uses SQLite by default — zero external services.

```bash
git clone https://github.com/cdameworth/adp.git
cd adp
go build ./cmd/adp-mcp
go build ./cmd/adp-server   # optional: HTTP API server
go build ./cmd/adp-cli      # optional: CLI
```

Point your agent at it (Claude Code example):

```json
{
  "mcpServers": {
    "proctor": {
      "command": "/absolute/path/to/adp-mcp"
    }
  }
}
```

> Binary and module renames (`adp-*` → `proctor-*`) land with the repo rename — see REBRAND.md. Until then the binaries keep their `adp-*` names.

Watch governance happen:

```bash
docker compose -f docker-compose.sqlite.yml up -d
# open http://localhost:3002 — sessions, decisions, approvals
```

## MCP tools

| Tool | Purpose |
|---|---|
| `adp_start_session` / `adp_end_session` / `adp_heartbeat` | Governance session lifecycle with agent identity + trust level; sessions expire after 8h idle |
| `adp_check_action` | Evaluate a proposed action against policy **before** execution — the primary enforcement call |
| `adp_request_approval` / `adp_get_approval` | Async human escalation for actions above the agent's trust level |
| `adp_log_decision` | Record a decision: reasoning, confidence, alternatives, outcome |
| `adp_prepare_commit` / `adp_verify_commit` | Bind file changes to the audit trail; verify a commit against it |
| `adp_get_context` | Token-budgeted task context (essential / task-relevant / supporting) |
| `adp_get_docs` | Retrieve auto-generated documentation |

## Policy engine

Unified engine, deny-by-default, evaluated per action:

- **Trust levels (1–5)** — autonomy scales with demonstrated reliability; reads are open, production deploys require level 5 or human approval.
- **Sensitive-file protection** — credential and secret paths blocked at any trust level.
- **Blast-radius analysis** — high-impact changes require approval.
- **Time-based policies** — e.g. block production deploys 22:00–06:00.
- **OPA/Rego** — extend or replace `policies/default.rego` with your own rules.

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
│     (git-hook endpoints, no extra process)       │
└────────────────────┬────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────┐
│              Core Domain                         │
│   Governance · Sessions · Audit · Context · Docs │
└────────────────────┬────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────┐
│   SQLite (default)   or   PostgreSQL (scale)     │
└──────────────────────────────────────────────────┘
```

`adp-server` exposes the full REST API (JWT / API key / external JWKS auth, RBAC) for the dashboard and non-MCP integrations. An AWS Bedrock AgentCore Gateway target lives in `integrations/agentcore/`; a GitHub App (check runs, commit statuses, webhooks) lives in `internal/integrations/github/`; TechDocs/Backstage export in `internal/techdocs/`.

## Configuration

Works out of the box (SQLite). For PostgreSQL:

```bash
ADP_STORE=postgres
ADP_DATABASE_POSTGRES_HOST=localhost
ADP_DATABASE_POSTGRES_PORT=5432
ADP_DATABASE_POSTGRES_DATABASE=adp
ADP_DATABASE_POSTGRES_USERNAME=adp
ADP_DATABASE_POSTGRES_PASSWORD=your-password
```

Auth (pick one or combine): `ADP_API_KEY`, `ADP_JWT_SECRET` (built-in local JWT + user management), or `ADP_AUTH_JWKS_URL` (external SSO). See [docs/DEVELOPER_GUIDE.md](docs/DEVELOPER_GUIDE.md) for the full reference.

## Status

**Alpha — core loop is real and tested; hardening in progress.**

Working today: MCP server, policy engine, decision ledger, sessions with cryptographic tokens, escalation workflow, documentation engine, git hooks, merge gate + reconciliation, HTTP API with RBAC, dashboard (SQLite mode), Docker Compose, GitHub Actions CI (build/vet/gofmt/test).

Honest gaps, tracked as issues:

- **P0 trust fixes**: PG-mode token validation (#12), AgentCore fail-open path (#13), sensitive-path coverage (#15)
- **Behavioral verification** as an attested second merge gate (#20, #21)
- Decision-lineage graph queries — writes exist, read-back in progress (#4)
- PG/SQLite feature parity (#10, #11); SAML end-to-end (#7); metrics endpoint (#6); Helm (#5)

See [ROADMAP.md](ROADMAP.md) for phases and non-goals.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). The fastest way to move something up the roadmap is to contribute it — Tier-0 trust fixes and test coverage are the highest-impact entry points.

## License

Apache License 2.0 — see [LICENSE](LICENSE).
