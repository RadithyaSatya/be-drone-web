package handlers

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (h *Handlers) UploadFootage(w http.ResponseWriter, r *http.Request) {
	const maxUploadSize = 1 << 30 // 1 GB
	r.ParseMultipartForm(maxUploadSize)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "File too large or invalid form"})
		return
	}

	uavIDStr := r.FormValue("uav_id")
	if uavIDStr == "" {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "uav_id is required"})
		return
	}

	uavID, err := strconv.Atoi(uavIDStr)
	if err != nil || uavID <= 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid uav_id"})
		return
	}

	var missionID sql.NullInt64
	missionIDStr := r.FormValue("mission_id")
	if missionIDStr != "" {
		parsed, err := strconv.Atoi(missionIDStr)
		if err != nil || parsed <= 0 {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid mission_id"})
			return
		}
		missionID = sql.NullInt64{Int64: int64(parsed), Valid: true}
	}
	var historyID sql.NullInt64
	historyIDStr := r.FormValue("history_id")
	if historyIDStr != "" {
		parsed, err := strconv.Atoi(historyIDStr)
		if err != nil || parsed <= 0 {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid history_id"})
			return
		}
		historyID = sql.NullInt64{Int64: int64(parsed), Valid: true}
	}
	if missionID.Valid != historyID.Valid {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "mission_id and history_id must be provided together"})
		return
	}
	if missionID.Valid {
		exists, err := missionHistoryExists(h.DB, int(historyID.Int64), int(missionID.Int64))
		if err != nil {
			log.Printf("Failed to validate mission history: %v", err)
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
			return
		}
		if !exists {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "history_id not found for mission"})
			return
		}
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "No file uploaded or field name must be 'file'"})
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	mediaType, ok := resolveMediaType(ext)
	if !ok {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Only image or video files are allowed"})
		return
	}
	timestamp := time.Now().Format("20060102_150405")
	storedFilename := fmt.Sprintf("uav_%d_%s%s", uavID, timestamp, ext)
	filePath := filepath.Join(footageUploadDir, storedFilename)

	dst, err := os.Create(filePath)
	if err != nil {
		log.Printf("Failed to create file on disk: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save file"})
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		log.Printf("Failed to write file: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save file"})
		return
	}

	var footageID int
	hasMissionID, err := footageSupportsMissionID(h.DB)
	if err != nil {
		log.Printf("Failed to check footages schema: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
		return
	}
	if missionID.Valid && !hasMissionID {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "mission_id not supported by database"})
		return
	}

	if hasMissionID {
		err = h.DB.QueryRow(`
        INSERT INTO footages (uav_id, mission_id, filename, file_path)
        VALUES ($1, $2, $3, $4)
        RETURNING id`,
			uavID,
			missionID,
			header.Filename,
			filePath,
		).Scan(&footageID)
	} else {
		err = h.DB.QueryRow(`
        INSERT INTO footages (uav_id, filename, file_path)
        VALUES ($1, $2, $3)
        RETURNING id`,
			uavID,
			header.Filename,
			filePath,
		).Scan(&footageID)
	}

	if err != nil {
		log.Printf("Failed to insert into footages table: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "File saved but database record failed"})
		return
	}

	if missionID.Valid {
		if err := insertMissionHistoryMedia(h.DB, int(historyID.Int64), int(missionID.Int64), mediaType, filePath); err != nil {
			log.Printf("Failed to attach mission history media: %v", err)
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to attach media to mission history"})
			return
		}
	}

	response := map[string]interface{}{
		"id":          footageID,
		"uav_id":      uavID,
		"mission_id":  nil,
		"media_type":  mediaType,
		"filename":    header.Filename,
		"uploaded_at": time.Now().Format(time.RFC3339),
		"message":     "Footage uploaded successfully",
	}
	if missionID.Valid {
		response["mission_id"] = missionID.Int64
	}
	respondWithJSON(w, http.StatusCreated, response)
}

func insertMissionHistoryMedia(db *sql.DB, historyID int, missionID int, mediaType, filePath string) error {
	_, err := db.Exec(`
        INSERT INTO mission_history_media (history_id, mission_id, media_type, file_path)
        VALUES ($1, $2, $3, $4)`, historyID, missionID, mediaType, filePath)
	return err
}

func missionHistoryExists(db *sql.DB, historyID int, missionID int) (bool, error) {
	var exists bool
	err := db.QueryRow(`
        SELECT EXISTS (
            SELECT 1 FROM mission_history WHERE id = $1 AND mission_id = $2
        )`, historyID, missionID).Scan(&exists)
	return exists, err
}

func resolveMediaType(ext string) (string, bool) {
	switch ext {
	case ".mp4", ".m4", ".m4v":
		return "video", true
	case ".jpg", ".jpeg", ".png", ".webp":
		return "image", true
	default:
		return "", false
	}
}

func footageSupportsMissionID(db *sql.DB) (bool, error) {
	var exists bool
	err := db.QueryRow(`
        SELECT EXISTS (
            SELECT 1
            FROM information_schema.columns
            WHERE table_schema = 'public'
              AND table_name = 'footages'
              AND column_name = 'mission_id'
        )`).Scan(&exists)
	return exists, err
}
