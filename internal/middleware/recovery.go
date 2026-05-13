package middleware

import (
	"log"
	"net/http"
	"runtime/debug"

	"github.com/kyenel64/invosit-api/internal/httpx"
)

// Recovery catches panics from downstream handlers, logs the stack trace
// server-side (never to the client), and returns a generic 500.
//
// http.ErrAbortHandler is a sentinel from net/http used to abort a handler
// without logging — re-panic so the server's own machinery handles it.
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { //nolint:contextcheck // closure reads r.Context() for log correlation only; no propagation needed
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			log.Printf("req=%s panic=%v\n%s", httpx.RequestID(r.Context()), rec, debug.Stack())
			httpx.RespondError(w, http.StatusInternalServerError, "INTERNAL", "something went wrong")
		}()
		next.ServeHTTP(w, r)
	})
}
