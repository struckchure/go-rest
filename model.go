package rest

import "reflect"

// Model is a type-erased handle to a Go type used as a request or response
// payload. It carries no value, only the reflect.Type, so it is cheap to pass
// around and safe to reuse across routes.
type Model struct {
	Type reflect.Type
}

// ModelOf captures T for schema generation:
//
//	rest.ModelOf[dto.UserAuthenticateRequestDto]()
//
// T may be a struct, a pointer to one, or a slice of either.
func ModelOf[T any]() Model {
	return Model{Type: reflect.TypeFor[T]()}
}

// IsZero reports whether the Model was never populated.
func (m Model) IsZero() bool { return m.Type == nil }

// deref walks through pointers to the underlying element type.
func deref(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}
