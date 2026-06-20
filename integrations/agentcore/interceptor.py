"""
AgentCore Gateway REQUEST interceptor that enforces ADP governance on every
tool call routed through the gateway.

Unlike the agent voluntarily calling adp_check_action, this runs on the gateway
for every `tools/call` regardless of agent cooperation — a chokepoint the agent
cannot skip. On each tool call it asks ADP `POST /v1/governance/check`; if ADP
denies or requires approval, the call is blocked (fail closed). Allowed calls
pass through unchanged.

Configure as the gateway's REQUEST interceptor (Lambda). See INTERCEPTOR.md.

Env:
  ADP_URL              base URL of adp-server (e.g. https://adp.internal:8080)
  ADP_API_KEY          optional; sent as X-API-Key
  ADP_SESSION_HEADER   optional request header carrying the ADP session id
                       (default x-adp-session-id; needs passRequestHeaders=true)
"""
import json
import logging
import os
import urllib.error
import urllib.request

logger = logging.getLogger()
logger.setLevel(logging.INFO)

ADP_URL = os.environ.get("ADP_URL", "").rstrip("/")
ADP_API_KEY = os.environ.get("ADP_API_KEY", "")
SESSION_HEADER = os.environ.get("ADP_SESSION_HEADER", "x-adp-session-id").lower()


class GovernanceDenied(Exception):
    """Raised to block a tool call. The gateway treats an interceptor failure as
    a blocked invocation (fail closed). Verify the exact deny contract for your
    AgentCore version — see INTERCEPTOR.md."""


def lambda_handler(event, context):
    mcp = event.get("mcp", {})

    # If this same Lambda is also wired as a RESPONSE interceptor, pass through.
    if mcp.get("gatewayResponse") is not None:
        gr = mcp["gatewayResponse"]
        return {
            "interceptorOutputVersion": "1.0",
            "mcp": {
                "transformedGatewayResponse": {
                    "body": gr.get("body", {}),
                    "statusCode": gr.get("statusCode", 200),
                }
            },
        }

    gateway_request = mcp.get("gatewayRequest", {})
    body = gateway_request.get("body", {}) or {}

    # Only govern tool invocations; pass everything else (initialize, list, ...).
    if body.get("method") != "tools/call":
        return _passthrough(body)

    params = body.get("params", {}) or {}
    tool_name = params.get("name", "unknown")
    arguments = params.get("arguments", {}) or {}

    session_id = arguments.get("session_id") or _header(gateway_request, SESSION_HEADER)
    if not session_id:
        raise GovernanceDenied(
            f"tool '{tool_name}' blocked: no ADP session id "
            f"(set arguments.session_id or the {SESSION_HEADER} header)"
        )

    try:
        result = _check_action(session_id, tool_name, arguments)
    except (urllib.error.URLError, urllib.error.HTTPError, TimeoutError) as e:
        raise GovernanceDenied(f"governance check failed for '{tool_name}': {e}")

    if not result.get("allowed", False) or result.get("requires_approval", False):
        reasons = result.get("denied_reasons") or result.get("policy_names") or []
        raise GovernanceDenied(
            f"tool '{tool_name}' denied by policy: "
            f"{', '.join(reasons) if reasons else 'not allowed'}"
        )

    logger.info("ADP allowed tool '%s' for session %s", tool_name, session_id)
    return _passthrough(body)


def _check_action(session_id, action_type, target):
    payload = json.dumps(
        {"session_id": session_id, "action_type": action_type, "target": target}
    ).encode()
    headers = {"Content-Type": "application/json"}
    if ADP_API_KEY:
        headers["X-API-Key"] = ADP_API_KEY
    req = urllib.request.Request(
        f"{ADP_URL}/v1/governance/check", data=payload, method="POST", headers=headers
    )
    with urllib.request.urlopen(req, timeout=10) as resp:
        parsed = json.loads(resp.read() or b"{}")
    # adp-server wraps the result as {"data": {...}}.
    return parsed.get("data", parsed)


def _passthrough(body):
    return {
        "interceptorOutputVersion": "1.0",
        "mcp": {"transformedGatewayRequest": {"body": body}},
    }


def _header(gateway_request, name):
    for k, v in (gateway_request.get("headers", {}) or {}).items():
        if k.lower() == name:
            return v
    return None
