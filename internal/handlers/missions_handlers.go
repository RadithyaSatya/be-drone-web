package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"xflight-backend/internal/models"

	"github.com/gorilla/mux"
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

	tx, err := h.DB.Begin()
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	var missionID int
	missionSQL := `
		INSERT INTO missions (user_id, uav_id, mission_name, schedule, is_recurring, status, timestamp) 
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
	rows, err := h.DB.Query(`SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, status, timestamp FROM missions ORDER BY timestamp DESC`)
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
	query := `
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, status, timestamp
        FROM missions
        WHERE user_id = $1`
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

	query += " ORDER BY timestamp DESC"

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

func (h *Handlers) GetLastMission(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID, err := strconv.Atoi(vars["user_id"])
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid user ID"})
		return
	}

	query := `
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, status, timestamp
        FROM missions
        WHERE user_id = $1 AND status = $2`

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

func (h *Handlers) GetMissionsByUserAndUAV(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID, err := strconv.Atoi(vars["user_id"])
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid User ID"})
		return
	}
	uavIDStr := r.URL.Query().Get("uav_id")
	query := `
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, status, timestamp
        FROM missions
        WHERE user_id = $1`

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

	query += " ORDER BY timestamp DESC"

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
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, status, timestamp
        FROM missions
        WHERE id = $1`

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

func (h *Handlers) GetMissionsByUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID, err := strconv.Atoi(vars["user_id"])
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid user ID"})
		return
	}

	missionQuery := `
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, status, timestamp
        FROM missions
        WHERE user_id = $1
        ORDER BY timestamp DESC`

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

	var userID int
	var uavID int
	err = tx.QueryRow(`SELECT user_id, uav_id FROM missions WHERE id = $1`, missionID).Scan(&userID, &uavID)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "Mission not found"})
		return
	}
	if err != nil {
		log.Printf("Failed to query mission: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}

	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1)`, uavID); err != nil {
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
        LIMIT 1`, missionID, uavID).Scan(&existingID, &existingMissionID)
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

	startedAt := time.Now().UTC()
	var historyID int
	err = tx.QueryRow(`
        INSERT INTO mission_history (mission_id, user_id, uav_id, status, started_at)
        VALUES ($1, $2, $3, $4, $5)
        RETURNING id`,
		missionID,
		userID,
		uavID,
		"InProgress",
		startedAt,
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
	err = tx.QueryRow(`SELECT is_recurring FROM missions WHERE id = $1`, missionID).Scan(&isRecurring)
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
	if isRecurring {
		newStatus = "Waiting"
	}

	if _, err := tx.Exec(`UPDATE missions SET status = $1 WHERE id = $2`, newStatus, missionID); err != nil {
		log.Printf("Failed to update mission status: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update mission status"})
		return
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
