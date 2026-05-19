// Maps GitHub claims (returned by Kratos's built-in `github` provider,
// which fetches user info + the verified primary email when the
// `user:email` scope is requested) onto our identity traits.
//
// Docs: https://www.ory.com/docs/kratos/social-signin/data-mapping
local claims = std.extVar('claims');

{
  identity: {
    traits: {
      email: claims.email,
    },
  },
}
