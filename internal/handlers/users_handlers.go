package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"xflight-backend/internal/models"

	"github.com/lib/pq"
)

const (
	defaultUsersPage         = 1
	defaultUsersLimit        = 20
	maxUsersLimit            = 100
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

type updateProfileRequest struct {
	Email     *string `json:"email"`
	DOB       *string `json:"dob"`
	Phone     *string `json:"phone"`
	Username  *string `json:"username"`
	PilotCert *string `json:"pilot_cert"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
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

func (h *Handlers) UpdateMyProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	var req updateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
		return
	}

	setClauses := []string{}
	args := []interface{}{}

	if req.Email != nil {
		email := strings.TrimSpace(*req.Email)
		if email == "" {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "email cannot be empty"})
			return
		}
		setClauses = append(setClauses, fmt.Sprintf("email = $%d", len(args)+1))
		args = append(args, email)
	}

	if req.Username != nil {
		username := strings.TrimSpace(*req.Username)
		if username == "" {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "username cannot be empty"})
			return
		}
		setClauses = append(setClauses, fmt.Sprintf("username = $%d", len(args)+1))
		args = append(args, username)
	}

	if req.DOB != nil {
		dobStr := strings.TrimSpace(*req.DOB)
		var dob sql.NullTime
		if dobStr != "" {
			parsed, err := time.Parse("2006-01-02", dobStr)
			if err != nil {
				respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "dob must be YYYY-MM-DD"})
				return
			}
			dob = sql.NullTime{Time: parsed, Valid: true}
		}
		setClauses = append(setClauses, fmt.Sprintf("dob = $%d", len(args)+1))
		args = append(args, dob)
	}

	if req.Phone != nil {
		phone := strings.TrimSpace(*req.Phone)
		phoneNull := sql.NullString{}
		if phone != "" {
			phoneNull = sql.NullString{String: phone, Valid: true}
		}
		setClauses = append(setClauses, fmt.Sprintf("phone = $%d", len(args)+1))
		args = append(args, phoneNull)
	}

	if req.PilotCert != nil {
		pilot := strings.TrimSpace(*req.PilotCert)
		pilotNull := sql.NullString{}
		if pilot != "" {
			pilotNull = sql.NullString{String: pilot, Valid: true}
		}
		setClauses = append(setClauses, fmt.Sprintf("pilot_cert = $%d", len(args)+1))
		args = append(args, pilotNull)
	}

	if len(setClauses) == 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "No fields provided to update"})
		return
	}

	query := fmt.Sprintf(`
		UPDATE users
		SET %s
		WHERE id = $%d
		RETURNING id, email, dob, phone, username, pilot_cert, created_at`,
		strings.Join(setClauses, ", "),
		len(args)+1,
	)
	args = append(args, userID)

	var item models.UserListItem
	var dob sql.NullTime
	var phone sql.NullString
	var pilotCert sql.NullString

	err := h.DB.QueryRow(query, args...).Scan(
		&item.ID,
		&item.Email,
		&dob,
		&phone,
		&item.Username,
		&pilotCert,
		&item.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			respondWithJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			respondWithJSON(w, http.StatusConflict, map[string]string{"error": "email or username already exists"})
			return
		}
		log.Printf("Failed to update profile: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update profile"})
		return
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

	respondWithJSON(w, http.StatusOK, item)
}

func (h *Handlers) GetMyProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	var item models.UserListItem
	var dob sql.NullTime
	var phone sql.NullString
	var pilotCert sql.NullString

	err := h.DB.QueryRow(`
		SELECT id, email, dob, phone, username, pilot_cert, created_at
		FROM users
		WHERE id = $1`, userID,
	).Scan(
		&item.ID,
		&item.Email,
		&dob,
		&phone,
		&item.Username,
		&pilotCert,
		&item.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			respondWithJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}
		log.Printf("Failed to load profile: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load user"})
		return
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

	respondWithJSON(w, http.StatusOK, item)
}

func (h *Handlers) ChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
		return
	}

	if strings.TrimSpace(req.CurrentPassword) == "" || strings.TrimSpace(req.NewPassword) == "" {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "current_password and new_password are required"})
		return
	}

	var storedHash string
	if err := h.DB.QueryRow(`SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&storedHash); err != nil {
		if err == sql.ErrNoRows {
			respondWithJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}
		log.Printf("Failed to load password hash: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load user"})
		return
	}

	ok, err := verifyPassword(req.CurrentPassword, storedHash)
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid password hash"})
		return
	}
	if !ok {
		respondWithJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	hashed, err := hashPassword(req.NewPassword)
	if err != nil {
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to hash password"})
		return
	}

	result, err := h.DB.Exec(`UPDATE users SET password_hash = $1 WHERE id = $2`, hashed, userID)
	if err != nil {
		log.Printf("Failed to update password: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update password"})
		return
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"message": "Password updated successfully"})
}

func nullString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}
