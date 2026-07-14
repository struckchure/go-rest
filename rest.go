package rest

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
)

// Rest collects routes and renders them as an OpenAPI 3 document. It does not
// serve HTTP; it only describes it.
type Rest struct {
	info    openapi3.Info
	servers openapi3.Servers
	routes  []*Route

	securitySchemes map[string]*openapi3.SecurityScheme
	defaultSecurity openapi3.SecurityRequirements
}

// Option configures a Rest instance at construction.
type Option func(*Rest)

// WithTitle sets the document title.
func WithTitle(title string) Option {
	return func(r *Rest) { r.info.Title = title }
}

// WithVersion sets the API version.
func WithVersion(version string) Option {
	return func(r *Rest) { r.info.Version = version }
}

// WithDescription sets the document description.
func WithDescription(description string) Option {
	return func(r *Rest) { r.info.Description = description }
}

// WithServer appends a server URL.
func WithServer(url, description string) Option {
	return func(r *Rest) {
		r.servers = append(r.servers, &openapi3.Server{URL: url, Description: description})
	}
}

// New creates a Rest instance. Title and version fall back to defaults, since
// OpenAPI requires both to be non-empty.
func New(opts ...Option) *Rest {
	r := &Rest{
		info:            openapi3.Info{Title: "API", Version: "0.0.0"},
		securitySchemes: map[string]*openapi3.SecurityScheme{},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Routes returns the registered routes in declaration order.
func (r *Rest) Routes() []*Route { return r.routes }

func (r *Rest) add(method, path string) *Route {
	route := &Route{method: method, path: path}
	r.routes = append(r.routes, route)
	return route
}

// Get registers a GET route.
func (r *Rest) Get(path string) *Route { return r.add(http.MethodGet, path) }

// Post registers a POST route.
func (r *Rest) Post(path string) *Route { return r.add(http.MethodPost, path) }

// Put registers a PUT route.
func (r *Rest) Put(path string) *Route { return r.add(http.MethodPut, path) }

// Patch registers a PATCH route.
func (r *Rest) Patch(path string) *Route { return r.add(http.MethodPatch, path) }

// Delete registers a DELETE route.
func (r *Rest) Delete(path string) *Route { return r.add(http.MethodDelete, path) }

// Head registers a HEAD route.
func (r *Rest) Head(path string) *Route { return r.add(http.MethodHead, path) }

// Options registers an OPTIONS route.
func (r *Rest) Options(path string) *Route { return r.add(http.MethodOptions, path) }
