package handlers

import (
	"database/sql"
	"log"
	"net/http"
	"time"

	"xflight-backend/internal/models"
)

func (h *Handlers) GetMissionHistoryState(w http.ResponseWriter, r *http.Request) {
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

	lastEventAt, err := h.loadMissionHistoryLastEventAt(historyID)
	if err != nil {
		log.Printf("Failed to load mission history state: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load mission history state"})
		return
	}

	respondWithJSON(w, http.StatusOK, models.MissionHistoryStateResponse{
		HistoryID:     ctx.HistoryID,
		MissionID:     ctx.MissionID,
		UserID:        ctx.UserID,
		UavID:         ctx.UavID,
		DockingID:     ctx.DockingID,
		Status:        ctx.Status,
		IsTerminal:    isTerminalMissionHistoryStatus(ctx.Status),
		FailureCode:   ctx.FailureCode,
		FailureReason: ctx.FailureReason,
		CompletedAt:   ctx.CompletedAt,
		LastEventAt:   lastEventAt,
	})
}

func (h *Handlers) loadMissionHistoryLastEventAt(historyID int) (*time.Time, error) {
	var lastEventAt sql.NullTime
	if err := h.DB.QueryRow(`
		SELECT MAX(created_at)
		FROM mission_event
		WHERE history_id = $1`,
		historyID,
	).Scan(&lastEventAt); err != nil {
		return nil, err
	}
	if !lastEventAt.Valid {
		return nil, nil
	}

	value := lastEventAt.Time
	return &value, nil
}
