package handlers

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"xflight-backend/internal/auth"

	"github.com/golang-jwt/jwt/v5"
)

type wsTokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
}

func (h *Handlers) GenerateWSToken(w http.ResponseWriter, r *http.Request) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "JWT_SECRET not set"})
		return
	}

	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	ttlSeconds := getEnvOrDefaultInt("WS_TOKEN_TTL_SECONDS", 120)
	if ttlSeconds <= 0 {
		ttlSeconds = 120
	}

	now := time.Now().UTC()
	wsClaims := jwt.RegisteredClaims{
		Subject:   claims.Subject,
		Issuer:    "xflight-backend",
		Audience:  []string{"ws"},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(ttlSeconds) * time.Second)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, wsClaims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to sign token"})
		return
	}

	respondWithJSON(w, http.StatusOK, wsTokenResponse{
		Token:     signed,
		ExpiresIn: ttlSeconds,
	})
}

func getEnvOrDefaultInt(key string, fallback int) int {
	value := getEnvOrDefault(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
