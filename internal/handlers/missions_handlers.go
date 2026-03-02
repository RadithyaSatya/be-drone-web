package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"xflight-backend/internal/models"

	"github.com/gorilla/mux"
)

const (
	defaultMissionPage  = 1
	defaultMissionLimit = 20
	maxMissionLimit     = 100
)

func (h *Handlers) SaveNewMission(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	var req models.FullMissionRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
		return
	}
	req.UserID = userID
	scheduleTime, ok := parseSchedule(req.Schedule)
	if !ok {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid schedule format"})
		return
	}
	now := time.Now().In(scheduleTime.Location())
	if !scheduleTime.After(now) {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Schedule must be in the future"})
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	var missionID int
	missionSQL := `
		INSERT INTO missions (user_id, uav_id, mission_name, schedule, is_recurring, status, created_at) 
        VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`

	err = tx.QueryRow(
		missionSQL,
		req.UserID,
		req.UavID,
		req.MissionName,
		req.Schedule,
		req.IsRecurring,
		req.Status,
		time.Now().UTC(),
	).Scan(&missionID)

	if err != nil {
		log.Printf("Error inserting mission: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save mission details"})
		return
	}
	waypointSQL := `
        INSERT INTO waypoints (mission_id, sequence_order, latitude, longitude, altitude, action, action_duration) 
        VALUES ($1, $2, $3, $4, $5, $6, $7)`

	for _, wp := range req.Waypoints {
		_, err = tx.Exec(waypointSQL,
			missionID,
			wp.SequenceOrder,
			wp.Latitude,
			wp.Longitude,
			wp.Altitude,
			wp.Action,
			wp.ActionDuration)
		if err != nil {
			log.Printf("Error inserting waypoint: %v", err)
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save waypoint details"})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		log.Printf("Error committing transaction: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Transaction commit failed"})
		return
	}

	respondWithJSON(w, http.StatusCreated, map[string]interface{}{"id": missionID, "message": "Mission saved successfully"})
}

func (h *Handlers) GetAllMissions(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, status, created_at FROM missions WHERE deleted_at IS NULL ORDER BY created_at DESC`)
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	missions := []models.Mission{}
	for rows.Next() {
		var m models.Mission
		if err := rows.Scan(&m.ID, &m.UserID, &m.UavID, &m.MissionName, &m.Schedule, &m.IsRecurring, &m.Status, &m.Timestamp); err != nil {
			log.Printf("Error scanning mission: %v", err)
			continue
		}
		missions = append(missions, m)
	}

	respondWithJSON(w, http.StatusOK, missions)
}

func (h *Handlers) GetMissionsForCurrentUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	uavIDStr := r.URL.Query().Get("uav_id")
	page, limit := parsePagination(r, defaultMissionPage, defaultMissionLimit, maxMissionLimit)
	offset := (page - 1) * limit

	countQuery := `
		SELECT COUNT(*)
		FROM missions
		WHERE user_id = $1 AND deleted_at IS NULL`
	countArgs := []interface{}{userID}

	query := `
        SELECT m.id, m.user_id, m.uav_id, m.mission_name, m.schedule, m.is_recurring, m.status, m.created_at, m.deleted_at,
		       COALESCE(COUNT(w.id), 0) AS waypoint_count,
			   u.id, u.serial_number, u.name, u.model, u.firmware_version, u.camera_spec, u.image_url,
			   u.max_range_meter, u.max_flight_time_min, u.owner_id, u.is_active, u.created_at
        FROM missions m
		LEFT JOIN waypoints w ON w.mission_id = m.id
		LEFT JOIN uav u ON u.id = m.uav_id
        WHERE m.user_id = $1 AND m.deleted_at IS NULL`
	args := []interface{}{userID}

	if uavIDStr != "" {
		uavID, err := strconv.Atoi(uavIDStr)
		if err != nil {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid UAV ID"})
			return
		}
		query += " AND m.uav_id = $2"
		args = append(args, uavID)
		countQuery += " AND uav_id = $2"
		countArgs = append(countArgs, uavID)
	}

	query += `
		GROUP BY m.id, m.user_id, m.uav_id, m.mission_name, m.schedule, m.is_recurring, m.status, m.created_at, m.deleted_at,
		         u.id, u.serial_number, u.name, u.model, u.firmware_version, u.camera_spec, u.image_url,
				 u.max_range_meter, u.max_flight_time_min, u.owner_id, u.is_active, u.created_at
		ORDER BY m.created_at DESC
		LIMIT $%d OFFSET $%d`

	args = append(args, limit, offset)
	query = fmt.Sprintf(query, len(args)-1, len(args))

	var total int
	if err := h.DB.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
		log.Printf("Error counting missions: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load missions"})
		return
	}

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	missions := []models.MissionListItem{}
	for rows.Next() {
		var m models.MissionListItem
		var waypointCount int
		var deletedAt sql.NullTime

		var uavID sql.NullInt32
		var uavSerial sql.NullString
		var uavName sql.NullString
		var uavModel sql.NullString
		var uavFirmware sql.NullString
		var uavCamera sql.NullString
		var uavImageURL sql.NullString
		var uavMaxRange sql.NullInt32
		var uavMaxFlight sql.NullInt32
		var uavOwner sql.NullInt32
		var uavIsActive sql.NullBool
		var uavCreatedAt sql.NullTime

		if err := rows.Scan(
			&m.ID,
			&m.UserID,
			&m.UavID,
			&m.MissionName,
			&m.Schedule,
			&m.IsRecurring,
			&m.Status,
			&m.Timestamp,
			&deletedAt,
			&waypointCount,
			&uavID,
			&uavSerial,
			&uavName,
			&uavModel,
			&uavFirmware,
			&uavCamera,
			&uavImageURL,
			&uavMaxRange,
			&uavMaxFlight,
			&uavOwner,
			&uavIsActive,
			&uavCreatedAt,
		); err != nil {
			log.Printf("Error scanning mission: %v", err)
			continue
		}
		m.WaypointCount = &waypointCount
		if deletedAt.Valid {
			value := deletedAt.Time
			m.DeletedAt = &value
		}

		if uavID.Valid {
			uav := models.Uav{
				ID:        int(uavID.Int32),
				IsActive:  uavIsActive.Bool,
				CreatedAt: uavCreatedAt.Time,
			}
			if uavSerial.Valid {
				value := uavSerial.String
				uav.SerialNumber = &value
			}
			if uavName.Valid {
				value := uavName.String
				uav.Name = &value
			}
			if uavModel.Valid {
				value := uavModel.String
				uav.Model = &value
			}
			if uavFirmware.Valid {
				value := uavFirmware.String
				uav.FirmwareVersion = &value
			}
			if uavCamera.Valid {
				value := uavCamera.String
				uav.CameraSpec = &value
			}
			if uavImageURL.Valid {
				value := uavImageURL.String
				uav.ImageURL = &value
			}
			if uavMaxRange.Valid {
				value := int(uavMaxRange.Int32)
				uav.MaxRangeMeter = &value
			}
			if uavMaxFlight.Valid {
				value := int(uavMaxFlight.Int32)
				uav.MaxFlightTimeMin = &value
			}
			if uavOwner.Valid {
				value := int(uavOwner.Int32)
				uav.OwnerID = &value
			}
			m.Uav = &uav
		}

		missions = append(missions, m)
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

	respondWithJSON(w, http.StatusOK, models.MissionListResponse{
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
		HasNext:    hasNext,
		HasPrev:    hasPrev,
		NextPage:   nextPage,
		PrevPage:   prevPage,
		Items:      missions,
	})
}

func (h *Handlers) GetLastMission(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID, err := strconv.Atoi(vars["user_id"])
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid user ID"})
		return
	}

	query := `
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, status, created_at
        FROM missions
        WHERE user_id = $1 AND status = $2 AND deleted_at IS NULL`

	rows, err := h.DB.Query(query, userID, "Waiting")
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	now := time.Now()
	var mission models.Mission
	var bestSchedule time.Time
	found := false
	for rows.Next() {
		var candidate models.Mission
		if err := rows.Scan(
			&candidate.ID,
			&candidate.UserID,
			&candidate.UavID,
			&candidate.MissionName,
			&candidate.Schedule,
			&candidate.IsRecurring,
			&candidate.Status,
			&candidate.Timestamp,
		); err != nil {
			log.Printf("Error scanning mission: %v", err)
			continue
		}

		scheduleTime, ok := parseSchedule(candidate.Schedule)
		if !ok {
			continue
		}
		if scheduleTime.Before(now) {
			continue
		}
		if !found || scheduleTime.Before(bestSchedule) {
			bestSchedule = scheduleTime
			mission = candidate
			found = true
		}
	}
	if !found {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "No upcoming waiting missions found for this user"})
		return
	}

	waypointsQuery := `
        SELECT 
            id, sequence_order, latitude, longitude, altitude, action, action_duration 
        FROM waypoints
        WHERE mission_id = $1
        ORDER BY sequence_order ASC`

	wpRows, err := h.DB.Query(waypointsQuery, mission.ID)
	if err != nil {
		log.Printf("Error querying waypoints: %v", err)
		respondWithJSON(w, http.StatusOK, mission)
		return
	}
	defer wpRows.Close()

	waypoints := []models.Waypoint{}
	for wpRows.Next() {
		var wp models.Waypoint
		if err := wpRows.Scan(
			&wp.ID,
			&wp.SequenceOrder,
			&wp.Latitude,
			&wp.Longitude,
			&wp.Altitude,
			&wp.Action,
			&wp.ActionDuration); err != nil {
			log.Printf("Error scanning waypoint: %v", err)
			continue
		}
		waypoints = append(waypoints, wp)
	}
	mission.Waypoints = waypoints

	respondWithJSON(w, http.StatusOK, mission)
}

func parseSchedule(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}

	for _, layout := range layouts {
		if strings.Contains(layout, "Z07:00") {
			if parsed, err := time.Parse(layout, value); err == nil {
				return parsed, true
			}
			continue
		}
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, true
		}
	}

	return time.Time{}, false
}

func formatScheduleLike(raw string, t time.Time) string {
	value := strings.TrimSpace(raw)
	switch {
	case strings.Contains(value, "T"):
		return t.Format(time.RFC3339)
	case len(value) == len("2006-01-02 15:04"):
		return t.Format("2006-01-02 15:04")
	case len(value) == len("2006-01-02"):
		return t.Format("2006-01-02")
	default:
		return t.Format("2006-01-02 15:04:05")
	}
}

func (h *Handlers) GetMissionsByUserAndUAV(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID, err := strconv.Atoi(vars["user_id"])
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid User ID"})
		return
	}
	uavIDStr := r.URL.Query().Get("uav_id")
	query := `
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, status, created_at
        FROM missions
        WHERE user_id = $1 AND deleted_at IS NULL`

	args := []interface{}{userID}

	if uavIDStr != "" {
		uavID, err := strconv.Atoi(uavIDStr)
		if err != nil {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid UAV ID"})
			return
		}
		query += " AND uav_id = $2"
		args = append(args, uavID)
	}

	query += " ORDER BY created_at DESC"

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	missions := []models.Mission{}
	for rows.Next() {
		var m models.Mission
		if err := rows.Scan(&m.ID, &m.UserID, &m.UavID, &m.MissionName, &m.Schedule, &m.IsRecurring, &m.Status, &m.Timestamp); err != nil {
			log.Printf("Error scanning mission: %v", err)
			continue
		}
		missions = append(missions, m)
	}

	respondWithJSON(w, http.StatusOK, missions)
}

func (h *Handlers) GetMissionByID(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	missionIDStr := vars["id"]
	missionID, err := strconv.Atoi(missionIDStr)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid Mission ID"})
		return
	}

	var mission models.Mission
	missionQuery := `
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, status, created_at
        FROM missions
        WHERE id = $1 AND deleted_at IS NULL`

	row := h.DB.QueryRow(missionQuery, missionID)
	err = row.Scan(
		&mission.ID,
		&mission.UserID,
		&mission.UavID,
		&mission.MissionName,
		&mission.Schedule,
		&mission.IsRecurring,
		&mission.Status,
		&mission.Timestamp)

	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "Mission not found"})
		return
	}
	if err != nil {
		log.Printf("Error scanning mission details: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	waypointsQuery := `
        SELECT 
            id, sequence_order, latitude, longitude, altitude, action, action_duration 
        FROM waypoints
        WHERE mission_id = $1
        ORDER BY sequence_order ASC`

	rows, err := h.DB.Query(waypointsQuery, missionID)
	if err != nil {
		log.Printf("Error querying waypoints: %v", err)
		respondWithJSON(w, http.StatusOK, mission)
		return
	}
	defer rows.Close()

	waypoints := []models.Waypoint{}
	for rows.Next() {
		var wp models.Waypoint
		if err := rows.Scan(
			&wp.ID,
			&wp.SequenceOrder,
			&wp.Latitude,
			&wp.Longitude,
			&wp.Altitude,
			&wp.Action,
			&wp.ActionDuration); err != nil {
			log.Printf("Error scanning waypoint: %v", err)
			continue
		}
		waypoints = append(waypoints, wp)
	}
	mission.Waypoints = waypoints
	respondWithJSON(w, http.StatusOK, mission)
}

func (h *Handlers) loadDockingsByUav(uavID int) ([]models.Docking, error) {
	rows, err := h.DB.Query(`
		SELECT d.id, d.uav_id, d.name, d.location_name, d.latitude, d.longitude,
		       d.is_primary, d.is_active, d.created_at,
		       s.door_open, s.drone_present, s.charging, s.temperature, s.is_online, s.last_heartbeat
		FROM docking d
		LEFT JOIN docking_status s ON s.docking_id = d.id
		WHERE d.uav_id = $1
		ORDER BY d.is_primary DESC, d.created_at DESC`, uavID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dockings := []models.Docking{}
	for rows.Next() {
		var docking models.Docking
		var name sql.NullString
		var location sql.NullString
		var latitude sql.NullFloat64
		var longitude sql.NullFloat64

		var doorOpen sql.NullBool
		var dronePresent sql.NullBool
		var charging sql.NullBool
		var temperature sql.NullFloat64
		var isOnline sql.NullBool
		var lastHeartbeat sql.NullTime

		if err := rows.Scan(
			&docking.ID,
			&docking.UavID,
			&name,
			&location,
			&latitude,
			&longitude,
			&docking.IsPrimary,
			&docking.IsActive,
			&docking.CreatedAt,
			&doorOpen,
			&dronePresent,
			&charging,
			&temperature,
			&isOnline,
			&lastHeartbeat,
		); err != nil {
			log.Printf("Error scanning docking: %v", err)
			continue
		}

		if name.Valid {
			value := name.String
			docking.Name = &value
		}
		if location.Valid {
			value := location.String
			docking.LocationName = &value
		}
		if latitude.Valid {
			value := latitude.Float64
			docking.Latitude = &value
		}
		if longitude.Valid {
			value := longitude.Float64
			docking.Longitude = &value
		}

		hasStatus := doorOpen.Valid || dronePresent.Valid || charging.Valid || temperature.Valid || isOnline.Valid || lastHeartbeat.Valid
		if hasStatus {
			status := models.DockingStatus{}
			if doorOpen.Valid {
				value := doorOpen.Bool
				status.DoorOpen = &value
			}
			if dronePresent.Valid {
				value := dronePresent.Bool
				status.DronePresent = &value
			}
			if charging.Valid {
				value := charging.Bool
				status.Charging = &value
			}
			if temperature.Valid {
				value := temperature.Float64
				status.Temperature = &value
			}
			if isOnline.Valid {
				value := isOnline.Bool
				status.IsOnline = &value
			}
			if lastHeartbeat.Valid {
				value := lastHeartbeat.Time
				status.LastHeartbeat = &value
			}
			docking.Status = &status
		}

		dockings = append(dockings, docking)
	}

	if len(dockings) == 0 {
		return nil, nil
	}
	return dockings, nil
}

func (h *Handlers) GetMissionsByUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID, err := strconv.Atoi(vars["user_id"])
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid user ID"})
		return
	}

	missionQuery := `
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, status, created_at
        FROM missions
        WHERE user_id = $1 AND deleted_at IS NULL
        ORDER BY created_at DESC`

	rows, err := h.DB.Query(missionQuery, userID)
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	var missions []models.Mission

	for rows.Next() {
		var m models.Mission
		err := rows.Scan(
			&m.ID,
			&m.UserID,
			&m.UavID,
			&m.MissionName,
			&m.Schedule,
			&m.IsRecurring,
			&m.Status,
			&m.Timestamp,
		)
		if err != nil {
			log.Printf("Error scanning mission: %v", err)
			continue
		}

		wpQuery := `
            SELECT sequence_order, latitude, longitude, altitude, action, action_duration
            FROM waypoints
            WHERE mission_id = $1
            ORDER BY sequence_order ASC`

		wpRows, err := h.DB.Query(wpQuery, m.ID)
		if err != nil {
			log.Printf("Error querying waypoints for mission %d: %v", m.ID, err)
			m.Waypoints = []models.Waypoint{}
		} else {
			defer wpRows.Close()
			var waypoints []models.Waypoint
			for wpRows.Next() {
				var wp models.Waypoint
				var actionDuration sql.NullInt64

				err := wpRows.Scan(
					&wp.SequenceOrder,
					&wp.Latitude,
					&wp.Longitude,
					&wp.Altitude,
					&wp.Action,
					&actionDuration,
				)
				if err != nil {
					log.Printf("Error scanning waypoint: %v", err)
					continue
				}

				if actionDuration.Valid {
					wp.ActionDuration = &actionDuration.Int64
				} else {
					wp.ActionDuration = nil
				}

				waypoints = append(waypoints, wp)
			}
			m.Waypoints = waypoints
		}

		missions = append(missions, m)
	}

	respondWithJSON(w, http.StatusOK, missions)
}

type startMissionResponse struct {
	HistoryID int    `json:"history_id"`
	Status    string `json:"status"`
}

func (h *Handlers) StartMission(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	missionID, err := strconv.Atoi(vars["id"])
	if err != nil || missionID <= 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid mission ID"})
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	var mission models.Mission
	err = tx.QueryRow(`
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, status, created_at
        FROM missions
        WHERE id = $1 AND deleted_at IS NULL`, missionID).Scan(
		&mission.ID,
		&mission.UserID,
		&mission.UavID,
		&mission.MissionName,
		&mission.Schedule,
		&mission.IsRecurring,
		&mission.Status,
		&mission.Timestamp,
	)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "Mission not found"})
		return
	}
	if err != nil {
		log.Printf("Failed to query mission: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}

	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1)`, mission.UavID); err != nil {
		log.Printf("Failed to acquire UAV lock: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}

	var existingID int
	var existingMissionID int
	err = tx.QueryRow(`
        SELECT id, mission_id
        FROM mission_history
        WHERE status = 'InProgress' AND (mission_id = $1 OR uav_id = $2)
        ORDER BY created_at DESC
        LIMIT 1`, missionID, mission.UavID).Scan(&existingID, &existingMissionID)
	if err == nil {
		message := "Mission already in progress"
		if existingMissionID != missionID {
			message = "UAV already has a mission in progress"
		}
		respondWithJSON(w, http.StatusConflict, map[string]interface{}{
			"message":    message,
			"history_id": existingID,
			"mission_id": existingMissionID,
		})
		return
	}
	if err != sql.ErrNoRows {
		log.Printf("Failed to check mission history: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}

	wpRows, err := tx.Query(`
        SELECT id, sequence_order, latitude, longitude, altitude, action, action_duration
        FROM waypoints
        WHERE mission_id = $1
        ORDER BY sequence_order ASC`, missionID)
	if err != nil {
		log.Printf("Failed to load mission waypoints: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}
	defer wpRows.Close()

	waypoints := []models.Waypoint{}
	for wpRows.Next() {
		var wp models.Waypoint
		var actionDuration sql.NullInt64
		if err := wpRows.Scan(
			&wp.ID,
			&wp.SequenceOrder,
			&wp.Latitude,
			&wp.Longitude,
			&wp.Altitude,
			&wp.Action,
			&actionDuration,
		); err != nil {
			log.Printf("Error scanning waypoint: %v", err)
			continue
		}
		if actionDuration.Valid {
			wp.ActionDuration = &actionDuration.Int64
		}
		waypoints = append(waypoints, wp)
	}
	mission.Waypoints = waypoints

	missionSnapshot, err := json.Marshal(mission)
	if err != nil {
		log.Printf("Failed to build mission snapshot: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}

	startedAt := time.Now().UTC()
	var historyID int
	err = tx.QueryRow(`
        INSERT INTO mission_history (mission_id, user_id, uav_id, status, started_at, mission_snapshot)
        VALUES ($1, $2, $3, $4, $5, $6)
        RETURNING id`,
		missionID,
		mission.UserID,
		mission.UavID,
		"InProgress",
		startedAt,
		missionSnapshot,
	).Scan(&historyID)
	if err != nil {
		log.Printf("Failed to insert mission history: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}

	if _, err := tx.Exec(`UPDATE missions SET status = $1 WHERE id = $2`, "InProgress", missionID); err != nil {
		log.Printf("Failed to update mission status: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Failed to commit mission start: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}

	respondWithJSON(w, http.StatusCreated, startMissionResponse{
		HistoryID: historyID,
		Status:    "InProgress",
	})
}

func (h *Handlers) CompleteMissionByHistoryID(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	historyID, err := strconv.Atoi(vars["history_id"])
	if err != nil || historyID <= 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid history_id"})
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	var missionID int
	var currentStatus string
	err = tx.QueryRow(`SELECT mission_id, status FROM mission_history WHERE id = $1`, historyID).
		Scan(&missionID, &currentStatus)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "History not found"})
		return
	}
	if err != nil {
		log.Printf("Failed to query mission history: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to complete mission"})
		return
	}
	if currentStatus != "InProgress" {
		respondWithJSON(w, http.StatusConflict, map[string]string{"error": "history is not in progress"})
		return
	}

	var isRecurring bool
	var scheduleRaw string
	err = tx.QueryRow(`SELECT is_recurring, schedule FROM missions WHERE id = $1`, missionID).Scan(&isRecurring, &scheduleRaw)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "Mission not found"})
		return
	}
	if err != nil {
		log.Printf("Failed to query mission: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to complete mission"})
		return
	}

	newStatus := "Completed"
	var nextSchedule string
	if isRecurring {
		newStatus = "Waiting"
		if scheduleTime, ok := parseSchedule(scheduleRaw); ok {
			nextTime := scheduleTime.Add(24 * time.Hour)
			now := time.Now().In(scheduleTime.Location())
			for nextTime.Before(now) {
				nextTime = nextTime.Add(24 * time.Hour)
			}
			nextSchedule = formatScheduleLike(scheduleRaw, nextTime)
		}
	}

	if nextSchedule != "" {
		if _, err := tx.Exec(`UPDATE missions SET status = $1, schedule = $2 WHERE id = $3`, newStatus, nextSchedule, missionID); err != nil {
			log.Printf("Failed to update mission status/schedule: %v", err)
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update mission status"})
			return
		}
	} else {
		if _, err := tx.Exec(`UPDATE missions SET status = $1 WHERE id = $2`, newStatus, missionID); err != nil {
			log.Printf("Failed to update mission status: %v", err)
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update mission status"})
			return
		}
	}

	completedAt := time.Now().UTC()
	result, err := tx.Exec(`
        UPDATE mission_history
        SET status = $1, completed_at = $2
        WHERE id = $3`, "Completed", completedAt, historyID)
	if err != nil {
		log.Printf("Failed to update mission history: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save mission history"})
		return
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"error": "history not found for mission"})
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Failed to commit mission completion: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to complete mission"})
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"message": "Mission status updated", "status": newStatus})
}
