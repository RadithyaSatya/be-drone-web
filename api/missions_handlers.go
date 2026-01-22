package api

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
	"xflight-backend/models"

	"github.com/gorilla/mux"
)

func (h *Handlers) SaveNewMission(w http.ResponseWriter, r *http.Request) {
	var req models.FullMissionRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
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

func (h *Handlers) GetLastMission(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID, err := strconv.Atoi(vars["user_id"])
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid user ID"})
		return
	}

	var mission models.Mission
	query := `
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, status, timestamp
        FROM missions
        WHERE user_id = $1
        ORDER BY timestamp DESC
        LIMIT 1`

	row := h.DB.QueryRow(query, userID)
	err = row.Scan(&mission.ID, &mission.UserID, &mission.UavID, &mission.MissionName, &mission.Schedule, &mission.IsRecurring, &mission.Status, &mission.Timestamp)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "No missions found for this user"})
		return
	}
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	respondWithJSON(w, http.StatusOK, mission)
}

func (h *Handlers) GetRecurringMissions(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`
        SELECT id, user_id, mission_name, is_recurring, status, timestamp 
        FROM missions 
        WHERE is_recurring = TRUE 
        ORDER BY timestamp DESC`)

	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	missions := []models.Mission{}
	for rows.Next() {
		var m models.Mission
		if err := rows.Scan(&m.ID, &m.UserID, &m.MissionName, &m.IsRecurring, &m.Status, &m.Timestamp); err != nil {
			log.Printf("Error scanning recurring mission: %v", err)
			continue
		}
		missions = append(missions, m)
	}

	respondWithJSON(w, http.StatusOK, missions)
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

func (h *Handlers) GetScheduledMissions(w http.ResponseWriter, r *http.Request) {
	query := `
        SELECT id, user_id, uav_id, mission_name, is_recurring, status, timestamp 
        FROM missions 
        WHERE status = $1 
        ORDER BY timestamp DESC`

	rows, err := h.DB.Query(query, "Scheduled")

	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	missions := []models.Mission{}
	for rows.Next() {
		var m models.Mission
		if err := rows.Scan(&m.ID, &m.UserID, &m.UavID, &m.MissionName, &m.IsRecurring, &m.Status, &m.Timestamp); err != nil {
			log.Printf("Error scanning scheduled mission: %v", err)
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

func (h *Handlers) CompleteMission(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	missionID, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid mission ID"})
		return
	}

	var isRecurring bool
	err = h.DB.QueryRow(`SELECT is_recurring FROM missions WHERE id = $1`, missionID).Scan(&isRecurring)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "Mission not found"})
		return
	}
	if err != nil {
		log.Printf("Failed to query mission recurrence: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update mission status"})
		return
	}

	newStatus := "Completed"
	if isRecurring {
		newStatus = "Waiting"
	}

	_, err = h.DB.Exec(`UPDATE missions SET status = $1 WHERE id = $2`, newStatus, missionID)
	if err != nil {
		log.Printf("Failed to update mission status: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update mission status"})
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"message": "Mission status updated", "status": newStatus})
}
