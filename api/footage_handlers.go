package api

import (
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

	file, header, err := r.FormFile("file")
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "No file uploaded or field name must be 'file'"})
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".m4" && ext != ".mp4" {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Only .m4 and .mp4 files are allowed"})
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
	err = h.DB.QueryRow(`
        INSERT INTO footages (uav_id, filename, file_path)
        VALUES ($1, $2, $3)
        RETURNING id`,
		uavID,
		header.Filename,
		filePath,
	).Scan(&footageID)

	if err != nil {
		log.Printf("Failed to insert into footages table: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "File saved but database record failed"})
		return
	}
	respondWithJSON(w, http.StatusCreated, map[string]interface{}{
		"id":          footageID,
		"uav_id":      uavID,
		"filename":    header.Filename,
		"uploaded_at": time.Now().Format(time.RFC3339),
		"message":     "Footage uploaded successfully",
	})
}
