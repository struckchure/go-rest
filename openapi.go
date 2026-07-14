package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/invopop/yaml"
)

// OpenAPI renders the registered routes as an OpenAPI 3 document. The document
// is validated before it is returned, so mistakes surface here rather than in a
// consumer's UI.
func (r *Rest) OpenAPI() (*openapi3.T, error) {
	gen := newGenerator()

	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &r.info,
		Servers: r.servers,
		Paths:   openapi3.NewPaths(),
		Components: &openapi3.Components{
			Schemas:         gen.schemas,
			SecuritySchemes: openapi3.SecuritySchemes{},
		},
		// No document-level `security`: it would apply to every operation, and
		// routes opt in individually with RequireSecurity.
	}

	for name, scheme := range r.securitySchemes {
		doc.Components.SecuritySchemes[name] = &openapi3.SecuritySchemeRef{Value: scheme}
	}

	seen := map[string]bool{}

	for _, route := range r.routes {
		path := normalizePath(route.path)

		key := route.method + " " + path
		if seen[key] {
			return nil, fmt.Errorf("rest: duplicate route %s", key)
		}
		seen[key] = true

		op, err := r.operation(gen, route)
		if err != nil {
			return nil, fmt.Errorf("rest: %s: %w", key, err)
		}

		doc.AddOperation(path, route.method, op)
	}

	if err := gen.resolveRefs(); err != nil {
		return nil, fmt.Errorf("rest: %w", err)
	}

	if err := doc.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("rest: invalid document: %w", err)
	}

	return doc, nil
}

func (r *Rest) operation(gen *generator, route *Route) (*openapi3.Operation, error) {
	security, err := r.security(route)
	if err != nil {
		return nil, err
	}

	op := &openapi3.Operation{
		Summary:     route.summary,
		Description: route.description,
		OperationID: route.operationID,
		Tags:        route.tags,
		Deprecated:  route.deprecated,
		Security:    security,
	}

	if !route.request.IsZero() {
		params, err := gen.parameters(route.request.Type)
		if err != nil {
			return nil, fmt.Errorf("parameters: %w", err)
		}
		op.Parameters = params

		// Only models with json-tagged fields carry a body; one made purely of
		// path/query/header fields must not emit an empty object.
		if hasJSONBody(route.request.Type) {
			schema, err := gen.bodyRef(route.request.Type)
			if err != nil {
				return nil, fmt.Errorf("request body: %w", err)
			}
			op.RequestBody = &openapi3.RequestBodyRef{
				Value: openapi3.NewRequestBody().WithRequired(true).WithJSONSchemaRef(schema),
			}
		}
	}

	responses, err := gen.responses(route.responses)
	if err != nil {
		return nil, err
	}
	op.Responses = responses

	return op, nil
}

func (g *generator) responses(models map[int]Model) (*openapi3.Responses, error) {
	if len(models) == 0 {
		// OpenAPI requires at least one response.
		responses := openapi3.NewResponsesWithCapacity(1)
		responses.Set("default", &openapi3.ResponseRef{Value: openapi3.NewResponse().WithDescription("")})
		return responses, nil
	}

	statuses := make([]int, 0, len(models))
	for status := range models {
		statuses = append(statuses, status)
	}
	sort.Ints(statuses) // deterministic output

	responses := openapi3.NewResponsesWithCapacity(len(statuses))

	for _, status := range statuses {
		schema, err := g.bodyRef(models[status].Type)
		if err != nil {
			return nil, fmt.Errorf("response %d: %w", status, err)
		}

		response := openapi3.NewResponse().
			WithDescription(http.StatusText(status)).
			WithJSONSchemaRef(schema)

		responses.Set(strconv.Itoa(status), &openapi3.ResponseRef{Value: response})
	}

	return responses, nil
}

// security resolves what a route asked for into the operation's requirements,
// and checks that every scheme it names was actually registered.
//
// A nil result means the route is public: it never called RequireSecurity.
func (r *Rest) security(route *Route) (*openapi3.SecurityRequirements, error) {
	var requirements openapi3.SecurityRequirements

	switch {
	case route.useDefault:
		if len(r.defaultSecurity) == 0 {
			return nil, fmt.Errorf("RequireSecurity() needs a default: call SetDefaultSecurity, or name a scheme on the route")
		}
		requirements = r.defaultSecurity

	case route.security != nil:
		requirements = *route.security

	default:
		return nil, nil
	}

	for _, requirement := range requirements {
		for name := range requirement {
			if _, ok := r.securitySchemes[name]; !ok {
				return nil, fmt.Errorf("unknown security scheme %q: register it with AddSecurityScheme", name)
			}
		}
	}

	// Copy, so a route can never mutate the shared default.
	resolved := append(openapi3.SecurityRequirements{}, requirements...)

	return &resolved, nil
}

// JSON renders the document as indented JSON.
func (r *Rest) JSON() ([]byte, error) {
	doc, err := r.OpenAPI()
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(doc, "", "  ")
}

// YAML renders the document as YAML.
func (r *Rest) YAML() ([]byte, error) {
	doc, err := r.OpenAPI()
	if err != nil {
		return nil, err
	}
	// invopop/yaml routes through openapi3.T's MarshalJSON, so the OpenAPI key
	// names survive the trip.
	return yaml.Marshal(doc)
}

// WriteFile writes the document to path, choosing the encoding from the file
// extension: .json, .yaml or .yml.
func (r *Rest) WriteFile(path string) error {
	var (
		data []byte
		err  error
	)

	switch ext := filepath.Ext(path); ext {
	case ".json":
		data, err = r.JSON()
	case ".yaml", ".yml":
		data, err = r.YAML()
	default:
		return fmt.Errorf("rest: cannot infer format from extension %q: use .json, .yaml or .yml", ext)
	}
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}
