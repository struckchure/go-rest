package rest

import (
	"regexp"

	"github.com/getkin/kin-openapi/openapi3"
)

// Route is a single operation: one HTTP method at one path. Every method
// returns the Route so calls can be chained.
type Route struct {
	method string
	path   string

	summary     string
	description string
	operationID string
	tags        []string
	deprecated  bool

	request   Model
	responses map[int]Model

	// Security is opt-in: a route that asks for none is public. useDefault
	// records a bare RequireSecurity(), which borrows the document default;
	// security holds the schemes a route named for itself.
	useDefault bool
	security   *openapi3.SecurityRequirements
}

// HasRequestModel sets the request payload. Fields tagged `json` become the
// request body; fields tagged `query`, `param` or `header` become parameters.
// A field may carry several of these tags, and untagged fields are ignored.
//
// A field is required unless its type is nillable — a pointer, slice, map or
// interface can be absent, anything else cannot. Add `,optional` or `,required`
// to the tag where the type alone gets it wrong (`,omitempty` also means
// optional). Path parameters are always required.
func (r *Route) HasRequestModel(m Model) *Route {
	r.request = m
	return r
}

// HasResponseModel sets the payload returned for the given status code.
func (r *Route) HasResponseModel(status int, m Model) *Route {
	if r.responses == nil {
		r.responses = map[int]Model{}
	}
	r.responses[status] = m
	return r
}

// HasSummary sets the operation's short summary.
func (r *Route) HasSummary(s string) *Route {
	r.summary = s
	return r
}

// HasDescription sets the operation's long description.
func (r *Route) HasDescription(s string) *Route {
	r.description = s
	return r
}

// HasOperationId sets the operation's unique id.
func (r *Route) HasOperationId(s string) *Route {
	r.operationID = s
	return r
}

// HasTags appends tags used to group the operation.
func (r *Route) HasTags(tags ...string) *Route {
	r.tags = append(r.tags, tags...)
	return r
}

// IsDeprecated marks the operation as deprecated.
func (r *Route) IsDeprecated() *Route {
	r.deprecated = true
	return r
}

// RequireSecurity requires authentication for this route.
//
// Called with no arguments it applies whatever Rest.SetDefaultSecurity named;
// with arguments it overrides that default for this route alone. Listing several
// schemes means any one of them is sufficient.
//
//	route.RequireSecurity()          // the document default
//	route.RequireSecurity("apiKey")  // this route uses an API key instead
//
// Security is opt-in: a route that never calls this is public.
func (r *Route) RequireSecurity(names ...string) *Route {
	if len(names) == 0 {
		r.useDefault = true
		return r
	}

	r.useDefault = false
	for _, name := range names {
		r.require(name, []string{})
	}

	return r
}

// RequireScopes requires the named scheme with a set of scopes, for OAuth2 and
// OpenID Connect. Like RequireSecurity, repeating it records alternatives.
func (r *Route) RequireScopes(name string, scopes ...string) *Route {
	if scopes == nil {
		scopes = []string{}
	}

	r.useDefault = false
	r.require(name, scopes)

	return r
}

func (r *Route) require(name string, scopes []string) {
	if r.security == nil {
		r.security = openapi3.NewSecurityRequirements()
	}
	*r.security = append(*r.security, openapi3.SecurityRequirement{name: scopes})
}

// echoParam matches an Echo-style path segment such as `:id`.
var echoParam = regexp.MustCompile(`:([^/]+)`)

// normalizePath rewrites Echo-style `:id` segments into the `{id}` form OpenAPI
// requires. Paths already using braces are left alone, as are trailing slashes.
func normalizePath(path string) string {
	return echoParam.ReplaceAllString(path, "{$1}")
}
