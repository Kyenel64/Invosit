# CLAUDE.md — Invosit

This file gives Claude context about the Invosit repository so it can assist
effectively without needing to be re-explained from scratch each session.

---

## What is Invosit?

Invosit syncs encrypted files alongside a git repo — for files that shouldn't be committed.

It lets teams push and pull encrypted files alongside a repo without committing
them. A small manifest file (`.invosit.yaml`) is committed to git, tracking
what files belong to the repo. The actual files live in encrypted blob storage
and are pulled down by teammates via the CLI.

Think of it as "Infisical but for arbitrary files" — the same auth model,
workspace concept, and CLI pattern, but for files instead of env vars.

**Tagline:** Encrypted file sync for files that shouldn't be in git.

**Security is the core value proposition.** Every design decision should be
evaluated through a security lens first. When in doubt, choose the more
secure option even if it adds complexity.

---

## Project layout

This repo (`github.com/kyenel64/Invosit`) holds both halves of the product:

| Component | Location | Status |
|---|---|---|
| **API server** | `cmd/server/`, `internal/`, `migrations/`, `kratos/`, `db/init/` | shipping — most of this file describes it |
| **CLI** | `cli/` | not yet built — see "The CLI" section below for intent |
| **Documentation** | `docs/` | shared between API and CLI |

The API currently lives at the repo root. The CLI will land under `cli/` when
work starts on it. If we later restructure to symmetrise (`api/` + `cli/`),
this section gets updated and the API directories move accordingly.

---

## The API server

The Go API server handles:

- Validating Kratos sessions and resolving local user records
- Workspace and member management
- File metadata, versioning, and access control
- Issuing short-lived signed storage URLs for encrypted blob upload/download
- Audit logging of every sensitive action

Authentication itself (registration, login, password hashing, sessions,
recovery, verification) is delegated to **Ory Kratos**, which runs as a
separate container in the same `docker compose`. The API trusts Kratos
session tokens validated via `/sessions/whoami`.

It does **not** handle encryption — that happens client-side in the CLI.
The API never sees plaintext file content. Ever.

### Tech stack

| Layer | Technology |
|---|---|
| Language | Go 1.26+ |
| HTTP framework | Go stdlib `net/http` (no third-party router) |
| Validation | `go-playground/validator/v10` (struct tags) |
| Identity / auth | Ory Kratos (self-hosted, separate container) |
| Database | Postgres |
| Cache / rate limiting | Redis |
| Blob storage | Pluggable — R2 (default), S3, GCS (planned) |
| Containerisation | Docker Compose |
| Reverse proxy | Nginx Proxy Manager (external, already running on VPS) |

### Directory structure (API)

```
Invosit/
├── cmd/
│   └── server/
│       └── main.go          # startup, config loading, route registration
├── internal/
│   ├── kratos/              # thin Kratos client (whoami)
│   ├── workspace/           # workspace + member management
│   ├── files/               # file metadata, versions, manifest
│   ├── access/              # DEK wrapping, permission checks, grants
│   ├── audit/               # audit log writes and queries
│   ├── storage/             # storage interface + provider implementations
│   │   ├── storage.go       # Storage interface, Config, New factory, errors, expiry validator
│   │   └── s3.go            # AWS S3 + Cloudflare R2 (S3-compatible, diff endpoint)
│   │                        # gcs.go is planned; not yet implemented
│   ├── middleware/          # net/http middleware (see middleware section)
│   ├── httpx/               # JSON bind/respond helpers, request ctx accessors
│   ├── ids/                 # prefixed random ID generator
│   ├── handler/             # HTTP handlers, one file per resource group
│   │   ├── kratos_hook.go   # after-registration webhook from Kratos
│   │   ├── me.go            # /auth/me
│   │   ├── workspace.go
│   │   ├── members.go
│   │   ├── files.go
│   │   ├── access.go
│   │   └── audit.go
│   └── db/                  # Postgres connection, query helpers
├── kratos/                  # Kratos config (kratos.yml, identity schema, hooks)
├── db/init/                 # Postgres init scripts (CREATE DATABASE kratos)
├── migrations/              # numbered SQL files (001_init.sql, etc.)
└── docs/
    └── openapi.yaml         # OpenAPI 3.0 spec — lives here, not in frontend
```

### Routing and middleware

We use stdlib `net/http` only — `http.ServeMux` with the method+path pattern
syntax added in Go 1.22. No third-party router. Path params come from
`r.PathValue("name")`.

Middleware is the standard `func(http.Handler) http.Handler` shape and
composed via `middleware.Chain`. Auth-scoped and workspace-scoped routes are
expressed by wrapping subsets of routes with extra middleware before they
land on the mux — there is no Gin-style "router group", and we don't need one.

```go
mux := http.NewServeMux()

// Public — no auth (registration/login/recovery served by Kratos itself).
mux.HandleFunc("GET /api/v1/health", h.Health)

// Internal — gated by shared-secret header inside the handler.
mux.HandleFunc("POST /internal/hooks/kratos/after-registration", h.AfterRegistration)

// Authenticated — wrap individual handlers with Kratos session middleware
authed := middleware.RequireKratosSession(kc, db)
mux.Handle("GET  /api/v1/auth/me",    authed(http.HandlerFunc(h.Me)))
mux.Handle("GET  /api/v1/workspaces", authed(http.HandlerFunc(h.workspace.List)))
mux.Handle("POST /api/v1/workspaces", authed(http.HandlerFunc(h.workspace.Create)))

// Workspace-scoped — Kratos session + workspace membership
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
    db         *sql.DB
    kratos     *kratos.Client
    storage    storage.Storage
    redis      *redis.Client
    webhookKey string
}
```

Handler funcs use the standard signature:

```go
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) { ... }
```

For per-request state (request ID, user ID), use `r.Context()` plus the
typed accessors in `internal/httpx`. Never store request-scoped data in
package-level variables.

### Database schema

The API uses one Postgres database (`invosit`) for application data.
Identities, sessions, and password hashes live in a separate `kratos`
Postgres database managed entirely by Ory Kratos. The two are linked
by `users.kratos_identity_id`.

```sql
workspaces
  id, name, created_by, created_at

users
  id, email, kratos_identity_id, public_key, created_at
  -- kratos_identity_id (UUID, UNIQUE) maps to the Kratos identity
  -- created on registration via the Kratos after-registration webhook

workspace_members
  workspace_id, user_id, role, joined_at, expires_at
  role: admin | member | viewer | no_access
  expires_at: NULL = permanent membership; otherwise membership ends at that time

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
  -- user_id, workspace_id, file_id are nullable (login/logout has no workspace)
```

### API routes

Base path: `/api/v1`

Registration, login, and logout are served by Ory Kratos directly on
`:4433` (native JSON API; CLI hits Kratos straight). Password recovery
and email verification are disabled for MVP — re-enable in `kratos.yml`
once a courier and UI exist. The API only exposes `/auth/me` plus an
internal webhook.

```
GET    /auth/me

GET    /workspaces
POST   /workspaces
GET    /workspaces/:workspaceId
DELETE /workspaces/:workspaceId

GET    /workspaces/:workspaceId/members
POST   /workspaces/:workspaceId/members
DELETE /workspaces/:workspaceId/members/:userId

GET    /workspaces/:workspaceId/environments
POST   /workspaces/:workspaceId/environments   # admin only

# Files are scoped per environment. The schema enforces uniqueness on
# (environment_id, path), and access_grants carry environment_id, so the
# environment is the natural parent of a file in the URL.
GET    /workspaces/:workspaceId/environments/:environmentId/files
POST   /workspaces/:workspaceId/environments/:environmentId/files
GET    /workspaces/:workspaceId/environments/:environmentId/files/:fileId
DELETE /workspaces/:workspaceId/environments/:environmentId/files/:fileId
GET    /workspaces/:workspaceId/environments/:environmentId/files/:fileId/versions
POST   /workspaces/:workspaceId/environments/:environmentId/files/:fileId/rollback

GET    /workspaces/:workspaceId/access
POST   /workspaces/:workspaceId/access
DELETE /workspaces/:workspaceId/access/:grantId

GET    /workspaces/:workspaceId/audit
```

Full request/response schemas in `docs/openapi.yaml`.

---

## The CLI

Not yet built. When it lands it will live in `cli/` in this repo.

### Responsibilities (intended)

- **Encryption boundary.** All AES-256-GCM encryption/decryption of file contents happens here, not in the API. Generates per-file DEKs and wraps each with the recipient's public key (sourced from `users.public_key` via the API).
- **Manifest management.** Reads and writes `.invosit.yaml` in the working directory — the file committed to git that lists which files belong to the workspace, their paths, and current content hashes.
- **Auth.** Authenticates against Kratos directly via the **API flow** (`POST /self-service/login/api`); stores the returned `session_token` locally and sends it as `Authorization: Bearer <token>` on every API request.
- **Storage I/O.** Uploads/downloads encrypted blobs **directly** to the storage provider using short-lived signed URLs issued by the API. Bytes never pass through the API server.
- **Local key material.** Generates and stores the user's keypair locally; the public key gets registered with the API on first run, the private key never leaves the machine.

### Shared with the API

- `docs/openapi.yaml` — single source of truth for request/response shapes. The CLI generates or hand-writes a client off this; the API serves it.
- ID prefix conventions (`ws_`, `usr_`, `file_`, `ver_`, `grant_`, `log_`).
- Error code stability — the API returns short stable codes (`NOT_FOUND`, `INVALID_REQUEST`, etc.) that the CLI maps to user-facing messages.

When the CLI starts taking shape, the structure tree above gets an expanded
`cli/` subtree, this section grows real entry points and commands, and
"Running locally" splits into per-component subsections.

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

- Identity, registration, login, logout, recovery, verification, and
  password hashing (Argon2id) are all delegated to **Ory Kratos**
- Kratos issues a `session_token` from its **API flow** (`POST
  /self-service/login/api`); the CLI sends it as
  `Authorization: Bearer <session_token>` on every request. The future
  dashboard will use Kratos browser flow with cookies — both land at the
  same `/sessions/whoami` validation. (Per Ory's docs: "Native applications
  must use the API flows which don't set any cookies.")
- `RequireKratosSession` middleware reads the `Authorization: Bearer ...`
  header (or the Kratos session cookie), forwards it to `/sessions/whoami`,
  looks up the local `users` row by `kratos_identity_id`, and attaches the
  local user ID to the request context via `httpx.WithUserID` — never
  trust user ID from request body or query params
- The `/internal/hooks/kratos/after-registration` endpoint is gated by a
  shared-secret header (`X-Kratos-Webhook-Secret`, constant-time compared)
  and creates the local `users` row when Kratos creates an identity
- When OIDC / passkey is added later, the CLI will use API flow with
  `return_session_token_exchange_code=true` and a loopback redirect for the
  browser detour only those methods need. The API server is unaffected —
  it just keeps validating Bearer tokens
- No Redis caching of session validity — Kratos is the single source of
  truth (revisit if whoami latency becomes a bottleneck)

### Authorization — four layers

Every file access request must pass all four:

1. **Kratos session middleware** (`RequireKratosSession`) — is the user authenticated?
2. **Workspace membership middleware** (`WorkspaceMember`) — does the user belong to this workspace?
3. **Environment scope middleware** (`EnvironmentScoped`) — does `{environmentId}` belong to the workspace from layer 2? Files are uniquely identified by `(environment_id, path)`, so the env must be resolved and confirmed before any file lookup.
4. **Wrapped DEK check** — does a `wrapped_deks` row exist for this user + file?
   No row = 403 regardless of role or workspace membership.

Access is cryptographically enforced (layer 4), not just a permission check. A member
with no wrapped DEK cannot decrypt the file even if they bypass the API.

**MVP status:** Layer 4 is not yet wired — file content is unencrypted in the
M3 iteration (issue #11) so that the wire shape and storage plumbing can be
validated end-to-end. Encryption + wrapped DEKs land in M4; until then layers
1–3 are the only gate.

### Rate limiting

Redis-backed, per IP, applied to API routes:

- File push: **60 req/min**
- All other endpoints: **300 req/min**
- Return `429` with `Retry-After` header always

Kratos has its own brute-force protection on `/self-service/login` and
`/self-service/registration` (configured in `kratos/kratos.yml`). Tune
that side independently rather than re-implementing it on the API.

### Input validation

`go-playground/validator/v10` struct tags on all request structs. Decode +
validate via `httpx.Bind`, which uses `json.DisallowUnknownFields` and
returns an error on either malformed JSON or failed validation:

```go
type CreateWorkspaceRequest struct {
    Name string `json:"name" validate:"required,min=1,max=64"`
}

func (h *Handler) CreateWorkspace(w http.ResponseWriter, r *http.Request) {
    var req CreateWorkspaceRequest
    if err := httpx.Bind(r, &req); err != nil {
        httpx.RespondError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid workspace name")
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
- Kratos session tokens / cookies
- `Authorization` or `Cookie` headers
- `encrypted_dek` column values
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
6. **RequireKratosSession** — validate the Bearer token / Kratos cookie via `/sessions/whoami`, look up the local user, attach `userID` to request context
7. **WorkspaceMember** — verify membership for `{workspaceId}` (workspace routes only)
8. **EnvironmentScoped** — verify `{environmentId}` belongs to the workspace from layer 7, attach `environmentID` to context (file routes only)

---

## Storage interface

All blob storage goes through a single interface. Never import a provider
directly outside of `internal/storage/`:

```go
type Storage interface {
    SignedPutURL(ctx context.Context, key string, expiry time.Duration) (string, error)
    SignedGetURL(ctx context.Context, key string, expiry time.Duration) (string, error)
    Delete(ctx context.Context, key string) error
}
```

No `Put`/`Get` methods: the CLI uploads/downloads encrypted blobs directly
against the provider via signed URLs, so API server bytes never touch the
blob path. Adding server-side streaming methods would invite a handler to
proxy bytes through the API, which breaks both the encryption boundary
and R2's zero-egress economics. Add them only when a concrete handler
needs them.

`Delete` is idempotent: deleting a non-existent key is a no-op, not an error.

The package exports `MaxSignedURLExpiry = 15 * time.Minute` and
`ErrExpiryTooLong`; callers requesting a longer expiry get a hard error,
not a silent clamp.

Provider selected at startup via `STORAGE_PROVIDER`:

| Value | Provider | Status |
|---|---|---|
| `r2` | Cloudflare R2 (zero egress fees) | shipped — default when `STORAGE_PROVIDER` is empty |
| `s3` | AWS S3 | shipped |
| `gcs` | Google Cloud Storage | planned, not yet implemented (returns `ErrUnknownProvider` today) |

R2 and S3 share one implementation — R2 is S3-compatible, different endpoint
(plus `region=auto` and path-style addressing).

---

## Environment variables

```bash
# Server
PORT=8080
ENV=production                  # production | development

# Ory Kratos — set all three secrets per Ory's production guide
# (the v1.3 config schema only accepts default/cookie/cipher under
# `secrets:`, despite older docs mentioning a `pagination` key):
# https://www.ory.com/docs/kratos/guides/production
KRATOS_PUBLIC_URL=http://kratos:4433
KRATOS_WEBHOOK_SECRET=          # generate: openssl rand -hex 32
KRATOS_DEFAULT_SECRET=          # generate: openssl rand -hex 32
KRATOS_COOKIE_SECRET=           # generate: openssl rand -hex 32
KRATOS_CIPHER_SECRET=           # exactly 32 chars (e.g. openssl rand -hex 16)

# Postgres (one container hosts two databases: invosit + kratos)
DATABASE_URL=postgres://invosit:secret@postgres:5432/invosit

# Redis
REDIS_URL=redis://redis:6379

# Storage
STORAGE_PROVIDER=r2             # r2 | s3 (gcs planned, not yet implemented)
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

These apply across the monorepo. API-specific conventions are noted as such.

- **API:** stdlib `net/http` only — no third-party router or web framework
- **API:** `http.ServeMux` 1.22+ patterns: `"POST /api/v1/auth/register"`, `"GET /workspaces/{id}"`
- **API:** all middleware is `func(http.Handler) http.Handler`; compose with `middleware.Chain`
- **API:** decode + validate request bodies via `httpx.Bind` (rejects unknown JSON fields)
- **API:** read user ID / request ID from `r.Context()` via `httpx` accessors — never globals
- No global state — all dependencies injected via handler struct (or equivalent on CLI side)
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
docker compose up -d         # starts Postgres + Redis + Kratos
go run ./cmd/server          # starts API on :8080
```

Migrations run automatically on startup.

CLI build instructions will go here when `cli/` exists.
