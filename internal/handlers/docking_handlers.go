package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

type dockingUpsertRequest struct {
	UavID        *int     `json:"uav_id"`
	Name         *string  `json:"name"`
	LocationName *string  `json:"location_name"`
	Latitude     *float64 `json:"latitude"`
	Longitude    *float64 `json:"longitude"`
	IsPrimary    *bool    `json:"is_primary"`
	IsActive     *bool    `json:"is_active"`
}

func (h *Handlers) CreateDocking(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	var req dockingUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
		return
	}

	if req.UavID == nil || *req.UavID <= 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "uav_id is required"})
		return
	}

	if !h.ensureUavOwner(w, r, userID, *req.UavID) {
		return
	}

	isPrimary := false
	if req.IsPrimary != nil {
		isPrimary = *req.IsPrimary
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	lat := nullFloat64Ptr(req.Latitude)
	lon := nullFloat64Ptr(req.Longitude)

	tx, err := h.DB.Begin()
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	var dockingID int
	err = tx.QueryRow(`
		INSERT INTO docking (uav_id, name, location_name, latitude, longitude, is_primary, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		*req.UavID,
		nullStringPtr(req.Name),
		nullStringPtr(req.LocationName),
		lat,
		lon,
		isPrimary,
		isActive,
	).Scan(&dockingID)
	if err != nil {
		log.Printf("Failed to create docking: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create docking"})
		return
	}

	if isPrimary {
		if _, err := tx.Exec(`UPDATE docking SET is_primary = false WHERE uav_id = $1 AND id <> $2`, *req.UavID, dockingID); err != nil {
			log.Printf("Failed to update primary docking: %v", err)
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update docking"})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Failed to commit docking create: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create docking"})
		return
	}

	respondWithJSON(w, http.StatusCreated, map[string]interface{}{
		"id":      dockingID,
		"message": "Docking created successfully",
	})
}

func (h *Handlers) UpdateDocking(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	vars := mux.Vars(r)
	dockingID, err := strconv.Atoi(vars["id"])
	if err != nil || dockingID <= 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid docking ID"})
		return
	}

	var req dockingUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
		return
	}
	if req.UavID != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "uav_id cannot be updated"})
		return
	}

	uavID, ok := h.ensureDockingOwner(w, r, userID, dockingID)
	if !ok {
		return
	}

	setClauses := []string{}
	args := []interface{}{}

	if req.Name != nil {
		args = append(args, nullStringPtr(req.Name))
		setClauses = append(setClauses, "name = $"+strconv.Itoa(len(args)))
	}
	if req.LocationName != nil {
		args = append(args, nullStringPtr(req.LocationName))
		setClauses = append(setClauses, "location_name = $"+strconv.Itoa(len(args)))
	}
	if req.Latitude != nil {
		args = append(args, nullFloat64Ptr(req.Latitude))
		setClauses = append(setClauses, "latitude = $"+strconv.Itoa(len(args)))
	}
	if req.Longitude != nil {
		args = append(args, nullFloat64Ptr(req.Longitude))
		setClauses = append(setClauses, "longitude = $"+strconv.Itoa(len(args)))
	}
	if req.IsPrimary != nil {
		args = append(args, *req.IsPrimary)
		setClauses = append(setClauses, "is_primary = $"+strconv.Itoa(len(args)))
	}
	if req.IsActive != nil {
		args = append(args, *req.IsActive)
		setClauses = append(setClauses, "is_active = $"+strconv.Itoa(len(args)))
	}

	if len(setClauses) == 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "No fields to update"})
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	args = append(args, dockingID)
	query := "UPDATE docking SET " + joinClauses(setClauses, ", ") + " WHERE id = $" + strconv.Itoa(len(args))
	if _, err := tx.Exec(query, args...); err != nil {
		log.Printf("Failed to update docking: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update docking"})
		return
	}

	if req.IsPrimary != nil && *req.IsPrimary {
		if _, err := tx.Exec(`UPDATE docking SET is_primary = false WHERE uav_id = $1 AND id <> $2`, uavID, dockingID); err != nil {
			log.Printf("Failed to update primary docking: %v", err)
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update docking"})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Failed to commit docking update: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update docking"})
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"message": "Docking updated successfully"})
}

func (h *Handlers) DeleteDocking(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	vars := mux.Vars(r)
	dockingID, err := strconv.Atoi(vars["id"])
	if err != nil || dockingID <= 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid docking ID"})
		return
	}

	if _, ok := h.ensureDockingOwner(w, r, userID, dockingID); !ok {
		return
	}

	if _, err := h.DB.Exec(`DELETE FROM docking WHERE id = $1`, dockingID); err != nil {
		log.Printf("Failed to delete docking: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete docking"})
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"message": "Docking deleted successfully"})
}

func (h *Handlers) ensureUavOwner(w http.ResponseWriter, r *http.Request, userID, uavID int) bool {
	var owner sql.NullInt32
	err := h.DB.QueryRow(`SELECT owner_id FROM uav WHERE id = $1 AND deleted_at IS NULL`, uavID).Scan(&owner)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "UAV not found"})
		return false
	}
	if err != nil {
		log.Printf("Failed to load UAV owner: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
		return false
	}
	if !owner.Valid || int(owner.Int32) != userID {
		respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return false
	}
	return true
}

func (h *Handlers) ensureDockingOwner(w http.ResponseWriter, r *http.Request, userID, dockingID int) (int, bool) {
	var uavID int
	var owner sql.NullInt32
	err := h.DB.QueryRow(`
		SELECT d.uav_id, u.owner_id
		FROM docking d
		JOIN uav u ON u.id = d.uav_id
		WHERE d.id = $1 AND u.deleted_at IS NULL`, dockingID).Scan(&uavID, &owner)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "Docking not found"})
		return 0, false
	}
	if err != nil {
		log.Printf("Failed to load docking owner: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
		return 0, false
	}
	if !owner.Valid || int(owner.Int32) != userID {
		respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return 0, false
	}
	return uavID, true
}

func (h *Handlers) ensureDockingExists(w http.ResponseWriter, r *http.Request, dockingID int) (int, bool) {
	var uavID int
	err := h.DB.QueryRow(`
		SELECT d.uav_id
		FROM docking d
		JOIN uav u ON u.id = d.uav_id
		WHERE d.id = $1 AND u.deleted_at IS NULL`, dockingID).Scan(&uavID)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "Docking not found"})
		return 0, false
	}
	if err != nil {
		log.Printf("Failed to load docking: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
		return 0, false
	}
	return uavID, true
}

func nullFloat64Ptr(value *float64) sql.NullFloat64 {
	if value == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *value, Valid: true}
}

func joinClauses(clauses []string, sep string) string {
	if len(clauses) == 0 {
		return ""
	}
	result := clauses[0]
	for i := 1; i < len(clauses); i++ {
		result += sep + clauses[i]
	}
	return result
}
