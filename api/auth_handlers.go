package api

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string `json:"token"`
}

func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	user := getEnvOrDefault("AUTH_USERNAME", "admin")
	pass := getEnvOrDefault("AUTH_PASSWORD", "admin123")
	if req.Username != user || req.Password != pass {
		respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "JWT_SECRET not set"})
		return
	}

	claims := jwt.RegisteredClaims{
		Subject:   req.Username,
		IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
		Issuer:    "xflight-backend",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to sign token"})
		return
	}

	respondWithJSON(w, http.StatusOK, loginResponse{
		Token: signed,
	})
}

func getEnvOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
