# Security Policy

## Project Status

ADP is in **alpha**. The codebase is under active development and has not undergone a formal security audit. Use accordingly.

## Supported Versions

Only the latest version on the `main` branch receives security fixes. There are no stable releases yet.

| Version | Supported |
|---------|-----------|
| `main` (HEAD) | Yes |
| All other branches | No |

## Reporting a Vulnerability

If you discover a security vulnerability in ADP, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

### How to report

1. Go to the repository's **Security** tab on GitHub.
2. Click **Report a vulnerability** under "Security Advisories."
3. Provide a description of the vulnerability, steps to reproduce, and the potential impact.

If GitHub Security Advisories are not available for this repository, email the maintainers directly with the subject line `[SECURITY] ADP vulnerability report`.

### What to expect

- **Acknowledgment**: We will acknowledge receipt within 7 days.
- **Assessment**: We will evaluate the report and determine severity within 14 days.
- **Fix timeline**: Critical issues will be prioritized. Given this is an alpha project maintained by a small team, fix timelines depend on severity and complexity.
- **Disclosure**: We will coordinate disclosure with you. We ask that you do not publicly disclose the vulnerability until a fix is available.

### What qualifies

- Authentication or authorization bypass
- SQL injection, command injection, or path traversal
- Policy engine bypass (agent circumventing governance checks)
- Session hijacking or spoofing
- Sensitive data exposure (credentials, tokens, PII in logs)
- MCP protocol vulnerabilities that could allow unauthorized agent actions

### Out of scope

- Vulnerabilities in dependencies (report those to the upstream project)
- Issues that require physical access to the server
- Denial of service via resource exhaustion (known limitation in alpha)
- Issues in the web dashboard (it is a non-functional skeleton)
- Social engineering attacks
- Vulnerabilities in third-party services (PostgreSQL, Neo4j, etc.)

## Security Considerations

ADP processes governance decisions for AI coding agents. Key security surfaces include:

- **MCP server**: Agents connect via stdio. The MCP server trusts the agent identity provided at session start. There is no mutual authentication yet.
- **JWT authentication**: The HTTP API validates JWT tokens but SAML integration is not yet wired end-to-end.
- **Policy engine**: Policy evaluation uses OPA/Rego. Custom policies should be reviewed before deployment.
- **Data storage**: Decision records may contain sensitive information about code changes. Secure your PostgreSQL/SQLite database accordingly.

## Version History

| Version | Date | Changes |
|---------|------|---------|
| 1.0.0 | 2026-02-06 | Initial security policy |
