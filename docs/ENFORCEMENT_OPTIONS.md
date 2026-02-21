# ADP Policy Enforcement Options

This document describes the three enforcement models available in ADP and their trust assumptions.

## Overview

ADP provides policy governance for AI agents through multiple enforcement mechanisms. Each mechanism offers different trade-offs between security, flexibility, and integration complexity.

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        Policy Enforcement Models                        │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐                 │
│  │   Advisory  │    │     MCP     │    │   Gateway   │                 │
│  │    (API)    │    │   Server    │    │   (Proxy)   │                 │
│  └─────────────┘    └─────────────┘    └─────────────┘                 │
│                                                                         │
│  Trust Model:       Trust Model:       Trust Model:                     │
│  Agent cooperates   MCP protocol       Zero trust -                     │
│  voluntarily        enforces calls     all actions                      │
│                                        intercepted                      │
│                                                                         │
│  Integration:       Integration:       Integration:                     │
│  HTTP API calls     MCP tool hooks     Proxy layer                      │
│                                                                         │
│  Enforcement:       Enforcement:       Enforcement:                     │
│  Advisory only      Protocol-level     Mandatory                        │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 1. Advisory Model (REST API)

**Trust Level**: Cooperative - agent voluntarily calls governance endpoints

### How It Works

```
┌──────────┐     1. Check action     ┌──────────────┐
│          │ ───────────────────────>│              │
│  Agent   │                         │   ADP API    │
│          │ <───────────────────────│              │
└──────────┘     2. Allow/Deny       └──────────────┘
     │
     │ 3. Agent decides whether
     │    to proceed or not
     ▼
┌──────────┐
│  Target  │
│  System  │
└──────────┘
```

### API Endpoints

| Endpoint | Purpose |
|----------|---------|
| `POST /v1/governance/check` | Check if action is allowed |
| `POST /v1/governance/approvals` | Request human approval |
| `GET /v1/governance/approvals/{id}` | Check approval status |
| `PATCH /v1/governance/approvals/{id}` | Resolve approval |

### Example: Check Action

```bash
curl -X POST http://localhost:8080/v1/governance/check \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "agent-session-123",
    "trust_level": 3,
    "action_type": "modify_file",
    "target": {
      "paths": ["src/config.go", ".env"]
    }
  }'
```

Response:
```json
{
  "data": {
    "allowed": false,
    "requires_approval": true,
    "denied_reasons": [
      "access to sensitive file '.env' blocked by pattern '.env'"
    ],
    "policy_names": ["Deny Sensitive Files", "Blast Radius Limit"]
  }
}
```

### Trust Assumptions

- ✅ Agent calls `/v1/governance/check` before actions
- ✅ Agent respects the response (doesn't proceed if denied)
- ✅ Agent doesn't bypass the API to access systems directly
- ❌ No enforcement if agent ignores the API

### Best For

- Development and testing environments
- Trusted internal agents
- Gradual adoption of governance
- Auditing agent behavior (even if advisory)

---

## 2. MCP Server Model

**Trust Level**: Protocol-enforced - governance integrated into tool execution

### How It Works

```
┌──────────┐     MCP Protocol      ┌──────────────┐
│          │ ─────────────────────>│              │
│  Agent   │  adp_check_action     │  ADP MCP     │───> Policy Engine
│          │ <─────────────────────│   Server     │
└──────────┘     Allow/Deny        └──────────────┘
     │                                    │
     │ If allowed, agent uses             │
     │ other MCP tools                    │
     ▼                                    ▼
┌──────────┐                       ┌──────────────┐
│  Target  │                       │   Session    │
│  System  │                       │   Tracking   │
└──────────┘                       └──────────────┘
```

### MCP Tools

| Tool | Purpose | Policy Check |
|------|---------|--------------|
| `adp_start_session` | Initialize agent session | Assigns trust level |
| `adp_check_action` | Evaluate action against policies | **Primary enforcement** |
| `adp_request_approval` | Escalate for human approval | Creates escalation |
| `adp_get_approval` | Poll approval status | Returns decision |
| `adp_log_decision` | Record audit trail | No blocking |
| `adp_prepare_commit` | Validate commit intent | Checks file policies |
| `adp_get_context` | Retrieve relevant context | Token-budgeted |

### Configuration

```yaml
# claude_desktop_config.json (for Claude Desktop)
{
  "mcpServers": {
    "adp": {
      "command": "/path/to/adp-mcp",
      "args": [],
      "env": {
        "ADP_API_URL": "http://localhost:8080",
        "ADP_POSTGRES_DSN": "postgres://..."
      }
    }
  }
}
```

### Trust Assumptions

- ✅ Agent uses MCP protocol (tool calls go through ADP)
- ✅ Policy checks happen on every `adp_check_action` call
- ✅ Session state tracks trust level and constraints
- ⚠️ Agent could still bypass MCP for direct file/network access
- ⚠️ Requires MCP-compatible agent

### Best For

- MCP-native agents (Claude, etc.)
- Controlled development environments
- Teams wanting integrated governance
- Audit trail requirements

---

## 3. Gateway/Proxy Model (Future)

**Trust Level**: Zero trust - all agent actions flow through ADP

### Proposed Architecture

```
┌──────────┐                      ┌──────────────────┐
│          │                      │                  │
│  Agent   │──── All Actions ────>│   ADP Gateway    │
│          │                      │                  │
└──────────┘                      └────────┬─────────┘
                                           │
                    ┌──────────────────────┼──────────────────────┐
                    │                      │                      │
                    ▼                      ▼                      ▼
             ┌──────────┐           ┌──────────┐           ┌──────────┐
             │   File   │           │   API    │           │  Deploy  │
             │  System  │           │  Calls   │           │ Pipeline │
             └──────────┘           └──────────┘           └──────────┘
```

### Components

1. **File System Proxy**
   - FUSE mount or container overlay
   - All file reads/writes routed through ADP
   - Policy evaluation before every operation

2. **Network Proxy**
   - HTTP/HTTPS proxy for API calls
   - Intercept and evaluate external requests
   - Block unauthorized endpoints

3. **Command Executor**
   - Sandboxed shell execution
   - Pre-command policy evaluation
   - Output capture for audit

4. **Git Hooks**
   - Pre-commit: Validate commit against session
   - Pre-push: Verify audit trail complete
   - Server-side: Reject unverified commits

### Trust Assumptions

- ✅ Agent cannot bypass proxy (containerized/sandboxed)
- ✅ Every action evaluated against policies
- ✅ Complete audit trail guaranteed
- ✅ No cooperation required from agent
- ⚠️ Higher integration complexity
- ⚠️ Performance overhead

### Implementation Status

| Component | Status |
|-----------|--------|
| Policy Engine | ✅ Implemented (unified engine with 6 builtins + OPA/Rego) |
| REST API | ✅ Implemented (43 operations, auth/RBAC, security headers) |
| MCP Server | ✅ Implemented (11 tools, HTTP sidecar for git hooks) |
| User Auth | ✅ Implemented (local JWT, API key, external JWKS, RBAC) |
| Git Hooks | ✅ Implemented (pre-commit, post-commit, pre-push with sidecar) |
| File Proxy | ❌ Not started |
| Network Proxy | ❌ Not started |
| Command Sandbox | ❌ Not started |

---

## Policy Engine Details

All three models use the same unified policy engine:

### Built-in Policies

| Policy | Description | Configurable |
|--------|-------------|--------------|
| `deny_sensitive_files` | Block access to .env, .pem, .key files | patterns |
| `blast_radius_limit` | Limit files modified per action | max_files, trust_override |
| `off_hours_production` | Block prod deploys 22:00-06:00 | start_hour, end_hour, min_trust |
| `cost_limit` | Trust-based cost limits | limits_by_trust |
| `require_migration_approval` | Force approval for DB migrations | action_types |

### Custom Rego Policies

```rego
package policy

deny[msg] {
    input.action.type == "deploy"
    input.context.environment == "production"
    input.session.trust_level < 4
    msg := "Production deployments require trust level 4+"
}

deny[msg] {
    input.action.type == "modify_file"
    some path in input.action.target.paths
    contains(path, "vendor/")
    msg := "Cannot modify vendor directory"
}
```

### Trust Levels

| Level | Name | Typical Permissions |
|-------|------|---------------------|
| 1 | Observer | Read-only access |
| 2 | Contributor | Limited modifications, review required |
| 3 | Developer | Standard development, 50 file limit |
| 4 | Maintainer | Most operations, some restrictions |
| 5 | Admin | Full access, no restrictions |

---

## Choosing an Enforcement Model

| Factor | Advisory | MCP | Gateway |
|--------|----------|-----|---------|
| Setup Complexity | Low | Medium | High |
| Agent Compatibility | Any | MCP-only | Any |
| Enforcement Strength | Weak | Medium | Strong |
| Performance Impact | Low | Low | Medium |
| Audit Completeness | Partial | Good | Complete |
| Trust Required | High | Medium | None |

### Recommendations

**Development/Testing**: Start with Advisory model
- Quick to integrate
- Test policies without blocking work
- Gather data on agent behavior

**Production with MCP Agents**: Use MCP Server model
- Integrated governance
- Session tracking
- Good audit trail

**High-Security/Compliance**: Plan for Gateway model
- Zero-trust architecture
- Complete enforcement
- Regulatory compliance

---

## Version History

| Version | Date | Changes |
|---------|------|---------|
| 1.1.0 | 2026-02-21 | Updated implementation status: MCP server, REST API, Git hooks, and user auth all fully implemented. |
| 1.0.0 | 2026-01-30 | Initial documentation |
