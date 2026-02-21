# Contributing to Agent Developer Portal (ADP)

This guide covers everything you need to start contributing to ADP. The goal is to get you from clone to working PR with minimal friction.

## Quick setup

Prerequisites: Go 1.25+. That is it. No Docker, no external databases. SQLite is the default development store.

```bash
# Clone and build
git clone https://github.com/your-org/adp.git
cd adp

# Build all three binaries
go build ./cmd/adp-server
go build ./cmd/adp-cli
go build ./cmd/adp-mcp

# Run the test suite
go test ./...

# Check for common issues
go vet ./...
```

If all three binaries compile and `go test ./...` passes, your environment is ready.

## How to contribute

### Issues

Before opening an issue, search existing issues to avoid duplicates.

**Bug reports** should include:
- ADP version and Go version
- Operating system
- Steps to reproduce
- Expected behavior vs. actual behavior
- Relevant error output or logs

**Feature requests** should include:
- What problem it solves
- Proposed behavior
- Any context on implementation approach (optional)

### Pull requests

1. Fork the repository and create a feature branch from `main`.
2. Make your changes.
3. Add or update tests for the code you changed.
4. Run `go fmt ./...`, `go vet ./...`, and `go test ./...` before pushing.
5. Open a PR against `main`.

Keep PRs focused. One logical change per PR. Large PRs that mix unrelated changes are harder to review and more likely to stall.

**In your PR description, include:**
- What changed and why
- How you tested it
- Related issue numbers (e.g., `Closes #42`)

## Code style

Follow standard Go conventions. The authoritative references are [Effective Go](https://go.dev/doc/effective_go) and the [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments) wiki.

The short version:

- Run `go fmt ./...` before every commit. Non-negotiable.
- Run `go vet ./...` to catch issues the compiler misses.
- Exported types and functions must have doc comments.
- Wrap errors with context: `fmt.Errorf("failed to load session %s: %w", id, err)`.
- Handle every error. Do not discard errors with `_` unless there is a documented reason.
- Keep functions focused. If a function needs a paragraph to explain, it probably needs to be split.

```go
// EvaluatePolicy checks the given action against the loaded Rego policies.
// It returns an EvaluationResult indicating whether the action is allowed,
// denied, or requires escalation.
func (e *Engine) EvaluatePolicy(ctx context.Context, input PolicyInput) (*EvaluationResult, error) {
    result, err := e.opa.Eval(ctx, input)
    if err != nil {
        return nil, fmt.Errorf("policy evaluation failed for action %s: %w", input.Action, err)
    }
    return result, nil
}
```

## Testing expectations

All new code should include tests. Use table-driven tests where there are multiple input variations to cover.

```go
func TestAutonomyLevel(t *testing.T) {
    tests := []struct {
        name       string
        trustLevel TrustLevel
        want       AutonomyLevel
    }{
        {"observer gets no autonomy", Observer, None},
        {"contributor proposes only", Contributor, ProposeOnly},
        {"maintainer executes safe", Maintainer, ExecuteSafe},
        {"admin gets full autonomy", Admin, Full},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := MapTrustToAutonomy(tt.trustLevel)
            if got != tt.want {
                t.Errorf("MapTrustToAutonomy(%v) = %v, want %v", tt.trustLevel, got, tt.want)
            }
        })
    }
}
```

Run the full suite before pushing:

```bash
go test ./...
```

For verbose output on a specific package:

```bash
go test -v ./internal/domain/governance/...
```

## Commit message format

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <short description>

<optional body explaining what and why>

<optional footer, e.g., Closes #123>
```

**Types:**

| Type       | Use for                                  |
|------------|------------------------------------------|
| `feat`     | New functionality                        |
| `fix`      | Bug fixes                                |
| `test`     | Adding or updating tests                 |
| `docs`     | Documentation changes                    |
| `refactor` | Code restructuring with no behavior change |
| `chore`    | Build, tooling, dependency updates       |

**Examples:**

```
feat(governance): add time-based policy constraints

Policies can now include time windows that restrict certain actions
outside business hours. Configurable per trust level.

Closes #87
```

```
test(audit): add unit tests for decision export

Covers CSV and JSON export paths, including edge cases for
empty decision logs and malformed timestamps.
```

## Areas where help is needed

This is an early-stage project. There are significant gaps, and contributions in these areas have immediate impact.

**Testing (highest priority).** Roughly 8 of 28 packages have any test coverage. The following packages have zero tests and would benefit most from contributions:

- `internal/domain/governance/` -- The policy engine, escalation logic, blast radius analysis, and autonomy mapping. This is the most complex package in the project and has no tests.
- `internal/api/handlers/` -- HTTP handler tests (request parsing, response formatting, error cases).
- `internal/domain/context/` -- Context orchestration and token budgeting logic.
- `internal/domain/agent/` -- Agent identity, session lifecycle, trust level management.

If you are looking for a first contribution, writing tests for any of these packages is a great starting point. You will learn the codebase and provide real value.

**Policy examples.** The `policies/` directory contains Rego policy files for the OPA engine. Additional examples covering common governance scenarios (rate limiting, file scope restrictions, time-based rules) would help users understand the system.

**Documentation.** Improvements to the `docs/` directory, inline code comments, and usage examples for the MCP tools and CLI are all welcome.

## Project structure

```
adp/
├── cmd/                    # Binary entry points
│   ├── adp-server/        # Main HTTP API server
│   ├── adp-cli/           # CLI tool
│   └── adp-mcp/           # MCP server for agent integration
├── internal/              # Private application code
│   ├── api/               # HTTP API layer (router, handlers, middleware)
│   ├── config/            # Configuration loading (Viper)
│   ├── domain/            # Core business logic
│   │   ├── agent/         # Agent identity & trust levels
│   │   ├── audit/         # Decision logging & export
│   │   ├── auth/          # RBAC types
│   │   ├── context/       # Context orchestration & token budgeting
│   │   ├── documentation/ # Auto-generated session docs (package: docengine)
│   │   ├── governance/    # Policy engine, escalation, blast radius
│   │   └── tenant/        # Multi-tenant support
│   ├── infrastructure/    # External service clients
│   │   └── database/      # PostgreSQL, SQLite, Neo4j, Qdrant, ClickHouse
│   ├── mcp/               # MCP protocol implementation
│   ├── observability/     # Metrics & health checks
│   ├── server/            # HTTP server setup
│   └── store/             # Store interfaces & types
├── migrations/            # Database migrations (postgres, neo4j, clickhouse, sqlite)
├── policies/              # OPA/Rego policy files
├── hooks/                 # Git hooks
├── docs/                  # Documentation
└── deploy/                # Docker configs
```

## Getting help

- Check the `docs/` directory
- Search existing issues
- Open a discussion on the repository for questions

## License

ADP is licensed under [Apache 2.0](LICENSE). By submitting a pull request, you agree that your contribution is licensed under the same terms.
