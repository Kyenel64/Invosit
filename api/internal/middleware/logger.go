package middleware

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/kyenel64/invosit/api/internal/httpx"
	"github.com/kyenel64/invosit/api/internal/ids"
)

// Logger logs method, path (no query string), status, duration, request ID,
// and userID if set in context. Sets X-Request-ID on every response so
// clients can quote it in bug reports.
//
// Never logs: bodies, query strings, Authorization headers, refresh tokens.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := ids.New("req")

		ctx := httpx.WithRequestID(r.Context(), requestID)
		w.Header().Set("X-Request-ID", requestID)

		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r.WithContext(ctx))

		userField := ""
		if uid := httpx.UserID(ctx); uid != "" {
			userField = " user=" + uid
		}

		// Quote the request path — it's attacker-controlled and could otherwise inject newlines into logs.
		log.Printf("req=%s method=%s path=%s status=%d dur=%s%s", //nolint:gosec // strconv.Quote escapes control chars
			requestID, r.Method, strconv.Quote(r.URL.Path), rec.statusOrDefault(), time.Since(start), userField)
	})
}

// statusRecorder captures the response status so the logger can record it
// after the handler returns. Defaults to 200 if the handler called Write
// without an explicit WriteHeader.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wroteHeader {
		s.status = http.StatusOK
		s.wroteHeader = true
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) statusOrDefault() int {
	if s.status == 0 {
		return http.StatusOK
	}
	return s.status
}
