package rest_test

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	rest "github.com/struckchure/go-rest"
)

var update = flag.Bool("update", false, "rewrite the golden files")

type LoginRequest struct {
	Email    string   `json:"email"`            // not nillable: required
	Password string   `json:"password"`         // not nillable: required
	Remember *bool    `json:"remember"`         // nillable: optional
	Scopes   []string `json:"scopes"`           // nillable: optional
	Device   string   `json:"device,omitempty"` // optional by tag
	Nonce    *string  `json:"nonce,required"`   // nillable, but required by tag

	Internal string // untagged: must never appear
}

type LoginResponse struct {
	AccessToken string    `json:"accessToken"`
	IssuedAt    time.Time `json:"issuedAt"`
}

type GetUserRequest struct {
	Id      string  `param:"id"`                   // path: always required
	Page    int     `query:"page"`                 // not nillable: required
	Include *string `query:"include"`              // nillable: optional
	Cursor  string  `query:"cursor,optional"`      // required by type, optional by tag
	TraceId *string `header:"X-Trace-Id,required"` // nillable, but required by tag

	Internal string // untagged: must never appear
}

type User struct {
	Id    string `json:"id"`
	Email string `json:"email"`
}

// Status declares its own values, so every field of this type is an enum.
type Status string

const (
	StatusActive   Status = "active"
	StatusArchived Status = "archived"
)

func (Status) Values() []string {
	return []string{string(StatusActive), string(StatusArchived)}
}

// Priority is an integer enum with a pointer receiver, returning a slice of
// itself rather than of a primitive.
type Priority int

func (*Priority) Values() []Priority { return []Priority{1, 2, 3} }

type SearchRequest struct {
	Status   Status   `json:"status"`                       // enum from the type
	Was      *Status  `json:"was"`                          // pointer to an enum
	Any      []Status `json:"any"`                          // slice of an enum
	Priority Priority `json:"priority"`                     // integer enum
	Colour   string   `json:"colour" enum:"red,green,blue"` // enum from a tag
	Sort     string   `query:"sort" enum:"asc,desc"`        // enum on a parameter
	Kinds    []string `query:"kinds" enum:"a,b"`            // enum on an array parameter
	Mode     Status   `query:"mode"`                        // typed enum on a parameter
}

// fixture builds the document every test works from.
func fixture() *rest.Rest {
	api := rest.New(
		rest.WithTitle("Test API"),
		rest.WithVersion("1.0.0"),
	)

	api.AddSecurityScheme("bearerAuth", rest.BearerAuth())
	api.AddSecurityScheme("apiKey", rest.APIKeyAuth("X-API-Key", "header"))
	api.SetDefaultSecurity("bearerAuth")

	// No RequireSecurity: public.
	api.Post("/api/user/authenticate/").
		HasSummary("Authenticate a user").
		HasRequestModel(rest.ModelOf[LoginRequest]()).
		HasResponseModel(http.StatusOK, rest.ModelOf[LoginResponse]())

	// Bare RequireSecurity: the document default.
	api.Get("/api/user/:id").
		RequireSecurity().
		HasRequestModel(rest.ModelOf[GetUserRequest]()).
		HasResponseModel(http.StatusOK, rest.ModelOf[User]())

	// Overrides the default.
	api.Get("/api/user/").
		RequireSecurity("apiKey").
		HasResponseModel(http.StatusOK, rest.ModelOf[[]User]())

	api.Post("/api/user/search/").
		HasRequestModel(rest.ModelOf[SearchRequest]()).
		HasResponseModel(http.StatusOK, rest.ModelOf[[]User]())

	return api
}

func mustDoc(t *testing.T, api *rest.Rest) *openapi3.T {
	t.Helper()

	doc, err := api.OpenAPI()
	if err != nil {
		t.Fatalf("OpenAPI(): %v", err)
	}
	return doc
}

func TestPathIsNormalized(t *testing.T) {
	doc := mustDoc(t, fixture())

	if doc.Paths.Find("/api/user/{id}") == nil {
		t.Errorf("expected `:id` to be rewritten to `{id}`, got paths %v", doc.Paths.Map())
	}
}

func TestRequestBodyUsesComponentRef(t *testing.T) {
	doc := mustDoc(t, fixture())

	body := doc.Paths.Find("/api/user/authenticate/").Post.RequestBody.Value
	schema := body.Content.Get("application/json").Schema

	if got, want := schema.Ref, "#/components/schemas/LoginRequest"; got != want {
		t.Errorf("request body ref = %q, want %q (it should not be inlined)", got, want)
	}
	if _, ok := doc.Components.Schemas["LoginRequest"]; !ok {
		t.Error("LoginRequest missing from components.schemas")
	}
}

// A field is required unless its type is nillable; `,optional`, `,omitempty` and
// `,required` override that. openapi3gen emits no `required` at all, so this is
// also the guard on our own hook.
func TestRequiredFollowsNillability(t *testing.T) {
	doc := mustDoc(t, fixture())

	required := map[string]bool{}
	for _, name := range doc.Components.Schemas["LoginRequest"].Value.Required {
		required[name] = true
	}

	for field, want := range map[string]bool{
		"email":    true,  // string
		"password": true,  // string
		"remember": false, // *bool
		"scopes":   false, // []string
		"device":   false, // string, but omitempty
		"nonce":    true,  // *string, but `,required`
	} {
		if required[field] != want {
			t.Errorf("%q required = %v, want %v", field, required[field], want)
		}
	}

	if len(required) != 3 {
		t.Errorf("required = %v, want exactly 3 entries", required)
	}
}

func TestUntaggedFieldsAreIgnored(t *testing.T) {
	doc := mustDoc(t, fixture())

	if _, ok := doc.Components.Schemas["LoginRequest"].Value.Properties["Internal"]; ok {
		t.Error("untagged field leaked into the body schema")
	}

	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "Internal") {
		t.Errorf("untagged field appears somewhere in the document:\n%s", out)
	}
}

func TestParametersAndNoBody(t *testing.T) {
	doc := mustDoc(t, fixture())

	op := doc.Paths.Find("/api/user/{id}").Get

	if op.RequestBody != nil {
		t.Error("a model of only param/query/header fields must not produce a request body")
	}
	if len(op.Parameters) != 5 {
		t.Fatalf("got %d parameters, want 5 (the untagged field must be skipped)", len(op.Parameters))
	}

	want := map[string]struct {
		in       string
		required bool
	}{
		"id":         {in: "path", required: true},   // path: always required
		"page":       {in: "query", required: true},  // int: not nillable
		"include":    {in: "query", required: false}, // *string: nillable
		"cursor":     {in: "query", required: false}, // string, but `,optional`
		"X-Trace-Id": {in: "header", required: true}, // *string, but `,required`
	}

	for _, ref := range op.Parameters {
		param := ref.Value

		expected, ok := want[param.Name]
		if !ok {
			t.Errorf("unexpected parameter %q", param.Name)
			continue
		}
		if param.In != expected.in {
			t.Errorf("parameter %q in = %q, want %q", param.Name, param.In, expected.in)
		}
		if param.Required != expected.required {
			t.Errorf("parameter %q required = %v, want %v", param.Name, param.Required, expected.required)
		}
		if param.Schema == nil || param.Schema.Value == nil {
			t.Errorf("parameter %q has no schema", param.Name)
			continue
		}
		// A ref here would mean the parameter generator leaked into components.
		if param.Schema.Ref != "" {
			t.Errorf("parameter %q schema should be inline, got ref %q", param.Name, param.Schema.Ref)
		}
	}
}

func TestSliceModelBecomesArray(t *testing.T) {
	doc := mustDoc(t, fixture())

	schema := doc.Paths.Find("/api/user/").Get.Responses.
		Status(http.StatusOK).Value.Content.Get("application/json").Schema

	if schema.Value == nil || !schema.Value.Type.Is("array") {
		t.Fatalf("want an array schema, got %+v", schema.Value)
	}
	if got, want := schema.Value.Items.Ref, "#/components/schemas/User"; got != want {
		t.Errorf("items ref = %q, want %q", got, want)
	}
}

// time.Time is a struct, so openapi3gen componentises it, but its schema has no
// properties and is therefore never written to components. It must be inlined.
func TestTimeIsInlined(t *testing.T) {
	doc := mustDoc(t, fixture())

	issuedAt := doc.Components.Schemas["LoginResponse"].Value.Properties["issuedAt"]

	if issuedAt.Ref != "" {
		t.Errorf("time.Time should be inlined, got ref %q", issuedAt.Ref)
	}
	if !issuedAt.Value.Type.Is("string") || issuedAt.Value.Format != "date-time" {
		t.Errorf("time.Time = %v/%q, want string/date-time", issuedAt.Value.Type, issuedAt.Value.Format)
	}
	if _, ok := doc.Components.Schemas["Time"]; ok {
		t.Error("time.Time leaked into components.schemas")
	}
}

// Go records nothing about a named type's constants, so an enum declares itself
// with a Values method or an `enum` tag.
func TestEnums(t *testing.T) {
	doc := mustDoc(t, fixture())

	properties := doc.Components.Schemas["SearchRequest"].Value.Properties

	statuses := []any{"active", "archived"}

	for _, tc := range []struct {
		field string
		want  []any
	}{
		{"status", statuses},                      // Values() on the type
		{"was", statuses},                         // *Status: the pointer is followed
		{"colour", []any{"red", "green", "blue"}}, // `enum` tag
	} {
		if got := properties[tc.field].Value.Enum; !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q enum = %v, want %v", tc.field, got, tc.want)
		}
	}

	// A slice of an enum puts the values on items, not on the array.
	if got := properties["any"].Value.Enum; got != nil {
		t.Errorf("the array itself should carry no enum, got %v", got)
	}
	if got := properties["any"].Value.Items.Value.Enum; !reflect.DeepEqual(got, statuses) {
		t.Errorf("items enum = %v, want %v", got, statuses)
	}

	// An integer enum stays numeric rather than becoming strings.
	if got := properties["priority"].Value.Enum; !reflect.DeepEqual(got, []any{int64(1), int64(2), int64(3)}) {
		t.Errorf("priority enum = %v, want [1 2 3]", got)
	}
}

func TestEnumsOnParameters(t *testing.T) {
	doc := mustDoc(t, fixture())

	params := map[string]*openapi3.Parameter{}
	for _, ref := range doc.Paths.Find("/api/user/search/").Post.Parameters {
		params[ref.Value.Name] = ref.Value
	}

	if got, want := params["sort"].Schema.Value.Enum, []any{"asc", "desc"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sort enum = %v, want %v", got, want)
	}
	if got, want := params["mode"].Schema.Value.Enum, []any{"active", "archived"}; !reflect.DeepEqual(got, want) {
		t.Errorf("mode enum = %v, want %v (from the type)", got, want)
	}

	// As in a body, an array parameter carries its enum on items.
	kinds := params["kinds"].Schema.Value
	if kinds.Enum != nil {
		t.Errorf("the array itself should carry no enum, got %v", kinds.Enum)
	}
	if got, want := kinds.Items.Value.Enum, []any{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("kinds items enum = %v, want %v", got, want)
	}
}

func TestBadEnumTagIsAnError(t *testing.T) {
	type Bad struct {
		Count int `json:"count" enum:"one,two"` // not integers
	}

	api := rest.New()
	api.Post("/x").HasRequestModel(rest.ModelOf[Bad]())

	_, err := api.OpenAPI()
	if err == nil {
		t.Fatal("expected an error for an enum value that does not fit the field's type")
	}
	if !strings.Contains(err.Error(), "one") {
		t.Errorf("error should name the offending value, got: %v", err)
	}
}

func TestSecurityIsOptIn(t *testing.T) {
	doc := mustDoc(t, fixture())

	if _, ok := doc.Components.SecuritySchemes["bearerAuth"]; !ok {
		t.Fatal("bearerAuth missing from components.securitySchemes")
	}

	// No document-level security: it would silently secure every operation.
	if len(doc.Security) != 0 {
		t.Errorf("document security = %v, want none", doc.Security)
	}

	// A route that never called RequireSecurity is public.
	if public := doc.Paths.Find("/api/user/authenticate/").Post.Security; public != nil {
		t.Errorf("a route without RequireSecurity should be public, got %v", *public)
	}

	// Bare RequireSecurity() takes the default.
	def := doc.Paths.Find("/api/user/{id}").Get.Security
	if def == nil || len(*def) != 1 {
		t.Fatalf("RequireSecurity() should apply the default, got %v", def)
	}
	if _, ok := (*def)[0]["bearerAuth"]; !ok {
		t.Errorf("RequireSecurity() = %v, want bearerAuth", *def)
	}

	// RequireSecurity("apiKey") overrides it.
	override := doc.Paths.Find("/api/user/").Get.Security
	if override == nil || len(*override) != 1 {
		t.Fatalf("RequireSecurity(\"apiKey\") should override, got %v", override)
	}
	if _, ok := (*override)[0]["apiKey"]; !ok {
		t.Errorf("override = %v, want apiKey", *override)
	}
}

func TestUnknownSecuritySchemeIsAnError(t *testing.T) {
	api := rest.New()
	api.Get("/x").RequireSecurity("nope")

	_, err := api.OpenAPI()
	if err == nil {
		t.Fatal("expected an error for an unregistered security scheme")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error should name the scheme, got: %v", err)
	}
}

// RequireSecurity() with nothing to fall back on is a mistake worth catching.
func TestRequireSecurityWithoutDefaultIsAnError(t *testing.T) {
	api := rest.New()
	api.AddSecurityScheme("bearerAuth", rest.BearerAuth())
	api.Get("/x").RequireSecurity()

	if _, err := api.OpenAPI(); err == nil {
		t.Fatal("expected an error: RequireSecurity() with no SetDefaultSecurity")
	}
}

func TestRequireScopes(t *testing.T) {
	api := rest.New()
	api.AddSecurityScheme("oauth2", rest.OIDCAuth("https://example.com/.well-known/openid-configuration"))
	api.Get("/x").RequireScopes("oauth2", "read:users")

	doc := mustDoc(t, api)

	security := doc.Paths.Find("/x").Get.Security
	if security == nil {
		t.Fatal("RequireScopes set no security")
	}
	if got := (*security)[0]["oauth2"]; len(got) != 1 || got[0] != "read:users" {
		t.Errorf("scopes = %v, want [read:users]", got)
	}
}

func TestDuplicateRouteIsAnError(t *testing.T) {
	api := rest.New()
	api.Get("/api/user/:id")
	api.Get("/api/user/{id}") // same path once normalized

	if _, err := api.OpenAPI(); err == nil {
		t.Fatal("expected an error for a duplicate route")
	}
}

// A document that only kin-openapi can read is not much use; check a consumer
// can load what we emit.
func TestGeneratedJSONRoundTrips(t *testing.T) {
	data, err := fixture().JSON()
	if err != nil {
		t.Fatal(err)
	}

	loader := openapi3.NewLoader()

	doc, err := loader.LoadFromData(data)
	if err != nil {
		t.Fatalf("loading the generated document: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("validating the reloaded document: %v", err)
	}
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"openapi.json", "openapi.yaml"} {
		path := filepath.Join(dir, name)

		if err := fixture().WriteFile(path); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			t.Errorf("WriteFile(%s) wrote nothing", name)
		}
	}

	if err := fixture().WriteFile(filepath.Join(dir, "openapi.txt")); err == nil {
		t.Error("expected an error for an unknown extension")
	}
}

func TestGolden(t *testing.T) {
	for _, tc := range []struct {
		name   string
		golden string
		render func() ([]byte, error)
	}{
		{"json", "testdata/openapi.json", fixture().JSON},
		{"yaml", "testdata/openapi.yaml", fixture().YAML},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.render()
			if err != nil {
				t.Fatal(err)
			}

			if *update {
				if err := os.WriteFile(tc.golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}

			want, err := os.ReadFile(tc.golden)
			if err != nil {
				t.Fatalf("%v (run `go test -update` to create it)", err)
			}
			if string(got) != string(want) {
				t.Errorf("%s is out of date; run `go test -update` to refresh\n--- got ---\n%s", tc.golden, got)
			}
		})
	}
}
