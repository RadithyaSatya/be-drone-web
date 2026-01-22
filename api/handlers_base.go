package api

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
)

const footageUploadDir = "./uploads/footages"

type Handlers struct {
	DB         *sql.DB
	DockingHub *DockingHub
}

func init() {
	if err := os.MkdirAll(footageUploadDir, os.ModePerm); err != nil {
		log.Fatalf("Failed to create upload directory: %v", err)
	}
}

func NewHandlers(db *sql.DB) *Handlers {
	hub := NewDockingHub()
	go hub.Run()
	return &Handlers{DB: db, DockingHub: hub}
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}
