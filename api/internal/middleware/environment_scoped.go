package middleware

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/kyenel64/invosit/api/internal/httpx"
)

// Verifies {environmentId} belongs to the workspace already established by
// WorkspaceMember, then attaches the environment id to the request context.
//
// Behaviour:
//   - missing workspace id in context (middleware misordered) → 403
//   - empty {environmentId} path value                        → 403
//   - environment row not found or belongs to another ws      → 403
//
// 403 (not 404) is intentional: leaking environment existence to a non-member
// is itself an information leak.
func EnvironmentScoped(db *sql.DB) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			workspaceID := httpx.WorkspaceID(r.Context())
			if workspaceID == "" {
				httpx.RespondError(w, http.StatusForbidden, "FORBIDDEN", "access denied")
				return
			}

			envID := r.PathValue("environmentId")
			if envID == "" {
				httpx.RespondError(w, http.StatusForbidden, "FORBIDDEN", "access denied")
				return
			}

			var found string
			err := db.QueryRowContext(r.Context(),
				`SELECT id FROM environments WHERE id = $1 AND workspace_id = $2`,
				envID, workspaceID,
			).Scan(&found)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					httpx.RespondError(w, http.StatusForbidden, "FORBIDDEN", "access denied")
					return
				}
				httpx.InternalError(w, r, err)
				return
			}

			next.ServeHTTP(w, r.WithContext(httpx.WithEnvironmentID(r.Context(), envID)))
		})
	}
}
