# go-rest

Build an OpenAPI 3 document from Go types, and render it as JSON or YAML.

Routes are declared with a fluent chain and payloads are described by your DTO
structs — no hand-written YAML, no codegen comments, no build step. Built on
[kin-openapi](https://github.com/getkin/kin-openapi).

This package describes an API; it does not serve one. It has no router and no
handlers, so it sits alongside whatever HTTP stack you already use.

```bash
go get github.com/struckchure/go-rest
```

## Usage

```go
package main

import (
	"fmt"
	"log"
	"net/http"

	rest "github.com/struckchure/go-rest"
	"myapp/dto"
)

func main() {
	api := rest.New(
		rest.WithTitle("User API"),
		rest.WithVersion("1.0.0"),
		rest.WithServer("https://api.example.com", "production"),
	)

	api.AddSecurityScheme("bearerAuth", rest.BearerAuth())
	api.SetDefaultSecurity("bearerAuth")

	api.Post("/api/user/authenticate/").
		HasSummary("Authenticate a user").
		HasTags("user").
		HasRequestModel(rest.ModelOf[dto.UserAuthenticateRequestDto]()).
		HasResponseModel(http.StatusOK, rest.ModelOf[dto.UserAuthenticateResponseDto]())

	api.Get("/api/user/:id").
		RequireSecurity().
		HasRequestModel(rest.ModelOf[dto.UserGetRequestDto]()).
		HasResponseModel(http.StatusOK, rest.ModelOf[dto.UserDto]())

	out, err := api.YAML()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(string(out))
}
```

A runnable version lives in [`example/`](example/main.go):

```bash
go run ./example
```

## Models

`rest.ModelOf[T]()` captures a type. `T` can be a struct, a pointer to one, or a
slice — `rest.ModelOf[[]dto.UserDto]()` renders as an array of `$ref`.

Each struct becomes a named entry under `components/schemas` and is referenced by
`$ref`, so a DTO used by several routes is described once.

## Binding tags

A request model binds Echo-style, and one model can draw from several places at
once. `json` fields become the request body; `param`, `query` and `header` fields
become parameters.

```go
type UserGetRequestDto struct {
	Id      string  `param:"id"`
	Page    int     `query:"page"`
	Include *string `query:"include"`
	TraceId string  `header:"X-Trace-Id"`
}
```

**A field with none of those tags is ignored entirely** — it appears nowhere in
the document. Tags are the only opt-in, so unexported state and internal
bookkeeping fields on a DTO stay private by default.

A model made only of `param`/`query`/`header` fields produces no request body, so
the `GET` above is described with four parameters and nothing else.

## Required fields

A field is **required unless its type can be nil**. Pointers, slices, maps and
interfaces can be absent; anything else cannot. Where the type alone gets it
wrong, `,required` and `,optional` override it (`,omitempty` also means
optional).

```go
type LoginRequest struct {
	Email    string   `json:"email"`             // required  (not nillable)
	Remember *bool    `json:"remember"`          // optional  (nillable)
	Scopes   []string `json:"scopes"`            // optional  (nillable)
	Device   string   `json:"device,omitempty"`  // optional  (tag)
	Nonce    *string  `json:"nonce,required"`    // required  (tag beats the type)
}
```

The same rule drives parameters. Path parameters are always required, whatever
the field says — the spec allows nothing else.

## Security

Security is opt-in per route. A route that asks for nothing is public.

```go
api.AddSecurityScheme("bearerAuth", rest.BearerAuth())
api.AddSecurityScheme("apiKey", rest.APIKeyAuth("X-API-Key", "header"))
api.SetDefaultSecurity("bearerAuth")

api.Post("/api/user/authenticate/")                      // public: never asked
api.Get("/api/user/:id").RequireSecurity()               // the default, bearerAuth
api.Get("/api/user/").RequireSecurity("apiKey")          // overrides the default
api.Get("/api/report/").RequireScopes("oauth2", "read:reports")
```

`SetDefaultSecurity` is the template a bare `RequireSecurity()` draws from; it
does not secure anything by itself. No document-level `security` key is emitted,
deliberately: OpenAPI applies that to *every* operation, which would leave no way
to describe a public route without overriding it back to empty.

Schemes: `BearerAuth()`, `BasicAuth()`, `APIKeyAuth(name, in)`, `OIDCAuth(url)`,
and `CustomAuth(*openapi3.SecurityScheme)` for anything else, such as OAuth2
flows.

Naming a scheme that was never registered, or calling `RequireSecurity()` with no
default set, is an error rather than a silently wrong document.

## Output

```go
doc, err := api.OpenAPI()      // *openapi3.T, validated
data, err := api.JSON()        // indented JSON
data, err := api.YAML()        // YAML
err := api.WriteFile("openapi.yaml")  // .json, .yaml or .yml
```

The document is validated before it is returned, so a malformed spec surfaces
where you build it rather than in a consumer's UI.

## Route options

| Method | Effect |
| --- | --- |
| `HasRequestModel(m)` | Request body and parameters |
| `HasResponseModel(status, m)` | Response for a status code; call once per code |
| `HasSummary(s)` / `HasDescription(s)` | Documentation |
| `HasTags(...)` | Groups the operation |
| `HasOperationId(s)` | Sets `operationId` |
| `IsDeprecated()` | Marks the operation deprecated |
| `RequireSecurity(names...)` | Requires auth; no arguments uses the default |
| `RequireScopes(name, scopes...)` | Requires auth with OAuth2/OIDC scopes |

Verbs: `Get`, `Post`, `Put`, `Patch`, `Delete`, `Head`, `Options`. Echo-style
`:id` path segments are rewritten to `{id}` for you.

## Development

```bash
go test ./...            # run the tests
go test ./... -update    # refresh the golden files in testdata/
```
