package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"xflight-backend/internal/auth"

	"github.com/gorilla/mux"
)

const (
	missionHistoryStatusPreparingDock = "PreparingDock"
	missionHistoryStatusSafeToFly     = "SafeToFly"
	missionHistoryStatusTakeoff       = "Takeoff"
	missionHistoryStatusLanded        = "Landed"
	missionHistoryStatusDockConfirmed = "DockConfirmed"
	missionHistoryStatusCompleted     = "Completed"
	missionHistoryStatusFailed        = "Failed"
	missionHistoryStatusAborted       = "Aborted"
)

type missionHistoryStateUpdateRequest struct {
	Status        string `json:"status"`
	Message       string `json:"message"`
	FailureCode   string `json:"failure_code"`
	FailureReason string `json:"failure_reason"`
}

type missionHistoryRuntimeContext struct {
	HistoryID          int
	MissionID          int
	UserID             int
	UavID              int
	DockingID          *int
	Status             string
	IsRecurring        bool
	RecurrenceUnit     *string
	RecurrenceInterval *int
	Schedule           string
	CompletedAt        *time.Time
	FailureCode        *string
	FailureReason      *string
}

func normalizeMissionHistoryStatus(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, " ", "")

	switch normalized {
	case "preparingdock":
		return missionHistoryStatusPreparingDock
	case "safetofly":
		return missionHistoryStatusSafeToFly
	case "takeoff":
		return missionHistoryStatusTakeoff
	case "landed":
		return missionHistoryStatusLanded
	case "dockconfirmed":
		return missionHistoryStatusDockConfirmed
	case "completed":
		return missionHistoryStatusCompleted
	case "failed":
		return missionHistoryStatusFailed
	case "aborted":
		return missionHistoryStatusAborted
	default:
		return ""
	}
}

func isTerminalMissionHistoryStatus(status string) bool {
	switch status {
	case missionHistoryStatusCompleted, missionHistoryStatusFailed, missionHistoryStatusAborted:
		return true
	default:
		return false
	}
}

func canTransitionMissionHistoryStatus(from, to string) bool {
	switch from {
	case missionHistoryStatusPreparingDock:
		return to == missionHistoryStatusSafeToFly || to == missionHistoryStatusFailed || to == missionHistoryStatusAborted
	case missionHistoryStatusSafeToFly:
		return to == missionHistoryStatusTakeoff || to == missionHistoryStatusFailed || to == missionHistoryStatusAborted
	case missionHistoryStatusTakeoff:
		return to == missionHistoryStatusLanded || to == missionHistoryStatusFailed || to == missionHistoryStatusAborted
	case missionHistoryStatusLanded:
		return to == missionHistoryStatusDockConfirmed || to == missionHistoryStatusFailed || to == missionHistoryStatusAborted
	case missionHistoryStatusDockConfirmed:
		return to == missionHistoryStatusCompleted || to == missionHistoryStatusFailed || to == missionHistoryStatusAborted
	default:
		return false
	}
}

func (h *Handlers) UpdateMissionHistoryState(w http.ResponseWriter, r *http.Request) {
	historyID, err := parseMissionHistoryID(r)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var req missionHistoryStateUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
		return
	}

	targetStatus := normalizeMissionHistoryStatus(req.Status)
	if targetStatus == "" {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid mission history status"})
		return
	}
	if targetStatus == missionHistoryStatusCompleted {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Use POST /mission-history/{history_id}/complete to finalize the mission"})
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	ctx, err := h.loadMissionHistoryRuntimeContext(tx, historyID)
	if err != nil {
		h.respondMissionHistoryContextError(w, err)
		return
	}

	if !h.authorizeMissionHistoryAccess(w, r, ctx) {
		return
	}

	if ctx.Status == targetStatus {
		respondWithJSON(w, http.StatusConflict, map[string]string{"error": "mission history is already in that status"})
		return
	}
	if !canTransitionMissionHistoryStatus(ctx.Status, targetStatus) {
		respondWithJSON(w, http.StatusConflict, map[string]string{
			"error": fmt.Sprintf("invalid mission history transition from %s to %s", ctx.Status, targetStatus),
		})
		return
	}

	failureCode := strings.TrimSpace(req.FailureCode)
	if failureCode != "" {
		exists, err := h.failureCodeExists(tx, failureCode)
		if err != nil {
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to validate failure code"})
			return
		}
		if !exists {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "failure_code not found"})
			return
		}
	}

	message := strings.TrimSpace(req.Message)
	failureReason := strings.TrimSpace(req.FailureReason)
	if failureReason == "" && isTerminalMissionHistoryStatus(targetStatus) {
		failureReason = message
	}

	now := time.Now().UTC()
	if isTerminalMissionHistoryStatus(targetStatus) {
		newMissionStatus, nextSchedule := resolveMissionTemplateAfterTerminalState(
			ctx.IsRecurring,
			ctx.Schedule,
			ctx.RecurrenceUnit,
			ctx.RecurrenceInterval,
			targetStatus,
			now,
		)
		if _, err := tx.Exec(`
			UPDATE mission_history
			SET status = $1,
				failure_code = $2,
				failure_reason = $3,
				completed_at = $4
			WHERE id = $5`,
			targetStatus,
			nullableStringOrNil(failureCode),
			nullableStringOrNil(failureReason),
			now,
			historyID,
		); err != nil {
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update mission history"})
			return
		}
		if nextSchedule != "" {
			if _, err := tx.Exec(`UPDATE missions SET status = $1, schedule = $2 WHERE id = $3`, newMissionStatus, nextSchedule, ctx.MissionID); err != nil {
				respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update mission"})
				return
			}
		} else {
			if _, err := tx.Exec(`UPDATE missions SET status = $1 WHERE id = $2`, newMissionStatus, ctx.MissionID); err != nil {
				respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update mission"})
				return
			}
		}
	} else {
		if _, err := tx.Exec(`UPDATE mission_history SET status = $1 WHERE id = $2`, targetStatus, historyID); err != nil {
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update mission history"})
			return
		}
		if _, err := tx.Exec(`UPDATE missions SET status = $1 WHERE id = $2`, "InProgress", ctx.MissionID); err != nil {
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update mission"})
			return
		}
	}

	if err := insertMissionEvent(
		tx,
		historyID,
		nullableStringOrNil(ctx.Status),
		nullableStringOrNil(targetStatus),
		terminalResult(targetStatus),
		nullableStringOrNil(failureCode),
		nullableStringOrNil(message),
		isTerminalMissionHistoryStatus(targetStatus),
	); err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to record mission event"})
		return
	}

	if err := tx.Commit(); err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to commit mission history update"})
		return
	}

	response := map[string]interface{}{
		"history_id": historyID,
		"status":     targetStatus,
	}
	if isTerminalMissionHistoryStatus(targetStatus) {
		missionStatus, nextSchedule := resolveMissionTemplateAfterTerminalState(
			ctx.IsRecurring,
			ctx.Schedule,
			ctx.RecurrenceUnit,
			ctx.RecurrenceInterval,
			targetStatus,
			now,
		)
		response["completed_at"] = now
		response["mission_status"] = missionStatus
		if nextSchedule != "" {
			response["next_schedule"] = nextSchedule
		}
	}
	respondWithJSON(w, http.StatusOK, response)
}

func parseMissionHistoryID(r *http.Request) (int, error) {
	vars := mux.Vars(r)
	historyID, err := strconv.Atoi(vars["history_id"])
	if err != nil || historyID <= 0 {
		return 0, errors.New("Invalid history_id")
	}
	return historyID, nil
}

type missionHistoryRuntimeQuerier interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

func (h *Handlers) loadMissionHistoryRuntimeContext(tx *sql.Tx, historyID int) (*missionHistoryRuntimeContext, error) {
	return loadMissionHistoryRuntimeContextQuerier(tx, historyID, true)
}

func (h *Handlers) loadMissionHistoryRuntimeContextReadOnly(historyID int) (*missionHistoryRuntimeContext, error) {
	return loadMissionHistoryRuntimeContextQuerier(h.DB, historyID, false)
}

func loadMissionHistoryRuntimeContextQuerier(q missionHistoryRuntimeQuerier, historyID int, forUpdate bool) (*missionHistoryRuntimeContext, error) {
	var ctx missionHistoryRuntimeContext
	var dockingID sql.NullInt32
	var schedule sql.NullString
	var recurrenceUnit sql.NullString
	var recurrenceInterval sql.NullInt32
	var completedAt sql.NullTime
	var failureCode sql.NullString
	var failureReason sql.NullString

	query := `
		SELECT mh.id, mh.mission_id, mh.user_id, mh.uav_id, mh.docking_id, mh.status,
		       m.is_recurring, m.recurrence_unit, m.recurrence_interval, m.schedule, mh.completed_at, mh.failure_code, mh.failure_reason
		FROM mission_history mh
		JOIN missions m ON m.id = mh.mission_id
		WHERE mh.id = $1`
	if forUpdate {
		query += `
		FOR UPDATE`
	}

	err := q.QueryRow(query,
		historyID,
	).Scan(
		&ctx.HistoryID,
		&ctx.MissionID,
		&ctx.UserID,
		&ctx.UavID,
		&dockingID,
		&ctx.Status,
		&ctx.IsRecurring,
		&recurrenceUnit,
		&recurrenceInterval,
		&schedule,
		&completedAt,
		&failureCode,
		&failureReason,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, fmt.Errorf("load mission history: %w", err)
	}

	if dockingID.Valid {
		value := int(dockingID.Int32)
		ctx.DockingID = &value
	}
	if schedule.Valid {
		ctx.Schedule = schedule.String
	}
	if recurrenceUnit.Valid {
		value := recurrenceUnit.String
		ctx.RecurrenceUnit = &value
	}
	if recurrenceInterval.Valid {
		value := int(recurrenceInterval.Int32)
		ctx.RecurrenceInterval = &value
	}
	if completedAt.Valid {
		value := completedAt.Time
		ctx.CompletedAt = &value
	}
	if failureCode.Valid {
		value := failureCode.String
		ctx.FailureCode = &value
	}
	if failureReason.Valid {
		value := failureReason.String
		ctx.FailureReason = &value
	}
	if ctx.IsRecurring {
		unit, interval, err := normalizeMissionRecurrence(true, ctx.RecurrenceUnit, ctx.RecurrenceInterval)
		if err == nil {
			ctx.RecurrenceUnit = unit
			ctx.RecurrenceInterval = interval
		}
	}

	return &ctx, nil
}

func (h *Handlers) respondMissionHistoryContextError(w http.ResponseWriter, err error) {
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "History not found"})
		return
	}
	respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load mission history"})
}

func (h *Handlers) authorizeMissionStart(w http.ResponseWriter, r *http.Request, missionUserID, missionUavID int) bool {
	if deviceClaims, ok := auth.DeviceClaimsFromContext(r.Context()); ok && deviceClaims != nil {
		switch deviceClaims.ScopeType {
		case auth.DeviceScopeUav:
			if deviceClaims.UavID != nil && *deviceClaims.UavID == missionUavID {
				return true
			}
		case auth.DeviceScopeDocking:
			if deviceClaims.DockingID != nil {
				dockingUavID, err := h.resolveDockingUavID(*deviceClaims.DockingID)
				if err == nil && dockingUavID == missionUavID {
					return true
				}
			}
		}
		respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return false
	}

	userID, ok := h.requireUserID(w, r)
	if !ok {
		return false
	}
	if userID != missionUserID {
		respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return false
	}
	return true
}

func (h *Handlers) authorizeMissionHistoryAccess(w http.ResponseWriter, r *http.Request, ctx *missionHistoryRuntimeContext) bool {
	if deviceClaims, ok := auth.DeviceClaimsFromContext(r.Context()); ok && deviceClaims != nil {
		switch deviceClaims.ScopeType {
		case auth.DeviceScopeUav:
			if deviceClaims.UavID != nil && *deviceClaims.UavID == ctx.UavID {
				return true
			}
		case auth.DeviceScopeDocking:
			if deviceClaims.DockingID != nil && ctx.DockingID != nil && *deviceClaims.DockingID == *ctx.DockingID {
				return true
			}
		}
		respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return false
	}

	userID, ok := h.requireUserID(w, r)
	if !ok {
		return false
	}
	if userID != ctx.UserID {
		respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return false
	}
	return true
}

func (h *Handlers) failureCodeExists(tx *sql.Tx, code string) (bool, error) {
	if strings.TrimSpace(code) == "" {
		return true, nil
	}
	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM failure_code WHERE code = $1)`, code).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func insertMissionEvent(tx *sql.Tx, historyID int, fromState, toState, result, failureCode, message *string, isTerminal bool) error {
	_, err := tx.Exec(`
		INSERT INTO mission_event (history_id, from_state, to_state, result, failure_code, message, is_terminal)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		historyID,
		fromState,
		toState,
		result,
		failureCode,
		message,
		isTerminal,
	)
	return err
}

func nullableStringOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func terminalResult(status string) *string {
	if isTerminalMissionHistoryStatus(status) {
		return &status
	}
	return nil
}
