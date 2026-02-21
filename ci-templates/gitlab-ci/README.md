# ADP GitLab CI Templates

GitLab CI templates for integrating ADP (Agent Developer Portal) verification into your pipelines.

## Quick Start

### Option 1: Include from remote project

```yaml
include:
  - project: 'your-org/adp'
    ref: main
    file: '/ci-templates/gitlab-ci/adp-verify.yml'

variables:
  ADP_URL: https://adp.example.com

stages:
  - test
  - build
  - deploy
```

### Option 2: Copy to your project

Copy `adp-verify.yml` to your repository and include it:

```yaml
include:
  - local: '.gitlab/ci/adp-verify.yml'

stages:
  - test
  - build
  - deploy
```

## Configuration

### Required Variables

Set these in your GitLab CI/CD Settings (Settings → CI/CD → Variables):

| Variable | Description | Masked | Protected |
|----------|-------------|--------|-----------|
| `ADP_TOKEN` | ADP API token | Yes | Optional |

Or set them in your `.gitlab-ci.yml`:

```yaml
variables:
  ADP_URL: https://adp.example.com
  # ADP_TOKEN should be set in CI/CD Settings as a masked variable
```

### Optional Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `ADP_FAIL_ON_UNVERIFIED` | Fail pipeline on unverified commits | `true` |

## Available Jobs

### `adp-verify`

Main verification job. Runs on both push and merge request pipelines.

```yaml
include:
  - project: 'your-org/adp'
    ref: main
    file: '/ci-templates/gitlab-ci/adp-verify.yml'

stages:
  - test

# The adp-verify job is automatically included
```

### `adp-verify-mr`

Verification job specifically for merge requests.

### `adp-verify-push`

Verification job specifically for push events.

### `adp-verify-warn`

Warning-only mode - reports results but doesn't fail the pipeline.

```yaml
include:
  - project: 'your-org/adp'
    ref: main
    file: '/ci-templates/gitlab-ci/adp-verify.yml'

# Override to use warning mode
adp-verify:
  extends: .adp-verify
  variables:
    ADP_FAIL_ON_UNVERIFIED: "false"
  allow_failure: true
```

## Examples

### Basic Setup

```yaml
include:
  - project: 'your-org/adp'
    ref: main
    file: '/ci-templates/gitlab-ci/adp-verify.yml'

variables:
  ADP_URL: https://adp.example.com

stages:
  - test
  - build
  - deploy

# adp-verify job runs automatically in test stage

build:
  stage: build
  script:
    - make build

deploy:
  stage: deploy
  script:
    - make deploy
  only:
    - main
```

### Conditional Deployment

Deploy only if ADP verification passes:

```yaml
include:
  - project: 'your-org/adp'
    ref: main
    file: '/ci-templates/gitlab-ci/adp-verify.yml'

stages:
  - verify
  - deploy

adp-verify:
  stage: verify

deploy-production:
  stage: deploy
  needs:
    - adp-verify
  script:
    - echo "Deploying verified code..."
  only:
    - main
```

### Custom Verification Job

Extend the base template for custom behavior:

```yaml
include:
  - project: 'your-org/adp'
    ref: main
    file: '/ci-templates/gitlab-ci/adp-verify.yml'

adp-verify-custom:
  extends: .adp-verify
  stage: compliance
  variables:
    ADP_FAIL_ON_UNVERIFIED: "true"
  after_script:
    - |
      if [ "$CI_JOB_STATUS" = "failed" ]; then
        curl -X POST "$SLACK_WEBHOOK" \
          -H 'Content-type: application/json' \
          -d '{"text":"ADP verification failed for '"$CI_PROJECT_NAME"'"}'
      fi
  rules:
    - if: $CI_COMMIT_BRANCH == "main"
```

### Multiple Environments

Different rules for different branches:

```yaml
include:
  - project: 'your-org/adp'
    ref: main
    file: '/ci-templates/gitlab-ci/adp-verify.yml'

# Strict verification for main branch
adp-verify-strict:
  extends: .adp-verify
  variables:
    ADP_FAIL_ON_UNVERIFIED: "true"
  rules:
    - if: $CI_COMMIT_BRANCH == "main"

# Warning-only for feature branches
adp-verify-warn:
  extends: .adp-verify
  variables:
    ADP_FAIL_ON_UNVERIFIED: "false"
  allow_failure: true
  rules:
    - if: $CI_COMMIT_BRANCH != "main"
```

## Protected Branch Settings

To enforce ADP compliance, configure protected branches:

1. Go to Settings → Repository → Protected branches
2. Select your protected branch (e.g., `main`)
3. Enable "Require status checks to pass"
4. The `adp-verify` job will block merges if verification fails

## Troubleshooting

### "Failed to contact ADP server"

- Verify `ADP_URL` is correct and accessible from GitLab runners
- Check if your runners need network access configured
- Ensure the token has the required permissions

### "Variable ADP_TOKEN is not set"

- Set the variable in CI/CD Settings as a masked variable
- Or add it to your `.gitlab-ci.yml` (not recommended for security)

### Pipeline passes but commits aren't verified

- Ensure `ADP_FAIL_ON_UNVERIFIED` is set to `true`
- Check that the job is not set to `allow_failure: true`

### Skipping verification

If you need to bypass verification for specific commits:

```bash
ADP_BYPASS=1 git commit -m "Human-made commit"
```

Or add to pipeline variables for emergency situations (not recommended for regular use).
