# ADP → TechDocs external publisher

Publishes ADP's auto-generated documentation (session summaries, risk reports,
pattern reports) as **first-class Backstage TechDocs sites**, one per entity, so
each ADP-managed service gets a "Docs" tab rendered by the standard TechDocs
reader. This is model **(B)** from the plugins README — it surfaces ADP docs
standalone, complementing the in-reader TechDocs Addon (model A).

## Pipeline

```
adp-mcp (doc engine) ──writes──► DocStore ──read──► adp-server  GET /v1/docs
                                                          │
                              adp-techdocs (this tool) ───┘  enumerates services,
                                 maps docs→entity via session service_scope,
                                 writes MkDocs source per entity
                                                          │
                              techdocs-cli generate ──► techdocs-cli publish
                                                          │
                              TechDocs storage (S3/GCS/…) keyed by namespace/kind/name
                                                          │
                              Backstage TechDocs (builder: external) serves the Docs tab
```

ADP docs are session-scoped; this tool groups them onto service entities using
each session's `service_scope`. The entity key is
`<namespace>/<kind>/<SanitizeName(service name)>`, matching the entity the
`AdpEntityProvider` creates.

## Prerequisites

- **adp-server reachable with `/v1/docs`.** Docs are persisted only in SQLite
  mode (PostgreSQL has no doc store → `/v1/docs` returns 503). The doc engine
  runs inside **adp-mcp**, so adp-server must share the same database (the
  `~/.adp/adp.db` file, or the same Postgres once a PG doc store exists).
- ADP services registered (so there are entities to attach docs to) and at least
  one ended session that produced docs.
- [`techdocs-cli`](https://backstage.io/docs/features/techdocs/cli) installed
  (`npm i -g @techdocs/cli`) plus a MkDocs generator (local `mkdocs` +
  `mkdocs-techdocs-core`, or Docker).
- Cloud credentials for your publisher (e.g. AWS creds for `awsS3`).

## Steps

```bash
# 1. Build the exporter
go build -o adp-techdocs ./cmd/adp-techdocs

# 2. Export MkDocs source trees from ADP (one dir per entity)
ADP_URL=http://localhost:8080 ADP_API_KEY=$ADP_API_KEY \
  ./adp-techdocs --out ./techdocs-out --publisher awsS3 --bucket my-techdocs-bucket
#   -> writes ./techdocs-out/default/Component/<name>/{mkdocs.yml,docs/...}
#   -> prints the techdocs-cli generate/publish commands

# 3. Build + publish each site to your TechDocs storage
SRC_DIR=./techdocs-out PUBLISHER=awsS3 BUCKET=my-techdocs-bucket \
  ./integrations/techdocs/publish.sh
```

## Backstage configuration

Run TechDocs as a read-only server of externally-built sites:

```yaml
# app-config.yaml
techdocs:
  builder: 'external'
  publisher:
    type: 'awsS3'
    awsS3:
      bucketName: 'my-techdocs-bucket'
      region: 'us-east-1'
```

Give ADP-managed entities a Docs tab by setting the entity-provider option added
to the ADP backend plugin (writes `backstage.io/techdocs-ref` onto each ADP
entity):

```yaml
adp:
  baseUrl: ${ADP_BASE_URL}
  entityProvider:
    techdocsRef: 'dir:.'   # presence enables the Docs tab; content comes from storage
```

## Scheduling

Run steps 2–3 on a schedule (cron/CI, e.g. hourly) so docs refresh as sessions
end and the doc engine generates new records. The export is idempotent — it
overwrites each entity's source tree from the current DocStore contents.

## Caveats

- **PostgreSQL mode** has no doc store today, so `/v1/docs` is `503` there and
  this tool finds nothing — run against an adp-server in SQLite mode that shares
  the doc engine's database.
- **Entity-ref casing** matters: `techdocs-cli publish --entity` is
  case-sensitive and must match how your catalog references the entity. If your
  TechDocs serves lowercased refs, run `adp-techdocs --kind component` so the
  published triplet matches.
- A service with no docs is skipped (no empty sites are published).
- This is model **(B)**. For services that already have repo-based TechDocs, the
  in-reader Addon (model A, `AdpGovernanceDocsAddon`) is the lighter option; the
  two can coexist.
