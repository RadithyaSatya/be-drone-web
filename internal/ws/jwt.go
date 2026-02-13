package ws

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

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
	authorization := r.Header.Get("Authorization")
	if authorization == "" {
		return errors.New("missing authorization header")
	}
	parts := strings.SplitN(authorization, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return errors.New("invalid authorization header")
	}
	tokenString := parts[1]

	claims := jwt.RegisteredClaims{}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	token, err := parser.ParseWithClaims(tokenString, &claims, func(token *jwt.Token) (interface{}, error) {
		return a.secret, nil
	})
	if err != nil || !token.Valid {
		return fmt.Errorf("invalid token: %w", err)
	}
	if claims.ExpiresAt != nil && claims.ExpiresAt.Time.Before(time.Now()) {
		return errors.New("token expired")
	}
	return nil
}
