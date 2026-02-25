package handlers

import (
	"database/sql"
	"log"
	"net/http"
	"strconv"

	"xflight-backend/internal/models"

	"github.com/gorilla/mux"
)

func (h *Handlers) GetMissionHistory(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	vars := mux.Vars(r)
	missionID, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid mission ID"})
		return
	}

	mission, err := h.fetchMissionWithWaypoints(missionID, userID)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "Mission not found"})
		return
	}
	if err != nil {
		log.Printf("Error loading mission details: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load mission"})
		return
	}

	rows, err := h.DB.Query(`
        SELECT id, mission_id, user_id, uav_id, status, failure_reason, started_at, completed_at, created_at, mission_snapshot
        FROM mission_history
        WHERE mission_id = $1 AND user_id = $2
        ORDER BY completed_at DESC NULLS LAST, created_at DESC`, missionID, userID)
	if err != nil {
		log.Printf("Error querying mission history: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load mission history"})
		return
	}
	defer rows.Close()

	history := []models.MissionHistoryEntry{}
	historyIDs := []int{}
	for rows.Next() {
		var entry models.MissionHistoryEntry
		var startedAt sql.NullTime
		var completedAt sql.NullTime
		var failureReason sql.NullString

		if err := rows.Scan(
			&entry.ID,
			&entry.MissionID,
			&entry.UserID,
			&entry.UavID,
			&entry.Status,
			&failureReason,
			&startedAt,
			&completedAt,
			&entry.CreatedAt,
			&entry.MissionSnapshot,
		); err != nil {
			log.Printf("Error scanning mission history: %v", err)
			continue
		}

		if startedAt.Valid {
			entry.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			entry.CompletedAt = &completedAt.Time
		}
		if failureReason.Valid {
			entry.FailureReason = &failureReason.String
		}

		history = append(history, entry)
		historyIDs = append(historyIDs, entry.ID)
	}

	mediaByHistory, err := h.loadMissionHistoryMedia(missionID, historyIDs)
	if err != nil {
		log.Printf("Error loading mission history media: %v", err)
	} else {
		for i := range history {
			if media, ok := mediaByHistory[history[i].ID]; ok {
				history[i].Media = media
			}
		}
	}

	respondWithJSON(w, http.StatusOK, models.MissionHistoryResponse{
		Mission: mission,
		History: history,
	})
}

func (h *Handlers) loadMissionHistoryMedia(missionID int, historyIDs []int) (map[int][]models.MissionHistoryMedia, error) {
	if len(historyIDs) == 0 {
		return map[int][]models.MissionHistoryMedia{}, nil
	}
	rows, err := h.DB.Query(`
        SELECT id, history_id, mission_id, media_type, file_path, created_at
        FROM mission_history_media
        WHERE mission_id = $1
        ORDER BY created_at DESC`, missionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[int][]models.MissionHistoryMedia{}
	for rows.Next() {
		var media models.MissionHistoryMedia
		if err := rows.Scan(
			&media.ID,
			&media.HistoryID,
			&media.MissionID,
			&media.MediaType,
			&media.FilePath,
			&media.CreatedAt,
		); err != nil {
			log.Printf("Error scanning mission history media: %v", err)
			continue
		}
		result[media.HistoryID] = append(result[media.HistoryID], media)
	}
	return result, nil
}

func (h *Handlers) fetchMissionWithWaypoints(missionID, userID int) (models.Mission, error) {
	var mission models.Mission
	missionQuery := `
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, status, timestamp
        FROM missions
        WHERE id = $1 AND user_id = $2`

	row := h.DB.QueryRow(missionQuery, missionID, userID)
	err := row.Scan(
		&mission.ID,
		&mission.UserID,
		&mission.UavID,
		&mission.MissionName,
		&mission.Schedule,
		&mission.IsRecurring,
		&mission.Status,
		&mission.Timestamp)
	if err != nil {
		return mission, err
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
		return mission, nil
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

	return mission, nil
}
