package httpx

import (
	"github.com/gin-gonic/gin"
)

// RespondError writes a uniform JSON error and aborts the request.
// Shape: {"error": "<safe message>", "code": "<MACHINE_CODE>"}.
// Never include internal error text — log it server-side via InternalError.
func RespondError(c *gin.Context, status int, code, msg string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error": msg,
		"code":  code,
	})
}

// InternalError stashes the underlying error for the logger middleware
// (future) and returns a generic 500 to the client.
func InternalError(c *gin.Context, err error) {
	c.Set("internal_error", err)
	RespondError(c, 500, "INTERNAL", "something went wrong")
}
