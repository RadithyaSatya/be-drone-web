package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"xflight-backend/internal/telemetry"
)

type realtimeTelemetryRequest struct {
	DroneID string                 `json:"drone_id"`
	Kind    string                 `json:"kind"`
	Metric  string                 `json:"metric,omitempty"`
	Payload map[string]interface{} `json:"payload"`
}

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
	if req.Kind == telemetry.KindTelemetry && req.Metric == "" {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "metric is required for telemetry kind"})
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
