package models

import (
	"encoding/json"
	"time"
)

type MissionHistoryEntry struct {
	ID              int                   `json:"id"`
	MissionID       int                   `json:"mission_id"`
	UserID          int                   `json:"user_id"`
	UavID           int                   `json:"uav_id"`
	Status          string                `json:"status"`
	FailureReason   *string               `json:"failure_reason"`
	StartedAt       *time.Time            `json:"started_at"`
	CompletedAt     *time.Time            `json:"completed_at"`
	MediaCount      int                   `json:"media_count"`
	CreatedAt       time.Time             `json:"created_at"`
	MissionSnapshot json.RawMessage       `json:"mission_snapshot"`
	Media           []MissionHistoryMedia `json:"media"`
}

type MissionHistoryMedia struct {
	ID           int       `json:"id"`
	HistoryID    int       `json:"history_id"`
	EventID      *int      `json:"event_id,omitempty"`
	MediaType    string    `json:"media_type"`
	FilePath     string    `json:"file_path"`
	PublicPath   string    `json:"public_path,omitempty"`
	DownloadPath string    `json:"download_path,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type MissionHistoryEvent struct {
	ID          int64     `json:"id"`
	HistoryID   int       `json:"history_id"`
	FromState   *string   `json:"from_state"`
	ToState     *string   `json:"to_state"`
	Result      *string   `json:"result"`
	FailureCode *string   `json:"failure_code"`
	Message     *string   `json:"message"`
	IsTerminal  bool      `json:"is_terminal"`
	CreatedAt   time.Time `json:"created_at"`
}

type MissionHistoryListItem struct {
	ID            int        `json:"id"`
	MissionID     int        `json:"mission_id"`
	MissionName   string     `json:"mission_name"`
	UserID        int        `json:"user_id"`
	UserName      *string    `json:"user_name"`
	UavID         int        `json:"uav_id"`
	Status        string     `json:"status"`
	FailureReason *string    `json:"failure_reason"`
	StartedAt     *time.Time `json:"started_at"`
	CompletedAt   *time.Time `json:"completed_at"`
	DurationSec   *int64     `json:"duration_seconds"`
	CreatedAt     time.Time  `json:"created_at"`
}

type MissionHistoryListResponse struct {
	Page       int                   `json:"page"`
	Limit      int                   `json:"limit"`
	Total      int                   `json:"total"`
	TotalPages int                   `json:"total_pages"`
	HasNext    bool                  `json:"has_next"`
	HasPrev    bool                  `json:"has_prev"`
	NextPage   *int                  `json:"next_page"`
	PrevPage   *int                  `json:"prev_page"`
	Items      []MissionHistoryEntry `json:"items"`
}

type MissionHistoryEventListResponse struct {
	HistoryID int                   `json:"history_id"`
	Count     int                   `json:"count"`
	Items     []MissionHistoryEvent `json:"items"`
}

type MissionHistoryStateResponse struct {
	HistoryID     int        `json:"history_id"`
	MissionID     int        `json:"mission_id"`
	UserID        int        `json:"user_id"`
	UavID         int        `json:"uav_id"`
	DockingID     *int       `json:"docking_id,omitempty"`
	Status        string     `json:"status"`
	IsTerminal    bool       `json:"is_terminal"`
	FailureCode   *string    `json:"failure_code"`
	FailureReason *string    `json:"failure_reason"`
	CompletedAt   *time.Time `json:"completed_at"`
	LastEventAt   *time.Time `json:"last_event_at"`
}
