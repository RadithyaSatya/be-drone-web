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

func (h *Handlers) ensureUavUpdateAccess(w http.ResponseWriter, r *http.Request, uavID int) bool {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return false
	}

	if deviceClaims, ok := auth.DeviceClaimsFromContext(r.Context()); ok && deviceClaims != nil {
		switch deviceClaims.ScopeType {
		case auth.DeviceScopeUav:
			if deviceClaims.UavID == nil || *deviceClaims.UavID != uavID {
				respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
				return false
			}
			return true
		case auth.DeviceScopeDocking:
			if deviceClaims.DockingID == nil || *deviceClaims.DockingID <= 0 {
				respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
				return false
			}
			resolvedUavID, err := h.resolveDockingUavID(*deviceClaims.DockingID)
			if err != nil {
				if err.Error() == "docking not found" {
					respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "Docking not found"})
					return false
				}
				log.Printf("Failed to resolve docking uav for update: %v", err)
				respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
				return false
			}
			if resolvedUavID != uavID {
				respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
				return false
			}
			return true
		default:
			respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return false
		}
	}

	if strings.TrimSpace(claims.Subject) == "device" {
		respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return false
	}

	userID, ok := h.requireUserID(w, r)
	if !ok {
		return false
	}
	return h.ensureUavOwner(w, r, userID, uavID)
}

func (h *Handlers) ensureDockingUpdateAccess(w http.ResponseWriter, r *http.Request, dockingID int) (int, bool) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return 0, false
	}

	if deviceClaims, ok := auth.DeviceClaimsFromContext(r.Context()); ok && deviceClaims != nil {
		uavID, exists := h.ensureDockingExists(w, r, dockingID)
		if !exists {
			return 0, false
		}

		switch deviceClaims.ScopeType {
		case auth.DeviceScopeUav:
			if deviceClaims.UavID == nil || *deviceClaims.UavID != uavID {
				respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
				return 0, false
			}
			return uavID, true
		case auth.DeviceScopeDocking:
			if deviceClaims.DockingID == nil || *deviceClaims.DockingID != dockingID {
				respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
				return 0, false
			}
			return uavID, true
		default:
			respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return 0, false
		}
	}

	if strings.TrimSpace(claims.Subject) == "device" {
		respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return 0, false
	}

	userID, ok := h.requireUserID(w, r)
	if !ok {
		return 0, false
	}
	return h.ensureDockingOwner(w, r, userID, dockingID)
}

func isDeviceScopedRequest(r *http.Request) bool {
	deviceClaims, ok := auth.DeviceClaimsFromContext(r.Context())
	return ok && deviceClaims != nil
}
