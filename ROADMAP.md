# Proctor Roadmap

**Version**: v0.3.0-plan
**Last updated**: 2026-09-03
**Status**: Active — supersedes the v0.1.0 roadmap (which predates the June 2026 work)

> Rename to **Proctor** is proposed in REBRAND.md (PR #22). This document uses the new name.

---

## What this document is

An honest statement of where Proctor stands and where it is going. No inflated
completion percentages, no features that don't exist. The v0.3.0 release plan
tracks execution (issue: v0.3.0 release plan); this roadmap is the longer arc.

## Product thesis

Proctor is the **governance and evidence layer for AI coding agents**. Agents
connect over MCP; Proctor decides what they're allowed to do, records why every
change was made, and blocks unverified work at the merge gate — producing the
audit trail that engineering, security, and compliance actually trust.

Three pillars, in priority order:

1. **Policy** — graduated trust levels, OPA/Rego, deny-by-default. *(exists)*
2. **Provenance** — the decision ledger: reasoning, confidence, policy outcome,
   linked to commits. *(exists; graph read-back pending)*
3. **Proof** — behavioral verification: attested evidence that agent output
   works, enforced as a merge gate. *(the moat; first slice in v0.3.0)*

## Where we are (verified 2026-09-03)

Working: MCP server (11 tools), unified policy engine, decision ledger, session
management with cryptographic tokens, escalation workflow, documentation engine,
git hooks, server-side merge gate + reconciliation backstop, HTTP API with RBAC,
Next.js dashboard (SQLite mode), Docker Compose, GitHub Actions CI, GitHub App
integration, Bedrock AgentCore Gateway target, TechDocs/Backstage export.

Known trust gaps (P0, specs on the issues): #12, #13, #15.

## Release arc

### v0.3.0 — "Proctor" (trust + first proof slice + rename)

Tracked in the v0.3.0 release-plan issue. Phases:

- **A. Trust fixes**: #12 (PG token validation), #13 (AgentCore fail-closed),
  #15 (sensitive-path coverage). Includes Postgres in CI + store conformance
  suite so store parity is enforced structurally.
- **B. Store parity**: #10 (persist reconciliation findings), #11 (PG DocStore).
- **C. Behavioral verification slice 1**: #20 — `require_behavioral_verification`
  policy builtin; attestation recorded on the audit trail, surfaced via API/docs.
- **D. Rename execution**: module path, binaries, MCP tool prefix, env vars,
  data dir — one release, alias windows, loud notes (REBRAND.md checklist).

### v0.4.0 — lineage + second gate

- #21 — ephemeral-environment check as a second merge gate; sessions bound to
  environments (builds on #20's attestation primitive)
- #4 — Neo4j decision-lineage read/query: graph traversal, API endpoints,
  dashboard lineage view. This is the demo moment for stakeholders.
- #14 — build-verify the Backstage plugin; decide its repo home
- Dashboard wired fully to live API data (no placeholder content)

### Backlog (pull forward only on design-partner demand)

- #3 Qdrant vector search for semantic context retrieval
- #5 Kubernetes/Helm manifests
- #6 Prometheus metrics endpoint
- #7 SAML SSO end-to-end

## Business model direction (deliberate revision)

The previous roadmap declared "no SaaS, self-hosted only." That is revised:

- **Core stays open source (Apache 2.0)**: policy engine, MCP server, audit
  trail, git hooks, merge gate, reconciliation. Open source is the distribution
  and trust channel; security buyers must be able to audit enforcement code.
- **Commercial layer (license TBD, likely BSL)**: managed control plane, hosted
  audit retention, behavioral-verification service, lineage dashboard, SAML,
  compliance evidence packs (SOC 2 / EU AI Act formats), multi-org, SLA support.
- **Near-term gate**: no commercial build-out until 3 design partners are
  running the merge gate as a required check on real agent PRs. Their usage
  decides what the paid tier is.

## Non-goals (unchanged unless noted)

- **Multi-cloud deployment tooling** — containers; cloud infra is the operator's job.
- **Agent-specific integrations beyond MCP** — MCP is the universal protocol.
  (The AgentCore Gateway target is config/assets, not agent-specific code.)
- **An agent sandbox** — Proctor governs the git boundary; pair with a sandboxed
  runtime (e.g. AgentCore) for egress control. We document this boundary rather
  than pretend it away.
- **A general IDP** — Proctor integrates with Backstage (TechDocs export,
  plugin) rather than replacing it.
- **Built-in LLM hosting** — external LLM APIs only.
- **Mobile UI**.

## How to influence this roadmap

GitHub Issues for bugs/features (`enhancement` label), Discussions for
architecture proposals, PRs to move anything up the queue. Trust fixes and
behavioral-verification work are the highest-impact contributions right now.

## Version history

| Version | Date | Changes |
|---------|------|---------|
| v0.3.0-plan | 2026-09-03 | Supersedes v0.1.0 roadmap; reflects June 2026 work (CI, merge gate, reconciliation, AgentCore, TechDocs), sequences v0.3.0/v0.4.0, revises business-model non-goal toward open-core |
| v0.1.0 | 2026-02-06 | Initial honest roadmap replacing archived aspirational version |
