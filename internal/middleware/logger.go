package middleware

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kyenel64/invosit-api/internal/ids"
)

// Logger replaces gin.Logger() with one that:
//   - Logs method, path (no query string), status, duration, request id, userID if present.
//   - Surfaces any internal_error stashed by httpx.InternalError to stderr,
//     so 500s aren't silent.
//   - Sets X-Request-ID on the response so clients can quote it in bug reports.
//
// Never logs: bodies, query strings, Authorization headers, refresh tokens.
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		requestID := ids.New("req")

		c.Set("request_id", requestID)
		c.Writer.Header().Set("X-Request-ID", requestID)

		c.Next()

		dur := time.Since(start)
		method := c.Request.Method
		path := c.Request.URL.Path
		status := c.Writer.Status()

		userID, _ := c.Get("userID")
		userField := ""
		if uid, ok := userID.(string); ok && uid != "" {
			userField = " user=" + uid
		}

		log.Printf("req=%s method=%s path=%s status=%d dur=%s%s",
			requestID, method, path, status, dur, userField)

		if errVal, ok := c.Get("internal_error"); ok {
			if err, ok := errVal.(error); ok && err != nil {
				log.Printf("req=%s internal_error=%q", requestID, err.Error())
			}
		}
	}
}
