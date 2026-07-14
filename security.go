package rest

import "github.com/getkin/kin-openapi/openapi3"

// AddSecurityScheme registers a scheme under a name that routes reference with
// Route.HasSecurity. It ends up in components.securitySchemes.
func (r *Rest) AddSecurityScheme(name string, scheme *openapi3.SecurityScheme) *Rest {
	r.securitySchemes[name] = scheme
	return r
}

// SetDefaultSecurity names the schemes that a bare Route.RequireSecurity()
// applies. Listing several means any one of them is sufficient.
//
// It does not secure anything on its own: routes opt in. That is deliberate — a
// document-level `security` key would apply to every operation, and a route
// could then only be made public by explicitly overriding it back to empty.
func (r *Rest) SetDefaultSecurity(names ...string) *Rest {
	for _, name := range names {
		r.defaultSecurity = append(r.defaultSecurity, openapi3.SecurityRequirement{name: []string{}})
	}
	return r
}

// BearerAuth is an HTTP bearer scheme carrying a JWT.
func BearerAuth() *openapi3.SecurityScheme {
	return openapi3.NewJWTSecurityScheme()
}

// BasicAuth is an HTTP basic scheme.
func BasicAuth() *openapi3.SecurityScheme {
	return openapi3.NewSecurityScheme().WithType("http").WithScheme("basic")
}

// APIKeyAuth is an API key carried in "header", "query" or "cookie".
func APIKeyAuth(name, in string) *openapi3.SecurityScheme {
	return openapi3.NewSecurityScheme().WithType("apiKey").WithName(name).WithIn(in)
}

// OIDCAuth is an OpenID Connect scheme discovered at the given URL.
func OIDCAuth(url string) *openapi3.SecurityScheme {
	return openapi3.NewOIDCSecurityScheme(url)
}

// CustomAuth passes a scheme through untouched, for anything the helpers above
// do not cover (OAuth2 flows, for instance).
func CustomAuth(scheme *openapi3.SecurityScheme) *openapi3.SecurityScheme {
	return scheme
}
