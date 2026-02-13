package telemetry

import "time"

const (
	KindStatus    = "status"
	KindTelemetry = "telemetry"
)

type Message struct {
	DroneID   string      `json:"drone_id"`
	Kind      string      `json:"kind"`
	Metric    string      `json:"metric,omitempty"`
	Timestamp time.Time   `json:"ts"`
	Payload   interface{} `json:"payload"`
}

type StatusPayload struct {
	Armed          *bool    `json:"armed,omitempty"`
	Mode           string   `json:"mode,omitempty"`
	Altitude       *float64 `json:"altitude,omitempty"`
	Velocity       *float64 `json:"velocity,omitempty"`
	Failsafe       *bool    `json:"failsafe,omitempty"`
	BatteryPercent *float64 `json:"battery_percent,omitempty"`
	State          string   `json:"state,omitempty"`
}

type TelemetryPayload map[string]interface{}
