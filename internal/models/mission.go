package models

import (
	"time"
)

type Mission struct {
	ID            int        `json:"id" db:"id"`
	UserID        int        `json:"user_id" db:"user_id"`
	UavID         int        `json:"uav_id" db:"uav_id"`
	MissionName   string     `json:"mission_name" db:"mission_name"`
	Schedule      string     `json:"schedule" db:"schedule"`
	IsRecurring   bool       `json:"is_recurring" db:"is_recurring"`
	Status        string     `json:"status" db:"status"`
	Timestamp     time.Time  `json:"timestamp" db:"created_at"`
	DeletedAt     *time.Time `json:"deleted_at" db:"deleted_at"`
	Waypoints     []Waypoint `json:"waypoints"`
	WaypointCount *int       `json:"waypoint_count"`
}

type MissionListItem struct {
	ID            int        `json:"id"`
	UserID        int        `json:"user_id"`
	UavID         int        `json:"uav_id"`
	MissionName   string     `json:"mission_name"`
	Schedule      string     `json:"schedule"`
	IsRecurring   bool       `json:"is_recurring"`
	Status        string     `json:"status"`
	Timestamp     time.Time  `json:"timestamp"`
	DeletedAt     *time.Time `json:"deleted_at"`
	WaypointCount *int       `json:"waypoint_count"`
	Uav           *Uav       `json:"uav"`
}

type MissionListResponse struct {
	Page       int               `json:"page"`
	Limit      int               `json:"limit"`
	Total      int               `json:"total"`
	TotalPages int               `json:"total_pages"`
	HasNext    bool              `json:"has_next"`
	HasPrev    bool              `json:"has_prev"`
	NextPage   *int              `json:"next_page"`
	PrevPage   *int              `json:"prev_page"`
	Items      []MissionListItem `json:"items"`
}

type Waypoint struct {
	ID            int     `json:"id" db:"id"`
	MissionID     int     `json:"mission_id" db:"mission_id"`
	SequenceOrder int     `json:"sequence_order" db:"sequence_order"`
	Latitude      float64 `json:"latitude" db:"latitude"`
	Longitude     float64 `json:"longitude" db:"longitude"`
	Altitude      float64 `json:"altitude" db:"altitude"`
	Action        *string `json:"action" db:"action"`
	// ActionDuration *int      `json:"action_duration" db:"action_duration"`
	ActionDuration *int64 `json:"action_duration"`
}
type FullMissionRequest struct {
	UserID      int        `json:"user_id"`
	UavID       int        `json:"uav_id"`
	MissionName string     `json:"mission_name"`
	Schedule    string     `json:"schedule"`
	IsRecurring bool       `json:"is_recurring"`
	Status      string     `json:"status"`
	Waypoints   []Waypoint `json:"waypoints"`
}
type Location struct {
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Timestamp time.Time `json:"timestamp"`
}
type VideoUploadResponse struct {
	ID       int    `json:"id"`
	UAVID    int    `json:"uav_id"`
	Filename string `json:"filename"`
	Message  string `json:"message"`
}
type TelemetryResponse struct {
	Telemetry []TelemetryLog `json:"telemetry"`
}
type TelemetryLog struct {
	MissionID  int     `json:"mission_id"`
	UAVID      int     `json:"uav_id"`
	Waypoints  int     `json:"waypoints"`
	Altitude   float64 `json:"altitude"`
	Coordinate string  `json:"coordinate"`
	Battery    float64 `json:"battery"`
	Timestamp  string  `json:"timestamp"`
}

type BatchMissionLogRequest struct {
	Logs []TelemetryLog `json:"logs"`
}
