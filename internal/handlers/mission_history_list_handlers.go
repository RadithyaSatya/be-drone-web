package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"xflight-backend/internal/models"
)

const (
	defaultHistoryPage  = 1
	defaultHistoryLimit = 20
	maxHistoryLimit     = 100
)

func (h *Handlers) ListMissionHistory(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	missionIDStr := r.URL.Query().Get("mission_id")
	var missionID int
	hasMissionFilter := false
	if missionIDStr != "" {
		parsed, err := strconv.Atoi(missionIDStr)
		if err != nil || parsed <= 0 {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid mission ID"})
			return
		}
		missionID = parsed
		hasMissionFilter = true
	}

	page, limit := parsePagination(r, defaultHistoryPage, defaultHistoryLimit, maxHistoryLimit)
	offset := (page - 1) * limit

	var total int
	countQuery := `SELECT COUNT(*) FROM mission_history WHERE user_id = $1`
	countArgs := []interface{}{userID}
	if hasMissionFilter {
		countQuery += " AND mission_id = $2"
		countArgs = append(countArgs, missionID)
	}
	if err := h.DB.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
		log.Printf("Error counting mission history: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load mission history"})
		return
	}

	query := `
        SELECT id, mission_id, user_id, uav_id, status, failure_reason,
               started_at, completed_at,
               (
                   SELECT COUNT(*)
                   FROM mission_media mm
                   WHERE mm.history_id = mission_history.id
               ) AS media_count,
               created_at, mission_snapshot
        FROM mission_history
        WHERE user_id = $1`
	args := []interface{}{userID}
	if hasMissionFilter {
		query += " AND mission_id = $2"
		args = append(args, missionID)
	}
	query += `
        ORDER BY
            CASE
                WHEN completed_at IS NULL
                 AND status NOT IN ('Completed', 'Failed', 'Aborted')
                THEN 0
                ELSE 1
            END ASC,
            CASE
                WHEN completed_at IS NULL
                 AND status NOT IN ('Completed', 'Failed', 'Aborted')
                THEN created_at
                ELSE NULL
            END DESC,
            completed_at DESC NULLS LAST,
            created_at DESC
        LIMIT $%d OFFSET $%d`
	args = append(args, limit, offset)
	query = fmt.Sprintf(query, len(args)-1, len(args))

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		log.Printf("Error querying mission history list: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load mission history"})
		return
	}
	defer rows.Close()

	items := []models.MissionHistoryEntry{}
	for rows.Next() {
		var item models.MissionHistoryEntry
		var startedAt sql.NullTime
		var completedAt sql.NullTime
		var failureReason sql.NullString
		if err := rows.Scan(
			&item.ID,
			&item.MissionID,
			&item.UserID,
			&item.UavID,
			&item.Status,
			&failureReason,
			&startedAt,
			&completedAt,
			&item.MediaCount,
			&item.CreatedAt,
			&item.MissionSnapshot,
		); err != nil {
			log.Printf("Error scanning mission history list: %v", err)
			continue
		}
		if startedAt.Valid {
			item.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			item.CompletedAt = &completedAt.Time
		}
		if failureReason.Valid {
			item.FailureReason = &failureReason.String
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

	respondWithJSON(w, http.StatusOK, models.MissionHistoryListResponse{
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

func parsePagination(r *http.Request, defaultPage, defaultLimit, maxLimit int) (int, int) {
	page := defaultPage
	limit := defaultLimit

	if raw := r.URL.Query().Get("page"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	return page, limit
}
