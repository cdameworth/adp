# Railway Deployment Guide

Deploy the ADP API server and dashboard to Railway with PostgreSQL.

## Architecture

```
[Railway PostgreSQL] <-- sslmode=require --> [adp-server (API)] <-- CORS --> [adp-dashboard (Next.js)]
```

Both services are deployed as separate Railway services from the same repository.

## Prerequisites

- Railway account with a project created
- Railway CLI installed (`npm i -g @railway/cli`)

## Step 1: Create Services

1. Create a new Railway project
2. Add a **PostgreSQL** database from the Railway dashboard
3. Create two services:
   - `api` — from `adp/Dockerfile`
   - `dashboard` — from `adp/web/Dockerfile`

## Step 2: Configure API Service

Set these environment variables on the `api` service:

### Required

| Variable | Value | Notes |
|----------|-------|-------|
| `ADP_STORE` | `postgres` | Use PostgreSQL backend |
| `ADP_ENVIRONMENT` | `production` | Enables auth validation |
| `ADP_API_KEY` | `<generate>` | `openssl rand -hex 32` |
| `ADP_DATABASE_POSTGRES_HOST` | `<from Railway PG>` | Use Railway's `PGHOST` reference |
| `ADP_DATABASE_POSTGRES_PORT` | `5432` | Default PostgreSQL port |
| `ADP_DATABASE_POSTGRES_DATABASE` | `railway` | Railway's default DB name |
| `ADP_DATABASE_POSTGRES_USERNAME` | `postgres` | Railway's default user |
| `ADP_DATABASE_POSTGRES_PASSWORD` | `<from Railway PG>` | Use Railway's `PGPASSWORD` reference |
| `ADP_DATABASE_POSTGRES_SSLMODE` | `require` | Public proxy only — see Troubleshooting |
| `ADP_CORS_ALLOWED_ORIGINS` | `https://<dashboard>.railway.app` | Dashboard URL |

A single `ADP_DATABASE_POSTGRES_DATABASE_URL` (Railway's `DATABASE_URL`
reference) works instead of the discrete host/port/name/user/password
variables and takes precedence when both are set.

### Local Auth (Recommended)

| Variable | Value | Notes |
|----------|-------|-------|
| `ADP_JWT_SECRET` | `<generate>` | `openssl rand -hex 32` — enables built-in user auth |
| `ADP_OPEN_REGISTRATION` | `false` | Set `true` to allow public signup |

With `ADP_JWT_SECRET` set, the first `POST /v1/auth/register` creates the admin account. The dashboard uses these credentials to authenticate via NextAuth.js.

### Optional

| Variable | Default | Notes |
|----------|---------|-------|
| `ADP_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `ADP_LOG_FORMAT` | `json` | `json` or `text` |
| `ADP_AUTH_JWKS_URL` | (none) | Set to enable external JWT auth alongside API key |
| `ADP_AUTH_ISSUER` | (none) | Required when JWKS_URL is set |

Railway automatically injects `PORT` which the server respects.

## Step 3: Configure Dashboard Service

Set these environment variables on the `dashboard` service:

| Variable | Value | Notes |
|----------|-------|-------|
| `NEXT_PUBLIC_ADP_API_URL` | `https://<api>.railway.app` | **Build-time** — must be set before deploy |
| `ADP_API_URL` | `https://<api>.railway.app` | Runtime server-side proxy |
| `NEXTAUTH_SECRET` | `<generate>` | `openssl rand -hex 32` — session encryption for NextAuth |
| `NEXTAUTH_URL` | `https://<dashboard>.railway.app` | Dashboard's public URL |

**Note**: `NEXT_PUBLIC_ADP_API_URL` is baked into the Next.js build. After changing it, trigger a redeploy.

## Step 4: Deploy

```bash
# From the adp/ directory
railway up
```

Or push to a connected GitHub repository for automatic deployments.

## Security Checklist

- [x] `ADP_API_KEY` set to a strong random value
- [x] `ADP_JWT_SECRET` set to a strong random value (enables user auth)
- [x] `NEXTAUTH_SECRET` set on dashboard service
- [x] `ADP_DATABASE_POSTGRES_SSLMODE` correct for your endpoint (see below)
- [x] `ADP_ENVIRONMENT=production`
- [x] `ADP_CORS_ALLOWED_ORIGINS` restricted to dashboard domain
- [x] First `POST /v1/auth/register` creates the admin account
- [ ] Consider enabling external JWT auth via `ADP_AUTH_JWKS_URL` for enterprise SSO

## Health Checks

- `GET /health` — always returns `{"status":"ok"}` (used by Railway)
- `GET /ready` — checks database connectivity, returns 503 if unhealthy

## Troubleshooting

### "1/1 replicas never became healthy!"

The container starts but Railway's HTTP check against `/health` never
succeeds. In order of likelihood:

1. **Startup took longer than the healthcheck window.** PG connect +
   migrations on a cold network can exceed a short `healthcheckTimeout`.
   `railway.toml` sets 180s — if your project overrides it, raise it.
2. **PostgreSQL unreachable or misconfigured.** The server retries PG for 90s
   and logs the non-secret target (`host`, `port`, `db`, `sslmode`) on every
   attempt. Open the deploy logs and read the `Connecting to PostgreSQL`
   line — 90% of these incidents are a wrong host reference or SSL mode:
   - **`postgres.railway.internal` (private network): no TLS.** Use
     `ADP_DATABASE_POSTGRES_SSLMODE=disable` there.
   - **`*.proxy.rlwy.net` (public TCP proxy): TLS required.** Use
     `ADP_DATABASE_POSTGRES_SSLMODE=require` there.
   - Mixing those up produces connection failures that look like a dead app.
3. **No PG variables at all.** The server exits with an explicit
   "No PostgreSQL configuration found" message naming the expected variables.
   For a zero-dependency smoke deploy, set `ADP_STORE=sqlite` instead (data
   lives on the container's ephemeral disk unless you attach a volume).
4. **Crash before bind.** Any `Failed to initialize ...` / `Failed to run
   PostgreSQL migrations` line in the deploy log names the failing stage;
   migration duration is logged so a slow migration is visible.

### Other issues

**Build fails with architecture error**: Ensure `TARGETARCH` is not overridden. The Dockerfile defaults to `amd64` which matches Railway's infrastructure.

**Database connection refused**: Check that PostgreSQL credentials match Railway's provided values. Use Railway's variable references (e.g., `${{Postgres.PGHOST}}`).

**CORS errors in dashboard**: Verify `ADP_CORS_ALLOWED_ORIGINS` includes the exact dashboard URL with `https://` prefix.

**401 Unauthorized**: Include `X-API-Key: <your-key>` header or `Authorization: Bearer <your-key>` in API requests.
