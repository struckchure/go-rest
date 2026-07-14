package dto

import "time"

type UserAuthenticateRequestDto struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type UserAuthenticateResponseDto struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

type UserRefreshTokenRequestDto struct {
	RefreshToken string `json:"refreshToken"`
}

type UserRefreshTokenResponseDto struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

// UserGetRequestDto binds Echo-style from three different places at once, and
// carries one untagged field that must not leak into the document.
type UserGetRequestDto struct {
	Id      string  `param:"id"`                   // path: always required
	Page    int     `query:"page"`                 // not nillable: required
	Include *string `query:"include"`              // nillable: optional
	TraceId string  `header:"X-Trace-Id,optional"` // required by type, optional by tag

	InternalOnly string // no tag: invisible to the spec
}

type UserDto struct {
	Id        string    `json:"id"`
	Email     string    `json:"email"`
	Nickname  *string   `json:"nickname,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}
