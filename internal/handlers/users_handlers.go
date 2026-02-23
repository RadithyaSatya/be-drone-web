package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"xflight-backend/internal/models"

	"github.com/lib/pq"
)

const (
	defaultUsersPage  = 1
	defaultUsersLimit = 20
	maxUsersLimit     = 100
	defaultBootstrapKey      = "change-me"
	defaultBootstrapEmail    = "default@example.com"
	defaultBootstrapUsername = "default"
	defaultBootstrapPassword = "change-me"
)

type createUserRequest struct {
	Email        string `json:"email"`
	DOB          string `json:"dob"`
	Phone        string `json:"phone"`
	Username     string `json:"username"`
	PilotCert    string `json:"pilot_cert"`
	Password     string `json:"password"`
	PasswordHash string `json:"password_hash"`
}

type createUserResponse struct {
	ID      int    `json:"id"`
	Message string `json:"message"`
}

type bootstrapUserRequest struct {
	Key string `json:"key"`
}

type bootstrapUserResponse struct {
	ID      int    `json:"id,omitempty"`
	Message string `json:"message"`
}

func (h *Handlers) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	req.Username = strings.TrimSpace(req.Username)
	if req.Email == "" || req.Username == "" {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "email and username are required"})
		return
	}

	if req.PasswordHash == "" {
		if strings.TrimSpace(req.Password) == "" {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "password or password_hash is required"})
			return
		}
		hashed, err := hashPassword(req.Password)
		if err != nil {
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to hash password"})
			return
		}
		req.PasswordHash = hashed
	} else {
		if err := validatePasswordHash(req.PasswordHash); err != nil {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported password_hash format"})
			return
		}
	}

	var dob sql.NullTime
	if strings.TrimSpace(req.DOB) != "" {
		parsed, err := time.Parse("2006-01-02", req.DOB)
		if err != nil {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "dob must be YYYY-MM-DD"})
			return
		}
		dob = sql.NullTime{Time: parsed, Valid: true}
	}

	phone := nullString(req.Phone)
	pilotCert := nullString(req.PilotCert)

	var id int
	query := `
		INSERT INTO users (email, dob, phone, username, pilot_cert, password_hash)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`
	err := h.DB.QueryRow(query, req.Email, dob, phone, req.Username, pilotCert, req.PasswordHash).Scan(&id)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			respondWithJSON(w, http.StatusConflict, map[string]string{"error": "email or username already exists"})
			return
		}
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create user"})
		return
	}

	respondWithJSON(w, http.StatusCreated, createUserResponse{
		ID:      id,
		Message: "User created successfully",
	})
}

func (h *Handlers) CreateDefaultUser(w http.ResponseWriter, r *http.Request) {
	var req bootstrapUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
		return
	}

	if strings.TrimSpace(req.Key) == "" || req.Key != defaultBootstrapKey {
		respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid key"})
		return
	}

	var existingID int
	err := h.DB.QueryRow(
		`SELECT id FROM users WHERE username = $1 OR email = $2`,
		defaultBootstrapUsername,
		defaultBootstrapEmail,
	).Scan(&existingID)
	if err == nil {
		respondWithJSON(w, http.StatusConflict, bootstrapUserResponse{
			ID:      existingID,
			Message: "account default sudah ada",
		})
		return
	}
	if err != sql.ErrNoRows {
		log.Printf("Failed to check default user: %v", err)
		respondWithErrorDetail(w, http.StatusInternalServerError, "Failed to create user", err)
		return
	}

	hashed, err := hashPassword(defaultBootstrapPassword)
	if err != nil {
		respondWithErrorDetail(w, http.StatusInternalServerError, "failed to hash password", err)
		return
	}

	var id int
	query := `
		INSERT INTO users (email, dob, phone, username, pilot_cert, password_hash)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`
	err = h.DB.QueryRow(
		query,
		defaultBootstrapEmail,
		sql.NullTime{},
		sql.NullString{},
		defaultBootstrapUsername,
		sql.NullString{},
		hashed,
	).Scan(&id)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			respondWithJSON(w, http.StatusConflict, bootstrapUserResponse{
				Message: "account default sudah ada",
			})
			return
		}
		respondWithErrorDetail(w, http.StatusInternalServerError, "Failed to create user", err)
		return
	}

	respondWithJSON(w, http.StatusCreated, bootstrapUserResponse{
		ID:      id,
		Message: "User created successfully",
	})
}

func (h *Handlers) ListUsers(w http.ResponseWriter, r *http.Request) {
	page, limit := parsePagination(r, defaultUsersPage, defaultUsersLimit, maxUsersLimit)
	offset := (page - 1) * limit

	var total int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&total); err != nil {
		log.Printf("Error counting users: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load users"})
		return
	}

	rows, err := h.DB.Query(`
        SELECT id, email, dob, phone, username, pilot_cert, created_at
        FROM users
        ORDER BY created_at DESC
        LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		log.Printf("Error querying users: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load users"})
		return
	}
	defer rows.Close()

	items := []models.UserListItem{}
	for rows.Next() {
		var item models.UserListItem
		var dob sql.NullTime
		var phone sql.NullString
		var pilotCert sql.NullString
		if err := rows.Scan(
			&item.ID,
			&item.Email,
			&dob,
			&phone,
			&item.Username,
			&pilotCert,
			&item.CreatedAt,
		); err != nil {
			log.Printf("Error scanning user: %v", err)
			continue
		}
		if dob.Valid {
			item.DOB = &dob.Time
		}
		if phone.Valid {
			item.Phone = &phone.String
		}
		if pilotCert.Valid {
			item.PilotCert = &pilotCert.String
		}
		items = append(items, item)
	}

	totalPages := 0
	if total > 0 {
		totalPages = (total + limit - 1) / limit
	}
	hasNext := totalPages > 0 && page < totalPages
	hasPrev := totalPages > 0 && page > 1
	var nextPage *int
	var prevPage *int
	if hasNext {
		n := page + 1
		nextPage = &n
	}
	if hasPrev {
		p := page - 1
		prevPage = &p
	}

	respondWithJSON(w, http.StatusOK, models.UserListResponse{
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
		HasNext:    hasNext,
		HasPrev:    hasPrev,
		NextPage:   nextPage,
		PrevPage:   prevPage,
		Items:      items,
	})
}

func nullString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}
