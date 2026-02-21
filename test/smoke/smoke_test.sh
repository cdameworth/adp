#!/usr/bin/env bash
# Smoke test for ADP git enforcement chain (prepare → register → verify).
# Builds adp-server, starts it in SQLite mode, exercises the commit lifecycle
# endpoints with curl, and reports pass/fail.
#
# Usage: bash test/smoke/smoke_test.sh
# Requirements: go, curl, jq

set -euo pipefail

PASS=0
FAIL=0
TMPDIR=$(mktemp -d)
BINARY="$TMPDIR/adp-server"
BODY_FILE="$TMPDIR/body.json"
PID=""

cleanup() {
    if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
        kill "$PID" 2>/dev/null || true
        wait "$PID" 2>/dev/null || true
    fi
    rm -rf "$TMPDIR"
}
trap cleanup EXIT

log_pass() { PASS=$((PASS + 1)); echo "  PASS: $1"; }
log_fail() { FAIL=$((FAIL + 1)); echo "  FAIL: $1 -- $2"; }

# Helper: POST JSON and capture body + HTTP code separately.
# Usage: HTTP_CODE=$(post_json URL PAYLOAD)
# Body is written to $BODY_FILE.
post_json() {
    curl -s -o "$BODY_FILE" -w "%{http_code}" -X POST "$1" \
        -H "Content-Type: application/json" \
        -d "$2"
}

# --- Build ---
echo "==> Building adp-server..."
cd "$(dirname "$0")/../.."
go build -o "$BINARY" ./cmd/adp-server
echo "    Built: $BINARY"

# --- Start server on random port ---
PORT=$(( (RANDOM % 10000) + 20000 ))
echo "==> Starting server on port $PORT (SQLite :memory:)..."

ADP_STORE=sqlite ADP_SQLITE_PATH=":memory:" ADP_SERVER_PORT="$PORT" \
    "$BINARY" > "$TMPDIR/server.log" 2>&1 &
PID=$!

# Wait for server to be ready (up to 5 seconds).
for i in $(seq 1 50); do
    if curl -s "http://localhost:$PORT/health" > /dev/null 2>&1; then
        break
    fi
    sleep 0.1
done

if ! curl -s "http://localhost:$PORT/health" > /dev/null 2>&1; then
    echo "FATAL: Server failed to start. Log:"
    cat "$TMPDIR/server.log"
    exit 1
fi
echo "    Server ready (PID $PID)"

BASE="http://localhost:$PORT"

# --- Test 1: Health check ---
echo "==> Test 1: Health check"
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/health")
if [ "$HTTP_CODE" = "200" ]; then
    log_pass "GET /health → 200"
else
    log_fail "GET /health" "expected 200, got $HTTP_CODE"
fi

# --- Test 2: Prepare commit (normal files) ---
echo "==> Test 2: Prepare commit (normal files)"
HTTP_CODE=$(post_json "$BASE/v1/commits/prepare" \
    '{"session_id":"smoke-session","files":["main.go","util.go"],"message":"smoke test commit"}')

TOKEN=$(jq -r '.data.commit_token // empty' "$BODY_FILE")
APPROVED=$(jq -r '.data.approved' "$BODY_FILE")

if [ "$HTTP_CODE" = "201" ]; then
    log_pass "POST /v1/commits/prepare → 201"
else
    log_fail "POST /v1/commits/prepare" "expected 201, got $HTTP_CODE"
fi

if [ -n "$TOKEN" ] && [[ "$TOKEN" == adp_* ]]; then
    log_pass "commit_token has adp_ prefix: ${TOKEN:0:20}..."
else
    log_fail "commit_token" "expected adp_* prefix, got '$TOKEN'"
fi

if [ "$APPROVED" = "true" ]; then
    log_pass "approved = true for normal files"
else
    log_fail "approved" "expected true, got '$APPROVED'"
fi

# --- Test 3: Register commit ---
echo "==> Test 3: Register commit"
SHA="smoke_sha_$(date +%s)"
HTTP_CODE=$(post_json "$BASE/v1/commits/register" \
    "{\"commit_token\":\"$TOKEN\",\"commit_sha\":\"$SHA\"}")
STATUS=$(jq -r '.data.status // empty' "$BODY_FILE")

if [ "$HTTP_CODE" = "200" ]; then
    log_pass "POST /v1/commits/register → 200"
else
    log_fail "POST /v1/commits/register" "expected 200, got $HTTP_CODE"
fi

if [ "$STATUS" = "committed" ]; then
    log_pass "status = committed"
else
    log_fail "register status" "expected 'committed', got '$STATUS'"
fi

# --- Test 4: Verify commit ---
echo "==> Test 4: Verify commit"
HTTP_CODE=$(post_json "$BASE/v1/commits/verify" \
    "{\"commit_sha\":\"$SHA\"}")
VERIFIED=$(jq -r '.data.verified' "$BODY_FILE")

if [ "$HTTP_CODE" = "200" ]; then
    log_pass "POST /v1/commits/verify → 200"
else
    log_fail "POST /v1/commits/verify" "expected 200, got $HTTP_CODE"
fi

if [ "$VERIFIED" = "true" ]; then
    log_pass "verified = true"
else
    log_fail "verified" "expected true, got '$VERIFIED'"
fi

# --- Test 5: Prepare with sensitive files ---
echo "==> Test 5: Prepare with sensitive files"
HTTP_CODE=$(post_json "$BASE/v1/commits/prepare" \
    '{"session_id":"smoke-session","files":[".env","main.go"],"message":"add config"}')
APPROVED=$(jq -r '.data.approved' "$BODY_FILE")

if [ "$HTTP_CODE" = "201" ]; then
    log_pass "POST /v1/commits/prepare (sensitive) → 201"
else
    log_fail "POST /v1/commits/prepare (sensitive)" "expected 201, got $HTTP_CODE"
fi

if [ "$APPROVED" = "false" ]; then
    log_pass "approved = false for sensitive files"
else
    log_fail "sensitive file approval" "expected false, got '$APPROVED'"
fi

# --- Test 6: Register unknown token ---
echo "==> Test 6: Register unknown token"
HTTP_CODE=$(post_json "$BASE/v1/commits/register" \
    '{"commit_token":"adp_nonexistent_token","commit_sha":"abc123"}')

if [ "$HTTP_CODE" = "404" ]; then
    log_pass "POST /v1/commits/register (unknown token) → 404"
else
    log_fail "register unknown token" "expected 404, got $HTTP_CODE"
fi

# --- Test 7: Verify unknown SHA ---
echo "==> Test 7: Verify unknown SHA"
HTTP_CODE=$(post_json "$BASE/v1/commits/verify" \
    '{"commit_sha":"never_registered_sha"}')
VERIFIED=$(jq -r '.data.verified' "$BODY_FILE")

if [ "$HTTP_CODE" = "200" ]; then
    log_pass "POST /v1/commits/verify (unknown SHA) → 200"
else
    log_fail "POST /v1/commits/verify (unknown SHA)" "expected 200, got $HTTP_CODE"
fi

if [ "$VERIFIED" = "false" ]; then
    log_pass "verified = false for unknown SHA"
else
    log_fail "unknown SHA verification" "expected false, got '$VERIFIED'"
fi

# --- Summary ---
echo ""
echo "==========================================="
TOTAL=$((PASS + FAIL))
echo "  Results: $PASS/$TOTAL passed, $FAIL failed"
echo "==========================================="

if [ "$FAIL" -gt 0 ]; then
    echo "  Server log:"
    cat "$TMPDIR/server.log"
    exit 1
fi
exit 0
