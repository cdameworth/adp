"""
AgentCore Gateway REQUEST interceptor that enforces ADP governance on every
tool call routed through the gateway.

Unlike the agent voluntarily calling adp_check_action, this runs on the gateway
for every `tools/call` regardless of agent cooperation — a chokepoint the agent
cannot skip. On each tool call it asks ADP `POST /v1/governance/check`; if ADP
denies, requires approval, or CANNOT BE REACHED, the call is blocked.

TRUST MODEL (issue #13): this interceptor FAILS CLOSED. Every failure mode —
connection refused, timeout, 5xx, malformed response, missing fields — maps to
deny. The only escape hatch is the explicit env var ADP_INTERCEPTOR_FAIL_OPEN=1,
intended for development only; when active it logs a WARNING on every bypassed
call. Never set it in production.

Deny reasons are prefixed so dashboards/alerting can distinguish them:
  policy_denied:          ADP evaluated the action and denied it
  governance_unavailable: ADP could not be reached or gave an unusable answer

Env:
  ADP_URL                    base URL of adp-server (e.g. https://adp.internal:8080).
                             REQUIRED — if unset, all tool calls are denied
                             (unless fail-open is enabled).
  ADP_API_KEY                optional; sent as X-API-Key
  ADP_SESSION_HEADER         optional request header carrying the ADP session id
                             (default x-adp-session-id; needs passRequestHeaders=true)
  ADP_INTERCEPTOR_FAIL_OPEN  "1" allows tool calls when governance is
                             unreachable (development only; logs WARNING)
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
FAIL_OPEN = os.environ.get("ADP_INTERCEPTOR_FAIL_OPEN") == "1"
CHECK_TIMEOUT_SECONDS = 3  # single attempt; retries multiply agent latency

if not ADP_URL:
    logger.error("ADP_URL is not set; all tool calls will be denied%s",
                 " (fail-open active)" if FAIL_OPEN else "")
if FAIL_OPEN:
    logger.warning("ADP_INTERCEPTOR_FAIL_OPEN=1: governance outages will ALLOW "
                   "tool calls. Development use only.")


class GovernanceDenied(Exception):
    """Raised to block a tool call. The gateway treats an interceptor failure as
    a blocked invocation (fail closed). Verify the exact deny contract for your
    AgentCore version — see INTERCEPTOR.md."""


def lambda_handler(event, context):
    mcp = event.get("mcp", {}) if isinstance(event, dict) else {}

    # If this same Lambda is also wired as a RESPONSE interceptor, pass through.
    if isinstance(mcp.get("gatewayResponse"), dict):
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

    gateway_request = mcp.get("gatewayRequest", {}) or {}
    body = gateway_request.get("body", {})

    # Unparseable/typed-wrong request bodies cannot be governed — deny.
    if not isinstance(body, dict):
        raise GovernanceDenied(
            "governance_unavailable: gateway request body missing or not an object"
        )

    # Only govern tool invocations; pass everything else (initialize, list, ...).
    if body.get("method") != "tools/call":
        return _passthrough(body)

    params = body.get("params", {}) or {}
    tool_name = params.get("name", "unknown")
    arguments = params.get("arguments", {}) or {}
    if not isinstance(arguments, dict):
        arguments = {}

    session_id = arguments.get("session_id") or _header(gateway_request, SESSION_HEADER)
    if not session_id:
        raise GovernanceDenied(
            f"policy_denied: tool '{tool_name}' blocked: no ADP session id "
            f"(set arguments.session_id or the {SESSION_HEADER} header)"
        )

    if not ADP_URL:
        return _governance_outage(body, tool_name, "ADP_URL not configured")

    try:
        result = _check_action(session_id, tool_name, arguments)
    except GovernanceDenied:
        raise
    except Exception as e:  # noqa: BLE001 — fail closed on ANY transport/parse failure
        return _governance_outage(body, tool_name, f"{type(e).__name__}: {e}")

    # Strict shape validation: allowed must be exactly boolean true.
    if not isinstance(result, dict) or "allowed" not in result:
        return _governance_outage(body, tool_name, "governance response missing 'allowed'")
    if result.get("allowed") is not True:
        _deny_policy(tool_name, result)
    if result.get("requires_approval") is True:
        raise GovernanceDenied(
            f"policy_denied: tool '{tool_name}' requires human approval"
        )

    logger.info("ADP allowed tool '%s' for session %s", tool_name, session_id)
    return _passthrough(body)


def _deny_policy(tool_name, result):
    reasons = result.get("denied_reasons") or result.get("policy_names") or []
    detail = ", ".join(str(r) for r in reasons) if reasons else "not allowed"
    raise GovernanceDenied(f"policy_denied: tool '{tool_name}' denied: {detail}")


def _governance_outage(body, tool_name, detail):
    """Governance could not produce an answer. Fail closed unless the explicit
    development escape hatch is set; fail-open passes the call through and
    logs a WARNING for every bypassed call."""
    if FAIL_OPEN:
        logger.warning(
            "governance_unavailable (%s) but ADP_INTERCEPTOR_FAIL_OPEN=1: "
            "ALLOWING tool '%s' without a policy decision", detail, tool_name,
        )
        return _passthrough(body)
    raise GovernanceDenied(f"governance_unavailable: {detail}")


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
    with urllib.request.urlopen(req, timeout=CHECK_TIMEOUT_SECONDS) as resp:
        raw = resp.read() or b"{}"
    parsed = json.loads(raw)  # JSONDecodeError propagates -> fail closed
    if not isinstance(parsed, dict):
        raise ValueError("governance response was not a JSON object")
    # adp-server wraps the result as {"data": {...}}.
    data = parsed.get("data", parsed)
    if not isinstance(data, dict):
        raise ValueError("governance response 'data' was not a JSON object")
    return data


def _passthrough(body):
    return {
        "interceptorOutputVersion": "1.0",
        "mcp": {"transformedGatewayRequest": {"body": body}},
    }


def _header(gateway_request, name):
    headers = gateway_request.get("headers", {}) if isinstance(gateway_request, dict) else {}
    for k, v in (headers or {}).items():
        if k.lower() == name:
            return v
    return None
