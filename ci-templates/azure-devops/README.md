# ADP Azure DevOps Integration

Azure DevOps templates for integrating ADP (Agent Developer Portal) verification into your pipelines.

## Quick Start

### Option 1: Use the template

1. Copy `adp-verify-task.yml` to your repository
2. Reference it in your pipeline:

```yaml
# azure-pipelines.yml
trigger:
  - main

pool:
  vmImage: 'ubuntu-latest'

steps:
  - checkout: self
    fetchDepth: 0  # Required for commit history

  - template: adp-verify-task.yml
    parameters:
      adpUrl: $(ADP_URL)
      adpToken: $(ADP_TOKEN)

  - script: |
      echo "Build steps here..."
    displayName: 'Build'
```

### Option 2: Inline script

Copy the script directly into your pipeline for full customization:

```yaml
trigger:
  - main

pool:
  vmImage: 'ubuntu-latest'

steps:
  - checkout: self
    fetchDepth: 0

  - bash: |
      set -e
      # Verification script from adp-verify-task.yml
    displayName: 'Verify ADP Audit Trail'
    env:
      ADP_URL: $(ADP_URL)
      ADP_TOKEN: $(ADP_TOKEN)
```

## Configuration

### Required Variables

Set these in your pipeline or variable group:

| Variable | Description | Secret |
|----------|-------------|--------|
| `ADP_URL` | ADP server URL | No |
| `ADP_TOKEN` | ADP API token | Yes |

### Setting up variables

1. Go to Pipelines → Library → Variable groups
2. Create a new variable group (e.g., "ADP Settings")
3. Add variables:
   - `ADP_URL`: Your ADP server URL
   - `ADP_TOKEN`: Your API token (mark as secret)
4. Link the variable group to your pipeline

Or set them directly in the pipeline:

```yaml
variables:
  - group: 'ADP Settings'
  # Or inline:
  - name: ADP_URL
    value: 'https://adp.example.com'
```

### Template Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `adpUrl` | string | (required) | ADP server URL |
| `adpToken` | string | (required) | ADP API token |
| `failOnUnverified` | boolean | `true` | Fail pipeline on unverified commits |
| `verifyAllCommits` | boolean | `true` | Verify all commits, not just head |

## Examples

### Basic Pipeline

```yaml
trigger:
  branches:
    include:
      - main
      - develop

pool:
  vmImage: 'ubuntu-latest'

variables:
  - group: 'ADP Settings'

steps:
  - checkout: self
    fetchDepth: 0

  - template: adp-verify-task.yml
    parameters:
      adpUrl: $(ADP_URL)
      adpToken: $(ADP_TOKEN)

  - script: npm install && npm run build
    displayName: 'Build'

  - script: npm test
    displayName: 'Test'
```

### Warning Mode (Don't Fail)

```yaml
steps:
  - checkout: self
    fetchDepth: 0

  - template: adp-verify-task.yml
    parameters:
      adpUrl: $(ADP_URL)
      adpToken: $(ADP_TOKEN)
      failOnUnverified: false
```

### Conditional Deployment

```yaml
stages:
  - stage: Verify
    jobs:
      - job: ADPVerify
        pool:
          vmImage: 'ubuntu-latest'
        steps:
          - checkout: self
            fetchDepth: 0
          - template: adp-verify-task.yml
            parameters:
              adpUrl: $(ADP_URL)
              adpToken: $(ADP_TOKEN)

  - stage: Deploy
    dependsOn: Verify
    condition: succeeded()
    jobs:
      - deployment: Production
        environment: 'production'
        strategy:
          runOnce:
            deploy:
              steps:
                - script: echo "Deploying..."
```

### Multi-Stage with Output Variables

```yaml
stages:
  - stage: Verify
    jobs:
      - job: ADPVerify
        pool:
          vmImage: 'ubuntu-latest'
        steps:
          - checkout: self
            fetchDepth: 0
          - template: adp-verify-task.yml
            parameters:
              adpUrl: $(ADP_URL)
              adpToken: $(ADP_TOKEN)
              failOnUnverified: false
          - script: |
              echo "Verified: $(ADP_VERIFIED_COUNT) of $(ADP_TOTAL_COMMITS)"
            displayName: 'Show Results'

  - stage: Deploy
    dependsOn: Verify
    condition: and(succeeded(), eq(dependencies.Verify.outputs['ADPVerify.ADP_ALL_VERIFIED'], 'true'))
    jobs:
      - job: Deploy
        steps:
          - script: echo "Deploying verified code..."
```

## Branch Policies

To enforce ADP compliance, configure branch policies:

1. Go to Repos → Branches
2. Select your protected branch → Branch policies
3. Add a build validation policy
4. Select your pipeline with ADP verification
5. Enable "Required" to block PRs that fail verification

## Output Variables

The task sets these variables for use in subsequent steps:

| Variable | Description |
|----------|-------------|
| `ADP_TOTAL_COMMITS` | Total commits verified |
| `ADP_VERIFIED_COUNT` | Number of verified commits |
| `ADP_UNVERIFIED_COUNT` | Number of unverified commits |
| `ADP_ALL_VERIFIED` | `true` if all commits are verified |

## Troubleshooting

### "Failed to contact ADP server"

- Verify `ADP_URL` is accessible from Azure DevOps agents
- Check if your ADP server requires IP allowlisting for Microsoft-hosted agents
- Consider using self-hosted agents for private networks

### "Variable not found"

- Ensure variables are set in variable groups or pipeline variables
- Check that secret variables are marked correctly
- Verify variable group is linked to the pipeline

### Script fails with jq error

- The default Ubuntu image includes `jq`
- For other images, add: `sudo apt-get install -y jq`

### Fetch depth error

Add `fetchDepth: 0` to your checkout step:

```yaml
steps:
  - checkout: self
    fetchDepth: 0
```

This ensures full commit history is available for verification.
