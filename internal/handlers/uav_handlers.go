package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"xflight-backend/internal/models"

	"github.com/gorilla/mux"
)

const (
	defaultUavPage  = 1
	defaultUavLimit = 20
	maxUavLimit     = 100
)

type uavUpsertRequest struct {
	SerialNumber     *string `json:"serial_number"`
	Name             *string `json:"name"`
	Model            *string `json:"model"`
	FirmwareVersion  *string `json:"firmware_version"`
	CameraSpec       *string `json:"camera_spec"`
	ImageURL         *string `json:"image_url"`
	MaxRangeMeter    *int    `json:"max_range_meter"`
	MaxFlightTimeMin *int    `json:"max_flight_time_min"`
	IsActive         *bool   `json:"is_active"`
}

type createUavResponse struct {
	ID      int    `json:"id"`
	Message string `json:"message"`
}

type assignUavRequest struct {
	SerialNumber string `json:"serial_number"`
}

type uavDropdownItem struct {
	ID         int     `json:"id"`
	Name       *string `json:"name"`
	CameraSpec *string `json:"camera_spec"`
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (h *Handlers) CreateUAV(w http.ResponseWriter, r *http.Request) {
	var req uavUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
		return
	}

	maxRange, err := nullInt32Ptr(req.MaxRangeMeter)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "max_range_meter must be >= 0"})
		return
	}
	maxFlight, err := nullInt32Ptr(req.MaxFlightTimeMin)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "max_flight_time_min must be >= 0"})
		return
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	var id int
	var createdAt sql.NullTime
	query := `
		INSERT INTO uav (serial_number, name, model, firmware_version, camera_spec, image_url, max_range_meter, max_flight_time_min, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at`
	err = h.DB.QueryRow(
		query,
		nullStringPtr(req.SerialNumber),
		nullStringPtr(req.Name),
		nullStringPtr(req.Model),
		nullStringPtr(req.FirmwareVersion),
		nullStringPtr(req.CameraSpec),
		nullStringPtr(req.ImageURL),
		maxRange,
		maxFlight,
		isActive,
	).Scan(&id, &createdAt)
	if err != nil {
		log.Printf("Failed to create UAV: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create UAV"})
		return
	}

	respondWithJSON(w, http.StatusCreated, createUavResponse{
		ID:      id,
		Message: "UAV created successfully",
	})
}

func (h *Handlers) ListUAVs(w http.ResponseWriter, r *http.Request) {
	page, limit := parsePagination(r, defaultUavPage, defaultUavLimit, maxUavLimit)
	offset := (page - 1) * limit

	var total int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM uav WHERE deleted_at IS NULL`).Scan(&total); err != nil {
		log.Printf("Error counting UAVs: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load UAVs"})
		return
	}

	rows, err := h.DB.Query(`
		SELECT u.id, u.serial_number, u.name, u.model, u.firmware_version, u.camera_spec, u.image_url,
		       u.max_range_meter, u.max_flight_time_min, u.owner_id, u.is_active, u.created_at,
		       s.battery_percent, s.is_connected, s.is_in_flight, s.is_docked, s.last_heartbeat
		FROM uav u
		LEFT JOIN uav_status s ON s.uav_id = u.id
		WHERE u.deleted_at IS NULL
		ORDER BY u.created_at DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		log.Printf("Error querying UAVs: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load UAVs"})
		return
	}
	defer rows.Close()

	items := []models.Uav{}
	for rows.Next() {
		item, err := scanUavRow(rows)
		if err != nil {
			log.Printf("Error scanning UAV: %v", err)
			continue
		}
		items = append(items, item)
	}

	totalPages := 0
	if total > 0 {
		totalPages = (total + limit - 1) / limit
	}
	hasNext := totalPages > 0 && page < totalPages
	hasPrev := totalPages > 0 && page > 1
	var nextPage *int
	var prevPage *int
	if hasNext {
		n := page + 1
		nextPage = &n
	}
	if hasPrev {
		p := page - 1
		prevPage = &p
	}

	respondWithJSON(w, http.StatusOK, models.UavListResponse{
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
		HasNext:    hasNext,
		HasPrev:    hasPrev,
		NextPage:   nextPage,
		PrevPage:   prevPage,
		Items:      items,
	})
}

func (h *Handlers) ListMyUAVDropdown(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	rows, err := h.DB.Query(`
		SELECT id, name, camera_spec
		FROM uav
		WHERE deleted_at IS NULL AND owner_id = $1
		ORDER BY created_at DESC`, userID)
	if err != nil {
		log.Printf("Error querying UAV dropdown for user %d: %v", userID, err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load UAVs"})
		return
	}
	defer rows.Close()

	items := []uavDropdownItem{}
	for rows.Next() {
		var item uavDropdownItem
		var name sql.NullString
		var camera sql.NullString
		if err := rows.Scan(&item.ID, &name, &camera); err != nil {
			log.Printf("Error scanning UAV dropdown for user %d: %v", userID, err)
			continue
		}
		if name.Valid {
			value := name.String
			item.Name = &value
		}
		if camera.Valid {
			value := camera.String
			item.CameraSpec = &value
		}
		items = append(items, item)
	}

	respondWithJSON(w, http.StatusOK, items)
}

func (h *Handlers) ListMyUAVs(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	page, limit := parsePagination(r, defaultUavPage, defaultUavLimit, maxUavLimit)
	offset := (page - 1) * limit

	var total int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM uav WHERE deleted_at IS NULL AND owner_id = $1`, userID).Scan(&total); err != nil {
		log.Printf("Error counting UAVs for user %d: %v", userID, err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load UAVs"})
		return
	}

	rows, err := h.DB.Query(`
		SELECT u.id, u.serial_number, u.name, u.model, u.firmware_version, u.camera_spec, u.image_url,
		       u.max_range_meter, u.max_flight_time_min, u.owner_id, u.is_active, u.created_at,
		       s.battery_percent, s.is_connected, s.is_in_flight, s.is_docked, s.last_heartbeat
		FROM uav u
		LEFT JOIN uav_status s ON s.uav_id = u.id
		WHERE u.deleted_at IS NULL AND u.owner_id = $1
		ORDER BY u.created_at DESC
		LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		log.Printf("Error querying UAVs for user %d: %v", userID, err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load UAVs"})
		return
	}
	defer rows.Close()

	items := []models.Uav{}
	for rows.Next() {
		item, err := scanUavRow(rows)
		if err != nil {
			log.Printf("Error scanning UAV for user %d: %v", userID, err)
			continue
		}
		items = append(items, item)
	}

	totalPages := 0
	if total > 0 {
		totalPages = (total + limit - 1) / limit
	}
	hasNext := totalPages > 0 && page < totalPages
	hasPrev := totalPages > 0 && page > 1
	var nextPage *int
	var prevPage *int
	if hasNext {
		n := page + 1
		nextPage = &n
	}
	if hasPrev {
		p := page - 1
		prevPage = &p
	}

	respondWithJSON(w, http.StatusOK, models.UavListResponse{
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
		HasNext:    hasNext,
		HasPrev:    hasPrev,
		NextPage:   nextPage,
		PrevPage:   prevPage,
		Items:      items,
	})
}

func (h *Handlers) GetUAVByID(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil || id <= 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid UAV ID"})
		return
	}

	row := h.DB.QueryRow(`
		SELECT u.id, u.serial_number, u.name, u.model, u.firmware_version, u.camera_spec, u.image_url,
		       u.max_range_meter, u.max_flight_time_min, u.owner_id, u.is_active, u.created_at,
		       s.battery_percent, s.is_connected, s.is_in_flight, s.is_docked, s.last_heartbeat
		FROM uav u
		LEFT JOIN uav_status s ON s.uav_id = u.id
		WHERE u.id = $1 AND u.deleted_at IS NULL`, id)
	item, err := scanUavRow(row)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "UAV not found"})
		return
	}
	if err != nil {
		log.Printf("Error scanning UAV: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load UAV"})
		return
	}

	dockings, err := h.loadDockingsByUav(item.ID)
	if err != nil {
		log.Printf("Error loading dockings for UAV %d: %v", item.ID, err)
	} else if len(dockings) > 0 {
		item.Dockings = dockings
	}

	respondWithJSON(w, http.StatusOK, item)
}

func (h *Handlers) UpdateUAV(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil || id <= 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid UAV ID"})
		return
	}

	var req uavUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
		return
	}

	setClauses := []string{}
	args := []interface{}{}

	if req.SerialNumber != nil {
		setClauses = append(setClauses, fmt.Sprintf("serial_number = $%d", len(args)+1))
		args = append(args, nullStringPtr(req.SerialNumber))
	}
	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", len(args)+1))
		args = append(args, nullStringPtr(req.Name))
	}
	if req.Model != nil {
		setClauses = append(setClauses, fmt.Sprintf("model = $%d", len(args)+1))
		args = append(args, nullStringPtr(req.Model))
	}
	if req.FirmwareVersion != nil {
		setClauses = append(setClauses, fmt.Sprintf("firmware_version = $%d", len(args)+1))
		args = append(args, nullStringPtr(req.FirmwareVersion))
	}
	if req.CameraSpec != nil {
		setClauses = append(setClauses, fmt.Sprintf("camera_spec = $%d", len(args)+1))
		args = append(args, nullStringPtr(req.CameraSpec))
	}
	if req.ImageURL != nil {
		setClauses = append(setClauses, fmt.Sprintf("image_url = $%d", len(args)+1))
		args = append(args, nullStringPtr(req.ImageURL))
	}
	if req.MaxRangeMeter != nil {
		if *req.MaxRangeMeter < 0 {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "max_range_meter must be >= 0"})
			return
		}
		setClauses = append(setClauses, fmt.Sprintf("max_range_meter = $%d", len(args)+1))
		args = append(args, *req.MaxRangeMeter)
	}
	if req.MaxFlightTimeMin != nil {
		if *req.MaxFlightTimeMin < 0 {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "max_flight_time_min must be >= 0"})
			return
		}
		setClauses = append(setClauses, fmt.Sprintf("max_flight_time_min = $%d", len(args)+1))
		args = append(args, *req.MaxFlightTimeMin)
	}
	if req.IsActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_active = $%d", len(args)+1))
		args = append(args, *req.IsActive)
	}

	if len(setClauses) == 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "No fields provided to update"})
		return
	}

	query := fmt.Sprintf(`
		WITH updated AS (
			UPDATE uav
			SET %s
			WHERE id = $%d AND deleted_at IS NULL
			RETURNING id, serial_number, name, model, firmware_version, camera_spec, image_url,
			          max_range_meter, max_flight_time_min, owner_id, is_active, created_at
		)
		SELECT updated.id, updated.serial_number, updated.name, updated.model, updated.firmware_version, updated.camera_spec, updated.image_url,
		       updated.max_range_meter, updated.max_flight_time_min, updated.owner_id, updated.is_active, updated.created_at,
		       s.battery_percent, s.is_connected, s.is_in_flight, s.is_docked, s.last_heartbeat
		FROM updated
		LEFT JOIN uav_status s ON s.uav_id = updated.id`,
		strings.Join(setClauses, ", "),
		len(args)+1,
	)
	args = append(args, id)

	row := h.DB.QueryRow(query, args...)
	item, err := scanUavRow(row)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "UAV not found"})
		return
	}
	if err != nil {
		log.Printf("Failed to update UAV: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update UAV"})
		return
	}

	respondWithJSON(w, http.StatusOK, item)
}

func (h *Handlers) AssignUAVToUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	var req assignUavRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
		return
	}

	serial := strings.TrimSpace(req.SerialNumber)
	if serial == "" {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "serial_number is required"})
		return
	}

	row := h.DB.QueryRow(`
		WITH updated AS (
			UPDATE uav
			SET owner_id = $1
			WHERE serial_number = $2
			  AND deleted_at IS NULL
			  AND (owner_id IS NULL OR owner_id = $1)
			RETURNING id, serial_number, name, model, firmware_version, camera_spec, image_url,
			          max_range_meter, max_flight_time_min, owner_id, is_active, created_at
		)
		SELECT updated.id, updated.serial_number, updated.name, updated.model, updated.firmware_version, updated.camera_spec, updated.image_url,
		       updated.max_range_meter, updated.max_flight_time_min, updated.owner_id, updated.is_active, updated.created_at,
		       s.battery_percent, s.is_connected, s.is_in_flight, s.is_docked, s.last_heartbeat
		FROM updated
		LEFT JOIN uav_status s ON s.uav_id = updated.id`, userID, serial)

	item, err := scanUavRow(row)
	if err == sql.ErrNoRows {
		var owner sql.NullInt32
		err = h.DB.QueryRow(`
			SELECT owner_id
			FROM uav
			WHERE serial_number = $1 AND deleted_at IS NULL`, serial).Scan(&owner)
		if err == sql.ErrNoRows {
			respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "UAV not found"})
			return
		}
		if err != nil {
			log.Printf("Failed to load UAV owner: %v", err)
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to assign UAV"})
			return
		}
		if owner.Valid && int(owner.Int32) != userID {
			respondWithJSON(w, http.StatusConflict, map[string]string{"error": "UAV already assigned to another user"})
			return
		}
		respondWithJSON(w, http.StatusConflict, map[string]string{"error": "UAV assignment failed"})
		return
	}
	if err != nil {
		log.Printf("Failed to assign UAV: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to assign UAV"})
		return
	}

	respondWithJSON(w, http.StatusOK, item)
}

func (h *Handlers) DeleteUAV(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil || id <= 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid UAV ID"})
		return
	}

	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	result, err := h.DB.Exec(`
		UPDATE uav
		SET deleted_at = NOW(), deleted_by = $2, is_active = false
		WHERE id = $1 AND deleted_at IS NULL`, id, userID)
	if err != nil {
		log.Printf("Failed to delete UAV: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete UAV"})
		return
	}
	rowsAffected, err := result.RowsAffected()
	if err == nil && rowsAffected == 0 {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "UAV not found"})
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"message": "UAV deleted successfully"})
}

func scanUavRow(scanner rowScanner) (models.Uav, error) {
	var item models.Uav
	var serial sql.NullString
	var name sql.NullString
	var model sql.NullString
	var firmware sql.NullString
	var camera sql.NullString
	var imageURL sql.NullString
	var maxRange sql.NullInt32
	var maxFlight sql.NullInt32
	var owner sql.NullInt32
	var batteryPercent sql.NullInt32
	var isConnected sql.NullBool
	var isInFlight sql.NullBool
	var isDocked sql.NullBool
	var lastHeartbeat sql.NullTime

	err := scanner.Scan(
		&item.ID,
		&serial,
		&name,
		&model,
		&firmware,
		&camera,
		&imageURL,
		&maxRange,
		&maxFlight,
		&owner,
		&item.IsActive,
		&item.CreatedAt,
		&batteryPercent,
		&isConnected,
		&isInFlight,
		&isDocked,
		&lastHeartbeat,
	)
	if err != nil {
		return item, err
	}

	if serial.Valid {
		value := serial.String
		item.SerialNumber = &value
	}
	if name.Valid {
		value := name.String
		item.Name = &value
	}
	if model.Valid {
		value := model.String
		item.Model = &value
	}
	if firmware.Valid {
		value := firmware.String
		item.FirmwareVersion = &value
	}
	if camera.Valid {
		value := camera.String
		item.CameraSpec = &value
	}
	if imageURL.Valid {
		value := imageURL.String
		item.ImageURL = &value
	}
	if maxRange.Valid {
		value := int(maxRange.Int32)
		item.MaxRangeMeter = &value
	}
	if maxFlight.Valid {
		value := int(maxFlight.Int32)
		item.MaxFlightTimeMin = &value
	}
	if owner.Valid {
		value := int(owner.Int32)
		item.OwnerID = &value
	}
	if batteryPercent.Valid || isConnected.Valid || isInFlight.Valid || isDocked.Valid || lastHeartbeat.Valid {
		status := models.UavStatus{}
		if batteryPercent.Valid {
			value := int(batteryPercent.Int32)
			status.BatteryPercent = &value
		}
		if isConnected.Valid {
			value := isConnected.Bool
			status.IsConnected = &value
		}
		if isInFlight.Valid {
			value := isInFlight.Bool
			status.IsInFlight = &value
		}
		if isDocked.Valid {
			value := isDocked.Bool
			status.IsDocked = &value
		}
		if lastHeartbeat.Valid {
			value := lastHeartbeat.Time
			status.LastHeartbeat = &value
		}
		item.Status = &status
	}

	return item, nil
}

func nullStringPtr(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: trimmed, Valid: true}
}

func nullInt32Ptr(value *int) (sql.NullInt32, error) {
	if value == nil {
		return sql.NullInt32{}, nil
	}
	if *value < 0 {
		return sql.NullInt32{}, fmt.Errorf("value must be >= 0")
	}
	return sql.NullInt32{Int32: int32(*value), Valid: true}, nil
}
