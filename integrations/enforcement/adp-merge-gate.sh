#!/usr/bin/env bash
#
# ADP merge gate — fail unless every commit in a range has a verified ADP
# governance trail. Wire this as a *required* CI status check (branch
# protection) so agents cannot merge ungoverned commits, even if they skipped
# the client-side git hooks (--no-verify). It is the server-side, non-bypassable
# counterpart to hooks/pre-push.
#
# Fails closed: if ADP is unreachable, returns non-2xx, or any commit is
# unverified, the gate fails and the merge is blocked.
#
# Env:
#   ADP_URL       (required) e.g. https://adp.internal:8080
#   ADP_API_KEY   (optional) sent as X-API-Key
#   BASE_SHA, HEAD_SHA  commit range to check (or pass SHAs as args)
#
# Usage:
#   BASE_SHA=<base> HEAD_SHA=<head> ADP_URL=... ./adp-merge-gate.sh
#   ./adp-merge-gate.sh <sha1> <sha2> ...
#
set -euo pipefail

: "${ADP_URL:?set ADP_URL}"

if [[ "$#" -gt 0 ]]; then
  shas=("$@")
else
  : "${BASE_SHA:?set BASE_SHA/HEAD_SHA or pass SHAs as args}"
  : "${HEAD_SHA:?set BASE_SHA/HEAD_SHA or pass SHAs as args}"
  mapfile -t shas < <(git rev-list "${BASE_SHA}..${HEAD_SHA}")
fi

if [[ "${#shas[@]}" -eq 0 ]]; then
  echo "ADP gate: no commits to verify"
  exit 0
fi

payload="$(printf '%s\n' "${shas[@]}" | jq -R . | jq -s '{shas: .}')"

# -f makes curl exit non-zero on 4xx/5xx -> the gate fails closed.
resp="$(curl -fsS -X POST "${ADP_URL%/}/v1/commits/verify-batch" \
  -H 'Content-Type: application/json' \
  ${ADP_API_KEY:+-H "X-API-Key: ${ADP_API_KEY}"} \
  -d "$payload")"

if [[ "$(printf '%s' "$resp" | jq -r '.allowed')" == "true" ]]; then
  echo "ADP gate: all ${#shas[@]} commit(s) have a governance trail ✓"
  exit 0
fi

echo "ADP gate: BLOCKED — these commits have no verified governance trail:" >&2
printf '%s' "$resp" | jq -r '.unverified[]' | sed 's/^/  - /' >&2
echo "Re-create them inside an ADP session (adp_start_session + a governed git commit) before merging." >&2
exit 1
