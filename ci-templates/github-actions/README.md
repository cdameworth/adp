# ADP GitHub Actions

GitHub Actions for integrating ADP (Agent Developer Portal) verification into your CI/CD pipelines.

## adp-verify

Verifies that commits have valid ADP audit trails.

### Usage

```yaml
name: ADP Compliance Check

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main]

jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0  # Required for commit history

      - name: Verify ADP audit trail
        uses: your-org/adp-actions/adp-verify@v1
        with:
          adp-url: ${{ secrets.ADP_URL }}
          adp-token: ${{ secrets.ADP_TOKEN }}
```

### Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `adp-url` | ADP server URL | Yes | - |
| `adp-token` | ADP API token for authentication | Yes | - |
| `fail-on-unverified` | Fail the workflow if commits are not verified | No | `true` |
| `verify-all-commits` | Verify all commits in push, not just the head | No | `true` |

### Outputs

| Output | Description |
|--------|-------------|
| `verified` | Whether all commits are verified (`true`/`false`) |
| `total-commits` | Total number of commits checked |
| `verified-commits` | Number of verified commits |
| `unverified-commits` | Number of unverified commits |

### Examples

#### Basic usage with required branch protection

```yaml
name: ADP Compliance

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  adp-verify:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Verify ADP compliance
        uses: your-org/adp-actions/adp-verify@v1
        with:
          adp-url: ${{ secrets.ADP_URL }}
          adp-token: ${{ secrets.ADP_TOKEN }}
```

#### Warning mode (don't fail on unverified commits)

```yaml
- name: Check ADP compliance (warning only)
  uses: your-org/adp-actions/adp-verify@v1
  with:
    adp-url: ${{ secrets.ADP_URL }}
    adp-token: ${{ secrets.ADP_TOKEN }}
    fail-on-unverified: 'false'
```

#### Conditional deployment based on verification

```yaml
jobs:
  verify:
    runs-on: ubuntu-latest
    outputs:
      verified: ${{ steps.adp.outputs.verified }}
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Verify ADP compliance
        id: adp
        uses: your-org/adp-actions/adp-verify@v1
        with:
          adp-url: ${{ secrets.ADP_URL }}
          adp-token: ${{ secrets.ADP_TOKEN }}
          fail-on-unverified: 'false'

  deploy:
    needs: verify
    if: needs.verify.outputs.verified == 'true'
    runs-on: ubuntu-latest
    steps:
      - name: Deploy
        run: echo "Deploying verified code..."
```

### Setting up secrets

1. Go to your repository Settings → Secrets and variables → Actions
2. Add the following secrets:
   - `ADP_URL`: Your ADP server URL (e.g., `https://adp.example.com`)
   - `ADP_TOKEN`: API token with verification permissions

### Branch protection rules

To enforce ADP compliance, configure branch protection:

1. Go to Settings → Branches → Add rule
2. Enter branch pattern (e.g., `main`)
3. Enable "Require status checks to pass before merging"
4. Search for and add "ADP Compliance" status check
5. Save changes

## Troubleshooting

### "Failed to contact ADP server"

- Verify `ADP_URL` is correct and accessible from GitHub Actions runners
- Check if your ADP server requires IP allowlisting
- Ensure the token has the required permissions

### "No audit trail found"

- Commits must be made with an active ADP session
- Install ADP git hooks: `adp hooks install`
- Start a session before committing: `adp session start`

### Skipping verification for specific commits

Set the `ADP_BYPASS` environment variable when committing:

```bash
ADP_BYPASS=1 git commit -m "Human-made commit without ADP"
```

Note: Bypassed commits will fail verification unless your ADP server is configured to allow them.
