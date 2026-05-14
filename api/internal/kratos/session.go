package kratos

// Session is the minimal projection of Ory's *ory.Session that the API
// needs. We map ory.Session into this shape inside Whoami so the rest of
// the codebase doesn't import the SDK or have to nil-check optional
// pointer fields like Active and Identity.
type Session struct {
	ID         string
	Active     bool
	IdentityID string
}
