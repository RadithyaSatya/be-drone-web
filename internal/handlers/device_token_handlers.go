package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"xflight-backend/internal/auth"

	"github.com/gorilla/mux"
	"github.com/lib/pq"
)

type createDeviceTokenRequest struct {
	ExpiresAt *string `json:"expires_at"`
}

type deviceTokenResponse struct {
	Token     string    `json:"token"`
	TokenID   int64     `json:"token_id"`
	ScopeType string    `json:"scope_type"`
	UavID     *int      `json:"uav_id,omitempty"`
	DockingID *int      `json:"docking_id,omitempty"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type deviceContextResponse struct {
	TokenID       int64  `json:"token_id"`
	ScopeType     string `json:"scope_type"`
	UavID         *int   `json:"uav_id,omitempty"`
	DockingID     *int   `json:"docking_id,omitempty"`
	ResolvedUavID *int   `json:"resolved_uav_id,omitempty"`
}

func (h *Handlers) CreateUavDeviceToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	vars := mux.Vars(r)
	uavID, err := strconv.Atoi(vars["uav_id"])
	if err != nil || uavID <= 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid UAV ID"})
		return
	}

	if !h.ensureUavOwner(w, r, userID, uavID) {
		return
	}

	if ok := validateNoExpiryRequest(w, r); !ok {
		return
	}

	token, tokenID, createdAt, err := h.insertDeviceToken(auth.DeviceScopeUav, &uavID, nil)
	if err != nil {
		log.Printf("Failed to create UAV device token: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create device token"})
		return
	}

	respondWithJSON(w, http.StatusCreated, deviceTokenResponse{
		Token:     token,
		TokenID:   tokenID,
		ScopeType: auth.DeviceScopeUav,
		UavID:     &uavID,
		Message:   "Device token created (previous tokens revoked)",
		CreatedAt: createdAt,
	})
}

func (h *Handlers) CreateDockingDeviceToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	vars := mux.Vars(r)
	dockingID, err := strconv.Atoi(vars["docking_id"])
	if err != nil || dockingID <= 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid docking ID"})
		return
	}

	if _, ok := h.ensureDockingOwner(w, r, userID, dockingID); !ok {
		return
	}

	if ok := validateNoExpiryRequest(w, r); !ok {
		return
	}

	token, tokenID, createdAt, err := h.insertDeviceToken(auth.DeviceScopeDocking, nil, &dockingID)
	if err != nil {
		log.Printf("Failed to create docking device token: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create device token"})
		return
	}

	respondWithJSON(w, http.StatusCreated, deviceTokenResponse{
		Token:     token,
		TokenID:   tokenID,
		ScopeType: auth.DeviceScopeDocking,
		DockingID: &dockingID,
		Message:   "Device token created (previous tokens revoked)",
		CreatedAt: createdAt,
	})
}

func (h *Handlers) GetDeviceContext(w http.ResponseWriter, r *http.Request) {
	deviceClaims, ok := auth.DeviceClaimsFromContext(r.Context())
	if !ok || deviceClaims == nil {
		respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "device token required"})
		return
	}

	response := deviceContextResponse{
		TokenID:   deviceClaims.TokenID,
		ScopeType: deviceClaims.ScopeType,
		UavID:     deviceClaims.UavID,
		DockingID: deviceClaims.DockingID,
	}

	switch deviceClaims.ScopeType {
	case auth.DeviceScopeUav:
		if deviceClaims.UavID == nil || *deviceClaims.UavID <= 0 {
			respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		response.ResolvedUavID = deviceClaims.UavID
	case auth.DeviceScopeDocking:
		if deviceClaims.DockingID == nil || *deviceClaims.DockingID <= 0 {
			respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		uavID, err := h.resolveDockingUavID(*deviceClaims.DockingID)
		if err != nil {
			if err.Error() == "docking not found" {
				respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "Docking not found"})
				return
			}
			log.Printf("Failed to resolve device context docking uav_id: %v", err)
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
			return
		}
		response.ResolvedUavID = &uavID
	default:
		respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	respondWithJSON(w, http.StatusOK, response)
}

func validateNoExpiryRequest(w http.ResponseWriter, r *http.Request) bool {
	var req createDeviceTokenRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
			return false
		}
	}

	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "expires_at is not supported"})
		return false
	}

	return true
}

func (h *Handlers) insertDeviceToken(scopeType string, uavID *int, dockingID *int) (string, int64, time.Time, error) {
	pepper := strings.TrimSpace(os.Getenv("DEVICE_TOKEN_PEPPER"))
	if pepper == "" {
		return "", 0, time.Time{}, fmt.Errorf("DEVICE_TOKEN_PEPPER not set")
	}

	for i := 0; i < 3; i++ {
		token, err := generateDeviceToken()
		if err != nil {
			return "", 0, time.Time{}, err
		}
		hash := hashDeviceToken(token, pepper)

		tx, err := h.DB.Begin()
		if err != nil {
			return "", 0, time.Time{}, err
		}

		if err := revokeExistingDeviceTokens(tx, scopeType, uavID, dockingID); err != nil {
			_ = tx.Rollback()
			return "", 0, time.Time{}, err
		}

		var tokenID int64
		var createdAt time.Time
		err = tx.QueryRow(`
			INSERT INTO device_tokens (token_hash, scope_type, uav_id, docking_id)
			VALUES ($1, $2, $3, $4)
			RETURNING id, created_at`,
			hash,
			scopeType,
			uavID,
			dockingID,
		).Scan(&tokenID, &createdAt)
		if err != nil {
			if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
				_ = tx.Rollback()
				continue
			}
			_ = tx.Rollback()
			return "", 0, time.Time{}, err
		}

		if err := tx.Commit(); err != nil {
			return "", 0, time.Time{}, err
		}

		return token, tokenID, createdAt, nil
	}

	return "", 0, time.Time{}, fmt.Errorf("failed to generate unique token")
}

func generateDeviceToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashDeviceToken(token, pepper string) string {
	sum := sha256.Sum256([]byte(token + pepper))
	return hex.EncodeToString(sum[:])
}

func revokeExistingDeviceTokens(tx *sql.Tx, scopeType string, uavID *int, dockingID *int) error {
	switch scopeType {
	case auth.DeviceScopeUav:
		if uavID == nil {
			return fmt.Errorf("uav_id is required")
		}
		_, err := tx.Exec(
			`UPDATE device_tokens SET revoked_at = NOW() WHERE scope_type = $1 AND uav_id = $2 AND revoked_at IS NULL`,
			scopeType,
			*uavID,
		)
		return err
	case auth.DeviceScopeDocking:
		if dockingID == nil {
			return fmt.Errorf("docking_id is required")
		}
		_, err := tx.Exec(
			`UPDATE device_tokens SET revoked_at = NOW() WHERE scope_type = $1 AND docking_id = $2 AND revoked_at IS NULL`,
			scopeType,
			*dockingID,
		)
		return err
	default:
		return fmt.Errorf("invalid scope_type")
	}
}
