package middleware

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/kyenel64/invosit-api/internal/httpx"
)

// Verifies user id is a valid member of the workspace: /{workspaceId}.
// Attaches workspaceId and role to context
//
// Behaviour:
//   - missing user id in context (middleware misordered)        → 401
//   - empty {workspaceId} path value                            → 403
//   - no matching workspace_members row (incl. nonexistent ws)  → 403
//   - membership row exists but expires_at has passed           → 403
//
// 403 (not 404) is intentional: revealing whether a workspace exists to a
// non-member is itself an information leak.
func WorkspaceMember(db *sql.DB) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid := httpx.UserID(r.Context())
			if uid == "" {
				httpx.RespondError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
				return
			}

			workspaceID := r.PathValue("workspaceId")
			if workspaceID == "" {
				httpx.RespondError(w, http.StatusForbidden, "FORBIDDEN", "access denied")
				return
			}

			var role string
			err := db.QueryRowContext(r.Context(),
				`SELECT role
				   FROM workspace_members
				  WHERE workspace_id = $1
				    AND user_id = $2
				    AND (expires_at IS NULL OR expires_at > NOW())`,
				workspaceID, uid,
			).Scan(&role)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					httpx.RespondError(w, http.StatusForbidden, "FORBIDDEN", "access denied")
					return
				}
				httpx.InternalError(w, r, err)
				return
			}

			ctx := httpx.WithWorkspaceID(r.Context(), workspaceID)
			ctx = httpx.WithWorkspaceRole(ctx, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
