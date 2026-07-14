package rest

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3gen"
)

// generator turns Go types into schemas for one document.
//
// Two openapi3gen generators are deliberately kept apart:
//
//   - components generates body and response schemas, exporting every struct it
//     meets into the shared schemas map and returning a $ref.
//   - inline generates parameter schemas with no component export, so a `page`
//     query parameter stays an inline `{"type": "integer"}` instead of becoming
//     a component.
//
// They must not be merged: the component generator's export pass nils out the
// Value of any ref pointing at #/components/schemas/, which would hollow out
// parameter schemas that shared the same generator.
type generator struct {
	components *openapi3gen.Generator
	inline     *openapi3gen.Generator
	schemas    openapi3.Schemas

	// seen maps a component name back to the Go type it came from, so
	// resolveDanglingRefs can regenerate a schema openapi3gen dropped.
	seen map[string]reflect.Type
}

func newGenerator() *generator {
	g := &generator{
		schemas: openapi3.Schemas{},
		seen:    map[string]reflect.Type{},
	}

	opts := []openapi3gen.Option{
		openapi3gen.SchemaCustomizer(g.customizeSchema),
	}
	g.components = openapi3gen.NewGenerator(append(opts,
		openapi3gen.CreateComponentSchemas(openapi3gen.ExportComponentSchemasOptions{
			ExportComponentSchemas: true,
			ExportTopLevelSchema:   true,
		}),
	)...)
	g.inline = openapi3gen.NewGenerator(opts...)

	return g
}

const componentPrefix = "#/components/schemas/"

// resolveRefs makes every component $ref the generator emitted usable.
//
// openapi3gen's export pass leaves each ref with its Ref string set but its
// Value nil. Two things go wrong with that:
//
//   - kin-openapi considers a ref with no Value unresolved and refuses to
//     validate the document, since a loaded document carries both. Marshalling
//     still emits only the $ref, so re-attaching the Value costs nothing.
//   - A struct whose schema has no properties is never copied into the schemas
//     map at all, because the export pass guards on Properties != nil. time.Time
//     renders as string/date-time, and so does any type with a custom
//     MarshalJSON, so those refs point at a component that was never written.
//     Inline them, which is what a reader wants on the field anyway.
//
// Every SchemaRef the generator produced is reachable from SchemaRefs, and the
// document holds those same pointers, so patching here patches the document.
func (g *generator) resolveRefs() error {
	for ref := range g.components.SchemaRefs {
		name, ok := strings.CutPrefix(ref.Ref, componentPrefix)
		if !ok {
			continue
		}

		if component, exists := g.schemas[name]; exists {
			ref.Value = component.Value
			continue
		}

		t, known := g.seen[name]
		if !known {
			return fmt.Errorf("unresolved schema %q", name)
		}

		inlined, err := g.paramRef(t)
		if err != nil {
			return fmt.Errorf("inlining schema %q: %w", name, err)
		}

		ref.Ref = ""
		ref.Value = inlined.Value
	}

	return nil
}

// bodyRef builds the schema for a request or response payload. Named structs
// become components and are referenced by $ref; slices become arrays whose
// items reference the element's component.
func (g *generator) bodyRef(t reflect.Type) (*openapi3.SchemaRef, error) {
	t = deref(t)

	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		items, err := g.bodyRef(t.Elem())
		if err != nil {
			return nil, err
		}
		array := openapi3.NewArraySchema()
		array.Items = items
		return openapi3.NewSchemaRef("", array), nil
	}

	return g.components.NewSchemaRefForValue(reflect.New(t).Interface(), g.schemas)
}

// paramRef builds an inline schema for a single parameter's type.
//
// Passing nil for the schemas map keeps everything inline, and going through
// NewSchemaRefForValue (rather than GenerateSchemaRef) matters: the latter
// leaves Ref set to the bare type name, which would serialise as the nonsense
// `$ref: "string"`. The export pass inside NewSchemaRefForValue clears it.
func (g *generator) paramRef(t reflect.Type) (*openapi3.SchemaRef, error) {
	return g.inline.NewSchemaRefForValue(reflect.New(deref(t)).Interface(), nil)
}

// customizeSchema fills in what openapi3gen leaves out: `required` on every
// struct, and `enum` wherever a type or tag declares one. It also notes the type
// behind each component name.
//
// openapi3gen parses `,omitempty` but never acts on it and never emits
// `required` at all, so without this hook every field would look optional.
func (g *generator) customizeSchema(_ string, t reflect.Type, tag reflect.StructTag, schema *openapi3.Schema) error {
	if err := applyEnum(t, tag, schema); err != nil {
		return err
	}

	if t.Kind() != reflect.Struct {
		return nil
	}

	// openapi3gen names components after the bare type name.
	g.seen[t.Name()] = t

	if required := requiredFields(t); len(required) > 0 {
		schema.Required = required
	}

	return nil
}

// requiredFields lists the JSON names of the fields that must be present.
func requiredFields(t reflect.Type) []string {
	var required []string

	eachField(t, func(f reflect.StructField) {
		name, opts, ok := jsonTag(f)
		if !ok {
			return
		}
		if isRequired(f.Type, opts) && !opts.has("omitempty") {
			required = append(required, name)
		}
	})

	return required
}

// isRequired decides whether a field must be present.
//
// The type carries the intent: a nillable type can be absent, so it is optional;
// anything else has no way to say "missing" and is required. A `,required` or
// `,optional` tag option overrides that when the type alone gets it wrong.
func isRequired(t reflect.Type, opts tagOptions) bool {
	switch {
	case opts.has("required"):
		return true
	case opts.has("optional"):
		return false
	default:
		return !isNillable(t)
	}
}

// isNillable reports whether a value of this type can be nil, and so can
// legitimately be absent from a payload.
func isNillable(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Map, reflect.Interface:
		return true
	default:
		return false
	}
}

// hasJSONBody reports whether the type contributes a request body at all. A
// model made only of `param`/`query`/`header` fields must not produce an empty
// object body.
func hasJSONBody(t reflect.Type) bool {
	found := false

	eachField(deref(t), func(f reflect.StructField) {
		if _, _, ok := jsonTag(f); ok {
			found = true
		}
	})

	return found
}

// parameters decomposes a request model into path, query and header parameters,
// Echo-style. Fields carrying none of those tags are skipped entirely, so an
// untagged field appears nowhere in the document.
func (g *generator) parameters(t reflect.Type) (openapi3.Parameters, error) {
	var (
		params openapi3.Parameters
		err    error
	)

	eachField(deref(t), func(f reflect.StructField) {
		if err != nil {
			return
		}

		var param *openapi3.Parameter

		switch {
		case has(f, "param"):
			name, _, _ := tag(f, "param")
			// Path parameters are always required, whatever the field's type
			// says: the spec allows nothing else.
			param = openapi3.NewPathParameter(name)
		case has(f, "query"):
			name, opts, _ := tag(f, "query")
			param = openapi3.NewQueryParameter(name)
			param.Required = isRequired(f.Type, opts)
		case has(f, "header"):
			name, opts, _ := tag(f, "header")
			param = openapi3.NewHeaderParameter(name)
			param.Required = isRequired(f.Type, opts)
		default:
			return
		}

		var schema *openapi3.SchemaRef
		if schema, err = g.paramRef(f.Type); err != nil {
			return
		}
		param.Schema = schema

		// A type's Values method is picked up by the customizer during
		// generation, but an `enum` tag is not: parameters are generated from
		// the field's type alone.
		if raw, ok := f.Tag.Lookup("enum"); ok {
			if err = applyEnumTag(schema.Value, f.Type, raw); err != nil {
				err = fmt.Errorf("parameter %q: %w", param.Name, err)
				return
			}
		}

		params = append(params, &openapi3.ParameterRef{Value: param})
	})

	return params, err
}

// eachField visits every field of a struct, flattening anonymous embedded
// structs the way encoding/json promotes them.
func eachField(t reflect.Type, visit func(reflect.StructField)) {
	t = deref(t)
	if t == nil || t.Kind() != reflect.Struct {
		return
	}

	for i := range t.NumField() {
		f := t.Field(i)

		if f.Anonymous && !hasAnyTag(f) {
			eachField(f.Type, visit)
			continue
		}
		if f.IsExported() {
			visit(f)
		}
	}
}

// tagOptions are the comma-separated extras of a struct tag, e.g. the
// "omitempty" in `json:"name,omitempty"`.
type tagOptions []string

func (o tagOptions) has(opt string) bool {
	for _, candidate := range o {
		if candidate == opt {
			return true
		}
	}
	return false
}

// tag reads one struct tag, returning its name, its options, and whether it is
// usable (present and not "-").
func tag(f reflect.StructField, key string) (string, tagOptions, bool) {
	raw, ok := f.Tag.Lookup(key)
	if !ok || raw == "-" {
		return "", nil, false
	}

	parts := strings.Split(raw, ",")
	name := parts[0]
	if name == "" {
		name = f.Name
	}

	return name, tagOptions(parts[1:]), true
}

func jsonTag(f reflect.StructField) (string, tagOptions, bool) { return tag(f, "json") }

func has(f reflect.StructField, key string) bool {
	_, _, ok := tag(f, key)
	return ok
}

// bindingTags are the tags that make a field visible to this package. A field
// carrying none of them is ignored everywhere.
var bindingTags = []string{"json", "query", "param", "header"}

func hasAnyTag(f reflect.StructField) bool {
	for _, key := range bindingTags {
		if _, ok := f.Tag.Lookup(key); ok {
			return true
		}
	}
	return false
}
