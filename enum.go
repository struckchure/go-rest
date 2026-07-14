package rest

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// Go keeps no record of a named type's constants at runtime, so an enum has to
// declare itself. There are two ways to do that.
//
// The first, and the one to reach for, is a Values method on the type. It
// travels with the type, so every field of that type is described as an enum
// wherever it appears:
//
//	type Status string
//
//	const (
//		StatusActive   Status = "active"
//		StatusArchived Status = "archived"
//	)
//
//	func (Status) Values() []string {
//		return []string{string(StatusActive), string(StatusArchived)}
//	}
//
// The method may return a slice of anything — []string, []int, []Status, []any —
// and may hang off either the value or the pointer.
//
// The second is an `enum` tag, for a one-off on a plain type that has no named
// type of its own:
//
//	Sort string `query:"sort" enum:"asc,desc"`
//
// A Values method wins over a tag if a type somehow has both.
const enumMethod = "Values"

// enumOf calls the type's Values method, if it has a usable one.
func enumOf(t reflect.Type) ([]any, bool) {
	t = deref(t)
	if t == nil {
		return nil, false
	}

	// The method set of *T contains methods declared on both T and *T, so this
	// finds a Values method with either receiver.
	method := reflect.New(t).MethodByName(enumMethod)
	if !method.IsValid() {
		return nil, false
	}

	signature := method.Type()
	if signature.NumIn() != 0 || signature.NumOut() != 1 || signature.Out(0).Kind() != reflect.Slice {
		return nil, false
	}

	slice := method.Call(nil)[0]

	values := make([]any, 0, slice.Len())
	for i := range slice.Len() {
		values = append(values, plain(slice.Index(i)))
	}

	if len(values) == 0 {
		return nil, false
	}

	return values, true
}

// plain reduces a value to the primitive behind it, so a []Status comes out as
// ["active", "archived"] rather than as a list of named types.
func plain(v reflect.Value) any {
	if v.Kind() == reflect.Interface {
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint()
	case reflect.Float32, reflect.Float64:
		return v.Float()
	case reflect.Bool:
		return v.Bool()
	default:
		return v.Interface()
	}
}

// parseEnumTag reads `enum:"a,b,c"` into values of the field's own type, so an
// integer enum lands in the document as numbers rather than strings.
func parseEnumTag(t reflect.Type, raw string) ([]any, error) {
	t = deref(t)

	parts := strings.Split(raw, ",")
	values := make([]any, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		var (
			value any
			err   error
		)

		switch t.Kind() {
		case reflect.String:
			value = part
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			value, err = strconv.ParseInt(part, 10, 64)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			value, err = strconv.ParseUint(part, 10, 64)
		case reflect.Float32, reflect.Float64:
			value, err = strconv.ParseFloat(part, 64)
		case reflect.Bool:
			value, err = strconv.ParseBool(part)
		default:
			return nil, fmt.Errorf("enum tag on unsupported type %s", t)
		}
		if err != nil {
			return nil, fmt.Errorf("enum value %q is not a valid %s: %w", part, t, err)
		}

		values = append(values, value)
	}

	if len(values) == 0 {
		return nil, fmt.Errorf("enum tag is empty")
	}

	return values, nil
}

// applyEnum sets a schema's enum from the type's Values method, or failing that
// from an `enum` tag.
//
// Composite kinds are left alone on purpose. openapi3gen recurses into a slice's
// element and a map's value carrying the same struct tag, so it calls back here
// with the element type: letting that call do the work puts the enum on `items`,
// where it belongs, rather than on the array itself.
func applyEnum(t reflect.Type, tag reflect.StructTag, schema *openapi3.Schema) error {
	switch t.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.Struct:
		return nil
	}

	if values, ok := enumOf(t); ok {
		schema.Enum = values
		return nil
	}

	raw, ok := tag.Lookup("enum")
	if !ok {
		return nil
	}

	values, err := parseEnumTag(t, raw)
	if err != nil {
		return err
	}
	schema.Enum = values

	return nil
}

// applyEnumTag puts an `enum` tag onto an already-built parameter schema.
//
// Parameters are generated from the field's type alone, without its tags, so the
// tag has to be applied afterwards. It descends into an array's items, matching
// what openapi3gen does for body fields.
func applyEnumTag(schema *openapi3.Schema, t reflect.Type, raw string) error {
	t = deref(t)

	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		if schema.Items == nil || schema.Items.Value == nil {
			return nil
		}
		return applyEnumTag(schema.Items.Value, t.Elem(), raw)
	}

	values, err := parseEnumTag(t, raw)
	if err != nil {
		return err
	}
	schema.Enum = values

	return nil
}
