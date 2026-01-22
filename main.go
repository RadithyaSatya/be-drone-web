package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	"xflight-backend/api"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
)

var db *sql.DB

func main() {
	cfg := loadConfig()

	var err error
	db, err = sql.Open("postgres", cfg.dbConn)
	if err != nil {
		log.Fatal(err)
	}
	err = db.Ping()
	if err != nil {
		log.Fatal(fmt.Errorf("error connecting to database: %w", err))
	}

	log.Println("Successfully connected to PostgreSQL database!")

	h := api.NewHandlers(db)
	router := setupRouter(h)

	log.Printf("Server starting on port %s...", cfg.addr)
	log.Fatal(http.ListenAndServe(cfg.addr, corsMiddleware(router)))
}

func setupRouter(h *api.Handlers) *mux.Router {
	router := mux.NewRouter()

	router.HandleFunc("/missions", h.GetAllMissions).Methods("GET")
	router.HandleFunc("/missions/{id}", h.GetMissionByID).Methods("GET")
	router.HandleFunc("/missions/{id}/complete", h.CompleteMission).Methods("POST")
	router.HandleFunc("/missions/recurring", h.GetRecurringMissions).Methods("GET")
	router.HandleFunc("/missions/last/{user_id}", h.GetLastMission).Methods("GET")
	router.HandleFunc("/missions/scheduled", h.GetScheduledMissions).Methods("GET")
	// router.HandleFunc("/missions/user/{user_id}", h.GetMissionsByUserAndUAV).Methods("GET")
	router.HandleFunc("/missions/user/{user_id}", h.GetMissionsByUser).Methods("GET")

	router.HandleFunc("/register-mission", h.SaveNewMission).Methods("POST")
	router.HandleFunc("/get-location/{type}/{id}", h.GetLatestLocation).Methods("GET")
	router.HandleFunc("/upload-footage", h.UploadFootage).Methods("POST")
	router.PathPrefix("/footages/").Handler(
		http.StripPrefix("/footages/", http.FileServer(http.Dir("./uploads/footages"))))
	router.HandleFunc("/telemetry", h.SubmitBatchMissionLogs).Methods("POST")
	router.HandleFunc("/get-telemetry/{mission_id}", h.GetMissionTelemetry).Methods("GET")
	router.HandleFunc("/docking/heartbeat", h.DockingHeartbeat).Methods("POST")
	router.HandleFunc("/ws/docking", h.DockingStatusWS)
	// router.HandleFunc("/api/missions/{mission_id}/telemetry", h.GetMissionTelemetry).Methods("GET")

	return router
}

type config struct {
	dbConn string
	addr   string
}

func loadConfig() config {
	return config{
		dbConn: getEnv("DB_CONN", "user=macbook dbname=xflight sslmode=disable password="),
		addr:   getEnv("ADDR", "127.0.0.1:8080"),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
