# ADP Product Roadmap

**Version**: v0.1.0
**Last updated**: 2026-02-06
**Status**: Active -- replaces previous aspirational roadmap (archived)

---

## What This Document Is

An honest assessment of where ADP stands and where it is going. No inflated completion percentages. No enterprise features that do not exist yet. This roadmap reflects actual project state and realistic next steps.

ADP's core value proposition has been narrowed to two things:

1. **Decision lineage and audit trail** -- every agent decision logged with reasoning, confidence scores, and policy evaluation outcomes, stored in a queryable graph.
2. **Documentation engine** -- auto-generates human-readable session summaries, risk reports, and pattern reports from agent decision records.

The governance and policy engine is the foundation that enables both. The MCP server is how agents connect.

---

## What Exists Today (Alpha)

| Component | Status | Notes |
|-----------|--------|-------|
| MCP server (6 tools) | Working | Session, context, check_action, approval, log_decision, prepare_commit |
| Unified policy engine | Working | Trust levels, blast radius, time-based rules, sensitive files, custom Rego |
| SQLite store | Working | Zero-config local development |
| PostgreSQL store | Working | Production data store |
| Decision audit trail | Working | Writes to PostgreSQL/SQLite; no graph queries yet |
| Documentation engine | Working | Session summaries, risk reports, pattern reports |
| Escalation workflow | Working | Human approval for restricted actions |
| HTTP API with JWT auth | Partial | JWT validation exists; SAML defined but not integrated |
| Git hooks | Working | Pre-commit validation |
| Docker Compose | Working | Full stack orchestration |

### What Is Not Working

- **Test coverage**: 8 of 28 packages have tests. The governance engine -- the most critical component -- has 0% coverage.
- **Neo4j**: Write-only. Decision data goes in, but no graph queries exist for lineage traversal or visualization.
- **Qdrant**: Stub implementation only. No working vector search.
- **Web dashboard**: React skeleton exists with no backend integration.
- **Auth**: SAML flow defined in code but not wired end-to-end.
- **CI/CD**: No pipeline. No automated testing on push or PR.
- **OpenAPI spec**: Does not exist.
- **Kubernetes/Helm**: Not available.

---

## Phase 1: Foundation Hardening (Current Focus)

Get the existing code to a state where contributors can work on it with confidence.

- Write test suites for the governance engine (`internal/domain/governance/`), covering policy evaluation, autonomy mapping, escalation logic, and time-based constraints
- Add integration tests for SQLite and PostgreSQL stores to validate data persistence across session and decision workflows
- Add tests for API handlers and MCP server tool implementations
- Set up CI/CD pipeline with GitHub Actions (build, test, lint, vet on every PR)
- Author an OpenAPI specification for the REST API and generate docs from it
- Complete JWT authentication flow and wire SAML integration end-to-end
- Establish minimum test coverage thresholds and enforce them in CI

---

## Phase 2: Decision Lineage

Make the audit trail queryable and visual. The data writes already exist; this phase adds reads.

- Implement Neo4j graph queries for decision lineage traversal (parent-child chains, session timelines, policy impact paths)
- Add REST API endpoints for lineage queries: decision lookup, session history, filtered search by agent/policy/outcome
- Build lineage visualization in the web dashboard (decision graph, timeline view)
- Add decision search and filtering (by time range, agent identity, policy result, confidence threshold)
- Implement export capabilities for decision records (JSON and CSV)
- Add lineage summary statistics (decisions per session, approval rates, policy violation trends)

---

## Phase 3: Documentation Engine Enhancement

Expand the documentation engine from basic reports to a useful knowledge base.

- Wire LLM integration for documentation refinement (placeholder exists; needs Anthropic API connection)
- Add full-text search across generated documentation
- Implement cross-session pattern analysis to surface recurring decisions and common policy outcomes
- Expose a documentation API for external consumption (CI tools, dashboards, reporting systems)
- Support template customization so teams can control report format and content

---

## Phase 4: Developer Experience

Make ADP usable without reading source code.

- Connect the React web dashboard to real API endpoints (replace placeholder data with live queries)
- Improve the CLI with interactive policy management (create, test, validate policies from the terminal)
- Write onboarding documentation: getting started guide, first-policy tutorial, MCP integration walkthrough
- Build an example policies library covering common governance scenarios (file restrictions, review gates, trust escalation)
- Iterate on MCP tool design based on real usage feedback from agent developers

---

## Phase 5: Production Readiness

Prepare for real deployment beyond local development.

- Create Kubernetes manifests and Helm chart for production deployment
- Design and test horizontal scaling for the API server and policy engine
- Build backup and restore tooling for PostgreSQL and Neo4j data
- Run performance benchmarks and publish baseline numbers for decision throughput and policy evaluation latency
- Conduct a security audit of auth flows, policy evaluation, and data access paths

---

## Non-Goals

The following are explicitly out of scope for the foreseeable future. This may change, but not without a deliberate decision.

- **Multi-cloud deployment tooling** -- ADP runs on containers. Cloud-specific infrastructure is the operator's responsibility.
- **SaaS / hosted offering** -- ADP is self-hosted open source software. There are no plans for a managed service.
- **Agent-specific integrations beyond MCP** -- MCP is the universal protocol. ADP will not build custom plugins for individual agents (Claude Code, Cursor, Copilot, etc.). If an agent supports MCP, it works with ADP.
- **Mobile UI** -- The web dashboard targets desktop browsers. A mobile interface is not planned.
- **Built-in LLM hosting** -- ADP calls external LLM APIs for documentation refinement. It does not host or serve models.
- **Multi-tenant SaaS features** -- Tenant isolation, billing, usage metering, and similar capabilities are not in scope.

---

## How to Influence the Roadmap

This roadmap reflects current priorities, but it is not fixed. Community input directly shapes what gets built next.

- **GitHub Issues**: Open an issue to report bugs, request features, or flag gaps in the roadmap. Label feature requests with `enhancement` and roadmap discussions with `roadmap`.
- **GitHub Discussions**: Use Discussions for open-ended questions, architecture proposals, or to make a case for reprioritizing work. The `ideas` category is the right place for feature pitches.
- **Pull Requests**: The fastest way to move something up the roadmap is to contribute it. See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.
- **Phase 1 contributions are especially welcome** -- test coverage improvements have a low barrier to entry and high impact.

---

## Version History

| Version | Date | Changes |
|---------|------|---------|
| v0.1.0 | 2026-02-06 | Initial honest roadmap replacing archived aspirational version |
