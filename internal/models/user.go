package models

import "time"

type UserListItem struct {
	ID        int        `json:"id"`
	Email     string     `json:"email"`
	Username  string     `json:"username"`
	DOB       *time.Time `json:"dob,omitempty"`
	Phone     *string    `json:"phone,omitempty"`
	PilotCert *string    `json:"pilot_cert,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type UserListResponse struct {
	Page       int            `json:"page"`
	Limit      int            `json:"limit"`
	Total      int            `json:"total"`
	TotalPages int            `json:"total_pages"`
	HasNext    bool           `json:"has_next"`
	HasPrev    bool           `json:"has_prev"`
	NextPage   *int           `json:"next_page"`
	PrevPage   *int           `json:"prev_page"`
	Items      []UserListItem `json:"items"`
}
