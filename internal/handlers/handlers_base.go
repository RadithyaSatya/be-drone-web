package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"xflight-backend/internal/ws"
)

const (
	footageUploadDir  = "./uploads/footages"
	uavImageUploadDir = "./uploads/uav_images"
)

type Handlers struct {
	DB          *sql.DB
	RealtimeHub *ws.Hub
}

func init() {
	dirs := []string{footageUploadDir, uavImageUploadDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			log.Fatalf("Failed to create upload directory: %v", err)
		}
	}
}

func NewHandlers(db *sql.DB) *Handlers {
	return &Handlers{DB: db}
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

func respondWithErrorDetail(w http.ResponseWriter, code int, message string, err error) {
	payload := map[string]string{"error": message}
	if err != nil {
		payload["detail"] = err.Error()
	}
	respondWithJSON(w, code, payload)
}
