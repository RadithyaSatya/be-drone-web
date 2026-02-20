package ws

import (
	"errors"
	"net/http"
	"os"

	"xflight-backend/internal/auth"

	"github.com/golang-jwt/jwt/v5"
)

type JWTAuthenticator struct {
	secret []byte
}

func NewJWTAuthenticatorFromEnv() *JWTAuthenticator {
	secret := os.Getenv("JWT_SECRET")
	return &JWTAuthenticator{secret: []byte(secret)}
}

func (a *JWTAuthenticator) Authenticate(r *http.Request) error {
	if len(a.secret) == 0 {
		return errors.New("missing JWT secret")
	}

	tokenString := r.URL.Query().Get("token")
	if tokenString == "" {
		return errors.New("missing ws token")
	}

	claims := &jwt.RegisteredClaims{}
	if err := auth.ParseAndValidateJWT(a.secret, tokenString, claims); err != nil {
		return err
	}
	if !audienceContains(claims.Audience, "ws") {
		return errors.New("invalid token audience")
	}
	return nil
}

func audienceContains(audience []string, value string) bool {
	for _, aud := range audience {
		if aud == value {
			return true
		}
	}
	return false
}
