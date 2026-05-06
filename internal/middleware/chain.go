package middleware

import "net/http"

// Middleware is the standard net/http middleware shape.
type Middleware func(http.Handler) http.Handler

// Chain composes middleware so the leftmost runs outermost.
//
//	Chain(A, B, C)(h)  →  A(B(C(h)))
func Chain(mws ...Middleware) Middleware {
	return func(h http.Handler) http.Handler {
		for i := len(mws) - 1; i >= 0; i-- {
			h = mws[i](h)
		}
		return h
	}
}
