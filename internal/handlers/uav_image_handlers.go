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

	"github.com/gorilla/mux"
)

func (h *Handlers) UploadUAVImage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil || id <= 0 {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid UAV ID"})
		return
	}

	var exists bool
	if err := h.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM uav WHERE id = $1 AND deleted_at IS NULL)`, id).Scan(&exists); err != nil {
		log.Printf("Failed to check UAV existence: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
		return
	}
	if !exists {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "UAV not found"})
		return
	}

	const maxUploadSize = 20 << 20 // 20 MB
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "File too large or invalid form"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "No file uploaded or field name must be 'file'"})
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	mediaType, ok := resolveMediaType(ext)
	if !ok || mediaType != "image" {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Only image files are allowed"})
		return
	}

	timestamp := time.Now().Format("20060102_150405")
	storedFilename := fmt.Sprintf("uav_%d_%s%s", id, timestamp, ext)
	filePath := filepath.Join(uavImageUploadDir, storedFilename)

	dst, err := os.Create(filePath)
	if err != nil {
		log.Printf("Failed to create image file on disk: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save file"})
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		log.Printf("Failed to write image file: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save file"})
		return
	}

	imageURL := fmt.Sprintf("/uav-images/%s", storedFilename)
	var updatedID int
	err = h.DB.QueryRow(
		`UPDATE uav SET image_url = $1 WHERE id = $2 AND deleted_at IS NULL RETURNING id`,
		imageURL,
		id,
	).Scan(&updatedID)
	if err == sql.ErrNoRows {
		_ = os.Remove(filePath)
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "UAV not found"})
		return
	}
	if err != nil {
		_ = os.Remove(filePath)
		log.Printf("Failed to update UAV image_url: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update UAV image"})
		return
	}

	respondWithJSON(w, http.StatusCreated, map[string]interface{}{
		"uav_id":    updatedID,
		"image_url": imageURL,
		"message":   "UAV image uploaded successfully",
	})
}
