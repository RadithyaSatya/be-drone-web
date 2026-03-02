package models

import "time"

type DockingStatus struct {
	DoorOpen      *bool      `json:"door_open"`
	DronePresent  *bool      `json:"drone_present"`
	Charging      *bool      `json:"charging"`
	Temperature   *float64   `json:"temperature"`
	IsOnline      *bool      `json:"is_online"`
	LastHeartbeat *time.Time `json:"last_heartbeat"`
}

type Docking struct {
	ID           int            `json:"id"`
	UavID        int            `json:"uav_id"`
	Name         *string        `json:"name"`
	LocationName *string        `json:"location_name"`
	Latitude     *float64       `json:"latitude"`
	Longitude    *float64       `json:"longitude"`
	IsPrimary    bool           `json:"is_primary"`
	IsActive     bool           `json:"is_active"`
	CreatedAt    time.Time      `json:"created_at"`
	Status       *DockingStatus `json:"status"`
}
