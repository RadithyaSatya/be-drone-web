package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
	"xflight-backend/models"

	"github.com/gorilla/mux"
)

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

	respondWithJSON(w, http.StatusOK, models.TelemetryResponse{Telemetry: logs})
}
