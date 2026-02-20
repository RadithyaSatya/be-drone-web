package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func ParseBearerToken(authorization string) (string, error) {
	if authorization == "" {
		return "", errors.New("missing authorization header")
	}
	parts := strings.SplitN(authorization, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", errors.New("invalid authorization header")
	}
	return parts[1], nil
}

func ParseAndValidateJWT(secret []byte, tokenString string, claims *jwt.RegisteredClaims) error {
	if len(secret) == 0 {
		return errors.New("missing JWT secret")
	}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	token, err := parser.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return secret, nil
	})
	if err != nil || !token.Valid {
		if err != nil {
			return fmt.Errorf("invalid token: %w", err)
		}
		return errors.New("invalid token")
	}
	if claims.ExpiresAt != nil && claims.ExpiresAt.Time.Before(time.Now()) {
		return errors.New("token expired")
	}
	return nil
}
