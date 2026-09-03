# Rebrand: ADP → Proctor

**Status: proposal — awaiting owner decision.** This document explains the rename and
lists everything that has to change. Nothing in code is renamed yet; the README
uses the new name with pointers here.

## Why rename

1. **Trademark risk.** "ADP" is the mark of ADP, Inc. (automatic data processing) —
   a ~$100B public company with active trademark enforcement in software categories.
   Commercializing a product named ADP invites a C&D at exactly the wrong moment.
2. **Positioning.** "Agent Developer Portal" describes the surface, not the value.
   The product's differentiator is the decision ledger + merge-gate proof —
   provenance and verification, not a portal.
3. **The adjacent term is taken.** "Dark Factory" (the lights-out-agentic-SDLC
   concept this product complements) is publicly associated with StrongDM and Dan
   Shapiro's autonomy taxonomy. Building a brand on someone else's category term
   guarantees permanent comparison.

## Chosen name: Proctor

A proctor supervises an examination: verifies identity, enforces the rules, and
certifies the result — which is precisely what this product does for agent-authored
changes (including the anti-cheating angle: detecting test-gaming is on the
verification roadmap).

Checked 2026-09-03:

- `proctor.dev` — no DNS registration found (verify and register immediately)
- GitHub orgs `proctordev` and `useproctor` — available (404)
- `github.com/proctor` is taken (dormant); use `proctordev` for the org
- No conflicting devtool/governance product found in search

Runner-up candidates (if Proctor fails legal/search diligence): Inquest, Scrutineer,
Surety — all have taken .dev domains or weaker fit; Proctor is the recommendation.

## Rename checklist

**Decision gates (owner):**
- [ ] Confirm the name; register proctor.dev + GitHub org `proctordev`
- [ ] Rename this repository: `cdameworth/adp` → `proctordev/proctor` (GitHub keeps redirects)

**Breaking changes (batch into one release, v0.3.0):**
- [ ] Go module path: `github.com/adp/adp` → `github.com/proctordev/proctor` (breaking for importers; say so loudly in the release notes)
- [ ] Binary names: `adp-mcp` / `adp-server` / `adp-cli` → `proctor-mcp` / `proctor-server` / `proctor-cli`
- [ ] MCP tool prefix: `adp_*` → `proctor_*` (breaking for agent configs; consider a one-release alias window)
- [ ] Env var prefix: `ADP_*` → `PROCTOR_*` (same alias-window consideration)
- [ ] Data dir: `~/.adp/` → `~/.proctor/` (migrate-or-copy on first run)
- [ ] Docker images, Compose service names, Helm chart names

**Soft changes (any time):**
- [ ] Dashboard branding, login screen, docs
- [ ] AgentCore integration asset names/descriptions
- [ ] README badges and links post-repo-rename

## Positioning statement (use everywhere)

> **Proctor is the governance and evidence layer for AI coding agents.**
> Agents connect over MCP; Proctor decides what they're allowed to do, records
> why every change was made, and blocks unverified work at the merge gate —
> producing the audit trail that engineering, security, and compliance actually trust.

What Proctor is **not**: an MCP gateway (it audits decisions, not tool calls), an
agent sandbox (pair with one for egress control), or an IDP (it integrates with
Backstage rather than replacing it).
