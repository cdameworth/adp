# AgentCore Gateway interceptor — non-bypassable tool-call governance

The [Gateway OpenAPI target](./README.md) gives agents the ADP governance
*tools*, but an agent could still call an action tool without first calling
`adp_check_action`. A Gateway **REQUEST interceptor** closes that gap: it runs on
the gateway for **every** `tools/call`, so governance is enforced regardless of
agent cooperation.

[`interceptor.py`](./interceptor.py) is a Lambda REQUEST interceptor that, on
each tool call, invokes ADP `POST /v1/governance/check` and **blocks the call
(fail closed)** if ADP denies or requires approval; allowed calls pass through
unchanged.

```
agent ── tools/call ──► AgentCore Gateway ──(REQUEST interceptor)──► interceptor.py
                                                     │  POST /v1/governance/check
                                                     ▼
                              allowed?  yes → forward to target tool
                                        no  → block (agent gets an error)
```

## Trust model: fail closed, one explicit escape hatch (#13)

The interceptor **fails closed on every failure mode**: ADP unreachable,
timeout, 5xx/4xx, malformed JSON, or a response missing `allowed: true` all
block the tool call. Governance checks use a single short-timeout attempt
(3s) — retries would multiply agent latency.

Deny reasons are prefixed so alerting can distinguish them:

- `policy_denied:` — ADP evaluated the action and denied it (or it awaits
  human approval). Expected; not an outage.
- `governance_unavailable:` — ADP could not produce an answer. **Page on
  this.** While it persists, governed tools are unusable — that is the cost
  of a governance layer that means what it says.

**Escape hatch (development only):** setting `ADP_INTERCEPTOR_FAIL_OPEN=1`
passes tool calls through when governance is unreachable, logging a WARNING
for every bypassed call. Policy decisions that *were* returned still apply —
fail-open covers outages, not denials. Never set it in production.

`ADP_URL` is required. If unset, all tool calls deny with
`governance_unavailable:` (unless fail-open is active).

The failure matrix is enforced by tests: `python3 -m unittest discover -s
integrations/agentcore` (21 cases covering each failure mode above).

## Deploy

1. Package and create the Lambda (`interceptor.py`, handler
   `interceptor.lambda_handler`, Python 3.12), with env `ADP_URL` and optionally
   `ADP_API_KEY`. It must have network egress to adp-server.
2. Attach it as the gateway's **REQUEST interceptor** (a gateway has at most one).
   Grant the gateway execution role permission to invoke only this function.
3. If you correlate sessions via a header (below), set `passRequestHeaders: true`
   on the interceptor config and `ADP_SESSION_HEADER` on the Lambda.

## Session correlation (configure this)

The check needs the agent's ADP `session_id`. The interceptor resolves it from,
in order:

1. `arguments.session_id` on the tool call (simplest if your tools carry it), or
2. a request header (`ADP_SESSION_HEADER`, default `x-adp-session-id`) — requires
   `passRequestHeaders: true`.

If neither is present the call is **blocked** (can't govern ⇒ deny). Pick the
mechanism that fits how your agents authenticate to the gateway.

## Important caveats

- **Verify the deny contract for your AgentCore version.** The documented
  interceptor examples cover pass-through (returning `transformedGatewayRequest`)
  but not an explicit deny primitive. This Lambda blocks by *raising*, relying on
  the gateway treating an interceptor failure as a blocked invocation. Confirm
  this blocks (and doesn't fail open) in your environment; if your version
  supports short-circuiting with a `transformedGatewayResponse`, switch the deny
  path to return an error response instead. **Test a known-denied action end to
  end before relying on it.**
- **Fail closed:** ADP unreachable, a non-2xx, or a missing session all block the
  call. Size the Lambda timeout and ADP availability accordingly.
- **Scope:** governs tool calls routed through this gateway only. Effects the
  agent can perform outside the gateway (direct shell, direct git) need the other
  chokepoints (e.g. the git merge gate in `integrations/enforcement/`).
- Not runnable/verified outside AWS; this is the integration code + contract.
