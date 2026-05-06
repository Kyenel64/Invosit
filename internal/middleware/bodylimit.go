package middleware

import "net/http"

// BodyLimit caps the size of request bodies. Reading past the limit
// inside a handler will yield a *http.MaxBytesError; MaxBytesReader also
// signals the underlying connection so the client receives 413.
func BodyLimit(max int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, max)
			next.ServeHTTP(w, r)
		})
	}
}
