package telemetry

import "time"

const (
	KindStatus    = "status"
	KindTelemetry = "telemetry"
)

type Message struct {
	DroneID   string      `json:"drone_id"`
	Kind      string      `json:"kind"`
	Metric    string      `json:"metric"`
	Timestamp time.Time   `json:"ts"`
	Payload   interface{} `json:"payload"`
}

type StatusPayload struct {
	Armed          *bool    `json:"armed"`
	Mode           string   `json:"mode"`
	Altitude       *float64 `json:"altitude"`
	Velocity       *float64 `json:"velocity"`
	Failsafe       *bool    `json:"failsafe"`
	BatteryPercent *float64 `json:"battery_percent"`
	State          string   `json:"state"`
}

type TelemetryPayload map[string]interface{}
