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
	"xflight-backend/internal/telemetry"
)

type realtimeTelemetryRequest struct {
	DroneID string                 `json:"drone_id"`
	Kind    string                 `json:"kind"`
	Metric  string                 `json:"metric"`
	Payload map[string]interface{} `json:"payload"`
}

const (
	statusMetricUav     = "uav_status"
	statusMetricDocking = "docking_status"
)

func (h *Handlers) SubmitRealtimeTelemetry(w http.ResponseWriter, r *http.Request) {
	if h.RealtimeHub == nil {
		respondWithJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "realtime hub not ready"})
		return
	}

	var req realtimeTelemetryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	req.DroneID = strings.TrimSpace(req.DroneID)
	req.Kind = strings.TrimSpace(req.Kind)
	req.Metric = strings.TrimSpace(req.Metric)
	if req.DroneID == "" {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "drone_id is required"})
		return
	}
	if req.Payload == nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "payload is required"})
		return
	}
	if req.Kind != telemetry.KindStatus && req.Kind != telemetry.KindTelemetry {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "kind must be telemetry or status"})
		return
	}
	if req.Metric == "" {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "metric is required"})
		return
	}

	if req.Kind == telemetry.KindStatus {
		normalized := normalizeStatusMetric(req.Metric)
		if normalized == "" {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid metric for status kind"})
			return
		}
		req.Metric = normalized
		switch normalized {
		case statusMetricUav:
			h.handleRealtimeUavStatus(w, r, req)
		case statusMetricDocking:
			h.handleRealtimeDockingStatus(w, r, req)
		default:
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid metric for status kind"})
		}
		return
	}

	msg := &telemetry.Message{
		DroneID:   req.DroneID,
		Kind:      req.Kind,
		Metric:    req.Metric,
		Timestamp: time.Now().UTC(),
		Payload:   req.Payload,
	}
	h.RealtimeHub.Broadcast(msg)
	respondWithJSON(w, http.StatusAccepted, map[string]string{"message": "queued"})
}

func normalizeStatusMetric(metric string) string {
	switch strings.ToLower(strings.TrimSpace(metric)) {
	case "uav", statusMetricUav:
		return statusMetricUav
	case "docking", statusMetricDocking:
		return statusMetricDocking
	default:
		return ""
	}
}

func (h *Handlers) handleRealtimeUavStatus(w http.ResponseWriter, r *http.Request, req realtimeTelemetryRequest) {
	if req.Payload == nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "payload is required"})
		return
	}

	uavID, err := h.resolveUavID(req.DroneID)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if !h.ensureRealtimeUavAccess(w, r, uavID) {
		return
	}

	batteryPercent, err := getPayloadInt(req.Payload, "battery_percent")
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if batteryPercent != nil && (*batteryPercent < 0 || *batteryPercent > 100) {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "battery_percent must be between 0 and 100"})
		return
	}

	isInFlight, err := getPayloadBool(req.Payload, "is_in_flight")
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	isDocked, err := getPayloadBool(req.Payload, "is_docked")
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var battery sql.NullInt32
	if batteryPercent != nil {
		battery = sql.NullInt32{Int32: int32(*batteryPercent), Valid: true}
	}
	var inFlight sql.NullBool
	if isInFlight != nil {
		inFlight = sql.NullBool{Bool: *isInFlight, Valid: true}
	}
	var docked sql.NullBool
	if isDocked != nil {
		docked = sql.NullBool{Bool: *isDocked, Valid: true}
	}

	lastHeartbeat := time.Now().UTC()

	var storedBattery sql.NullInt32
	var storedInFlight sql.NullBool
	var storedDocked sql.NullBool
	var storedHeartbeat sql.NullTime

	err = h.DB.QueryRow(`
		INSERT INTO uav_status (uav_id, battery_percent, is_in_flight, is_docked, last_heartbeat)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (uav_id) DO UPDATE SET
			battery_percent = COALESCE(EXCLUDED.battery_percent, uav_status.battery_percent),
			is_in_flight = COALESCE(EXCLUDED.is_in_flight, uav_status.is_in_flight),
			is_docked = COALESCE(EXCLUDED.is_docked, uav_status.is_docked),
			last_heartbeat = EXCLUDED.last_heartbeat
		RETURNING battery_percent, is_in_flight, is_docked, last_heartbeat`,
		uavID,
		battery,
		inFlight,
		docked,
		lastHeartbeat,
	).Scan(&storedBattery, &storedInFlight, &storedDocked, &storedHeartbeat)
	if err != nil {
		log.Printf("Failed to upsert UAV status: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update UAV status"})
		return
	}

	status := models.UavStatus{}
	if storedBattery.Valid {
		value := int(storedBattery.Int32)
		status.BatteryPercent = &value
	}
	if storedInFlight.Valid {
		value := storedInFlight.Bool
		status.IsInFlight = &value
	}
	if storedDocked.Valid {
		value := storedDocked.Bool
		status.IsDocked = &value
	}
	if storedHeartbeat.Valid {
		value := storedHeartbeat.Time
		status.LastHeartbeat = &value
	}

	if h.RealtimeHub != nil {
		payload := map[string]interface{}{
			"uav_id":          uavID,
			"battery_percent": status.BatteryPercent,
			"is_in_flight":    status.IsInFlight,
			"is_docked":       status.IsDocked,
			"last_heartbeat":  status.LastHeartbeat,
		}
		h.RealtimeHub.Broadcast(&telemetry.Message{
			DroneID:   strconv.Itoa(uavID),
			Kind:      telemetry.KindStatus,
			Metric:    req.Metric,
			Timestamp: time.Now().UTC(),
			Payload:   payload,
		})
	}

	respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"uav_id":  uavID,
		"status":  status,
		"message": "UAV status updated",
	})
}

func (h *Handlers) handleRealtimeDockingStatus(w http.ResponseWriter, r *http.Request, req realtimeTelemetryRequest) {
	if req.Payload == nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "payload is required"})
		return
	}

	uavID, err := h.resolveUavID(req.DroneID)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	payloadDockingID, err := getPayloadInt(req.Payload, "docking_id")
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	deviceClaims, hasDevice := auth.DeviceClaimsFromContext(r.Context())
	var dockingID int
	if hasDevice && deviceClaims != nil {
		switch deviceClaims.ScopeType {
		case auth.DeviceScopeDocking:
			if deviceClaims.DockingID == nil {
				respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			if payloadDockingID != nil && *payloadDockingID != *deviceClaims.DockingID {
				respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
				return
			}
			dockingID = *deviceClaims.DockingID
			dockingUavID, err := h.resolveDockingUavID(dockingID)
			if err != nil {
				respondWithJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
				return
			}
			if dockingUavID != uavID {
				respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
				return
			}
		case auth.DeviceScopeUav:
			if !h.ensureRealtimeUavAccess(w, r, uavID) {
				return
			}
			if payloadDockingID != nil {
				dockingUavID, err := h.resolveDockingUavID(*payloadDockingID)
				if err != nil {
					respondWithJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
					return
				}
				if dockingUavID != uavID {
					respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
					return
				}
				dockingID = *payloadDockingID
			} else {
				dockingID, err = h.resolveDockingIDByUav(uavID)
				if err != nil {
					respondWithJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
					return
				}
			}
		default:
			respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
	} else {
		if !h.ensureRealtimeUavAccess(w, r, uavID) {
			return
		}
		if payloadDockingID != nil {
			dockingUavID, err := h.resolveDockingUavID(*payloadDockingID)
			if err != nil {
				respondWithJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
				return
			}
			if dockingUavID != uavID {
				respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
				return
			}
			dockingID = *payloadDockingID
		} else {
			dockingID, err = h.resolveDockingIDByUav(uavID)
			if err != nil {
				respondWithJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
				return
			}
		}
	}

	doorOpen, err := getPayloadBool(req.Payload, "door_open")
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	dronePresent, err := getPayloadBool(req.Payload, "drone_present")
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	charging, err := getPayloadBool(req.Payload, "charging")
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	isOnline, err := getPayloadBool(req.Payload, "is_online")
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if isOnline == nil {
		if alias, err := getPayloadBool(req.Payload, "online"); err == nil && alias != nil {
			isOnline = alias
		} else if err != nil {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	temperature, err := getPayloadFloat(req.Payload, "temperature")
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var doorOpenValue sql.NullBool
	if doorOpen != nil {
		doorOpenValue = sql.NullBool{Bool: *doorOpen, Valid: true}
	}
	var dronePresentValue sql.NullBool
	if dronePresent != nil {
		dronePresentValue = sql.NullBool{Bool: *dronePresent, Valid: true}
	}
	var chargingValue sql.NullBool
	if charging != nil {
		chargingValue = sql.NullBool{Bool: *charging, Valid: true}
	}
	var isOnlineValue sql.NullBool
	if isOnline != nil {
		isOnlineValue = sql.NullBool{Bool: *isOnline, Valid: true}
	}
	var temperatureValue sql.NullFloat64
	if temperature != nil {
		temperatureValue = sql.NullFloat64{Float64: *temperature, Valid: true}
	}

	lastHeartbeat := time.Now().UTC()

	var storedDoorOpen sql.NullBool
	var storedDronePresent sql.NullBool
	var storedCharging sql.NullBool
	var storedTemperature sql.NullFloat64
	var storedIsOnline sql.NullBool
	var storedHeartbeat sql.NullTime

	err = h.DB.QueryRow(`
		INSERT INTO docking_status (docking_id, door_open, drone_present, charging, temperature, is_online, last_heartbeat)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (docking_id) DO UPDATE SET
			door_open = COALESCE(EXCLUDED.door_open, docking_status.door_open),
			drone_present = COALESCE(EXCLUDED.drone_present, docking_status.drone_present),
			charging = COALESCE(EXCLUDED.charging, docking_status.charging),
			temperature = COALESCE(EXCLUDED.temperature, docking_status.temperature),
			is_online = COALESCE(EXCLUDED.is_online, docking_status.is_online),
			last_heartbeat = EXCLUDED.last_heartbeat
		RETURNING door_open, drone_present, charging, temperature, is_online, last_heartbeat`,
		dockingID,
		doorOpenValue,
		dronePresentValue,
		chargingValue,
		temperatureValue,
		isOnlineValue,
		lastHeartbeat,
	).Scan(&storedDoorOpen, &storedDronePresent, &storedCharging, &storedTemperature, &storedIsOnline, &storedHeartbeat)
	if err != nil {
		log.Printf("Failed to upsert docking status: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update docking status"})
		return
	}

	status := models.DockingStatus{}
	if storedDoorOpen.Valid {
		value := storedDoorOpen.Bool
		status.DoorOpen = &value
	}
	if storedDronePresent.Valid {
		value := storedDronePresent.Bool
		status.DronePresent = &value
	}
	if storedCharging.Valid {
		value := storedCharging.Bool
		status.Charging = &value
	}
	if storedTemperature.Valid {
		value := storedTemperature.Float64
		status.Temperature = &value
	}
	if storedIsOnline.Valid {
		value := storedIsOnline.Bool
		status.IsOnline = &value
	}
	if storedHeartbeat.Valid {
		value := storedHeartbeat.Time
		status.LastHeartbeat = &value
	}

	if h.RealtimeHub != nil {
		payload := map[string]interface{}{
			"docking_id":     dockingID,
			"uav_id":         uavID,
			"door_open":      status.DoorOpen,
			"drone_present":  status.DronePresent,
			"charging":       status.Charging,
			"temperature":    status.Temperature,
			"is_online":      status.IsOnline,
			"last_heartbeat": status.LastHeartbeat,
		}
		h.RealtimeHub.Broadcast(&telemetry.Message{
			DroneID:   strconv.Itoa(uavID),
			Kind:      telemetry.KindStatus,
			Metric:    req.Metric,
			Timestamp: time.Now().UTC(),
			Payload:   payload,
		})
	}

	respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"docking_id": dockingID,
		"status":     status,
		"message":    "Docking status updated",
	})
}

func (h *Handlers) ensureRealtimeUavAccess(w http.ResponseWriter, r *http.Request, uavID int) bool {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return false
	}

	if deviceClaims, ok := auth.DeviceClaimsFromContext(r.Context()); ok && deviceClaims != nil {
		if deviceClaims.ScopeType != auth.DeviceScopeUav {
			respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return false
		}
		if deviceClaims.UavID == nil || *deviceClaims.UavID != uavID {
			respondWithJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return false
		}
		return true
	}

	isDevice := strings.TrimSpace(claims.Subject) == "device"
	if isDevice {
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

func (h *Handlers) resolveDockingIDByUav(uavID int) (int, error) {
	var dockingID int
	err := h.DB.QueryRow(`
		SELECT id
		FROM docking
		WHERE uav_id = $1 AND is_active = true
		ORDER BY is_primary DESC, created_at DESC
		LIMIT 1`, uavID).Scan(&dockingID)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("docking not found")
	}
	if err != nil {
		log.Printf("Failed to resolve docking by uav_id: %v", err)
		return 0, fmt.Errorf("database error")
	}
	return dockingID, nil
}

func (h *Handlers) resolveDockingUavID(dockingID int) (int, error) {
	var uavID int
	err := h.DB.QueryRow(`
		SELECT uav_id
		FROM docking
		WHERE id = $1 AND is_active = true`,
		dockingID,
	).Scan(&uavID)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("docking not found")
	}
	if err != nil {
		log.Printf("Failed to resolve docking uav_id: %v", err)
		return 0, fmt.Errorf("database error")
	}
	return uavID, nil
}

func (h *Handlers) resolveUavID(droneID string) (int, error) {
	if id, err := strconv.Atoi(droneID); err == nil && id > 0 {
		return id, nil
	}

	var id int
	err := h.DB.QueryRow(`SELECT id FROM uav WHERE serial_number = $1 AND deleted_at IS NULL`, droneID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("uav not found")
	}
	if err != nil {
		log.Printf("Failed to resolve UAV by serial_number: %v", err)
		return 0, fmt.Errorf("database error")
	}
	return id, nil
}

func getPayloadInt(payload map[string]interface{}, key string) (*int, error) {
	value, ok := payload[key]
	if !ok || value == nil {
		return nil, nil
	}
	switch v := value.(type) {
	case float64:
		parsed := int(v)
		return &parsed, nil
	case int:
		parsed := v
		return &parsed, nil
	case int32:
		parsed := int(v)
		return &parsed, nil
	case int64:
		parsed := int(v)
		return &parsed, nil
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return nil, fmt.Errorf("%s must be a number", key)
		}
		return &parsed, nil
	default:
		return nil, fmt.Errorf("%s must be a number", key)
	}
}

func getPayloadFloat(payload map[string]interface{}, key string) (*float64, error) {
	value, ok := payload[key]
	if !ok || value == nil {
		return nil, nil
	}
	switch v := value.(type) {
	case float64:
		return &v, nil
	case float32:
		parsed := float64(v)
		return &parsed, nil
	case int:
		parsed := float64(v)
		return &parsed, nil
	case int32:
		parsed := float64(v)
		return &parsed, nil
	case int64:
		parsed := float64(v)
		return &parsed, nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return nil, fmt.Errorf("%s must be a number", key)
		}
		return &parsed, nil
	default:
		return nil, fmt.Errorf("%s must be a number", key)
	}
}

func getPayloadBool(payload map[string]interface{}, key string) (*bool, error) {
	value, ok := payload[key]
	if !ok || value == nil {
		return nil, nil
	}
	switch v := value.(type) {
	case bool:
		parsed := v
		return &parsed, nil
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return nil, fmt.Errorf("%s must be true or false", key)
		}
		return &parsed, nil
	default:
		return nil, fmt.Errorf("%s must be true or false", key)
	}
}
