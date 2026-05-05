# CLAUDE.md — invosit-api

This file gives Claude context about the invosit-api repository so it can
assist effectively without needing to be re-explained from scratch each session.

---

## What is Invosit?

Invosit is a git sidecar tool for files that shouldn't be in git.

It lets teams push and pull encrypted files alongside a repo without committing
them. A small manifest file (`.invosit.yaml`) is committed to git, tracking
what files belong to the repo. The actual files live in encrypted blob storage
and are pulled down by teammates via the CLI.

Think of it as "Infisical but for arbitrary files" — the same auth model,
workspace concept, and CLI pattern, but for files instead of env vars.

**Tagline:** Git sidecar for files that shouldn't be in git.

**Security is the core value proposition.** Every design decision should be
evaluated through a security lens first. When in doubt, choose the more
secure option even if it adds complexity.

---

## This repo

`invosit-api` is the Go API server for Invosit. It handles:

- Authentication (register, login, JWT issuance, refresh token rotation)
- Workspace and member management
- File metadata, versioning, and access control
- Issuing short-lived signed storage URLs for encrypted blob upload/download
- Audit logging of every sensitive action

It does **not** handle encryption — that happens client-side in the CLI.
The API never sees plaintext file content. Ever.

---

## Tech stack

| Layer | Technology |
|---|---|
| Language | Go 1.22+ |
| HTTP framework | Gin |
| Database | Postgres |
| Cache / rate limiting | Redis |
| Blob storage | Pluggable — R2 (default), S3, GCS |
| Containerisation | Docker Compose |
| Reverse proxy | Nginx Proxy Manager (external, already running on VPS) |

---

## Repo structure

```
invosit-api/
├── main.go
├── cmd/
│   └── server/
│       └── main.go          # startup, config loading, route registration
├── internal/
│   ├── auth/                # register, login, JWT, refresh token rotation
│   ├── workspace/           # workspace + member management
│   ├── files/               # file metadata, versions, manifest
│   ├── access/              # DEK wrapping, permission checks, grants
│   ├── audit/               # audit log writes and queries
│   ├── storage/             # storage interface + provider implementations
│   │   ├── storage.go       # interface definition only
│   │   ├── s3.go            # AWS S3 + Cloudflare R2 (S3-compatible, diff endpoint)
│   │   └── gcs.go           # Google Cloud Storage
│   ├── middleware/          # security middleware (see middleware section)
│   ├── handler/             # HTTP handlers, one file per resource group
│   │   ├── auth.go
│   │   ├── workspace.go
│   │   ├── members.go
│   │   ├── files.go
│   │   ├── access.go
│   │   └── audit.go
│   └── db/                  # Postgres connection, query helpers
├── migrations/              # numbered SQL files (001_init.sql, etc.)
├── docs/
│   └── openapi.yaml         # OpenAPI 3.0 spec — lives here, not in frontend
├── .env.example
├── docker-compose.yml
├── docker-compose.prod.yml
├── Dockerfile
├── CLAUDE.md
└── README.md
```

---

## Gin setup and routing

Use `gin.New()` not `gin.Default()` — we define our own middleware explicitly
so we control exactly what gets logged and recovered:

```go
r := gin.New()

// Global middleware
r.Use(middleware.Recovery())
r.Use(middleware.Logger())
r.Use(middleware.SecurityHeaders())
r.Use(middleware.RateLimiter(redisClient))
r.Use(middleware.RequestSizeLimit(10 << 20)) // 10MB max

// Public routes — no auth
public := r.Group("/api/v1")
public.POST("/auth/register", h.auth.Register)
public.POST("/auth/login", h.auth.Login)
public.POST("/auth/refresh", h.auth.Refresh)

// Authenticated routes
authed := r.Group("/api/v1")
authed.Use(middleware.JWT(jwtSecret))
authed.POST("/auth/logout", h.auth.Logout)
authed.GET("/auth/me", h.auth.Me)
authed.GET("/workspaces", h.workspace.List)
authed.POST("/workspaces", h.workspace.Create)

// Workspace-scoped — also checks workspace membership
ws := authed.Group("/workspaces/:workspaceId")
ws.Use(middleware.WorkspaceMember(db))
ws.GET("", h.workspace.Get)
ws.DELETE("", h.workspace.Delete)
ws.GET("/members", h.members.List)
// ... etc
```

Handlers receive dependencies via a struct — no global state:

```go
type Handler struct {
    db      *sql.DB
    storage storage.Storage
    redis   *redis.Client
}
```

---

## Database schema

```sql
workspaces
  id, name, created_by, created_at

users
  id, email, password_hash, public_key, created_at

workspace_members
  workspace_id, user_id, role, joined_at
  role: admin | developer | viewer

environments
  id, workspace_id, name, created_at

files
  id, workspace_id, environment_id, path, content_hash,
  size, pushed_by, pushed_at

file_versions
  id, file_id, blob_key, content_hash, size,
  pushed_by, pushed_at, message, is_current

wrapped_deks
  file_id, user_id, encrypted_dek
  -- one row per user per file
  -- no row = no access, cryptographically enforced
  -- DELETE to revoke instantly, no re-encryption needed

access_grants
  id, user_id, workspace_id, environment_id,
  path_pattern, granted_by, granted_at

audit_logs
  id, user_id, workspace_id, action, file_id, ip, timestamp
  action: push | pull | login | logout | grant | revoke | rollback | delete

refresh_tokens
  id, user_id, token_hash, expires_at, used_at, created_at
  -- token hashed with SHA-256 before storage, never stored plaintext
  -- reuse detection: if used_at is set, invalidate ALL user tokens immediately
```

---

## API routes

Base path: `/api/v1`

```
POST   /auth/register
POST   /auth/login
POST   /auth/logout
POST   /auth/refresh
GET    /auth/me

GET    /workspaces
POST   /workspaces
GET    /workspaces/:workspaceId
DELETE /workspaces/:workspaceId

GET    /workspaces/:workspaceId/members
POST   /workspaces/:workspaceId/members
DELETE /workspaces/:workspaceId/members/:userId

GET    /workspaces/:workspaceId/files
POST   /workspaces/:workspaceId/files
GET    /workspaces/:workspaceId/files/:fileId
DELETE /workspaces/:workspaceId/files/:fileId
GET    /workspaces/:workspaceId/files/:fileId/versions
POST   /workspaces/:workspaceId/files/:fileId/rollback

GET    /workspaces/:workspaceId/access
POST   /workspaces/:workspaceId/access
DELETE /workspaces/:workspaceId/access/:grantId

GET    /workspaces/:workspaceId/audit
```

Full request/response schemas in `docs/openapi.yaml`.

---

## Security architecture

This is a security-critical application. The following rules are non-negotiable.

### Encryption boundary

The API is outside the encryption boundary. It:
- Receives already-encrypted blob keys on push — never raw file content
- Stores wrapped DEKs (opaque to the server) in Postgres
- Returns the caller's wrapped DEK + a signed storage URL on pull
- Never logs, caches, or inspects wrapped DEK values

The CLI handles all AES-256-GCM encryption/decryption. If the server is
compromised, files remain encrypted and unreadable.

### Authentication

- Passwords hashed with **bcrypt**, cost factor 12 minimum
- JWT access tokens: **HS256**, 1 hour expiry, signed with `JWT_SECRET`
- Refresh tokens: cryptographically random 32 bytes, **hashed with SHA-256**
  before storage — raw token only exists in memory and is returned once
- Refresh token rotation on every use — old token invalidated, new token issued
- **Token reuse detection**: if a used refresh token is presented again,
  treat as token theft — immediately invalidate ALL tokens for that user
- JWT middleware extracts user ID from token and attaches to Gin context —
  never trust user ID from request body or query params

### Authorization — three layers

Every file access request must pass all three:

1. **JWT middleware** — is the user authenticated?
2. **Workspace membership middleware** — does the user belong to this workspace?
3. **Wrapped DEK check** — does a `wrapped_deks` row exist for this user + file?
   No row = 403 regardless of role or workspace membership.

Access is cryptographically enforced, not just a permission check. A member
with no wrapped DEK cannot decrypt the file even if they bypass the API.

### Rate limiting

Redis-backed, per IP:

- Auth endpoints (`/auth/login`, `/auth/register`): **10 req/min** — brute force
- File push: **60 req/min**
- All other endpoints: **300 req/min**
- Return `429` with `Retry-After` header always

### Input validation

Gin binding tags on all request structs:

```go
type LoginRequest struct {
    Email    string `json:"email" binding:"required,email,max=255"`
    Password string `json:"password" binding:"required,min=8,max=128"`
}
```

Additional validation rules:
- File paths: reject `..` traversal, absolute paths, null bytes, path separators
  at start
- Content hashes: must be valid SHA-256 hex (64 chars, hex only)
- File sizes: enforce workspace storage quota before accepting push
- Blob keys: validate format before storing or issuing signed URLs
- Never pass raw user input to SQL — parameterised queries only

### Security headers

Applied to every response via middleware:

```
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Strict-Transport-Security: max-age=31536000; includeSubDomains
Content-Security-Policy: default-src 'none'
Referrer-Policy: no-referrer
X-Request-ID: <uuid>           ← for log correlation, safe to expose
```

### Signed storage URLs

- Max expiry: **15 minutes**
- Generated server-side only after auth + access check passes
- Scoped to the specific blob key — no wildcard URLs
- Never issue a signed URL without first confirming wrapped DEK exists for caller

### What must never be logged

- Request bodies (may contain wrapped DEKs)
- JWT tokens or refresh tokens
- `encrypted_dek` column values
- Passwords or password hashes
- Storage credentials or signed URLs

Safe to log: method, path, status code, duration, user ID (not email),
workspace ID, IP address, request ID.

### SQL injection

Parameterised queries only. No exceptions:

```go
// NEVER
db.Query("SELECT * FROM files WHERE path = '" + userInput + "'")

// ALWAYS
db.Query("SELECT * FROM files WHERE path = $1", userInput)
```

### Information leakage

- Return **403 not 404** when a user lacks access to an existing resource —
  don't reveal whether a resource exists to unauthorised users
- Exception: workspace routes where membership is already confirmed by middleware
- Generic error messages to clients — full errors logged server-side only
- Don't include stack traces, DB errors, or internal paths in responses

### CORS

Production: restrict to known origins via `CORS_ALLOWED_ORIGINS` env var.
Development: allow localhost. Never use wildcard `*` in production.

---

## Middleware stack (in order)

1. **Recovery** — catch panics, return 500, log stack trace server-side only
2. **Logger** — log method, path, status, duration, userID, IP — never bodies
3. **SecurityHeaders** — set all security response headers
4. **RateLimiter** — Redis per-IP, stricter on auth routes
5. **RequestSizeLimit** — reject bodies over 10MB before they're read
6. **JWT** — validate Bearer token, attach `userID` to context (auth routes exempt)
7. **WorkspaceMember** — verify membership in `:workspaceId` (workspace routes only)

---

## Storage interface

All blob storage goes through a single interface. Never import a provider
directly outside of `internal/storage/`:

```go
type Storage interface {
    Put(ctx context.Context, key string, body io.Reader, size int64) error
    Get(ctx context.Context, key string) (io.ReadCloser, error)
    Delete(ctx context.Context, key string) error
    SignedURL(ctx context.Context, key string, expiry time.Duration) (string, error)
}
```

Provider selected at startup via `STORAGE_PROVIDER`:

| Value | Provider |
|---|---|
| `r2` | Cloudflare R2 (default, zero egress fees) |
| `s3` | AWS S3 |
| `gcs` | Google Cloud Storage |

R2 and S3 share one implementation — R2 is S3-compatible, different endpoint only.

---

## Environment variables

```bash
# Server
PORT=8080
ENV=production                  # production | development
JWT_SECRET=                     # minimum 32 random bytes
                                # generate: openssl rand -hex 32

# Postgres
DATABASE_URL=postgres://invosit:secret@postgres:5432/invosit

# Redis
REDIS_URL=redis://redis:6379

# Storage
STORAGE_PROVIDER=r2             # r2 | s3 | gcs
STORAGE_BUCKET=invosit-blobs
STORAGE_ENDPOINT=               # R2 or custom S3 endpoint, blank for AWS
STORAGE_ACCESS_KEY=
STORAGE_SECRET_KEY=
STORAGE_REGION=auto             # AWS region or "auto" for R2

# CORS
CORS_ALLOWED_ORIGINS=https://app.invosit.dev
```

---

## Environments (Invosit concept)

Every workspace has multiple environments (development, staging, production).
Files and access grants are scoped per environment. Fully isolated —
different DEKs, different access grants, no bleed between dev and prod.

---

## Error responses

Consistent JSON, safe messages only — never expose internal errors:

```json
{
  "error": "resource not found",
  "code": "NOT_FOUND"
}
```

Map internal errors to safe messages before responding. Log full internal
error server-side with request ID for tracing.

---

## Conventions

- `gin.New()` not `gin.Default()` — explicit middleware only
- No global state — all dependencies injected via handler struct
- `database/sql` directly — no ORM, parameterised queries only
- Migrations: numbered SQL files (`001_init.sql`, `002_add_environments.sql`)
- Timestamps: UTC always
- IDs: prefixed strings (`ws_`, `usr_`, `file_`, `ver_`, `grant_`, `log_`)
  generated with a small random helper — not UUIDs, not sequential integers
- Return 403 not 404 for unauthorised access to existing resources
- Validate all input at handler level before DB interaction

---

## Running locally

```bash
cp .env.example .env
docker compose up -d         # starts Postgres + Redis
go run ./cmd/server          # starts API on :8080
```

Migrations run automatically on startup.
