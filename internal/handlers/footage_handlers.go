package handlers

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"xflight-backend/internal/models"
)

const missionMediaParseMemory = 32 << 20 // 32 MB kept in memory before multipart spills to disk

type missionMediaUploadForm struct {
	EventID    *int
	MissionID  *int
	UavID      *int
	FileFields []*multipart.FileHeader
}

type uploadedMissionMediaItem struct {
	ID               int       `json:"id"`
	HistoryID        int       `json:"history_id"`
	EventID          *int      `json:"event_id,omitempty"`
	MediaType        string    `json:"media_type"`
	FilePath         string    `json:"file_path"`
	PublicPath       string    `json:"public_path"`
	DownloadPath     string    `json:"download_path"`
	OriginalFilename string    `json:"original_filename"`
	StoredFilename   string    `json:"stored_filename"`
	CreatedAt        time.Time `json:"created_at"`
}

func (h *Handlers) UploadMissionMedia(w http.ResponseWriter, r *http.Request) {
	historyID, err := parseMissionHistoryID(r)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	form, err := parseMissionMediaUploadForm(r)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	h.uploadMissionMediaForHistory(w, r, historyID, form)
}

func (h *Handlers) uploadMissionMediaForHistory(w http.ResponseWriter, r *http.Request, historyID int, form *missionMediaUploadForm) {
	ctx, err := h.loadMissionHistoryRuntimeContextReadOnly(historyID)
	if err != nil {
		h.respondMissionHistoryContextError(w, err)
		return
	}
	if !h.authorizeMissionHistoryAccess(w, r, ctx) {
		return
	}

	if form.MissionID != nil && *form.MissionID != ctx.MissionID {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "mission_id does not match history_id"})
		return
	}
	if form.UavID != nil && *form.UavID != ctx.UavID {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "uav_id does not match history_id"})
		return
	}
	if form.EventID != nil {
		exists, err := missionEventExists(h.DB, historyID, *form.EventID)
		if err != nil {
			log.Printf("Failed to validate mission event: %v", err)
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
			return
		}
		if !exists {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "event_id not found for mission history"})
			return
		}
	}

	historyDir := filepath.Join(footageUploadDir, strconv.Itoa(historyID))
	if err := os.MkdirAll(historyDir, os.ModePerm); err != nil {
		log.Printf("Failed to create mission media directory: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to prepare upload directory"})
		return
	}

	type pendingMissionMedia struct {
		EventID          *int
		MediaType        string
		FilePath         string
		PublicPath       string
		OriginalFilename string
		StoredFilename   string
	}

	pending := make([]pendingMissionMedia, 0, len(form.FileFields))
	writtenPaths := make([]string, 0, len(form.FileFields))
	now := time.Now().UTC()

	for index, header := range form.FileFields {
		ext := strings.ToLower(filepath.Ext(header.Filename))
		mediaType, ok := resolveMediaType(ext)
		if !ok {
			removeMissionMediaFiles(writtenPaths)
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Only image or video files are allowed"})
			return
		}

		src, err := header.Open()
		if err != nil {
			removeMissionMediaFiles(writtenPaths)
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Failed to read uploaded file"})
			return
		}

		storedFilename := fmt.Sprintf("%s_%03d%s", now.Format("20060102T150405.000000000"), index+1, ext)
		storedFilename = strings.ReplaceAll(storedFilename, ":", "")
		filePath := filepath.Join(historyDir, storedFilename)

		dst, err := os.Create(filePath)
		if err != nil {
			_ = src.Close()
			removeMissionMediaFiles(writtenPaths)
			log.Printf("Failed to create mission media file: %v", err)
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save file"})
			return
		}

		if _, err := io.Copy(dst, src); err != nil {
			_ = dst.Close()
			_ = src.Close()
			_ = os.Remove(filePath)
			removeMissionMediaFiles(writtenPaths)
			log.Printf("Failed to write mission media file: %v", err)
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save file"})
			return
		}

		_ = dst.Close()
		_ = src.Close()

		writtenPaths = append(writtenPaths, filePath)
		pending = append(pending, pendingMissionMedia{
			EventID:          form.EventID,
			MediaType:        mediaType,
			FilePath:         filePath,
			PublicPath:       buildMissionMediaPublicPath(filePath),
			OriginalFilename: header.Filename,
			StoredFilename:   storedFilename,
		})
	}

	tx, err := h.DB.Begin()
	if err != nil {
		removeMissionMediaFiles(writtenPaths)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	items := make([]uploadedMissionMediaItem, 0, len(pending))
	for _, item := range pending {
		var mediaID int
		var createdAt time.Time
		var eventID interface{}
		if item.EventID != nil {
			eventID = *item.EventID
		}

		err := tx.QueryRow(`
			INSERT INTO mission_media (history_id, event_id, media_type, file_path)
			VALUES ($1, $2, $3, $4)
			RETURNING id, created_at`,
			historyID,
			eventID,
			item.MediaType,
			item.FilePath,
		).Scan(&mediaID, &createdAt)
		if err != nil {
			removeMissionMediaFiles(writtenPaths)
			log.Printf("Failed to insert mission media: %v", err)
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to record mission media"})
			return
		}

		items = append(items, uploadedMissionMediaItem{
			ID:               mediaID,
			HistoryID:        historyID,
			EventID:          item.EventID,
			MediaType:        item.MediaType,
			FilePath:         item.FilePath,
			PublicPath:       item.PublicPath,
			DownloadPath:     buildMissionMediaDownloadPath(mediaID),
			OriginalFilename: item.OriginalFilename,
			StoredFilename:   item.StoredFilename,
			CreatedAt:        createdAt,
		})
	}

	if err := tx.Commit(); err != nil {
		removeMissionMediaFiles(writtenPaths)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save mission media"})
		return
	}

	response := map[string]interface{}{
		"history_id": historyID,
		"count":      len(items),
		"items":      items,
		"message":    "Mission media uploaded successfully",
	}
	if form.EventID != nil {
		response["event_id"] = *form.EventID
	}
	respondWithJSON(w, http.StatusCreated, response)
}

func (h *Handlers) ListMissionMedia(w http.ResponseWriter, r *http.Request) {
	historyID, err := parseMissionHistoryID(r)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx, err := h.loadMissionHistoryRuntimeContextReadOnly(historyID)
	if err != nil {
		h.respondMissionHistoryContextError(w, err)
		return
	}
	if !h.authorizeMissionHistoryAccess(w, r, ctx) {
		return
	}

	var filterEventID *int
	if raw := strings.TrimSpace(r.URL.Query().Get("event_id")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid event_id"})
			return
		}
		exists, err := missionEventExists(h.DB, historyID, parsed)
		if err != nil {
			log.Printf("Failed to validate mission event: %v", err)
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
			return
		}
		if !exists {
			respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "event_id not found for mission history"})
			return
		}
		filterEventID = &parsed
	}

	query := `
		SELECT id, history_id, event_id, media_type, file_path, created_at
		FROM mission_media
		WHERE history_id = $1`
	args := []interface{}{historyID}
	if filterEventID != nil {
		query += ` AND event_id = $2`
		args = append(args, *filterEventID)
	}
	query += ` ORDER BY created_at ASC, id ASC`

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		log.Printf("Failed to list mission media: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load mission media"})
		return
	}
	defer rows.Close()

	items := make([]models.MissionHistoryMedia, 0)
	for rows.Next() {
		var item models.MissionHistoryMedia
		var eventID sql.NullInt64
		if err := rows.Scan(&item.ID, &item.HistoryID, &eventID, &item.MediaType, &item.FilePath, &item.CreatedAt); err != nil {
			log.Printf("Failed to scan mission media: %v", err)
			continue
		}
		if eventID.Valid {
			value := int(eventID.Int64)
			item.EventID = &value
		}
		item.PublicPath = buildMissionMediaPublicPath(item.FilePath)
		item.DownloadPath = buildMissionMediaDownloadPath(item.ID)
		items = append(items, item)
	}

	response := map[string]interface{}{
		"history_id": historyID,
		"count":      len(items),
		"items":      items,
	}
	if filterEventID != nil {
		response["event_id"] = *filterEventID
	}
	respondWithJSON(w, http.StatusOK, response)
}

func (h *Handlers) DownloadMissionMedia(w http.ResponseWriter, r *http.Request) {
	mediaID, ok := parsePositiveMuxInt(w, r, "media_id", "Invalid media ID")
	if !ok {
		return
	}

	var historyID int
	var filePath string
	var mediaType string
	err := h.DB.QueryRow(`
		SELECT id, history_id, media_type, file_path
		FROM mission_media
		WHERE id = $1`,
		mediaID,
	).Scan(&mediaID, &historyID, &mediaType, &filePath)
	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "Mission media not found"})
		return
	}
	if err != nil {
		log.Printf("Failed to load mission media for download: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load mission media"})
		return
	}

	if _, err := os.Stat(filePath); err != nil {
		if os.IsNotExist(err) {
			respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "Mission media file not found"})
			return
		}
		log.Printf("Failed to stat mission media file: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to access mission media"})
		return
	}

	filename := buildMissionMediaDownloadFilename(historyID, mediaID, filePath)
	if contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(filePath))); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	http.ServeFile(w, r, filePath)
}

func parseMissionMediaUploadForm(r *http.Request) (*missionMediaUploadForm, error) {
	if err := r.ParseMultipartForm(missionMediaParseMemory); err != nil {
		return nil, fmt.Errorf("invalid multipart form")
	}

	eventID, err := parseOptionalPositiveInt(r.FormValue("event_id"), "event_id")
	if err != nil {
		return nil, err
	}
	missionID, err := parseOptionalPositiveInt(r.FormValue("mission_id"), "mission_id")
	if err != nil {
		return nil, err
	}
	uavID, err := parseOptionalPositiveInt(r.FormValue("uav_id"), "uav_id")
	if err != nil {
		return nil, err
	}

	files := make([]*multipart.FileHeader, 0)
	if r.MultipartForm != nil {
		files = append(files, r.MultipartForm.File["files"]...)
		files = append(files, r.MultipartForm.File["file"]...)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("at least one file is required using 'files' or 'file'")
	}

	return &missionMediaUploadForm{
		EventID:    eventID,
		MissionID:  missionID,
		UavID:      uavID,
		FileFields: files,
	}, nil
}

func parseOptionalPositiveInt(raw string, field string) (*int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return nil, fmt.Errorf("Invalid %s", field)
	}
	return &parsed, nil
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

func missionEventExists(db *sql.DB, historyID int, eventID int) (bool, error) {
	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1
			FROM mission_event
			WHERE id = $1 AND history_id = $2
		)`,
		eventID,
		historyID,
	).Scan(&exists)
	return exists, err
}

func buildMissionMediaPublicPath(filePath string) string {
	base := strings.TrimSuffix(strings.TrimPrefix(filepath.ToSlash(filepath.Clean(footageUploadDir)), "./"), "/") + "/"
	cleanPath := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(filePath)), "./")
	relative := strings.TrimPrefix(cleanPath, base)
	return "/footages/" + relative
}

func buildMissionMediaDownloadPath(mediaID int) string {
	return fmt.Sprintf("/mission-media/%d/download", mediaID)
}

func buildMissionMediaDownloadFilename(historyID int, mediaID int, filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == "" {
		return fmt.Sprintf("mission_%d_media_%d", historyID, mediaID)
	}
	return fmt.Sprintf("mission_%d_media_%d%s", historyID, mediaID, ext)
}

func removeMissionMediaFiles(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}
