package handlers

import (
	"database/sql"
	"log"
	"net/http"

	"xflight-backend/internal/models"
)

func (h *Handlers) ListMissionHistoryEvents(w http.ResponseWriter, r *http.Request) {
	historyID, err := parseMissionHistoryID(r)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx, err := h.loadMissionHistoryRuntimeContextReadOnly(historyID)
	if err != nil {
		h.respondMissionHistoryContextError(w, err)
		return
	}
	if !h.authorizeMissionHistoryAccess(w, r, ctx) {
		return
	}

	rows, err := h.DB.Query(`
		SELECT id, history_id, from_state, to_state, result, failure_code, message, is_terminal, created_at
		FROM mission_event
		WHERE history_id = $1
		ORDER BY created_at ASC, id ASC`,
		historyID,
	)
	if err != nil {
		log.Printf("Failed to list mission events: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load mission events"})
		return
	}
	defer rows.Close()

	items := make([]models.MissionHistoryEvent, 0)
	for rows.Next() {
		var item models.MissionHistoryEvent
		var fromState sql.NullString
		var toState sql.NullString
		var result sql.NullString
		var failureCode sql.NullString
		var message sql.NullString
		if err := rows.Scan(
			&item.ID,
			&item.HistoryID,
			&fromState,
			&toState,
			&result,
			&failureCode,
			&message,
			&item.IsTerminal,
			&item.CreatedAt,
		); err != nil {
			log.Printf("Failed to scan mission event: %v", err)
			continue
		}
		if fromState.Valid {
			value := fromState.String
			item.FromState = &value
		}
		if toState.Valid {
			value := toState.String
			item.ToState = &value
		}
		if result.Valid {
			value := result.String
			item.Result = &value
		}
		if failureCode.Valid {
			value := failureCode.String
			item.FailureCode = &value
		}
		if message.Valid {
			value := message.String
			item.Message = &value
		}
		items = append(items, item)
	}

	respondWithJSON(w, http.StatusOK, models.MissionHistoryEventListResponse{
		HistoryID: historyID,
		Count:     len(items),
		Items:     items,
	})
}
