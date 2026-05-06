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
| Language | Go 1.26+ |
| HTTP framework | Go stdlib `net/http` (no third-party router) |
| Validation | `go-playground/validator/v10` (struct tags) |
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
│   ├── middleware/          # net/http middleware (see middleware section)
│   ├── httpx/               # JSON bind/respond helpers, request ctx accessors
│   ├── ids/                 # prefixed random ID generator
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

## Routing and middleware

We use stdlib `net/http` only — `http.ServeMux` with the method+path pattern
syntax added in Go 1.22. No third-party router. Path params come from
`r.PathValue("name")`.

Middleware is the standard `func(http.Handler) http.Handler` shape and
composed via `middleware.Chain`. Auth-scoped and workspace-scoped routes are
expressed by wrapping subsets of routes with extra middleware before they
land on the mux — there is no Gin-style "router group", and we don't need one.

```go
mux := http.NewServeMux()

// Public — no auth
mux.HandleFunc("POST /api/v1/auth/register", h.auth.Register)
mux.HandleFunc("POST /api/v1/auth/login",    h.auth.Login)
mux.HandleFunc("POST /api/v1/auth/refresh",  h.auth.Refresh)

// Authenticated — wrap individual handlers with the JWT middleware
authed := middleware.JWT(jwtSecret)
mux.Handle("POST /api/v1/auth/logout",  authed(http.HandlerFunc(h.auth.Logout)))
mux.Handle("GET  /api/v1/auth/me",      authed(http.HandlerFunc(h.auth.Me)))
mux.Handle("GET  /api/v1/workspaces",   authed(http.HandlerFunc(h.workspace.List)))
mux.Handle("POST /api/v1/workspaces",   authed(http.HandlerFunc(h.workspace.Create)))

// Workspace-scoped — JWT + workspace membership
wsmw := middleware.Compose(authed, middleware.WorkspaceMember(db))
mux.Handle("GET    /api/v1/workspaces/{workspaceId}",         wsmw(http.HandlerFunc(h.workspace.Get)))
mux.Handle("DELETE /api/v1/workspaces/{workspaceId}",         wsmw(http.HandlerFunc(h.workspace.Delete)))
mux.Handle("GET    /api/v1/workspaces/{workspaceId}/members", wsmw(http.HandlerFunc(h.members.List)))
// ... etc

// Global middleware — outermost first
chain := middleware.Chain(
    middleware.Recovery,
    middleware.Logger,
    middleware.BodyLimit(10 << 20),
)

srv := &http.Server{
    Addr:              ":" + port,
    Handler:           chain(mux),
    ReadHeaderTimeout: 10 * time.Second,
    ReadTimeout:       30 * time.Second,
    WriteTimeout:      30 * time.Second,
    IdleTimeout:       60 * time.Second,
}
```

Handlers receive dependencies via a struct — no global state:

```go
type Handler struct {
    db      *sql.DB
    storage storage.Storage
    redis   *redis.Client
}
```

Handler funcs use the standard signature:

```go
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) { ... }
```

For per-request state (request ID, user ID), use `r.Context()` plus the
typed accessors in `internal/httpx`. Never store request-scoped data in
package-level variables.

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
- JWT middleware extracts user ID from token and attaches it to the request
  context via `httpx.WithUserID` — never trust user ID from request body
  or query params

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

`go-playground/validator/v10` struct tags on all request structs. Decode +
validate via `httpx.Bind`, which uses `json.DisallowUnknownFields` and
returns an error on either malformed JSON or failed validation:

```go
type LoginRequest struct {
    Email    string `json:"email"    validate:"required,email,max=255"`
    Password string `json:"password" validate:"required,min=8,max=128"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
    var req LoginRequest
    if err := httpx.Bind(r, &req); err != nil {
        httpx.RespondError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid email or password")
        return
    }
    // ...
}
```

Never echo validator error text back to the client — map to a safe message.

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

All middleware is `func(http.Handler) http.Handler`. Outermost runs first;
`middleware.Chain(A, B, C)(h)` produces `A(B(C(h)))`.

Global (wraps the entire mux):
1. **Recovery** — catch panics, return 500, log stack trace server-side only
2. **Logger** — log method, path, status, duration, userID, IP — never bodies; sets `X-Request-ID`
3. **SecurityHeaders** — set all security response headers
4. **RateLimiter** — Redis per-IP, stricter on auth routes
5. **BodyLimit** — `http.MaxBytesReader` at 10MB

Per-route (wraps specific handlers):
6. **JWT** — validate Bearer token, attach `userID` to request context (auth routes only)
7. **WorkspaceMember** — verify membership for `{workspaceId}` (workspace routes only)

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

- Stdlib `net/http` only — no third-party router or web framework
- `http.ServeMux` 1.22+ patterns: `"POST /api/v1/auth/register"`, `"GET /workspaces/{id}"`
- All middleware is `func(http.Handler) http.Handler`; compose with `middleware.Chain`
- Decode + validate request bodies via `httpx.Bind` (rejects unknown JSON fields)
- Read user ID / request ID from `r.Context()` via `httpx` accessors — never globals
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
