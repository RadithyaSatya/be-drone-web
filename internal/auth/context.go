package auth

import (
	"context"

	"github.com/golang-jwt/jwt/v5"
)

type claimsKey struct{}
type deviceClaimsKey struct{}

const (
	DeviceScopeUav     = "uav"
	DeviceScopeDocking = "docking"
)

type DeviceClaims struct {
	TokenID   int64
	ScopeType string
	UavID     *int
	DockingID *int
}

func WithClaims(ctx context.Context, claims *jwt.RegisteredClaims) context.Context {
	return context.WithValue(ctx, claimsKey{}, claims)
}

func ClaimsFromContext(ctx context.Context) (*jwt.RegisteredClaims, bool) {
	claims, ok := ctx.Value(claimsKey{}).(*jwt.RegisteredClaims)
	return claims, ok
}

func WithDeviceClaims(ctx context.Context, claims *DeviceClaims) context.Context {
	return context.WithValue(ctx, deviceClaimsKey{}, claims)
}

func DeviceClaimsFromContext(ctx context.Context) (*DeviceClaims, bool) {
	claims, ok := ctx.Value(deviceClaimsKey{}).(*DeviceClaims)
	return claims, ok
}
