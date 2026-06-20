# Server-side merge gate (non-bypassable git enforcement)

ADP's client-side hooks (`hooks/pre-commit`, `pre-push`) give fast local
feedback but are **advisory** — an agent can skip them (`--no-verify`) or simply
not install them. This gate moves the decision to a place the agent **cannot**
avoid: a **required CI status check** that the forge enforces at merge time.

## How it works

```
PR opened/updated
   │
   ▼
CI job ── collects commit SHAs (base..head) ──► POST /v1/commits/verify-batch
   │                                                    │
   │                              ADP checks each SHA has a verified
   │                              governance trail (prepared under policy +
   │                              registered) → { allowed, unverified[] }
   ▼
allowed=false  ⇒  job fails  ⇒  required check fails  ⇒  merge blocked
```

Branch protection requiring this check is what makes governance non-optional:
the PR cannot merge unless every commit went through ADP. The gate **fails
closed** — if ADP is unreachable or returns an error, the check fails and the
merge is blocked.

## Setup (GitHub)

1. Copy [`github-workflow-adp-gate.yml`](./github-workflow-adp-gate.yml) to
   `.github/workflows/adp-governance-gate.yml` in each governed repo.
2. Add repo/org secrets: `ADP_URL` (reachable from CI) and, if ADP uses API-key
   auth, `ADP_API_KEY`.
3. In **Branch protection** for your default branch, enable **Require status
   checks to pass** and select **ADP governance gate**. (This step is the
   enforcement — without it the check is just informational.)

## Other CI (GitLab, Jenkins, …)

Use [`adp-merge-gate.sh`](./adp-merge-gate.sh) (needs `bash`, `curl`, `jq`):

```bash
BASE_SHA="$CI_MERGE_REQUEST_DIFF_BASE_SHA" HEAD_SHA="$CI_COMMIT_SHA" \
ADP_URL="https://adp.internal:8080" ADP_API_KEY="$ADP_API_KEY" \
  ./adp-merge-gate.sh
# or: ./adp-merge-gate.sh <sha1> <sha2> ...
```

Make the job blocking/required in your pipeline so a failure prevents merge.

## Prerequisites

- adp-server reachable from CI with `POST /v1/commits/verify-batch` (backed by a
  commit store — available in both SQLite and PostgreSQL modes).
- Agents commit **through ADP's git chain** (`adp_prepare_commit` →
  `prepare/register/verify`), so each commit has a trail to verify. The chain
  runs the policy engine (OPA + builtin) at prepare time, so a verified commit
  is also a policy-passed one. Commits created outside ADP have no trail and are
  blocked by the gate.

## Reconciliation (detection backstop)

Prevention has gaps — repos without the required check, direct pushes, outright
bypasses. The reconciler catches them after the fact: a push webhook reports
observed commits, ADP flags any without a governance trail as an
**ungoverned-activity finding**, and they surface in the Backstage
**Enforcement** tab for triage (acknowledge / resolve).

1. Copy [`github-workflow-adp-reconcile.yml`](./github-workflow-adp-reconcile.yml)
   to `.github/workflows/adp-reconcile.yml` (non-blocking; runs on every push).
2. Findings are served at `GET /v1/enforcement/findings`; the plugin's
   Enforcement tab lists open ones with acknowledge/resolve actions (guarded by
   the `adp.enforcement.read` / `adp.enforcement.resolve` permissions).

Notes:
- adp-server wires a reconciler in both modes; findings are kept **in-memory**
  (bounded) — swap in a persistent `FindingStore` (`internal/domain/enforcement`)
  for durability across restarts.
- Any system that can list commits can feed
  `POST /v1/enforcement/commits/observed` with `{commits:[{sha,repo,ref,author}]}`,
  not just GitHub.

## Why this is the reliable layer

| Layer | Bypassable? | Role |
| --- | --- | --- |
| `adp_check_action` (agent self-report) | yes (agent may skip) | advisory, inline guidance |
| client git hooks | yes (`--no-verify`, not installed) | fast local feedback |
| **this merge gate (required check)** | **no** (forge-enforced, fails closed) | **authoritative enforcement** |
| reconciliation findings | n/a (detection) | catches what slipped past, surfaced in Backstage |

For agent *tool calls* (not git), the equivalent non-bypassable chokepoint is the
AgentCore Gateway REQUEST interceptor — see `integrations/agentcore/`.
