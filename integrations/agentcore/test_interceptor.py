"""Fail-closed test matrix for the AgentCore gateway interceptor (#13).

Every failure mode — unreachable server, timeout, 5xx, malformed JSON, wrong
types, missing fields — must deny. The only exceptions are the explicit
fail-open env var (development only) and non-tool-call methods.

Run: python3 -m unittest discover -s integrations/agentcore -p "test_*.py"
"""
import importlib
import io
import json
import os
import sys
import unittest
import urllib.error
from unittest import mock

sys.path.insert(0, os.path.dirname(__file__))


def load_interceptor(**env):
    """(Re)load the interceptor module with a controlled environment, since it
    reads config at import time."""
    for k in ("ADP_URL", "ADP_API_KEY", "ADP_INTERCEPTOR_FAIL_OPEN"):
        os.environ.pop(k, None)
    os.environ.update(env)
    import interceptor
    importlib.reload(interceptor)
    return interceptor


def make_event(tool="some_tool", session="sess-1", arguments=None):
    args = {"session_id": session}
    if arguments:
        args.update(arguments)
    return {
        "mcp": {
            "gatewayRequest": {
                "body": {
                    "jsonrpc": "2.0",
                    "id": 1,
                    "method": "tools/call",
                    "params": {"name": tool, "arguments": args},
                }
            }
        }
    }


class FakeResp:
    def __init__(self, payload: bytes):
        self._payload = payload

    def read(self):
        return self._payload

    def __enter__(self):
        return self

    def __exit__(self, *a):
        return False


def respond_with(payload):
    if isinstance(payload, (dict, list)):
        payload = json.dumps(payload).encode()
    return lambda *a, **k: FakeResp(payload)


class FailClosedMatrix(unittest.TestCase):
    """Default mode (fail-open NOT set): everything suspicious denies."""

    @classmethod
    def setUpClass(cls):
        cls.ic = load_interceptor(ADP_URL="https://adp.test:8080")

    def assert_denied(self, prefix=None):
        with self.assertRaises(self.ic.GovernanceDenied) as cm:
            self.ic.lambda_handler(make_event(), None)
        if prefix:
            self.assertTrue(
                str(cm.exception).startswith(prefix),
                f"expected reason prefix {prefix!r}, got: {cm.exception}",
            )

    def test_allowed_call_passes_through(self):
        with mock.patch("urllib.request.urlopen", respond_with({"data": {"allowed": True}})):
            out = self.ic.lambda_handler(make_event(), None)
        self.assertIn("transformedGatewayRequest", out["mcp"])

    def test_unwrapped_response_also_accepted(self):
        with mock.patch("urllib.request.urlopen", respond_with({"allowed": True})):
            out = self.ic.lambda_handler(make_event(), None)
        self.assertIn("transformedGatewayRequest", out["mcp"])

    def test_policy_deny(self):
        with mock.patch("urllib.request.urlopen",
                        respond_with({"data": {"allowed": False, "denied_reasons": ["sensitive file"]}})):
            self.assert_denied("policy_denied:")

    def test_requires_approval_denies(self):
        with mock.patch("urllib.request.urlopen",
                        respond_with({"data": {"allowed": True, "requires_approval": True}})):
            self.assert_denied("policy_denied:")

    def test_http_500_denies(self):
        err = urllib.error.HTTPError("https://x", 500, "boom", {}, None)
        with mock.patch("urllib.request.urlopen", side_effect=err):
            self.assert_denied("governance_unavailable:")

    def test_http_403_denies(self):
        err = urllib.error.HTTPError("https://x", 403, "forbidden", {}, None)
        with mock.patch("urllib.request.urlopen", side_effect=err):
            self.assert_denied("governance_unavailable:")

    def test_connection_refused_denies(self):
        with mock.patch("urllib.request.urlopen",
                        side_effect=urllib.error.URLError("connection refused")):
            self.assert_denied("governance_unavailable:")

    def test_timeout_denies(self):
        with mock.patch("urllib.request.urlopen", side_effect=TimeoutError("timed out")):
            self.assert_denied("governance_unavailable:")

    def test_malformed_json_denies(self):
        with mock.patch("urllib.request.urlopen", respond_with(b"not json")):
            self.assert_denied("governance_unavailable:")

    def test_empty_body_denies(self):
        with mock.patch("urllib.request.urlopen", respond_with(b"")):
            self.assert_denied("governance_unavailable:")

    def test_allowed_null_denies(self):
        with mock.patch("urllib.request.urlopen", respond_with({"allowed": None})):
            self.assert_denied("policy_denied:")

    def test_allowed_wrong_type_denies(self):
        with mock.patch("urllib.request.urlopen", respond_with({"allowed": "yes"})):
            self.assert_denied("policy_denied:")

    def test_allowed_key_missing_denies(self):
        with mock.patch("urllib.request.urlopen", respond_with({"data": {"status": "ok"}})):
            self.assert_denied("governance_unavailable:")

    def test_data_not_object_denies(self):
        with mock.patch("urllib.request.urlopen", respond_with({"data": [1, 2]})):
            self.assert_denied("governance_unavailable:")

    def test_no_session_id_denies(self):
        event = make_event()
        del event["mcp"]["gatewayRequest"]["body"]["params"]["arguments"]["session_id"]
        with self.assertRaises(self.ic.GovernanceDenied) as cm:
            self.ic.lambda_handler(event, None)
        self.assertIn("no ADP session id", str(cm.exception))

    def test_non_tool_call_passes_without_server(self):
        event = {"mcp": {"gatewayRequest": {"body": {"method": "initialize"}}}}
        out = self.ic.lambda_handler(event, None)
        self.assertIn("transformedGatewayRequest", out["mcp"])

    def test_non_dict_body_denies(self):
        event = {"mcp": {"gatewayRequest": {"body": "tools/call"}}}
        self.assert_denied  # silence linter about method reference
        with self.assertRaises(self.ic.GovernanceDenied) as cm:
            self.ic.lambda_handler(event, None)
        self.assertTrue(str(cm.exception).startswith("governance_unavailable:"))

    def test_timeout_is_short(self):
        captured = {}

        def spy(req, timeout=None):
            captured["timeout"] = timeout
            return FakeResp(json.dumps({"allowed": True}).encode())

        with mock.patch("urllib.request.urlopen", spy):
            self.ic.lambda_handler(make_event(), None)
        self.assertLessEqual(captured["timeout"], 3)


class MissingConfig(unittest.TestCase):
    def test_no_adp_url_denies(self):
        ic = load_interceptor()  # no ADP_URL
        with self.assertRaises(ic.GovernanceDenied) as cm:
            ic.lambda_handler(make_event(), None)
        self.assertTrue(str(cm.exception).startswith("governance_unavailable:"))


class FailOpenHatch(unittest.TestCase):
    """ADP_INTERCEPTOR_FAIL_OPEN=1: outage allows the call, with a WARNING."""

    def test_outage_allows_with_warning(self):
        ic = load_interceptor(ADP_URL="https://adp.test:8080",
                              ADP_INTERCEPTOR_FAIL_OPEN="1")
        with mock.patch("urllib.request.urlopen",
                        side_effect=urllib.error.URLError("down")):
            with self.assertLogs(level="WARNING") as logs:
                out = ic.lambda_handler(make_event(), None)
        self.assertIn("transformedGatewayRequest", out["mcp"])
        self.assertTrue(any("FAIL_OPEN" in m for m in logs.output))

    def test_policy_denials_still_deny_under_fail_open(self):
        ic = load_interceptor(ADP_URL="https://adp.test:8080",
                              ADP_INTERCEPTOR_FAIL_OPEN="1")
        with mock.patch("urllib.request.urlopen",
                        respond_with({"data": {"allowed": False}})):
            with self.assertRaises(ic.GovernanceDenied):
                ic.lambda_handler(make_event(), None)


if __name__ == "__main__":
    unittest.main()
