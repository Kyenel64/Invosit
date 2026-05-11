# invosit-api

> API server for [Invosit](https://github.com/yourorg/invosit) — git sidecar for files that shouldn't be in git.

Built with Go (stdlib `net/http`), Postgres, Redis, [Ory Kratos](https://www.ory.sh/kratos/),
and pluggable blob storage (R2 / S3 / GCS). Security is the core design
principle — files are encrypted client-side before they ever reach this
server, and identity / sessions are delegated entirely to Kratos.

---

## Requirements

- Go 1.26+
- Docker + Docker Compose
- A blob storage bucket (Cloudflare R2, AWS S3, or GCS)

---

## Getting started

```bash
git clone https://github.com/yourorg/invosit-api
cd invosit-api
cp .env.example .env        # fill in storage credentials and Kratos secrets
docker compose up -d        # start Postgres, Redis, Kratos
go run ./cmd/server         # start API on :8080
```

Compose brings up:

| Service | Host port | Purpose |
|---|---|---|
| `api` | `8080` | This Go server |
| `kratos` | `4433` | Kratos public API (also internal admin on `4434`, not host-exposed). Migrations run automatically on every boot — they're idempotent. |
| `postgres` | `5432` | Hosts both `invosit` and `kratos` databases |
| `redis` | `6379` | Cache / rate limiting |

The MVP runs Kratos as a JSON-API only — no self-service UI container, no
SMTP/courier. The CLI talks to Kratos directly via the native API flow
(`/self-service/registration/api`, `/self-service/login/api`). Email
verification and password recovery are disabled in `kratos.yml` until
there's a courier and a UI to host the flows.

Migrations run automatically on startup. API docs available at
`http://localhost:8080/docs`.

---

## Configuration

All config via environment variables. Copy `.env.example` to `.env`:

```bash
# Server
PORT=8080
ENV=development

# Ory Kratos — set all four secrets per Ory's production guide
# https://www.ory.com/docs/kratos/guides/production
KRATOS_PUBLIC_URL=http://kratos:4433
KRATOS_WEBHOOK_SECRET=           # openssl rand -hex 32

# Kratos itself (consumed by the kratos container)
KRATOS_DEFAULT_SECRET=           # openssl rand -hex 32
KRATOS_COOKIE_SECRET=            # openssl rand -hex 32
KRATOS_CIPHER_SECRET=            # exactly 32 chars

# Postgres (single container, two logical databases: invosit + kratos)
DATABASE_URL=postgres://invosit:secret@localhost:5432/invosit

# Redis
REDIS_URL=redis://localhost:6379

# Storage — pick one provider
STORAGE_PROVIDER=r2
STORAGE_BUCKET=invosit-blobs
STORAGE_ACCESS_KEY=
STORAGE_SECRET_KEY=
STORAGE_ENDPOINT=
STORAGE_REGION=auto

# CORS
CORS_ALLOWED_ORIGINS=http://localhost:3000
```

### Storage providers

**Cloudflare R2** (recommended — zero egress fees):
```bash
STORAGE_PROVIDER=r2
STORAGE_ENDPOINT=https://<accountid>.r2.cloudflarestorage.com
STORAGE_BUCKET=invosit-blobs
STORAGE_ACCESS_KEY=xxx
STORAGE_SECRET_KEY=xxx
STORAGE_REGION=auto
```

**AWS S3:**
```bash
STORAGE_PROVIDER=s3
STORAGE_REGION=us-east-1
STORAGE_BUCKET=invosit-blobs
STORAGE_ACCESS_KEY=xxx
STORAGE_SECRET_KEY=xxx
```

**Google Cloud Storage:**
```bash
STORAGE_PROVIDER=gcs
STORAGE_BUCKET=invosit-blobs
GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json
```

---

## API

Base URL: `/api/v1`

Registration, login, and logout are served by Ory Kratos directly on
`:4433` (native JSON API; the CLI hits Kratos straight, no UI container).
Password recovery and email verification are disabled in MVP. The API only
exposes `/auth/me` plus an internal Kratos webhook.

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/auth/me` | ✓ | Current user (resolved from Kratos session) |
| GET | `/workspaces` | ✓ | List workspaces |
| POST | `/workspaces` | ✓ | Create workspace |
| GET | `/workspaces/:id` | ✓ member | Get workspace |
| DELETE | `/workspaces/:id` | ✓ admin | Delete workspace |
| GET | `/workspaces/:id/members` | ✓ member | List members |
| POST | `/workspaces/:id/members` | ✓ admin | Invite member |
| DELETE | `/workspaces/:id/members/:userId` | ✓ admin | Remove + revoke member |
| GET | `/workspaces/:id/files` | ✓ member | List files |
| POST | `/workspaces/:id/files` | ✓ developer | Push file |
| GET | `/workspaces/:id/files/:fileId` | ✓ + DEK | Get signed download URL |
| DELETE | `/workspaces/:id/files/:fileId` | ✓ admin | Delete file |
| GET | `/workspaces/:id/files/:fileId/versions` | ✓ + DEK | List versions |
| POST | `/workspaces/:id/files/:fileId/rollback` | ✓ admin | Rollback version |
| GET | `/workspaces/:id/access` | ✓ admin | List access grants |
| POST | `/workspaces/:id/access` | ✓ admin | Grant file access |
| DELETE | `/workspaces/:id/access/:grantId` | ✓ admin | Revoke access |
| GET | `/workspaces/:id/audit` | ✓ admin | Audit log |

Full schemas: [`docs/openapi.yaml`](docs/openapi.yaml)

---

## Security model

### The server never sees plaintext

Files are encrypted client-side by the CLI before upload. This server only
ever handles encrypted blobs and opaque wrapped DEKs. Even a full server
compromise does not expose file contents.

### Access control is cryptographic

Every file has a `wrapped_dek` row per authorised user — a DEK encrypted
with that user's public key. No row means no access, enforced at the crypto
layer, not just the permission check layer. Revoking a user deletes their
wrapped DEK rows. Their next pull returns 403 instantly. No re-encryption needed.

### Authentication

Identity, registration, login, password hashing (Argon2id), session
lifecycle, recovery, and email verification are all delegated to **Ory
Kratos**. The API validates incoming requests by calling Kratos
`/sessions/whoami` — there is no JWT or refresh token rotation in this
codebase. When Kratos creates a new identity, it fires an after-registration
webhook that creates the corresponding `users` row locally (gated by a
shared-secret header).

### Rate limiting

- API endpoints: 300 requests/minute per IP
- Login / registration brute force is rate-limited by Kratos itself
  (configured in `kratos/kratos.yml`)

### Signed storage URLs

Download URLs expire in 15 minutes and are only issued after auth and
wrapped DEK verification pass. The server never proxies file content.

### Security headers

Every response includes:
```
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Strict-Transport-Security: max-age=31536000; includeSubDomains
Content-Security-Policy: default-src 'none'
Referrer-Policy: no-referrer
```

---

## Project structure

```
invosit-api/
├── cmd/server/          # entry point, route registration
├── internal/
│   ├── kratos/          # thin Kratos client (whoami)
│   ├── workspace/       # workspaces, members
│   ├── files/           # file metadata, versions
│   ├── access/          # DEK wrapping, grants
│   ├── audit/           # audit log
│   ├── storage/         # storage interface + R2/S3/GCS implementations
│   ├── middleware/      # Kratos session, rate limiting, security headers, logging
│   ├── httpx/           # JSON bind/respond helpers, request ctx
│   └── handler/         # net/http handlers, one file per resource group
├── kratos/              # Kratos config (kratos.yml, identity schema, hooks)
├── db/init/             # Postgres init scripts (CREATE DATABASE kratos)
├── migrations/          # SQL migration files
└── docs/
    └── openapi.yaml
```

---

## Deployment

The API runs as a Docker container behind Nginx Proxy Manager on a VPS.

```bash
# On your VPS
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d
```

Add a proxy host in Nginx Proxy Manager pointing at port `8080`.

### Calibrate Argon2 on the target host

The Argon2 parameters in `kratos/kratos.yml` are MVP starter values, not
calibrated for the target hardware. Per [Ory's guidance](https://www.ory.com/docs/kratos/guides/setting-up-password-hashing-parameters),
run on the production VPS and replace the values:

```bash
docker compose run --rm kratos hashers argon2 calibrate 1s
```

Aim for ~0.5–1s per hash. Too fast weakens the hash; too slow opens DoS on
login. Re-run this whenever the VPS's hardware changes.

---

## Related repos

- [`invosit-cli`](https://github.com/yourorg/invosit-cli) — CLI tool (handles encryption)
- [`invosit-web`](https://github.com/yourorg/invosit-web) — web dashboard

---

## License

MIT
