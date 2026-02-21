---
title: "I Built a Governance Layer for AI Coding Agents — Here's Why They Need One"
platform: devto
tags: [ai, opensource, devops, programming]
series: ""
published: false
journal_entry_ids: [f6ca45a3675d5b32]
generated_at: "2026-02-13T00:00:00Z"
---

**TL;DR:** Internal Developer Portals weren't built with AI coding agents in mind. I built ADP — an open-source governance layer that gives agents trust levels, policy guardrails, audit trails, and auto-generated documentation through MCP. It plugs into Backstage, enforces compliance at the git hook level, and turns agent decision records into readable reports automatically. Alpha is live, three binaries, works today.

---

I was reading a Medium article about Internal Developer Portals when it hit me — none of them were designed for what's actually happening on engineering teams right now. Claude Code, Cursor, Copilot — these agents are shipping real code, making real decisions, touching real infrastructure. And the portal treats them like they don't exist.

The developer portal knows who *you* are. It knows your services, your APIs, your runbooks. But the agent writing half your commits? Invisible. No identity, no guardrails, no audit trail.

That felt like a problem worth solving.

## What ADP Actually Does

ADP (Agent Developer Portal) is a governance and audit layer for AI coding agents. It connects to agents via [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) and does four things:

1. **Assigns trust levels** — agents get a trust score from 1 to 5 that determines what they can do autonomously
2. **Evaluates actions against policies** — every file modification, deployment, or sensitive operation goes through a policy engine before it happens
3. **Records decisions** — every action an agent takes gets logged with reasoning, confidence, and alternatives considered
4. **Generates documentation** — a background agent turns decision records into session summaries, risk reports, and pattern analysis automatically

It integrates with [Backstage](https://backstage.io/) via a plugin, so your existing developer portal gains visibility into what agents are doing across your org.

## The Policy Engine

This is the core of ADP. It's a unified engine that combines multiple evaluation strategies into a single decision:

**Trust levels** control autonomy. A level-3 agent can modify files. A level-5 agent can deploy to production. Below that, you need human approval.

**Sensitive file protection** blocks agents from touching things like `.env`, `secrets/`, and `*.key` — regardless of trust level.

**Blast radius analysis** estimates the impact of changes before they happen — how many files, which services, what's the risk score.

**Time-based policies** let you say "no production deploys between midnight and 6 AM" and have it actually enforced.

All of this is expressed in OPA/Rego, so policies are code you can version, review, and test:

```rego
package adp.governance

default allow := false

# Read always allowed
allow {
    input.action.type == "read"
}

# Modify requires trust level 3+
allow {
    input.action.type == "modify_file"
    input.session.trust_level >= 3
    not touches_sensitive_paths
}

# Production deploy requires level 5 or approval
requires_approval {
    input.action.type == "deploy"
    input.context.environment == "production"
    input.session.trust_level < 5
}

sensitive_paths := {".env*", "secrets/**", "*.key"}

touches_sensitive_paths {
    some path in input.action.target.paths
    some pattern in sensitive_paths
    glob.match(pattern, [], path)
}
```

That's the actual default policy. Readable, auditable, and enforceable.

## Git Hooks as the Enforcement Point

Policies are only useful if they're enforced. ADP ships git hooks that intercept commits at three points:

- **Pre-commit**: validates staged files against policies, blocks sensitive file commits, requires an active ADP session
- **Post-commit**: links the commit SHA back to the session and decision records
- **Pre-push**: validates push operations against branch protection rules

When an agent tries to commit, the pre-commit hook calls the ADP server, which evaluates the policy engine and returns an approve/deny decision with a reason. If denied, the commit doesn't happen — and the agent gets told why.

Humans get a token-verified bypass so your own commits aren't blocked:

```bash
# One-time setup
./install.sh --configure-bypass

# Your commits go through without ADP validation
ADP_BYPASS_TOKEN=your-token git commit -m "human commit"
```

The bypass still gets logged to the audit trail — ADP always knows who did what.

## How It Connects to Agents

ADP exposes six core MCP tools that agents interact with during a session:

| Tool | What It Does |
|------|-------------|
| `adp_start_session` | Initialize with agent identity and trust level |
| `adp_check_action` | "Can I modify this file?" — policy evaluation |
| `adp_log_decision` | Record what was decided and why |
| `adp_request_approval` | Escalate to a human when trust level is insufficient |
| `adp_get_approval` | Poll for human approval status |
| `adp_prepare_commit` | Register file changes before git commit |

A typical session flow: agent starts a session, checks if it can modify a file, gets approved, does the work, logs the decision with reasoning, prepares the commit, and the git hook validates everything matches up.

## The Architecture

```
AI Agents (Claude Code, Cursor, Copilot)
        ↓ MCP Protocol (stdio or SSE)
    adp-mcp (+ HTTP sidecar for git hooks)
        ↓
    Policy Engine (OPA + Trust + Blast Radius + Time)
        ↓
    Storage (SQLite local / PostgreSQL production)
```

Three binaries:

- **adp-mcp** — the MCP server agents connect to, with an embedded HTTP sidecar for git hooks
- **adp-server** — REST API for the web dashboard and non-MCP integrations
- **adp-cli** — command-line tool for policy management and session inspection

Storage starts with SQLite (zero config, just run it) and scales to PostgreSQL for production multi-tenancy.

## The Documentation Engine

Here's the thing about governance that nobody talks about: the audit trail is only useful if someone reads it. Raw decision logs are great for compliance, but they don't tell you *what happened* in a way a human can scan in 30 seconds.

ADP has a background documentation agent — a goroutine that runs inside the MCP server process, watching for completed sessions. When an agent's session ends, the doc agent picks it up and generates structured reports from the decision records. No manual step, no separate pipeline. Sessions end, docs appear.

It generates three types of reports:

**Session summaries** — the basics: which agent, what trust level, how many decisions, what files were touched, average confidence scores. Think of it as a shift report for your AI teammates.

**Risk reports** — generated only when something looks off. Policy violations, low-confidence decisions (below 0.7), denied actions. These are the reports that should trigger a human review. If a session was clean, you don't get one — no noise.

**Pattern reports** — generated for sessions with five or more decisions. Decision type distribution, file impact scope, session duration vs. trust level. Over time, these reveal whether your agents are operating within expected boundaries or drifting.

The reports are Markdown, generated from Go templates with session analysis baked in:

```go
type SessionAnalysis struct {
    SessionID        string
    Tool             string            // "claude-code", "cursor", etc.
    TrustLevel       int
    Duration         time.Duration
    DecisionCount    int
    DecisionsByType  map[string]int    // "modify_file": 12, "read": 45
    AvgConfidence    float64
    MinConfidence    float64
    FilesTouched     []string
    PolicyViolations int
    DeniedDecisions  int
}
```

By default, template output is the final output — zero token cost, zero external dependencies. But if you set an Anthropic API key (`ADP_DOC_LLM_API_KEY`), the doc agent sends the template draft through Claude to refine it into polished prose. The LLM doesn't invent data — it takes the structured output and makes it read like documentation a human wrote. If the API call fails, it falls back to the template output silently. No disruption.

The vision is bigger than session reports. The documentation engine is the foundation for keeping your entire developer portal's documentation perpetually current. Agents are already doing the work — they know what changed, why it changed, and what the impact was. That knowledge shouldn't die in a log file. It should flow into your runbooks, your architecture docs, your onboarding guides. ADP's doc engine is the first step toward documentation that writes itself from the decisions your team is actually making, human and agent alike.

The output is Markdown compatible with Backstage TechDocs, so it slots directly into your existing developer portal documentation structure.

## Getting Started

```bash
git clone https://github.com/cdameworth/adp.git
cd adp
go build ./cmd/adp-mcp
./adp-mcp
```

That's it for local development. SQLite database gets created automatically at `~/.adp/adp.db`. No PostgreSQL, no Redis, no infrastructure to set up.

For the full stack with Backstage integration:

```bash
docker compose -f docker-compose.sqlite.yml up -d
```

## What's Honest About the Current State

This is alpha. The governance engine, session management, audit trail, git hooks, and documentation engine all work today. The Backstage plugin has a React frontend with visualization components but isn't wired to real data yet. Neo4j is write-only (no query support yet). Test coverage needs work — the governance engine has 0% coverage, which I'm not proud of but I'm being upfront about.

The roadmap is: harden the foundation with tests and CI, then build out decision lineage visualization, then polish the developer experience.

## Why This Matters Now

We're past the point where AI agents are a novelty on engineering teams. They're committing code, modifying infrastructure, making architectural decisions. The question isn't whether to let them — it's how to let them safely.

Right now, most teams either give agents full access and hope for the best, or lock them down so much they're useless. ADP is the middle ground — graduated autonomy with guardrails, audit trails, and human escalation when it matters.

I built this because I want to work with AI agents as partners, not treat them as tools I have to babysit. But partnership requires trust, and trust requires accountability. That's what ADP provides.

## Things I Wish I Knew Before Starting

- **OPA/Rego is powerful but the learning curve is real.** The policy language is declarative, which means thinking about rules differently than imperative code. Worth it for the composability.
- **MCP stdio transport is simpler than SSE** for local development. Start there, add SSE when you need remote connections.
- **Git hooks are the right enforcement point** but they need a bypass mechanism from day one. Developers will revolt if their own commits get blocked by agent governance.
- **SQLite for dev, PostgreSQL for prod** is the right dual-store pattern. Don't make developers run a database just to try your tool.
- **Start with trust levels, add complexity later.** The blast radius and time-based policies came after the core trust model was working. Ship the simple version first.

---

ADP is open source and looking for contributors. If you're thinking about how to govern AI agents on your team — or you've already built something similar and want to compare notes — I'd love to hear from you.

[GitHub: cdameworth/adp](https://github.com/cdameworth/adp)
