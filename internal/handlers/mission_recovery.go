package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"time"
)

const (
	missionRecoverySweepInterval       = time.Minute
	missionWaitingRecoveryGrace        = time.Minute
	missionRecoveryFailureCode         = "MISSION_TIMEOUT"
	missionRecoveryPreparingDockMaxAge = 5 * time.Minute
	missionRecoverySafeToFlyMaxAge     = 8 * time.Minute
	missionRecoveryTakeoffMaxAge       = 30 * time.Minute
	missionRecoveryLandedMaxAge        = 10 * time.Minute
	missionRecoveryDockConfirmedMaxAge = 5 * time.Minute
)

type missionRecoveryCandidate struct {
	HistoryID          int
	MissionID          int
	Status             string
	IsRecurring        bool
	Schedule           string
	RecurrenceUnit     *string
	RecurrenceInterval *int
	LastActivity       time.Time
}

type missionAutoRecoveryDecision struct {
	TargetStatus  string
	FailureCode   string
	FailureReason string
	Message       string
}

func (h *Handlers) StartMissionRecoveryLoop() {
	ticker := time.NewTicker(missionRecoverySweepInterval)

	go func() {
		defer ticker.Stop()
		h.runMissionRecoverySweep()

		for range ticker.C {
			h.runMissionRecoverySweep()
		}
	}()
}

func (h *Handlers) runMissionRecoverySweep() {
	now := time.Now().UTC()
	waitingRecovered, err := h.recoverOverdueWaitingMissions(now)
	if err != nil {
		log.Printf("Mission waiting recovery sweep failed: %v", err)
	} else if waitingRecovered > 0 {
		log.Printf("Mission waiting recovery sweep updated %d overdue waiting mission(s)", waitingRecovered)
	}

	recovered, err := h.recoverStaleMissionRuns(now)
	if err != nil {
		log.Printf("Mission recovery sweep failed: %v", err)
		return
	}
	if recovered > 0 {
		log.Printf("Mission recovery sweep closed %d stale mission run(s)", recovered)
	}
}

func (h *Handlers) recoverOverdueWaitingMissions(now time.Time) (int, error) {
	tx, err := h.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT id, schedule, is_recurring, recurrence_unit, recurrence_interval
		FROM missions
		WHERE status = 'Waiting'
		  AND deleted_at IS NULL
		FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type overdueWaitingMission struct {
		MissionID          int
		Schedule           string
		ScheduleTime       time.Time
		IsRecurring        bool
		RecurrenceUnit     *string
		RecurrenceInterval *int
	}

	candidates := make([]overdueWaitingMission, 0)
	for rows.Next() {
		var missionID int
		var schedule string
		var isRecurring bool
		var recurrenceUnit sql.NullString
		var recurrenceInterval sql.NullInt32
		if err := rows.Scan(&missionID, &schedule, &isRecurring, &recurrenceUnit, &recurrenceInterval); err != nil {
			return 0, err
		}

		scheduleTime, ok := parseSchedule(schedule)
		if !ok || now.Before(scheduleTime.Add(missionWaitingRecoveryGrace)) {
			continue
		}

		var unit *string
		var interval *int
		if recurrenceUnit.Valid {
			value := recurrenceUnit.String
			unit = &value
		}
		if recurrenceInterval.Valid {
			value := int(recurrenceInterval.Int32)
			interval = &value
		}
		candidates = append(candidates, overdueWaitingMission{
			MissionID:          missionID,
			Schedule:           schedule,
			ScheduleTime:       scheduleTime,
			IsRecurring:        isRecurring,
			RecurrenceUnit:     unit,
			RecurrenceInterval: interval,
		})
	}

	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	recovered := 0
	for _, candidate := range candidates {
		mission, err := h.buildMissionDetail(tx, candidate.MissionID)
		if err != nil {
			return recovered, err
		}
		dockingID, err := resolveActiveDockingID(tx, mission.UavID)
		if err != nil {
			return recovered, err
		}
		missionSnapshot, err := json.Marshal(mission)
		if err != nil {
			return recovered, err
		}

		var historyID int
		if err := tx.QueryRow(`
			INSERT INTO mission_history (mission_id, user_id, uav_id, docking_id, status, failure_code, failure_reason, started_at, completed_at, mission_snapshot)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			RETURNING id`,
			mission.ID,
			mission.UserID,
			mission.UavID,
			dockingID,
			missionHistoryStatusFailed,
			nullableStringOrNil(missionRecoveryFailureCode),
			nullableStringOrNil("Mission schedule expired before the run was started"),
			candidate.ScheduleTime.UTC(),
			now,
			missionSnapshot,
		).Scan(&historyID); err != nil {
			return recovered, err
		}
		if err := insertMissionEvent(
			tx,
			historyID,
			nil,
			nullableStringOrNil(missionHistoryStatusFailed),
			terminalResult(missionHistoryStatusFailed),
			nullableStringOrNil(missionRecoveryFailureCode),
			nullableStringOrNil("Mission auto-failed because the waiting schedule expired before start"),
			true,
		); err != nil {
			return recovered, err
		}

		missionStatus, nextSchedule := resolveMissionTemplateAfterTerminalState(
			candidate.IsRecurring,
			candidate.Schedule,
			candidate.RecurrenceUnit,
			candidate.RecurrenceInterval,
			missionHistoryStatusFailed,
			now,
		)
		if nextSchedule != "" {
			if _, err := tx.Exec(`UPDATE missions SET status = $1, schedule = $2 WHERE id = $3`, missionStatus, nextSchedule, candidate.MissionID); err != nil {
				return recovered, err
			}
		} else {
			if _, err := tx.Exec(`UPDATE missions SET status = $1 WHERE id = $2`, missionStatus, candidate.MissionID); err != nil {
				return recovered, err
			}
		}
		recovered++
	}

	if err := tx.Commit(); err != nil {
		return recovered, err
	}
	return recovered, nil
}

func resolveActiveDockingID(tx *sql.Tx, uavID int) (sql.NullInt32, error) {
	var dockingID sql.NullInt32
	err := tx.QueryRow(`
		SELECT id
		FROM docking
		WHERE uav_id = $1 AND is_active = true
		ORDER BY is_primary DESC, created_at DESC
		LIMIT 1`, uavID).Scan(&dockingID)
	if err != nil && err != sql.ErrNoRows {
		return sql.NullInt32{}, err
	}
	if err == sql.ErrNoRows {
		return sql.NullInt32{}, nil
	}
	return dockingID, nil
}

func (h *Handlers) recoverStaleMissionRuns(now time.Time) (int, error) {
	tx, err := h.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT mh.id, mh.mission_id, mh.status,
		       m.is_recurring, m.schedule, m.recurrence_unit, m.recurrence_interval,
		       COALESCE(last_event.last_event_at, mh.started_at, mh.created_at) AS last_activity
		FROM mission_history mh
		JOIN missions m ON m.id = mh.mission_id
		LEFT JOIN (
			SELECT history_id, MAX(created_at) AS last_event_at
			FROM mission_event
			GROUP BY history_id
		) last_event ON last_event.history_id = mh.id
		WHERE mh.completed_at IS NULL
		  AND mh.status NOT IN ('Completed', 'Failed', 'Aborted')
		FOR UPDATE OF mh SKIP LOCKED`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	candidates := make([]missionRecoveryCandidate, 0)
	for rows.Next() {
		var candidate missionRecoveryCandidate
		var recurrenceUnit sql.NullString
		var recurrenceInterval sql.NullInt32

		if err := rows.Scan(
			&candidate.HistoryID,
			&candidate.MissionID,
			&candidate.Status,
			&candidate.IsRecurring,
			&candidate.Schedule,
			&recurrenceUnit,
			&recurrenceInterval,
			&candidate.LastActivity,
		); err != nil {
			return 0, err
		}

		if recurrenceUnit.Valid {
			value := recurrenceUnit.String
			candidate.RecurrenceUnit = &value
		}
		if recurrenceInterval.Valid {
			value := int(recurrenceInterval.Int32)
			candidate.RecurrenceInterval = &value
		}

		if _, ok := decideMissionAutoRecovery(candidate.Status, candidate.LastActivity, now); !ok {
			continue
		}
		candidates = append(candidates, candidate)
	}

	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	recovered := 0
	for _, candidate := range candidates {
		decision, ok := decideMissionAutoRecovery(candidate.Status, candidate.LastActivity, now)
		if !ok {
			continue
		}
		if err := h.applyMissionAutoRecovery(tx, candidate, decision, now); err != nil {
			return recovered, err
		}
		recovered++
	}
	if err := tx.Commit(); err != nil {
		return recovered, err
	}
	return recovered, nil
}

func decideMissionAutoRecovery(status string, lastActivity, now time.Time) (missionAutoRecoveryDecision, bool) {
	if lastActivity.IsZero() || !now.After(lastActivity) {
		return missionAutoRecoveryDecision{}, false
	}

	elapsed := now.Sub(lastActivity)
	switch status {
	case missionHistoryStatusPreparingDock:
		if elapsed < missionRecoveryPreparingDockMaxAge {
			return missionAutoRecoveryDecision{}, false
		}
		return missionAutoRecoveryDecision{
			TargetStatus:  missionHistoryStatusAborted,
			FailureCode:   missionRecoveryFailureCode,
			FailureReason: "Mission preparation timed out before launch clearance",
			Message:       "Mission auto-aborted after remaining in PreparingDock too long",
		}, true
	case missionHistoryStatusSafeToFly:
		if elapsed < missionRecoverySafeToFlyMaxAge {
			return missionAutoRecoveryDecision{}, false
		}
		return missionAutoRecoveryDecision{
			TargetStatus:  missionHistoryStatusAborted,
			FailureCode:   missionRecoveryFailureCode,
			FailureReason: "Mission timed out while waiting for takeoff",
			Message:       "Mission auto-aborted after remaining in SafeToFly too long",
		}, true
	case missionHistoryStatusTakeoff:
		if elapsed < missionRecoveryTakeoffMaxAge {
			return missionAutoRecoveryDecision{}, false
		}
		return missionAutoRecoveryDecision{
			TargetStatus:  missionHistoryStatusFailed,
			FailureCode:   missionRecoveryFailureCode,
			FailureReason: "Mission timed out while airborne after takeoff",
			Message:       "Mission auto-failed after remaining in Takeoff too long",
		}, true
	case missionHistoryStatusLanded:
		if elapsed < missionRecoveryLandedMaxAge {
			return missionAutoRecoveryDecision{}, false
		}
		return missionAutoRecoveryDecision{
			TargetStatus:  missionHistoryStatusFailed,
			FailureCode:   missionRecoveryFailureCode,
			FailureReason: "Mission timed out while waiting for dock confirmation after landing",
			Message:       "Mission auto-failed after remaining in Landed too long",
		}, true
	case missionHistoryStatusDockConfirmed:
		if elapsed < missionRecoveryDockConfirmedMaxAge {
			return missionAutoRecoveryDecision{}, false
		}
		return missionAutoRecoveryDecision{
			TargetStatus:  missionHistoryStatusFailed,
			FailureCode:   missionRecoveryFailureCode,
			FailureReason: "Dock recovery did not complete after the drone was confirmed at the dock",
			Message:       "Mission auto-failed after remaining in DockConfirmed too long",
		}, true
	default:
		return missionAutoRecoveryDecision{}, false
	}
}

func (h *Handlers) applyMissionAutoRecovery(tx *sql.Tx, candidate missionRecoveryCandidate, decision missionAutoRecoveryDecision, now time.Time) error {
	if _, err := tx.Exec(`
		UPDATE mission_history
		SET status = $1,
		    failure_code = $2,
		    failure_reason = $3,
		    completed_at = $4
		WHERE id = $5`,
		decision.TargetStatus,
		nullableStringOrNil(decision.FailureCode),
		nullableStringOrNil(decision.FailureReason),
		now,
		candidate.HistoryID,
	); err != nil {
		return err
	}

	missionStatus, nextSchedule := missionStatusAfterAutoRecovery(candidate, decision.TargetStatus, now)
	if nextSchedule != "" {
		if _, err := tx.Exec(`UPDATE missions SET status = $1, schedule = $2 WHERE id = $3`, missionStatus, nextSchedule, candidate.MissionID); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(`UPDATE missions SET status = $1 WHERE id = $2`, missionStatus, candidate.MissionID); err != nil {
			return err
		}
	}

	return insertMissionEvent(
		tx,
		candidate.HistoryID,
		nullableStringOrNil(candidate.Status),
		nullableStringOrNil(decision.TargetStatus),
		terminalResult(decision.TargetStatus),
		nullableStringOrNil(decision.FailureCode),
		nullableStringOrNil(decision.Message),
		true,
	)
}

func missionStatusAfterAutoRecovery(candidate missionRecoveryCandidate, terminalStatus string, now time.Time) (string, string) {
	return resolveMissionTemplateAfterTerminalState(
		candidate.IsRecurring,
		candidate.Schedule,
		candidate.RecurrenceUnit,
		candidate.RecurrenceInterval,
		terminalStatus,
		now,
	)
}

func resolveMissionTemplateAfterTerminalState(isRecurring bool, schedule string, recurrenceUnit *string, recurrenceInterval *int, terminalStatus string, now time.Time) (string, string) {
	if !isRecurring {
		return terminalStatus, ""
	}

	unit, interval, err := normalizeMissionRecurrence(true, recurrenceUnit, recurrenceInterval)
	if err != nil || unit == nil || interval == nil {
		return "Waiting", ""
	}

	nextSchedule, ok := nextRecurringSchedule(schedule, now, *unit, *interval)
	if !ok {
		return "Waiting", ""
	}

	return "Waiting", nextSchedule
}
