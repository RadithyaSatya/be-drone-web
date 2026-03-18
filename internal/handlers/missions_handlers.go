package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"xflight-backend/internal/auth"
	"xflight-backend/internal/models"

	"github.com/gorilla/mux"
)

const (
	defaultMissionPage  = 1
	defaultMissionLimit = 20
	maxMissionLimit     = 100

	recurrenceUnitHour = "hour"
	recurrenceUnitDay  = "day"
)

type missionLookupResponse struct {
	models.Mission
	HistoryID     *int    `json:"history_id,omitempty"`
	RuntimeStatus *string `json:"runtime_status,omitempty"`
}

type currentMissionResponse struct {
	ScopeType        string  `json:"scope_type"`
	UavID            int     `json:"uav_id"`
	DockingID        *int    `json:"docking_id,omitempty"`
	HasActiveMission bool    `json:"has_active_mission"`
	MissionID        *int    `json:"mission_id,omitempty"`
	HistoryID        *int    `json:"history_id,omitempty"`
	MissionHistoryID *int    `json:"mission_history_id,omitempty"`
	Status           *string `json:"status,omitempty"`
}

type currentMissionLookupResult struct {
	HistoryID int
	MissionID int
	DockingID *int
	Status    string
	Found     bool
}

func buildCurrentMissionResponse(scopeType string, uavID int, tokenDockingID *int, result currentMissionLookupResult) currentMissionResponse {
	response := currentMissionResponse{
		ScopeType:        scopeType,
		UavID:            uavID,
		DockingID:        tokenDockingID,
		HasActiveMission: result.Found,
	}
	if !result.Found {
		return response
	}

	historyID := result.HistoryID
	missionID := result.MissionID
	status := result.Status

	response.HistoryID = &historyID
	response.MissionHistoryID = &historyID
	response.MissionID = &missionID
	response.Status = &status
	if result.DockingID != nil {
		response.DockingID = result.DockingID
	}

	return response
}

func normalizeMissionRecurrence(isRecurring bool, unit *string, interval *int) (*string, *int, error) {
	if !isRecurring {
		return nil, nil, nil
	}

	normalizedUnit := recurrenceUnitDay
	if unit != nil && strings.TrimSpace(*unit) != "" {
		switch strings.ToLower(strings.TrimSpace(*unit)) {
		case "hour", "hours":
			normalizedUnit = recurrenceUnitHour
		case "day", "days":
			normalizedUnit = recurrenceUnitDay
		default:
			return nil, nil, fmt.Errorf("recurrence_unit must be one of: hour, day")
		}
	}

	normalizedInterval := 1
	if interval != nil {
		if *interval <= 0 {
			return nil, nil, fmt.Errorf("recurrence_interval must be greater than 0")
		}
		normalizedInterval = *interval
	}

	return &normalizedUnit, &normalizedInterval, nil
}

func applyMissionRecurrenceDefaults(mission *models.Mission) {
	if mission == nil || !mission.IsRecurring {
		mission.RecurrenceUnit = nil
		mission.RecurrenceInterval = nil
		return
	}
	unit, interval, err := normalizeMissionRecurrence(true, mission.RecurrenceUnit, mission.RecurrenceInterval)
	if err != nil {
		return
	}
	mission.RecurrenceUnit = unit
	mission.RecurrenceInterval = interval
}

func applyMissionListItemRecurrenceDefaults(item *models.MissionListItem) {
	if item == nil || !item.IsRecurring {
		item.RecurrenceUnit = nil
		item.RecurrenceInterval = nil
		return
	}
	unit, interval, err := normalizeMissionRecurrence(true, item.RecurrenceUnit, item.RecurrenceInterval)
	if err != nil {
		return
	}
	item.RecurrenceUnit = unit
	item.RecurrenceInterval = interval
}

func nextRecurringSchedule(raw string, from time.Time, unit string, interval int) (string, bool) {
	scheduleTime, ok := parseSchedule(raw)
	if !ok {
		return "", false
	}

	nextTime := scheduleTime
	for !nextTime.After(from) {
		switch unit {
		case recurrenceUnitHour:
			nextTime = nextTime.Add(time.Duration(interval) * time.Hour)
		case recurrenceUnitDay:
			nextTime = nextTime.AddDate(0, 0, interval)
		default:
			return "", false
		}
	}

	return formatScheduleLike(raw, nextTime), true
}

func assignMissionRecurrence(mission *models.Mission, unit sql.NullString, interval sql.NullInt32) {
	if unit.Valid {
		value := unit.String
		mission.RecurrenceUnit = &value
	}
	if interval.Valid {
		value := int(interval.Int32)
		mission.RecurrenceInterval = &value
	}
	applyMissionRecurrenceDefaults(mission)
}

func assignMissionListItemRecurrence(item *models.MissionListItem, unit sql.NullString, interval sql.NullInt32) {
	if unit.Valid {
		value := unit.String
		item.RecurrenceUnit = &value
	}
	if interval.Valid {
		value := int(interval.Int32)
		item.RecurrenceInterval = &value
	}
	applyMissionListItemRecurrenceDefaults(item)
}

func nullableIntPointerValue(value *int) interface{} {
	if value == nil {
		return nil
	}
	return *value
}

func nullableStringPointerValue(value *string) interface{} {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func (h *Handlers) SaveNewMission(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	var req models.FullMissionRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
		return
	}
	req.UserID = userID
	scheduleTime, ok := parseSchedule(req.Schedule)
	if !ok {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid schedule format"})
		return
	}
	now := time.Now().In(scheduleTime.Location())
	if !scheduleTime.After(now) {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Schedule must be in the future"})
		return
	}

	recurrenceUnit, recurrenceInterval, err := normalizeMissionRecurrence(req.IsRecurring, req.RecurrenceUnit, req.RecurrenceInterval)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		INSERT INTO missions (user_id, uav_id, mission_name, schedule, is_recurring, recurrence_unit, recurrence_interval, status, created_at) 
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`

	err = tx.QueryRow(
		missionSQL,
		req.UserID,
		req.UavID,
		req.MissionName,
		req.Schedule,
		req.IsRecurring,
		nullableStringPointerValue(recurrenceUnit),
		nullableIntPointerValue(recurrenceInterval),
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
	rows, err := h.DB.Query(`SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, recurrence_unit, recurrence_interval, status, created_at FROM missions WHERE deleted_at IS NULL ORDER BY created_at DESC`)
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	missions := []models.Mission{}
	for rows.Next() {
		var m models.Mission
		var recurrenceUnit sql.NullString
		var recurrenceInterval sql.NullInt32
		if err := rows.Scan(&m.ID, &m.UserID, &m.UavID, &m.MissionName, &m.Schedule, &m.IsRecurring, &recurrenceUnit, &recurrenceInterval, &m.Status, &m.Timestamp); err != nil {
			log.Printf("Error scanning mission: %v", err)
			continue
		}
		assignMissionRecurrence(&m, recurrenceUnit, recurrenceInterval)
		missions = append(missions, m)
	}

	respondWithJSON(w, http.StatusOK, missions)
}

func (h *Handlers) GetMissionsForCurrentUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	uavIDStr := r.URL.Query().Get("uav_id")
	dateStr := strings.TrimSpace(r.URL.Query().Get("date"))
	page, limit := parsePagination(r, defaultMissionPage, defaultMissionLimit, maxMissionLimit)
	offset := (page - 1) * limit

	countQuery := `
		SELECT COUNT(*)
		FROM missions
		WHERE user_id = $1 AND deleted_at IS NULL`
	countArgs := []interface{}{userID}

	query := `
        SELECT m.id, m.user_id, m.uav_id, m.mission_name, m.schedule, m.is_recurring, m.recurrence_unit, m.recurrence_interval, m.status, m.created_at, m.deleted_at,
		       COALESCE(COUNT(w.id), 0) AS waypoint_count,
			   u.id, u.serial_number, u.name, u.model, u.firmware_version, u.camera_spec, u.image_url,
			   u.max_range_meter, u.max_flight_time_min, u.owner_id, u.is_active, u.created_at
        FROM missions m
		LEFT JOIN waypoints w ON w.mission_id = m.id
		LEFT JOIN uav u ON u.id = m.uav_id
        WHERE m.user_id = $1 AND m.deleted_at IS NULL`
	args := []interface{}{userID}
	nextArgIndex := 2

	if uavIDStr != "" {
		uavID, err := strconv.Atoi(uavIDStr)
		if err != nil {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid UAV ID"})
			return
		}
		query += fmt.Sprintf(" AND m.uav_id = $%d", nextArgIndex)
		args = append(args, uavID)
		countQuery += fmt.Sprintf(" AND uav_id = $%d", nextArgIndex)
		countArgs = append(countArgs, uavID)
		nextArgIndex++
	}

	if dateStr != "" {
		if _, err := time.Parse("2006-01-02", dateStr); err != nil {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid date. Use YYYY-MM-DD"})
			return
		}
		query += fmt.Sprintf(" AND LEFT(m.schedule, 10) = $%d", nextArgIndex)
		args = append(args, dateStr)
		countQuery += fmt.Sprintf(" AND LEFT(schedule, 10) = $%d", nextArgIndex)
		countArgs = append(countArgs, dateStr)
		nextArgIndex++
	}

	query += `
		GROUP BY m.id, m.user_id, m.uav_id, m.mission_name, m.schedule, m.is_recurring, m.recurrence_unit, m.recurrence_interval, m.status, m.created_at, m.deleted_at,
		         u.id, u.serial_number, u.name, u.model, u.firmware_version, u.camera_spec, u.image_url,
				 u.max_range_meter, u.max_flight_time_min, u.owner_id, u.is_active, u.created_at
		ORDER BY m.created_at DESC
		LIMIT $%d OFFSET $%d`

	args = append(args, limit, offset)
	query = fmt.Sprintf(query, len(args)-1, len(args))

	var total int
	if err := h.DB.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
		log.Printf("Error counting missions: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load missions"})
		return
	}

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	missions := []models.MissionListItem{}
	for rows.Next() {
		var m models.MissionListItem
		var waypointCount int
		var deletedAt sql.NullTime
		var recurrenceUnit sql.NullString
		var recurrenceInterval sql.NullInt32

		var uavID sql.NullInt32
		var uavSerial sql.NullString
		var uavName sql.NullString
		var uavModel sql.NullString
		var uavFirmware sql.NullString
		var uavCamera sql.NullString
		var uavImageURL sql.NullString
		var uavMaxRange sql.NullInt32
		var uavMaxFlight sql.NullInt32
		var uavOwner sql.NullInt32
		var uavIsActive sql.NullBool
		var uavCreatedAt sql.NullTime

		if err := rows.Scan(
			&m.ID,
			&m.UserID,
			&m.UavID,
			&m.MissionName,
			&m.Schedule,
			&m.IsRecurring,
			&recurrenceUnit,
			&recurrenceInterval,
			&m.Status,
			&m.Timestamp,
			&deletedAt,
			&waypointCount,
			&uavID,
			&uavSerial,
			&uavName,
			&uavModel,
			&uavFirmware,
			&uavCamera,
			&uavImageURL,
			&uavMaxRange,
			&uavMaxFlight,
			&uavOwner,
			&uavIsActive,
			&uavCreatedAt,
		); err != nil {
			log.Printf("Error scanning mission: %v", err)
			continue
		}
		m.WaypointCount = &waypointCount
		assignMissionListItemRecurrence(&m, recurrenceUnit, recurrenceInterval)
		if deletedAt.Valid {
			value := deletedAt.Time
			m.DeletedAt = &value
		}

		if uavID.Valid {
			uav := models.Uav{
				ID:        int(uavID.Int32),
				IsActive:  uavIsActive.Bool,
				CreatedAt: uavCreatedAt.Time,
			}
			if uavSerial.Valid {
				value := uavSerial.String
				uav.SerialNumber = &value
			}
			if uavName.Valid {
				value := uavName.String
				uav.Name = &value
			}
			if uavModel.Valid {
				value := uavModel.String
				uav.Model = &value
			}
			if uavFirmware.Valid {
				value := uavFirmware.String
				uav.FirmwareVersion = &value
			}
			if uavCamera.Valid {
				value := uavCamera.String
				uav.CameraSpec = &value
			}
			if uavImageURL.Valid {
				value := uavImageURL.String
				uav.ImageURL = &value
			}
			if uavMaxRange.Valid {
				value := int(uavMaxRange.Int32)
				uav.MaxRangeMeter = &value
			}
			if uavMaxFlight.Valid {
				value := int(uavMaxFlight.Int32)
				uav.MaxFlightTimeMin = &value
			}
			if uavOwner.Valid {
				value := int(uavOwner.Int32)
				uav.OwnerID = &value
			}
			m.Uav = &uav
		}

		missions = append(missions, m)
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

	respondWithJSON(w, http.StatusOK, models.MissionListResponse{
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
		HasNext:    hasNext,
		HasPrev:    hasPrev,
		NextPage:   nextPage,
		PrevPage:   prevPage,
		Items:      missions,
	})
}

type missionMutationContext struct {
	ID                 int
	UserID             int
	UavID              int
	MissionName        string
	Schedule           string
	IsRecurring        bool
	RecurrenceUnit     *string
	RecurrenceInterval *int
	Status             string
}

func (h *Handlers) loadMissionMutationContext(tx *sql.Tx, missionID int) (*missionMutationContext, error) {
	var ctx missionMutationContext
	var recurrenceUnit sql.NullString
	var recurrenceInterval sql.NullInt32
	err := tx.QueryRow(`
		SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, recurrence_unit, recurrence_interval, status
		FROM missions
		WHERE id = $1 AND deleted_at IS NULL`, missionID,
	).Scan(
		&ctx.ID,
		&ctx.UserID,
		&ctx.UavID,
		&ctx.MissionName,
		&ctx.Schedule,
		&ctx.IsRecurring,
		&recurrenceUnit,
		&recurrenceInterval,
		&ctx.Status,
	)
	if err != nil {
		return nil, err
	}
	if recurrenceUnit.Valid {
		value := recurrenceUnit.String
		ctx.RecurrenceUnit = &value
	}
	if recurrenceInterval.Valid {
		value := int(recurrenceInterval.Int32)
		ctx.RecurrenceInterval = &value
	}
	return &ctx, nil
}

func (h *Handlers) ensureMissionMutable(tx *sql.Tx, missionID int) error {
	var activeHistoryID int
	err := tx.QueryRow(`
		SELECT id
		FROM mission_history
		WHERE mission_id = $1
		  AND completed_at IS NULL
		  AND status NOT IN ('Completed', 'Failed', 'Aborted')
		ORDER BY created_at DESC
		LIMIT 1`, missionID,
	).Scan(&activeHistoryID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("mission is currently in progress")
}

func (h *Handlers) buildMissionDetail(tx *sql.Tx, missionID int) (models.Mission, error) {
	var mission models.Mission
	var recurrenceUnit sql.NullString
	var recurrenceInterval sql.NullInt32
	err := tx.QueryRow(`
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, recurrence_unit, recurrence_interval, status, created_at
        FROM missions
        WHERE id = $1 AND deleted_at IS NULL`, missionID).Scan(
		&mission.ID,
		&mission.UserID,
		&mission.UavID,
		&mission.MissionName,
		&mission.Schedule,
		&mission.IsRecurring,
		&recurrenceUnit,
		&recurrenceInterval,
		&mission.Status,
		&mission.Timestamp,
	)
	if err != nil {
		return models.Mission{}, err
	}
	assignMissionRecurrence(&mission, recurrenceUnit, recurrenceInterval)

	rows, err := tx.Query(`
        SELECT id, sequence_order, latitude, longitude, altitude, action, action_duration
        FROM waypoints
        WHERE mission_id = $1
        ORDER BY sequence_order ASC`, missionID)
	if err != nil {
		return mission, err
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
			&wp.ActionDuration,
		); err != nil {
			return mission, err
		}
		waypoints = append(waypoints, wp)
	}
	mission.Waypoints = waypoints
	return mission, nil
}

func (h *Handlers) UpdateMission(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	missionID, ok := parsePositiveMuxInt(w, r, "id", "Invalid Mission ID")
	if !ok {
		return
	}

	var req models.UpdateMissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	ctx, err := h.loadMissionMutationContext(tx, missionID)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "Mission not found"})
		return
	}
	if err != nil {
		log.Printf("Failed to load mission for update: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load mission"})
		return
	}
	if ctx.UserID != userID {
		respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if err := h.ensureMissionMutable(tx, missionID); err != nil {
		if err.Error() == "mission is currently in progress" {
			respondWithJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		log.Printf("Failed to validate mission mutability: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update mission"})
		return
	}

	finalUavID := ctx.UavID
	if req.UavID != nil {
		finalUavID = *req.UavID
	}
	finalMissionName := ctx.MissionName
	if req.MissionName != nil {
		finalMissionName = strings.TrimSpace(*req.MissionName)
	}
	if finalMissionName == "" {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "mission_name is required"})
		return
	}
	finalSchedule := ctx.Schedule
	scheduleChanged := false
	if req.Schedule != nil {
		finalSchedule = strings.TrimSpace(*req.Schedule)
		scheduleChanged = true
	}
	if finalSchedule == "" {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "schedule is required"})
		return
	}
	if scheduleChanged {
		scheduleTime, ok := parseSchedule(finalSchedule)
		if !ok {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid schedule format"})
			return
		}
		now := time.Now().In(scheduleTime.Location())
		if !scheduleTime.After(now) {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Schedule must be in the future"})
			return
		}
	}

	finalStatus := ctx.Status
	if req.Status != nil {
		finalStatus = strings.TrimSpace(*req.Status)
	}
	if finalStatus == "" {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "status is required"})
		return
	}

	finalIsRecurring := ctx.IsRecurring
	if req.IsRecurring != nil {
		finalIsRecurring = *req.IsRecurring
	}

	finalRecurrenceUnit := ctx.RecurrenceUnit
	if req.RecurrenceUnit != nil {
		finalRecurrenceUnit = req.RecurrenceUnit
	}
	finalRecurrenceInterval := ctx.RecurrenceInterval
	if req.RecurrenceInterval != nil {
		finalRecurrenceInterval = req.RecurrenceInterval
	}
	recurrenceUnit, recurrenceInterval, err := normalizeMissionRecurrence(finalIsRecurring, finalRecurrenceUnit, finalRecurrenceInterval)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if !h.ensureMissionLookupUavAccess(w, r, finalUavID) {
		return
	}

	_, err = tx.Exec(`
		UPDATE missions
		SET uav_id = $1,
			mission_name = $2,
			schedule = $3,
			is_recurring = $4,
			recurrence_unit = $5,
			recurrence_interval = $6,
			status = $7,
			updated_at = $8
		WHERE id = $9`,
		finalUavID,
		finalMissionName,
		finalSchedule,
		finalIsRecurring,
		nullableStringPointerValue(recurrenceUnit),
		nullableIntPointerValue(recurrenceInterval),
		finalStatus,
		time.Now().UTC(),
		missionID,
	)
	if err != nil {
		log.Printf("Failed to update mission: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update mission"})
		return
	}

	if req.Waypoints != nil {
		if _, err := tx.Exec(`DELETE FROM waypoints WHERE mission_id = $1`, missionID); err != nil {
			log.Printf("Failed to delete existing waypoints: %v", err)
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update mission"})
			return
		}
		for _, wp := range *req.Waypoints {
			if _, err := tx.Exec(`
				INSERT INTO waypoints (mission_id, sequence_order, latitude, longitude, altitude, action, action_duration)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				missionID,
				wp.SequenceOrder,
				wp.Latitude,
				wp.Longitude,
				wp.Altitude,
				wp.Action,
				wp.ActionDuration,
			); err != nil {
				log.Printf("Failed to insert updated waypoint: %v", err)
				respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update mission"})
				return
			}
		}
	}

	mission, err := h.buildMissionDetail(tx, missionID)
	if err != nil {
		log.Printf("Failed to reload mission after update: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update mission"})
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Failed to commit mission update: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update mission"})
		return
	}

	respondWithJSON(w, http.StatusOK, mission)
}

func (h *Handlers) DeleteMission(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	missionID, ok := parsePositiveMuxInt(w, r, "id", "Invalid Mission ID")
	if !ok {
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	ctx, err := h.loadMissionMutationContext(tx, missionID)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "Mission not found"})
		return
	}
	if err != nil {
		log.Printf("Failed to load mission for delete: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete mission"})
		return
	}
	if ctx.UserID != userID {
		respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if err := h.ensureMissionMutable(tx, missionID); err != nil {
		if err.Error() == "mission is currently in progress" {
			respondWithJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		log.Printf("Failed to validate mission mutability: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete mission"})
		return
	}

	result, err := tx.Exec(`
		UPDATE missions
		SET deleted_at = $1, deleted_by = $2, updated_at = $1
		WHERE id = $3 AND deleted_at IS NULL`,
		time.Now().UTC(),
		userID,
		missionID,
	)
	if err != nil {
		log.Printf("Failed to soft delete mission: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete mission"})
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "Mission not found"})
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Failed to commit mission delete: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete mission"})
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"message": "Mission deleted successfully"})
}

func (h *Handlers) GetLastMission(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID, err := strconv.Atoi(vars["user_id"])
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid user ID"})
		return
	}

	query := `
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, recurrence_unit, recurrence_interval, status, created_at
        FROM missions
        WHERE user_id = $1 AND status = $2 AND deleted_at IS NULL`

	rows, err := h.DB.Query(query, userID, "Waiting")
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	now := time.Now()
	var mission models.Mission
	var bestSchedule time.Time
	found := false
	for rows.Next() {
		var candidate models.Mission
		var recurrenceUnit sql.NullString
		var recurrenceInterval sql.NullInt32
		if err := rows.Scan(
			&candidate.ID,
			&candidate.UserID,
			&candidate.UavID,
			&candidate.MissionName,
			&candidate.Schedule,
			&candidate.IsRecurring,
			&recurrenceUnit,
			&recurrenceInterval,
			&candidate.Status,
			&candidate.Timestamp,
		); err != nil {
			log.Printf("Error scanning mission: %v", err)
			continue
		}
		assignMissionRecurrence(&candidate, recurrenceUnit, recurrenceInterval)

		scheduleTime, ok := parseSchedule(candidate.Schedule)
		if !ok {
			continue
		}
		if scheduleTime.Before(now) {
			continue
		}
		if !found || scheduleTime.Before(bestSchedule) {
			bestSchedule = scheduleTime
			mission = candidate
			found = true
		}
	}
	if !found {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "No upcoming waiting missions found for this user"})
		return
	}

	mission.Waypoints, err = h.loadMissionWaypoints(mission.ID)
	if err != nil {
		log.Printf("Error querying waypoints: %v", err)
		respondWithJSON(w, http.StatusOK, mission)
		return
	}

	respondWithJSON(w, http.StatusOK, mission)
}

func (h *Handlers) GetNextWaitingMissionByUAV(w http.ResponseWriter, r *http.Request) {
	uavID, ok := parsePositiveMuxInt(w, r, "uav_id", "Invalid UAV ID")
	if !ok {
		return
	}
	if !h.ensureMissionLookupUavAccess(w, r, uavID) {
		return
	}

	rows, err := h.DB.Query(`
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, recurrence_unit, recurrence_interval, status, created_at
        FROM missions
        WHERE uav_id = $1 AND status = $2 AND deleted_at IS NULL`,
		uavID, "Waiting")
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	now := time.Now()
	var mission models.Mission
	var bestSchedule time.Time
	found := false
	for rows.Next() {
		var candidate models.Mission
		var recurrenceUnit sql.NullString
		var recurrenceInterval sql.NullInt32
		if err := rows.Scan(
			&candidate.ID,
			&candidate.UserID,
			&candidate.UavID,
			&candidate.MissionName,
			&candidate.Schedule,
			&candidate.IsRecurring,
			&recurrenceUnit,
			&recurrenceInterval,
			&candidate.Status,
			&candidate.Timestamp,
		); err != nil {
			log.Printf("Error scanning mission: %v", err)
			continue
		}
		assignMissionRecurrence(&candidate, recurrenceUnit, recurrenceInterval)

		scheduleTime, ok := parseSchedule(candidate.Schedule)
		if !ok {
			continue
		}
		if scheduleTime.Before(now) {
			continue
		}
		if !found || scheduleTime.Before(bestSchedule) {
			bestSchedule = scheduleTime
			mission = candidate
			found = true
		}
	}
	if !found {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "No upcoming waiting missions found for this UAV"})
		return
	}

	mission.Waypoints, err = h.loadMissionWaypoints(mission.ID)
	if err != nil {
		log.Printf("Error querying waypoints: %v", err)
		respondWithJSON(w, http.StatusOK, missionLookupResponse{Mission: mission})
		return
	}

	respondWithJSON(w, http.StatusOK, missionLookupResponse{Mission: mission})
}

func (h *Handlers) GetNextWaitingMissionForDevice(w http.ResponseWriter, r *http.Request) {
	uavID, ok := h.resolveMissionLookupUavIDFromDevice(w, r)
	if !ok {
		return
	}

	mission, found, err := h.findNextWaitingMissionByUavID(uavID)
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !found {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "No upcoming waiting missions found for this device"})
		return
	}

	respondWithJSON(w, http.StatusOK, missionLookupResponse{Mission: mission})
}

func (h *Handlers) GetSafeToFlyMissionByUAV(w http.ResponseWriter, r *http.Request) {
	uavID, ok := parsePositiveMuxInt(w, r, "uav_id", "Invalid UAV ID")
	if !ok {
		return
	}
	if !h.ensureMissionLookupUavAccess(w, r, uavID) {
		return
	}

	var historyID int
	var runtimeStatus string
	var mission models.Mission
	var recurrenceUnit sql.NullString
	var recurrenceInterval sql.NullInt32
	err := h.DB.QueryRow(`
        SELECT mh.id, mh.status,
               m.id, m.user_id, m.uav_id, m.mission_name, m.schedule, m.is_recurring, m.recurrence_unit, m.recurrence_interval, m.status, m.created_at
        FROM mission_history mh
        INNER JOIN missions m ON m.id = mh.mission_id
        WHERE mh.uav_id = $1
          AND mh.status = $2
          AND m.deleted_at IS NULL
        ORDER BY mh.started_at DESC NULLS LAST, mh.created_at DESC
        LIMIT 1`,
		uavID, missionHistoryStatusSafeToFly,
	).Scan(
		&historyID,
		&runtimeStatus,
		&mission.ID,
		&mission.UserID,
		&mission.UavID,
		&mission.MissionName,
		&mission.Schedule,
		&mission.IsRecurring,
		&recurrenceUnit,
		&recurrenceInterval,
		&mission.Status,
		&mission.Timestamp,
	)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "No SafeToFly mission found for this UAV"})
		return
	}
	if err != nil {
		log.Printf("Error loading SafeToFly mission for uav %d: %v", uavID, err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load mission"})
		return
	}
	assignMissionRecurrence(&mission, recurrenceUnit, recurrenceInterval)

	mission.Waypoints, err = h.loadMissionWaypoints(mission.ID)
	if err != nil {
		log.Printf("Error querying waypoints: %v", err)
		respondWithJSON(w, http.StatusOK, missionLookupResponse{
			Mission:       mission,
			HistoryID:     &historyID,
			RuntimeStatus: &runtimeStatus,
		})
		return
	}

	respondWithJSON(w, http.StatusOK, missionLookupResponse{
		Mission:       mission,
		HistoryID:     &historyID,
		RuntimeStatus: &runtimeStatus,
	})
}

func (h *Handlers) GetSafeToFlyMissionForDevice(w http.ResponseWriter, r *http.Request) {
	uavID, ok := h.resolveMissionLookupUavIDFromDevice(w, r)
	if !ok {
		return
	}

	historyID, runtimeStatus, mission, found, err := h.findSafeToFlyMissionByUavID(uavID)
	if err != nil {
		log.Printf("Error loading SafeToFly mission for device uav %d: %v", uavID, err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load mission"})
		return
	}
	if !found {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "No SafeToFly mission found for this device"})
		return
	}

	respondWithJSON(w, http.StatusOK, missionLookupResponse{
		Mission:       mission,
		HistoryID:     &historyID,
		RuntimeStatus: &runtimeStatus,
	})
}

func (h *Handlers) GetCurrentMissionForDevice(w http.ResponseWriter, r *http.Request) {
	scopeType, uavID, dockingID, ok := h.resolveMissionDeviceContext(w, r)
	if !ok {
		return
	}

	result, err := h.findCurrentMissionByDeviceScope(scopeType, uavID, dockingID)
	if err != nil {
		log.Printf("Error loading current mission for device scope %s: %v", scopeType, err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load current mission"})
		return
	}

	respondWithJSON(w, http.StatusOK, buildCurrentMissionResponse(scopeType, uavID, dockingID, result))
}

func parseSchedule(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}

	for _, layout := range layouts {
		if strings.Contains(layout, "Z07:00") {
			if parsed, err := time.Parse(layout, value); err == nil {
				return parsed, true
			}
			continue
		}
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, true
		}
	}

	return time.Time{}, false
}

func formatScheduleLike(raw string, t time.Time) string {
	value := strings.TrimSpace(raw)
	switch {
	case strings.Contains(value, "T"):
		return t.Format(time.RFC3339)
	case len(value) == len("2006-01-02 15:04"):
		return t.Format("2006-01-02 15:04")
	case len(value) == len("2006-01-02"):
		return t.Format("2006-01-02")
	default:
		return t.Format("2006-01-02 15:04:05")
	}
}

func (h *Handlers) loadMissionWaypoints(missionID int) ([]models.Waypoint, error) {
	rows, err := h.DB.Query(`
        SELECT 
            id, sequence_order, latitude, longitude, altitude, action, action_duration 
        FROM waypoints
        WHERE mission_id = $1
        ORDER BY sequence_order ASC`, missionID)
	if err != nil {
		return nil, err
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
			&wp.ActionDuration,
		); err != nil {
			log.Printf("Error scanning waypoint: %v", err)
			continue
		}
		waypoints = append(waypoints, wp)
	}
	return waypoints, nil
}

func parsePositiveMuxInt(w http.ResponseWriter, r *http.Request, key, message string) (int, bool) {
	value, err := strconv.Atoi(mux.Vars(r)[key])
	if err != nil || value <= 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": message})
		return 0, false
	}
	return value, true
}

func (h *Handlers) resolveMissionLookupUavIDFromDevice(w http.ResponseWriter, r *http.Request) (int, bool) {
	_, uavID, _, ok := h.resolveMissionDeviceContext(w, r)
	if !ok {
		return 0, false
	}
	return uavID, true
}

func (h *Handlers) resolveMissionDeviceContext(w http.ResponseWriter, r *http.Request) (string, int, *int, bool) {
	deviceClaims, ok := auth.DeviceClaimsFromContext(r.Context())
	if !ok || deviceClaims == nil {
		respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "device token required"})
		return "", 0, nil, false
	}

	switch deviceClaims.ScopeType {
	case auth.DeviceScopeUav:
		if deviceClaims.UavID == nil || *deviceClaims.UavID <= 0 {
			respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return "", 0, nil, false
		}
		return auth.DeviceScopeUav, *deviceClaims.UavID, nil, true
	case auth.DeviceScopeDocking:
		if deviceClaims.DockingID == nil || *deviceClaims.DockingID <= 0 {
			respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return "", 0, nil, false
		}
		uavID, err := h.resolveDockingUavID(*deviceClaims.DockingID)
		if err != nil {
			if err.Error() == "docking not found" {
				respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "Docking not found"})
				return "", 0, nil, false
			}
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
			return "", 0, nil, false
		}
		dockingID := *deviceClaims.DockingID
		return auth.DeviceScopeDocking, uavID, &dockingID, true
	default:
		respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return "", 0, nil, false
	}
}

func (h *Handlers) findCurrentMissionByDeviceScope(scopeType string, uavID int, dockingID *int) (currentMissionLookupResult, error) {
	result := currentMissionLookupResult{}

	var (
		row *sql.Row
		arg interface{}
	)
	switch scopeType {
	case auth.DeviceScopeDocking:
		if dockingID == nil || *dockingID <= 0 {
			return result, fmt.Errorf("missing docking_id for docking scope")
		}
		arg = *dockingID
		row = h.DB.QueryRow(`
	        SELECT id, mission_id, docking_id, status
	        FROM mission_history
	        WHERE docking_id = $1
	          AND completed_at IS NULL
	          AND status NOT IN ('Completed', 'Failed', 'Aborted')
	        ORDER BY started_at DESC NULLS LAST, created_at DESC
	        LIMIT 1`, arg)
	case auth.DeviceScopeUav:
		arg = uavID
		row = h.DB.QueryRow(`
	        SELECT id, mission_id, docking_id, status
	        FROM mission_history
	        WHERE uav_id = $1
	          AND completed_at IS NULL
	          AND status NOT IN ('Completed', 'Failed', 'Aborted')
	        ORDER BY started_at DESC NULLS LAST, created_at DESC
	        LIMIT 1`, arg)
	default:
		return result, fmt.Errorf("unsupported scope type %q", scopeType)
	}

	var nullableDockingID sql.NullInt32
	err := row.Scan(&result.HistoryID, &result.MissionID, &nullableDockingID, &result.Status)
	if err == sql.ErrNoRows {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if nullableDockingID.Valid {
		value := int(nullableDockingID.Int32)
		result.DockingID = &value
	}
	result.Found = true

	return result, nil
}

func (h *Handlers) findNextWaitingMissionByUavID(uavID int) (models.Mission, bool, error) {
	rows, err := h.DB.Query(`
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, recurrence_unit, recurrence_interval, status, created_at
        FROM missions
        WHERE uav_id = $1 AND status = $2 AND deleted_at IS NULL`,
		uavID, "Waiting")
	if err != nil {
		return models.Mission{}, false, err
	}
	defer rows.Close()

	now := time.Now()
	var mission models.Mission
	var bestSchedule time.Time
	found := false
	for rows.Next() {
		var candidate models.Mission
		var recurrenceUnit sql.NullString
		var recurrenceInterval sql.NullInt32
		if err := rows.Scan(
			&candidate.ID,
			&candidate.UserID,
			&candidate.UavID,
			&candidate.MissionName,
			&candidate.Schedule,
			&candidate.IsRecurring,
			&recurrenceUnit,
			&recurrenceInterval,
			&candidate.Status,
			&candidate.Timestamp,
		); err != nil {
			log.Printf("Error scanning mission: %v", err)
			continue
		}
		assignMissionRecurrence(&candidate, recurrenceUnit, recurrenceInterval)

		scheduleTime, ok := parseSchedule(candidate.Schedule)
		if !ok {
			continue
		}
		if scheduleTime.Before(now) {
			continue
		}
		if !found || scheduleTime.Before(bestSchedule) {
			bestSchedule = scheduleTime
			mission = candidate
			found = true
		}
	}
	if !found {
		return models.Mission{}, false, nil
	}

	waypoints, err := h.loadMissionWaypoints(mission.ID)
	if err != nil {
		log.Printf("Error querying waypoints: %v", err)
		return mission, true, nil
	}
	mission.Waypoints = waypoints
	return mission, true, nil
}

func (h *Handlers) findSafeToFlyMissionByUavID(uavID int) (int, string, models.Mission, bool, error) {
	var historyID int
	var runtimeStatus string
	var mission models.Mission
	var recurrenceUnit sql.NullString
	var recurrenceInterval sql.NullInt32
	err := h.DB.QueryRow(`
        SELECT mh.id, mh.status,
               m.id, m.user_id, m.uav_id, m.mission_name, m.schedule, m.is_recurring, m.recurrence_unit, m.recurrence_interval, m.status, m.created_at
        FROM mission_history mh
        INNER JOIN missions m ON m.id = mh.mission_id
        WHERE mh.uav_id = $1
          AND mh.status = $2
          AND m.deleted_at IS NULL
        ORDER BY mh.started_at DESC NULLS LAST, mh.created_at DESC
        LIMIT 1`,
		uavID, missionHistoryStatusSafeToFly,
	).Scan(
		&historyID,
		&runtimeStatus,
		&mission.ID,
		&mission.UserID,
		&mission.UavID,
		&mission.MissionName,
		&mission.Schedule,
		&mission.IsRecurring,
		&recurrenceUnit,
		&recurrenceInterval,
		&mission.Status,
		&mission.Timestamp,
	)
	if err == sql.ErrNoRows {
		return 0, "", models.Mission{}, false, nil
	}
	if err != nil {
		return 0, "", models.Mission{}, false, err
	}
	assignMissionRecurrence(&mission, recurrenceUnit, recurrenceInterval)

	waypoints, err := h.loadMissionWaypoints(mission.ID)
	if err != nil {
		log.Printf("Error querying waypoints: %v", err)
		return historyID, runtimeStatus, mission, true, nil
	}
	mission.Waypoints = waypoints
	return historyID, runtimeStatus, mission, true, nil
}

func (h *Handlers) ensureMissionLookupUavAccess(w http.ResponseWriter, r *http.Request, uavID int) bool {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return false
	}

	if deviceClaims, ok := auth.DeviceClaimsFromContext(r.Context()); ok && deviceClaims != nil {
		switch deviceClaims.ScopeType {
		case auth.DeviceScopeUav:
			if deviceClaims.UavID == nil || *deviceClaims.UavID != uavID {
				respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
				return false
			}
			return true
		case auth.DeviceScopeDocking:
			if deviceClaims.DockingID == nil {
				respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
				return false
			}
			resolvedUavID, err := h.resolveDockingUavID(*deviceClaims.DockingID)
			if err != nil {
				if err.Error() == "docking not found" {
					respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "Docking not found"})
					return false
				}
				respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
				return false
			}
			if resolvedUavID != uavID {
				respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
				return false
			}
			return true
		default:
			respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return false
		}
	}

	if strings.TrimSpace(claims.Subject) == "device" {
		respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return false
	}

	userID, ok := h.requireUserID(w, r)
	if !ok {
		return false
	}

	var owner sql.NullInt32
	err := h.DB.QueryRow(`SELECT owner_id FROM uav WHERE id = $1 AND deleted_at IS NULL`, uavID).Scan(&owner)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "UAV not found"})
		return false
	}
	if err != nil {
		log.Printf("Failed to load UAV owner: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
		return false
	}
	if !owner.Valid || int(owner.Int32) != userID {
		respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return false
	}

	return true
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
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, recurrence_unit, recurrence_interval, status, created_at
        FROM missions
        WHERE user_id = $1 AND deleted_at IS NULL`

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

	query += " ORDER BY created_at DESC"

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	missions := []models.Mission{}
	for rows.Next() {
		var m models.Mission
		var recurrenceUnit sql.NullString
		var recurrenceInterval sql.NullInt32
		if err := rows.Scan(&m.ID, &m.UserID, &m.UavID, &m.MissionName, &m.Schedule, &m.IsRecurring, &recurrenceUnit, &recurrenceInterval, &m.Status, &m.Timestamp); err != nil {
			log.Printf("Error scanning mission: %v", err)
			continue
		}
		assignMissionRecurrence(&m, recurrenceUnit, recurrenceInterval)
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
	var recurrenceUnit sql.NullString
	var recurrenceInterval sql.NullInt32
	missionQuery := `
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, recurrence_unit, recurrence_interval, status, created_at
        FROM missions
        WHERE id = $1 AND deleted_at IS NULL`

	row := h.DB.QueryRow(missionQuery, missionID)
	err = row.Scan(
		&mission.ID,
		&mission.UserID,
		&mission.UavID,
		&mission.MissionName,
		&mission.Schedule,
		&mission.IsRecurring,
		&recurrenceUnit,
		&recurrenceInterval,
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
	assignMissionRecurrence(&mission, recurrenceUnit, recurrenceInterval)

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

func (h *Handlers) loadDockingsByUav(uavID int) ([]models.Docking, error) {
	rows, err := h.DB.Query(`
		SELECT d.id, d.uav_id, d.name, d.location_name, d.latitude, d.longitude,
		       d.is_primary, d.is_active, d.created_at,
		       s.door_open, s.drone_present, s.charging, s.temperature, s.is_online, s.last_heartbeat
		FROM docking d
		LEFT JOIN docking_status s ON s.docking_id = d.id
		WHERE d.uav_id = $1
		ORDER BY d.is_primary DESC, d.created_at DESC`, uavID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dockings := []models.Docking{}
	for rows.Next() {
		var docking models.Docking
		var name sql.NullString
		var location sql.NullString
		var latitude sql.NullFloat64
		var longitude sql.NullFloat64

		var doorOpen sql.NullBool
		var dronePresent sql.NullBool
		var charging sql.NullBool
		var temperature sql.NullFloat64
		var isOnline sql.NullBool
		var lastHeartbeat sql.NullTime
		if err := rows.Scan(
			&docking.ID,
			&docking.UavID,
			&name,
			&location,
			&latitude,
			&longitude,
			&docking.IsPrimary,
			&docking.IsActive,
			&docking.CreatedAt,
			&doorOpen,
			&dronePresent,
			&charging,
			&temperature,
			&isOnline,
			&lastHeartbeat,
		); err != nil {
			log.Printf("Error scanning docking: %v", err)
			continue
		}

		if name.Valid {
			value := name.String
			docking.Name = &value
		}
		if location.Valid {
			value := location.String
			docking.LocationName = &value
		}
		if latitude.Valid {
			value := latitude.Float64
			docking.Latitude = &value
		}
		if longitude.Valid {
			value := longitude.Float64
			docking.Longitude = &value
		}

		hasStatus := doorOpen.Valid || dronePresent.Valid || charging.Valid || temperature.Valid || isOnline.Valid || lastHeartbeat.Valid
		if hasStatus {
			status := models.DockingStatus{}
			if doorOpen.Valid {
				value := doorOpen.Bool
				status.DoorOpen = &value
			}
			if dronePresent.Valid {
				value := dronePresent.Bool
				status.DronePresent = &value
			}
			if charging.Valid {
				value := charging.Bool
				status.Charging = &value
			}
			if temperature.Valid {
				value := temperature.Float64
				status.Temperature = &value
			}
			if isOnline.Valid {
				value := isOnline.Bool
				status.IsOnline = &value
			}
			if lastHeartbeat.Valid {
				value := lastHeartbeat.Time
				status.LastHeartbeat = &value
			}
			docking.Status = &status
		}

		dockings = append(dockings, docking)
	}

	if len(dockings) == 0 {
		return nil, nil
	}
	return dockings, nil
}

func (h *Handlers) GetMissionsByUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID, err := strconv.Atoi(vars["user_id"])
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid user ID"})
		return
	}

	missionQuery := `
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, recurrence_unit, recurrence_interval, status, created_at
        FROM missions
        WHERE user_id = $1 AND deleted_at IS NULL
        ORDER BY created_at DESC`

	rows, err := h.DB.Query(missionQuery, userID)
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	var missions []models.Mission

	for rows.Next() {
		var m models.Mission
		var recurrenceUnit sql.NullString
		var recurrenceInterval sql.NullInt32
		err := rows.Scan(
			&m.ID,
			&m.UserID,
			&m.UavID,
			&m.MissionName,
			&m.Schedule,
			&m.IsRecurring,
			&recurrenceUnit,
			&recurrenceInterval,
			&m.Status,
			&m.Timestamp,
		)
		if err != nil {
			log.Printf("Error scanning mission: %v", err)
			continue
		}
		assignMissionRecurrence(&m, recurrenceUnit, recurrenceInterval)

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

type startMissionResponse struct {
	HistoryID int    `json:"history_id"`
	Status    string `json:"status"`
}

func (h *Handlers) StartMission(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	missionID, err := strconv.Atoi(vars["id"])
	if err != nil || missionID <= 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid mission ID"})
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	var mission models.Mission
	var recurrenceUnit sql.NullString
	var recurrenceInterval sql.NullInt32
	err = tx.QueryRow(`
        SELECT id, user_id, uav_id, mission_name, schedule, is_recurring, recurrence_unit, recurrence_interval, status, created_at
        FROM missions
        WHERE id = $1 AND deleted_at IS NULL`, missionID).Scan(
		&mission.ID,
		&mission.UserID,
		&mission.UavID,
		&mission.MissionName,
		&mission.Schedule,
		&mission.IsRecurring,
		&recurrenceUnit,
		&recurrenceInterval,
		&mission.Status,
		&mission.Timestamp,
	)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "Mission not found"})
		return
	}
	if err != nil {
		log.Printf("Failed to query mission: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}
	assignMissionRecurrence(&mission, recurrenceUnit, recurrenceInterval)

	if !h.authorizeMissionStart(w, r, mission.UserID, mission.UavID) {
		return
	}

	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1)`, mission.UavID); err != nil {
		log.Printf("Failed to acquire UAV lock: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}

	var existingID int
	var existingMissionID int
	err = tx.QueryRow(`
	        SELECT id, mission_id
	        FROM mission_history
	        WHERE completed_at IS NULL
	          AND status NOT IN ('Completed', 'Failed', 'Aborted')
	          AND (mission_id = $1 OR uav_id = $2)
	        ORDER BY created_at DESC
	        LIMIT 1`, missionID, mission.UavID).Scan(&existingID, &existingMissionID)
	if err == nil {
		message := "Mission already in progress"
		if existingMissionID != missionID {
			message = "UAV already has a mission in progress"
		}
		respondWithJSON(w, http.StatusConflict, map[string]interface{}{
			"message":    message,
			"history_id": existingID,
			"mission_id": existingMissionID,
		})
		return
	}
	if err != sql.ErrNoRows {
		log.Printf("Failed to check mission history: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}

	wpRows, err := tx.Query(`
        SELECT id, sequence_order, latitude, longitude, altitude, action, action_duration
        FROM waypoints
        WHERE mission_id = $1
        ORDER BY sequence_order ASC`, missionID)
	if err != nil {
		log.Printf("Failed to load mission waypoints: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}
	defer wpRows.Close()

	waypoints := []models.Waypoint{}
	for wpRows.Next() {
		var wp models.Waypoint
		var actionDuration sql.NullInt64
		if err := wpRows.Scan(
			&wp.ID,
			&wp.SequenceOrder,
			&wp.Latitude,
			&wp.Longitude,
			&wp.Altitude,
			&wp.Action,
			&actionDuration,
		); err != nil {
			log.Printf("Error scanning waypoint: %v", err)
			continue
		}
		if actionDuration.Valid {
			wp.ActionDuration = &actionDuration.Int64
		}
		waypoints = append(waypoints, wp)
	}
	mission.Waypoints = waypoints

	missionSnapshot, err := json.Marshal(mission)
	if err != nil {
		log.Printf("Failed to build mission snapshot: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}

	startedAt := time.Now().UTC()
	var historyID int
	var dockingID sql.NullInt32
	err = tx.QueryRow(`
	        SELECT id
	        FROM docking
	        WHERE uav_id = $1 AND is_active = true
	        ORDER BY is_primary DESC, created_at DESC
	        LIMIT 1`, mission.UavID).Scan(&dockingID)
	if err != nil && err != sql.ErrNoRows {
		log.Printf("Failed to resolve active docking: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}
	err = tx.QueryRow(`
	        INSERT INTO mission_history (mission_id, user_id, uav_id, docking_id, status, started_at, mission_snapshot)
	        VALUES ($1, $2, $3, $4, $5, $6, $7)
	        RETURNING id`,
		missionID,
		mission.UserID,
		mission.UavID,
		dockingID,
		missionHistoryStatusPreparingDock,
		startedAt,
		missionSnapshot,
	).Scan(&historyID)
	if err != nil {
		log.Printf("Failed to insert mission history: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}

	if _, err := tx.Exec(`UPDATE missions SET status = $1 WHERE id = $2`, "InProgress", missionID); err != nil {
		log.Printf("Failed to update mission status: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}

	if err := insertMissionEvent(
		tx,
		historyID,
		nil,
		nullableStringOrNil(missionHistoryStatusPreparingDock),
		nil,
		nil,
		nullableStringOrNil("Mission run started"),
		false,
	); err != nil {
		log.Printf("Failed to insert mission event: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Failed to commit mission start: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start mission"})
		return
	}

	respondWithJSON(w, http.StatusCreated, startMissionResponse{
		HistoryID: historyID,
		Status:    missionHistoryStatusPreparingDock,
	})
}

func (h *Handlers) CompleteMissionByHistoryID(w http.ResponseWriter, r *http.Request) {
	historyID, err := parseMissionHistoryID(r)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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

	if ctx.Status != missionHistoryStatusDockConfirmed {
		respondWithJSON(w, http.StatusConflict, map[string]string{"error": "history must be DockConfirmed before completion"})
		return
	}

	now := time.Now().UTC()
	newMissionStatus, nextSchedule := resolveMissionTemplateAfterTerminalState(
		ctx.IsRecurring,
		ctx.Schedule,
		ctx.RecurrenceUnit,
		ctx.RecurrenceInterval,
		missionHistoryStatusCompleted,
		now,
	)

	if nextSchedule != "" {
		if _, err := tx.Exec(`UPDATE missions SET status = $1, schedule = $2 WHERE id = $3`, newMissionStatus, nextSchedule, ctx.MissionID); err != nil {
			log.Printf("Failed to update mission status/schedule: %v", err)
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update mission status"})
			return
		}
	} else {
		if _, err := tx.Exec(`UPDATE missions SET status = $1 WHERE id = $2`, newMissionStatus, ctx.MissionID); err != nil {
			log.Printf("Failed to update mission status: %v", err)
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update mission status"})
			return
		}
	}

	completedAt := now
	result, err := tx.Exec(`
	        UPDATE mission_history
	        SET status = $1, completed_at = $2
	        WHERE id = $3`, missionHistoryStatusCompleted, completedAt, historyID)
	if err != nil {
		log.Printf("Failed to update mission history: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save mission history"})
		return
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"error": "history not found for mission"})
		return
	}

	if err := insertMissionEvent(
		tx,
		historyID,
		nullableStringOrNil(ctx.Status),
		nullableStringOrNil(missionHistoryStatusCompleted),
		terminalResult(missionHistoryStatusCompleted),
		nil,
		nullableStringOrNil("Mission completed"),
		true,
	); err != nil {
		log.Printf("Failed to insert mission completion event: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to complete mission"})
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Failed to commit mission completion: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to complete mission"})
		return
	}

	response := map[string]string{
		"message":        "Mission completed",
		"status":         missionHistoryStatusCompleted,
		"mission_status": newMissionStatus,
	}
	if nextSchedule != "" {
		response["next_schedule"] = nextSchedule
	}
	respondWithJSON(w, http.StatusOK, response)
}
