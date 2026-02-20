package handlers

import (
	"database/sql"
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

	page, limit := parsePagination(r, defaultHistoryPage, defaultHistoryLimit, maxHistoryLimit)
	offset := (page - 1) * limit

	var total int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM mission_history WHERE user_id = $1`, userID).Scan(&total); err != nil {
		log.Printf("Error counting mission history: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load mission history"})
		return
	}

	rows, err := h.DB.Query(`
        SELECT mh.id, mh.mission_id, m.mission_name, mh.user_id, u.username, mh.uav_id, mh.status,
               mh.failure_reason, mh.started_at, mh.completed_at, mh.created_at
        FROM mission_history mh
        JOIN missions m ON m.id = mh.mission_id
        LEFT JOIN users u ON u.id = mh.user_id
        WHERE mh.user_id = $1
        ORDER BY mh.completed_at DESC NULLS LAST, mh.created_at DESC
        LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		log.Printf("Error querying mission history list: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load mission history"})
		return
	}
	defer rows.Close()

	items := []models.MissionHistoryListItem{}
	for rows.Next() {
		var item models.MissionHistoryListItem
		var startedAt sql.NullTime
		var completedAt sql.NullTime
		var userName sql.NullString
		var failureReason sql.NullString
		if err := rows.Scan(
			&item.ID,
			&item.MissionID,
			&item.MissionName,
			&item.UserID,
			&userName,
			&item.UavID,
			&item.Status,
			&failureReason,
			&startedAt,
			&completedAt,
			&item.CreatedAt,
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
		if userName.Valid {
			item.UserName = &userName.String
		}
		if failureReason.Valid {
			item.FailureReason = &failureReason.String
		}
		if startedAt.Valid && completedAt.Valid && !completedAt.Time.Before(startedAt.Time) {
			durationSec := int64(completedAt.Time.Sub(startedAt.Time).Seconds())
			item.DurationSec = &durationSec
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
