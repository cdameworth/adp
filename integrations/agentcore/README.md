# ADP → Amazon Bedrock AgentCore Gateway

Expose ADP's governance loop as MCP tools to **any** AgentCore-hosted agent
(Strands, LangGraph, CrewAI, custom) by registering ADP's REST API as an
**AgentCore Gateway OpenAPI target**. The agent never sees ADP's API key — the
Gateway injects it on outbound calls.

```
agent (AgentCore Runtime)
  │  MCP  (inbound: JWT/OAuth)
  ▼
AgentCore Gateway  ──OpenAPI target──►  adp-server  /v1/...
  └─ outbound API_KEY provider injects  X-API-Key: <ADP_API_KEY>
```

## Files

| File | Purpose |
| --- | --- |
| [`adp-governance.openapi.yaml`](./adp-governance.openapi.yaml) | Curated OpenAPI 3.0.3 spec — the Gateway target. 7 governance tools. |
| [`gateway-target.example.json`](./gateway-target.example.json) | `CreateGatewayTarget` payload (S3 schema + API_KEY outbound auth). |

## Tools exposed (operationId = MCP tool name)

| Tool | Method · Path | Purpose |
| --- | --- | --- |
| `adp_start_session` | POST `/v1/sessions` | Open a session; returns the `session_id` threaded into every other call. |
| `adp_check_action` | POST `/v1/governance/check` | Evaluate an action against policy **before** doing it. |
| `adp_request_approval` | POST `/v1/governance/approvals` | Escalate a blocked action for human approval. |
| `adp_get_approval` | GET `/v1/governance/approvals/{id}` | Poll an approval's status. |
| `adp_log_decision` | POST `/v1/audit/decisions` | Write a decision (+reasoning, confidence) to the audit trail. |
| `adp_heartbeat` | PATCH `/v1/sessions/{id}/heartbeat` | Keep a long-running session alive. |
| `adp_end_session` | DELETE `/v1/sessions/{id}` | Close the session (record retained for audit). |

## Why this is a trimmed, hand-authored spec (not ADP's full `api/openapi.yaml`)

These choices come straight from the AgentCore Gateway OpenAPI-target rules
(sources at bottom):

- **OpenAPI 3.0.3.** Gateway supports 3.0 and 3.1; 3.0.3 is used to stay clear of
  3.1-only constructs and maximize validator compatibility.
- **No `security` / `securitySchemes`.** Gateway rejects spec-level security
  schemes — "authentication must be configured using the Gateway's outbound
  authorization configuration." ADP's full spec declares `bearerAuth`/`apiKeyAuth`,
  so it can't be used as-is.
- **`operationId` on every operation** → it becomes the MCP tool name. Names are
  `adp_*`, ≤ 64 chars, `[a-z_]` only (LLM ToolSpec constraints).
- **No `oneOf`/`anyOf`/`allOf`**, only `application/json`, simple path/query
  params — all unsupported features avoided.
- **Curated to 7 agent-loop tools.** The dashboard/admin/report/policy endpoints
  are omitted to keep the agent's toolset focused (better tool selection).
- **`/v1/context` and `/v1/commits/*` are excluded** — see [Gaps](#gaps-and-caveats).

## Prerequisites

- A running **`adp-server`** (the REST API — not the MCP binary) reachable from
  AgentCore Gateway over **public HTTPS or PrivateLink/VPC egress**. Gateway
  blocks private IP ranges, so a purely internal ADP host will not work without
  an egress path.
- ADP API-key auth enabled: set `ADP_API_KEY` on the server; this is the value
  the Gateway will inject as `X-API-Key`.
- AWS account with Bedrock AgentCore, an S3 bucket, and a gateway **service
  role** (IAM role the Gateway assumes). `aws` CLI configured for your region.

## Setup

> Replace every `<...>` placeholder. Examples use the `bedrock-agentcore-control`
> control-plane API; the `agentcore` CLI and boto3 are equivalent (see sources).

### 1. Store the ADP API key as a credential provider

```bash
aws bedrock-agentcore-control create-api-key-credential-provider \
  --name adp-api-key \
  --api-key "$ADP_API_KEY"
# -> note the returned credential provider ARN (providerArn / credentialProviderArn)
```

### 2. Set up inbound auth (how agents authenticate TO the Gateway)

Use an OIDC provider (Cognito, Okta, Entra, …). Note its **discovery URL** and
the **audience** and/or **client IDs** you'll allow. (Console/CLI gateway
creation can also auto-create a Cognito pool for you.)

### 3. Create the Gateway (MCP protocol, custom-JWT inbound)

```bash
aws bedrock-agentcore-control create-gateway \
  --name adp-governance-gw \
  --protocol-type MCP \
  --role-arn arn:aws:iam::<ACCOUNT_ID>:role/<GATEWAY_SERVICE_ROLE> \
  --authorizer-type CUSTOM_JWT \
  --authorizer-configuration '{
    "customJWTAuthorizer": {
      "discoveryUrl": "https://<your-idp>/.well-known/openid-configuration",
      "allowedAudience": ["<audience>"],
      "allowedClients": ["<client-id>"]
    }
  }'
# -> note the gatewayId and the gateway MCP endpoint URL
```

(`allowedAudience` and `allowedClients` are both optional individually — provide
at least one.)

### 4. Point the spec at your ADP endpoint and upload it

```bash
# edit servers[0].url in adp-governance.openapi.yaml to your ADP HTTPS endpoint
aws s3 cp adp-governance.openapi.yaml s3://<YOUR_BUCKET>/adp/adp-governance.openapi.yaml
```

(Alternatively, skip S3 and inline the schema via
`targetConfiguration.mcp.openApiSchema.inlinePayload` as a string.)

### 5. Create the OpenAPI target (with API-key outbound auth)

Fill the placeholders in [`gateway-target.example.json`](./gateway-target.example.json)
(`gatewayIdentifier`, S3 `uri`/`bucketOwnerAccountId`, and the `providerArn` from
step 1), then:

```bash
aws bedrock-agentcore-control create-gateway-target \
  --cli-input-json file://gateway-target.example.json

# target validation is async; poll until status is READY:
aws bedrock-agentcore-control get-gateway-target \
  --gateway-identifier <YOUR_GATEWAY_ID> --target-id <TARGET_ID>
```

### 6. Grant the Gateway service role access to the credential

Attach to `<GATEWAY_SERVICE_ROLE>` (scope the ARNs to your gateway/credential/secret):

```json
{
  "Version": "2012-10-17",
  "Statement": [
    { "Sid": "GetWorkloadAccessToken", "Effect": "Allow",
      "Action": ["bedrock-agentcore:GetWorkloadAccessToken"],
      "Resource": [
        "arn:aws:bedrock-agentcore:<REGION>:<ACCOUNT_ID>:workload-identity-directory/default",
        "arn:aws:bedrock-agentcore:<REGION>:<ACCOUNT_ID>:workload-identity-directory/default/workload-identity/adp-governance-gw-*"
      ] },
    { "Sid": "GetResourceApiKey", "Effect": "Allow",
      "Action": ["bedrock-agentcore:GetResourceApiKey"],
      "Resource": ["arn:aws:bedrock-agentcore:<REGION>:<ACCOUNT_ID>:token-vault/default/apikeycredentialprovider/adp-api-key"] },
    { "Sid": "GetSecretValue", "Effect": "Allow",
      "Action": ["secretsmanager:GetSecretValue"],
      "Resource": ["arn:aws:secretsmanager:<REGION>:<ACCOUNT_ID>:secret:<SECRET_ID>"] }
  ]
}
```

The role also needs a trust policy allowing `bedrock-agentcore.amazonaws.com` to
assume it (scope with `aws:SourceAccount` / `aws:SourceArn`).

### 7. Verify

From an MCP client (or your agent framework) pointed at the gateway MCP endpoint
with a bearer token from your IdP, list tools — you should see `adp_start_session`,
`adp_check_action`, `adp_request_approval`, `adp_get_approval`, `adp_log_decision`,
`adp_heartbeat`, `adp_end_session`. A typical agent loop:
`adp_start_session` → `adp_check_action` → (`adp_request_approval` →
`adp_get_approval`)? → act → `adp_log_decision` → `adp_end_session`.

## Gaps and caveats

- **Context tool unavailable.** ADP's `adp_get_context` exists only as an MCP
  tool; there is no `/v1/context` REST endpoint, so it can't be a Gateway tool
  until ADP adds one. Use AgentCore Memory for agent memory in the meantime.
- **Commit/git governance excluded.** `/v1/commits/*` and the git-hook chain are
  workstation/CI concepts and don't map to a serverless runtime; keep that half
  of ADP in CI/CD.
- **No end-user identity propagation.** The Gateway authenticates the agent
  inbound, but outbound it sends one shared `X-API-Key`; ADP cannot see which
  human/agent acted. Set `user_id`/`organization_id` in `adp_start_session` for
  attribution, and treat it as agent-asserted, not verified. (For true identity
  propagation you'd need OAuth token-exchange outbound + ADP accepting per-user
  JWTs.)
- **Session bootstrapping is the agent's job.** The agent must call
  `adp_start_session` and thread `session_id` through subsequent tools;
  `trust_level` enforcement is decided by ADP policy, not the Gateway.
- **Response envelope.** ADP wraps single-object responses as `{ "data": {...} }`;
  the agent receives tool results under `data.*` (reflected in the spec).
- **If running ADP on PostgreSQL**, note `PgSessionAdapter.ValidateToken()` is
  currently a stub — see the repo's session-token caveats before production.
- **Verify against current AWS docs.** AgentCore is evolving; confirm API field
  names/Region availability against the live reference before rollout.

## Sources

- [OpenAPI schema targets](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/gateway-schema-openapi.html)
- [Define the gateway target configuration](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/gateway-add-target-api-target-config.html)
- [Specify target authorization type and credentials](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/gateway-building-adding-targets-authorization.html)
- [Set up outbound authorization](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/gateway-outbound-auth.html)
- [Set up inbound authorization](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/gateway-inbound-auth.html)
