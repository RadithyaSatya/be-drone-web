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
	FailureReason   *string               `json:"failure_reason,omitempty"`
	StartedAt       *time.Time            `json:"started_at,omitempty"`
	CompletedAt     *time.Time            `json:"completed_at,omitempty"`
	CreatedAt       time.Time             `json:"created_at"`
	MissionSnapshot json.RawMessage       `json:"mission_snapshot,omitempty"`
	Media           []MissionHistoryMedia `json:"media,omitempty"`
}

type MissionHistoryResponse struct {
	Mission Mission               `json:"mission"`
	History []MissionHistoryEntry `json:"history"`
}

type MissionHistoryMedia struct {
	ID        int       `json:"id"`
	HistoryID int       `json:"history_id"`
	MissionID int       `json:"mission_id"`
	MediaType string    `json:"media_type"`
	FilePath  string    `json:"file_path"`
	CreatedAt time.Time `json:"created_at"`
}

type MissionHistoryListItem struct {
	ID            int        `json:"id"`
	MissionID     int        `json:"mission_id"`
	MissionName   string     `json:"mission_name"`
	UserID        int        `json:"user_id"`
	UserName      *string    `json:"user_name,omitempty"`
	UavID         int        `json:"uav_id"`
	Status        string     `json:"status"`
	FailureReason *string    `json:"failure_reason,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	DurationSec   *int64     `json:"duration_seconds,omitempty"`
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
