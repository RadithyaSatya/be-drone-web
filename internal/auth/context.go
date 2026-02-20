package auth

import (
	"context"

	"github.com/golang-jwt/jwt/v5"
)

type claimsKey struct{}

func WithClaims(ctx context.Context, claims *jwt.RegisteredClaims) context.Context {
	return context.WithValue(ctx, claimsKey{}, claims)
}

func ClaimsFromContext(ctx context.Context) (*jwt.RegisteredClaims, bool) {
	claims, ok := ctx.Value(claimsKey{}).(*jwt.RegisteredClaims)
	return claims, ok
}
