package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"xflight-backend/models"

	"github.com/gorilla/mux"
)

const footageUploadDir = "./uploads/footages"

type Handlers struct {
	DB *sql.DB
}

func init() {
	if err := os.MkdirAll(footageUploadDir, os.ModePerm); err != nil {
		log.Fatalf("Failed to create upload directory: %v", err)
	}
}
func NewHandlers(db *sql.DB) *Handlers {
	return &Handlers{DB: db}
}
func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}
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
			m.Waypoints = []models.Waypoint{} // empty but safe
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
func (h *Handlers) GetLatestLocation(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	entityTypeStr := vars["type"]
	entityIDStr := vars["id"]

	entityType, err := strconv.Atoi(entityTypeStr)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid entity type"})
		return
	}

	entityID, err := strconv.Atoi(entityIDStr)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid entity ID"})
		return
	}

	query := `
        SELECT latitude, longitude, timestamp
        FROM locations
        WHERE entity_id = $1 AND type = $2
        ORDER BY timestamp DESC
        LIMIT 1`

	var location struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Timestamp string  `json:"timestamp"`
	}

	row := h.DB.QueryRow(query, entityID, entityType)
	err = row.Scan(&location.Latitude, &location.Longitude, &location.Timestamp)

	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "No location found for this entity"})
		return
	}
	if err != nil {
		log.Printf("Error querying location: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
		return
	}

	respondWithJSON(w, http.StatusOK, location)
}
func (h *Handlers) UploadFootage(w http.ResponseWriter, r *http.Request) {
	const maxUploadSize = 1 << 30 // 1 GB
	r.ParseMultipartForm(maxUploadSize)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "File too large or invalid form"})
		return
	}

	uavIDStr := r.FormValue("uav_id")
	if uavIDStr == "" {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "uav_id is required"})
		return
	}

	uavID, err := strconv.Atoi(uavIDStr)
	if err != nil || uavID <= 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid uav_id"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "No file uploaded or field name must be 'file'"})
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".m4" && ext != ".mp4" {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Only .m4 and .mp4 files are allowed"})
		return
	}
	timestamp := time.Now().Format("20060102_150405")
	storedFilename := fmt.Sprintf("uav_%d_%s%s", uavID, timestamp, ext)
	filePath := filepath.Join(footageUploadDir, storedFilename)

	dst, err := os.Create(filePath)
	if err != nil {
		log.Printf("Failed to create file on disk: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save file"})
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		log.Printf("Failed to write file: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save file"})
		return
	}

	var footageID int
	err = h.DB.QueryRow(`
        INSERT INTO footages (uav_id, filename, file_path)
        VALUES ($1, $2, $3)
        RETURNING id`,
		uavID,
		header.Filename,
		filePath,
	).Scan(&footageID)

	if err != nil {
		log.Printf("Failed to insert into footages table: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "File saved but database record failed"})
		return
	}
	respondWithJSON(w, http.StatusCreated, map[string]interface{}{
		"id":          footageID,
		"uav_id":      uavID,
		"filename":    header.Filename,
		"uploaded_at": time.Now().Format(time.RFC3339),
		"message":     "Footage uploaded successfully",
	})
}
func (h *Handlers) SubmitBatchMissionLogs(w http.ResponseWriter, r *http.Request) {
	var req models.BatchMissionLogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON payload"})
		return
	}

	if len(req.Logs) == 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "No logs provided"})
		return
	}

	if len(req.Logs) > 10000 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Too many logs (max 10000 per request)"})
		return
	}
	tx, err := h.DB.Begin()
	if err != nil {
		log.Printf("Failed to begin transaction: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
		return
	}
	defer tx.Rollback()
	insertSQL := `
        INSERT INTO mission_log (mission_id, uav_id, waypoints, altitude, coordinate, batt, time_in_air)
        VALUES ($1, $2, $3, $4, $5, $6, $7)`

	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		log.Printf("Failed to prepare statement: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
		return
	}
	defer stmt.Close()
	insertedCount := 0
	for _, logEntry := range req.Logs {
		if logEntry.MissionID <= 0 || logEntry.UAVID <= 0 || logEntry.Coordinate == "" {
			continue
		}

		_, err := stmt.Exec(
			logEntry.MissionID,
			logEntry.UAVID,
			logEntry.Waypoints,
			logEntry.Altitude,
			logEntry.Coordinate,
			logEntry.Battery,
			logEntry.Timestamp,
		)

		if err != nil {
			log.Printf("Failed to insert log for mission %d, uav %d: %v", logEntry.MissionID, logEntry.UAVID, err)
			continue
		}
		insertedCount++
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Failed to commit transaction: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save logs"})
		return
	}

	response := map[string]interface{}{
		"message":        "Batch mission logs processed",
		"total_received": len(req.Logs),
		"inserted":       insertedCount,
		"timestamp":      time.Now().Format(time.RFC3339),
	}

	respondWithJSON(w, http.StatusCreated, response)
}
func (h *Handlers) GetMissionTelemetry(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	missionIDStr := vars["mission_id"]
	missionID, err := strconv.Atoi(missionIDStr)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid mission ID"})
		return
	}

	rows, err := h.DB.Query(`
        SELECT waypoints, altitude, coordinate, batt AS battery, time_in_air
        FROM mission_log
        WHERE mission_id = $1
        ORDER BY time_in_air ASC`, missionID)
	if err != nil {
		log.Printf("Query error: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
		return
	}
	defer rows.Close()

	var logs []models.TelemetryLog
	for rows.Next() {
		var logEntry models.TelemetryLog
		var timeInAir string
		if err := rows.Scan(&logEntry.Waypoints, &logEntry.Altitude, &logEntry.Coordinate, &logEntry.Battery, &timeInAir); err != nil {
			log.Printf("Scan error: %v", err)
			continue
		}
		logEntry.Timestamp = timeInAir
		logs = append(logs, logEntry)
	}

	respondWithJSON(w, http.StatusOK, models.TelemetryResponse{Telemetry: logs}) // ← qualified
}
