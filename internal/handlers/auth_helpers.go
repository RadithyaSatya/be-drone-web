package handlers

import (
	"database/sql"
	"log"
	"net/http"
	"strings"

	"xflight-backend/internal/auth"
)

func (h *Handlers) requireUserID(w http.ResponseWriter, r *http.Request) (int, bool) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return 0, false
	}

	subject := strings.TrimSpace(claims.Subject)
	if subject == "" || subject == "device" {
		respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return 0, false
	}

	var userID int
	if err := h.DB.QueryRow(`SELECT id FROM users WHERE username = $1 OR email = $1`, subject).Scan(&userID); err != nil {
		if err == sql.ErrNoRows {
			respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return 0, false
		}
		log.Printf("Failed to load user for subject %q: %v", subject, err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load user"})
		return 0, false
	}

	return userID, true
}
