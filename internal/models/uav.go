package models

import "time"

type Uav struct {
	ID               int              `json:"id"`
	OwnerID          *int             `json:"owner_id"`
	SerialNumber     *string          `json:"serial_number"`
	Name             *string          `json:"name"`
	Model            *string          `json:"model"`
	FirmwareVersion  *string          `json:"firmware_version"`
	CameraSpec       *string          `json:"camera_spec"`
	ImageURL         *string          `json:"image_url"`
	MaxRangeMeter    *int             `json:"max_range_meter"`
	MaxFlightTimeMin *int             `json:"max_flight_time_min"`
	IsActive         bool             `json:"is_active"`
	CreatedAt        time.Time        `json:"created_at"`
	Status           *UavStatus       `json:"status"`
	Dockings         []Docking        `json:"dockings"`
}

type UavStatus struct {
	BatteryPercent *int       `json:"battery_percent"`
	IsInFlight     *bool      `json:"is_in_flight"`
	IsDocked       *bool      `json:"is_docked"`
	LastHeartbeat  *time.Time `json:"last_heartbeat"`
}

type UavListResponse struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int   `json:"total"`
	TotalPages int   `json:"total_pages"`
	HasNext    bool  `json:"has_next"`
	HasPrev    bool  `json:"has_prev"`
	NextPage   *int  `json:"next_page"`
	PrevPage   *int  `json:"prev_page"`
	Items      []Uav `json:"items"`
}
