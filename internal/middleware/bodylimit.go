package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// BodyLimit caps the size of request bodies. Reading past the limit
// inside a handler will yield an error and MaxBytesReader will write
// 413 Request Entity Too Large + close the connection.
func BodyLimit(max int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, max)
		c.Next()
	}
}
