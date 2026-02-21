# Git Hook Validation Checklist

Manual testing procedure for validating ADP git hooks in a temporary repository.
These scenarios exercise the real git integration that cannot be automated in unit tests.

## Prerequisites

- `adp-mcp` binary built (`go build ./cmd/adp-mcp`)
- `jq` and `curl` installed
- Bash shell

## Setup

The `adp-mcp` binary includes an HTTP sidecar that serves commit endpoints for git hooks.
No separate `adp-server` process is needed.

```bash
# 1. Start adp-mcp with a FIFO to keep stdin open (sidecar runs on port 9090)
mkfifo /tmp/adp-fifo
ADP_HTTP_PORT=9090 ./adp-mcp < /tmp/adp-fifo > /dev/null 2>&1 &
ADP_PID=$!

# 2. Create a temporary git repository
TMPDIR=$(mktemp -d)
cd "$TMPDIR"
git init
git commit --allow-empty -m "initial"

# 3. Install ADP hooks
/path/to/adp/hooks/install.sh

# 4. Set environment variables
# In a real agent workflow, these come from adp_start_session response
export ADP_URL="http://localhost:9090"
export ADP_SESSION_ID="hook-test-session"
export ADP_SESSION_TOKEN="mcp_hook-test-session"

# 5. Create the .git/adp directory for hook state
mkdir -p .git/adp
```

## Test A: Normal Commit Flow

**Goal:** Verify pre-commit approves normal files and post-commit registers the SHA.

```bash
# Create a normal file and stage it
echo "package main" > main.go
git add main.go

# Commit -- pre-commit should approve, post-commit should register
git commit -m "add main.go"
```

**Expected:**
- [ ] Pre-commit hook exits 0 (allows commit)
- [ ] Pre-commit output shows `approved: true`
- [ ] Post-commit hook exits 0
- [ ] Post-commit output confirms SHA registration
- [ ] Verify via API: `curl -s -X POST http://localhost:9090/v1/commits/verify -H "Content-Type: application/json" -d "{\"commit_sha\":\"$(git rev-parse HEAD)\"}" | jq .data.verified` returns `true`

## Test B: Sensitive File Blocked

**Goal:** Verify pre-commit rejects commits containing sensitive files.

```bash
# Create a sensitive file and stage it
echo "SECRET_KEY=abc123" > .env
git add .env

# Commit -- should be blocked by pre-commit
git commit -m "add config"
```

**Expected:**
- [ ] Pre-commit hook exits non-zero (blocks commit)
- [ ] Pre-commit output mentions "sensitive" files
- [ ] The commit does NOT proceed (`git log` still shows previous commit)
- [ ] Clean up: `git reset HEAD .env && rm .env`

## Test C: Pre-push Verification (All Commits Verified)

**Goal:** Verify pre-push allows pushing when all commits are registered.

```bash
# Set up a bare remote for testing
REMOTE=$(mktemp -d)
git init --bare "$REMOTE"
git remote add test-remote "$REMOTE"

# Create and commit a normal file
echo "func hello() {}" > hello.go
git add hello.go
git commit -m "add hello"

# Push -- pre-push should verify all commits and allow
git push test-remote main
```

**Expected:**
- [ ] Pre-push hook exits 0 (allows push)
- [ ] Pre-push output confirms all commits verified
- [ ] Push completes successfully

## Test D: Pre-push with Unregistered Commit

**Goal:** Verify pre-push blocks pushing when a commit lacks ADP registration.

```bash
# Temporarily disable post-commit hook
chmod -x .git/hooks/post-commit

# Create and commit a file (no post-commit registration)
echo "func world() {}" > world.go
git add world.go
git commit -m "unregistered commit"

# Re-enable post-commit hook
chmod +x .git/hooks/post-commit

# Push -- pre-push should block because the commit is unregistered
git push test-remote main
```

**Expected:**
- [ ] Pre-push hook exits non-zero (blocks push)
- [ ] Pre-push output indicates unverified commit SHA
- [ ] Push does NOT proceed
- [ ] Clean up: re-do the commit with hooks enabled, or amend

## Test E: Bypass Token Mechanism

**Goal:** Verify the bypass token allows skipping hook validation.

```bash
# Configure a bypass token
BYPASS_TOKEN="test-bypass-secret"
echo -n "$BYPASS_TOKEN" | shasum -a 256 | cut -d ' ' -f 1 > .git/adp/bypass_hash
chmod 600 .git/adp/bypass_hash

# Create a sensitive file
echo "DB_PASSWORD=hunter2" > .env
git add .env

# Commit WITH bypass token -- should succeed despite sensitive file
ADP_BYPASS_TOKEN="$BYPASS_TOKEN" git commit -m "add env with bypass"
```

**Expected:**
- [ ] Pre-commit hook exits 0 (bypass accepted)
- [ ] Commit proceeds despite sensitive file
- [ ] Verify: remove bypass and try again:
  ```bash
  echo "MORE_SECRETS=yes" >> .env
  git add .env
  unset ADP_BYPASS_TOKEN
  git commit -m "should fail"  # Should be blocked
  ```
- [ ] Without bypass token, sensitive file is blocked

### Invalid Bypass Token

```bash
# Try with a wrong bypass token
echo "EXTRA=data" >> .env
git add .env
ADP_BYPASS_TOKEN="wrong-token" git commit -m "bad bypass"
```

**Expected:**
- [ ] Pre-commit hook exits non-zero (bypass rejected)
- [ ] Output indicates invalid bypass token or sensitive file blocked
- [ ] Commit does NOT proceed

## Cleanup

```bash
# Stop the adp-mcp process (which includes the HTTP sidecar)
kill $ADP_PID

# Remove temporary directories
rm -rf "$TMPDIR" "$REMOTE"
```

## Troubleshooting

| Symptom | Likely Cause |
|---------|-------------|
| Hook exits 0 but no output | Hook script not finding `ADP_URL` env var |
| `curl: (7) Failed to connect` | `adp-mcp` not running or wrong `ADP_HTTP_PORT` |
| Post-commit fails silently | Sidecar returned error on register endpoint |
| Pre-push allows unregistered | Hook not checking all commits in push range |
| Bypass always works | Bypass hash file has wrong permissions or content |
