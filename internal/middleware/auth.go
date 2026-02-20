package middleware

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"xflight-backend/internal/auth"

	"github.com/golang-jwt/jwt/v5"
)

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deviceToken := strings.TrimSpace(r.Header.Get("X-Device-Token"))
		if deviceToken != "" {
			expected := strings.TrimSpace(os.Getenv("DEVICE_TOKEN"))
			if expected == "" {
				http.Error(w, "DEVICE_TOKEN not set", http.StatusInternalServerError)
				return
			}
			if subtle.ConstantTimeCompare([]byte(deviceToken), []byte(expected)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			claims := &jwt.RegisteredClaims{Subject: "device"}
			ctx := auth.WithClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		secret := os.Getenv("JWT_SECRET")
		if secret == "" {
			http.Error(w, "JWT_SECRET not set", http.StatusInternalServerError)
			return
		}

		bearer, err := auth.ParseBearerToken(r.Header.Get("Authorization"))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		claims := &jwt.RegisteredClaims{}
		if err := auth.ParseAndValidateJWT([]byte(secret), bearer, claims); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := auth.WithClaims(r.Context(), claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
