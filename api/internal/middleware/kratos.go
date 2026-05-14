package middleware

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/kyenel64/invosit/api/internal/httpx"
	"github.com/kyenel64/invosit/api/internal/kratos"
)

// RequireKratosSession validates the incoming request against Kratos and
// resolves the local user record. On success, attaches the local user_id
// to the request context via httpx.WithUserID.
//
// Behaviour:
//   - missing auth (no Authorization Bearer and no Cookie) → 401
//   - Kratos rejects credentials                            → 401
//   - Kratos says session inactive                          → 401
//   - no local users row matches kratos_identity_id         → 401
//   - any other error                                       → 500
func RequireKratosSession(client *kratos.Client, db *sql.DB) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sessionToken := bearerToken(r.Header.Get("Authorization"))
			cookieHeader := r.Header.Get("Cookie")
			if sessionToken == "" && cookieHeader == "" {
				httpx.RespondError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
				return
			}

			sess, err := client.Whoami(r.Context(), sessionToken, cookieHeader)
			if err != nil {
				if errors.Is(err, kratos.ErrUnauthenticated) {
					httpx.RespondError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
					return
				}
				httpx.InternalError(w, r, err)
				return
			}
			if !sess.Active || sess.IdentityID == "" {
				httpx.RespondError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
				return
			}

			var localID string
			err = db.QueryRowContext(r.Context(),
				`SELECT id FROM users WHERE kratos_identity_id = $1`,
				sess.IdentityID,
			).Scan(&localID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					httpx.RespondError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
					return
				}
				httpx.InternalError(w, r, err)
				return
			}

			ctx := httpx.WithUserID(r.Context(), localID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// bearerToken extracts the token from an `Authorization: Bearer <token>`
// header, case-insensitively. Returns "" if the header is empty or doesn't
// start with the Bearer scheme.
func bearerToken(authHeader string) string {
	const prefix = "bearer "
	if len(authHeader) <= len(prefix) {
		return ""
	}
	if !strings.EqualFold(authHeader[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(authHeader[len(prefix):])
}
