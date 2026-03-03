package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"xflight-backend/internal/auth"

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
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || strings.TrimSpace(req.Password) == "" {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}

	var storedHash string
	var username string
	query := `SELECT username, password_hash FROM users WHERE username = $1 OR email = $1 LIMIT 1`
	err := h.DB.QueryRow(query, req.Username).Scan(&username, &storedHash)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load user"})
		return
	}

	ok, err := verifyPassword(req.Password, storedHash)
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid password hash"})
		return
	}
	if !ok {
		respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "JWT_SECRET not set"})
		return
	}

	claims := jwt.RegisteredClaims{
		Subject:  username,
		IssuedAt: jwt.NewNumericDate(time.Now().UTC()),
		Issuer:   "xflight-backend",
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

type logoutResponse struct {
	Message string `json:"message"`
}

func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	deviceClaims, hasDevice := auth.DeviceClaimsFromContext(r.Context())
	if hasDevice && deviceClaims != nil {
		if h.DB == nil {
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "database not configured"})
			return
		}

		if _, err := h.DB.Exec(`UPDATE device_tokens SET revoked_at = NOW() WHERE id = $1 AND revoked_at IS NULL`, deviceClaims.TokenID); err != nil {
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to revoke device token"})
			return
		}

		respondWithJSON(w, http.StatusOK, logoutResponse{Message: "device token revoked"})
		return
	}

	respondWithJSON(w, http.StatusOK, logoutResponse{Message: "logout ok"})
}
