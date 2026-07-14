package main

import (
	"fmt"
	"log"
	"net/http"

	rest "github.com/struckchure/go-rest"
	"github.com/struckchure/go-rest/example/dto"
)

func main() {
	api := rest.New(
		rest.WithTitle("User API"),
		rest.WithVersion("1.0.0"),
		rest.WithServer("https://api.example.com", "production"),
	)

	api.AddSecurityScheme("bearerAuth", rest.BearerAuth())
	api.AddSecurityScheme("apiKey", rest.APIKeyAuth("X-API-Key", "header"))
	api.SetDefaultSecurity("bearerAuth")

	api.Post("/api/user/authenticate/").
		HasSummary("Authenticate a user").
		HasTags("user").
		HasRequestModel(rest.ModelOf[dto.UserAuthenticateRequestDto]()).
		HasResponseModel(http.StatusOK, rest.ModelOf[dto.UserAuthenticateResponseDto]())

	api.Post("/api/user/refresh-tokens/").
		HasTags("user").
		HasRequestModel(rest.ModelOf[dto.UserRefreshTokenRequestDto]()).
		HasResponseModel(http.StatusOK, rest.ModelOf[dto.UserRefreshTokenResponseDto]())

	// RequireSecurity() with no arguments uses the default, bearerAuth. The
	// request model is all param/query/header, so this has no request body.
	api.Get("/api/user/:id").
		RequireSecurity().
		HasTags("user").
		HasRequestModel(rest.ModelOf[dto.UserGetRequestDto]()).
		HasResponseModel(http.StatusOK, rest.ModelOf[dto.UserDto]())

	// Overrides the default with a different scheme. A slice model renders as
	// an array of $ref.
	api.Get("/api/user/").
		RequireSecurity("apiKey").
		HasTags("user").
		HasResponseModel(http.StatusOK, rest.ModelOf[[]dto.UserDto]())

	out, err := api.YAML()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(string(out))
}
