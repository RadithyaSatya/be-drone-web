package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"xflight-backend/api"
	"xflight-backend/internal/ws"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
)

var db *sql.DB

func main() {
	loadDotEnv(".env")
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
	wsHub := ws.NewHub()
	go wsHub.Run()
	h.RealtimeHub = wsHub

	var wsAuth ws.Authenticator
	if getEnv("WS_AUTH_DISABLED", "") == "" {
		wsAuth = ws.NewJWTAuthenticatorFromEnv()
	}
	wsHandler := ws.NewHandler(wsHub, wsAuth)

	router := setupRouter(h, wsHandler)

	log.Printf("Server starting on port %s...", cfg.addr)
	log.Fatal(http.ListenAndServe(cfg.addr, corsMiddleware(router)))
}

func setupRouter(h *api.Handlers, wsHandler http.Handler) *mux.Router {
	router := mux.NewRouter()

	// Mission management and mission telemetry history
	registerMissionRoutes(router, h)
	// Location and media assets
	registerAssetRoutes(router, h)
	// Realtime ingestion and websocket delivery
	registerRealtimeRoutes(router, h, wsHandler)
	// Authentication endpoints
	registerAuthRoutes(router, h)

	return router
}

func registerMissionRoutes(router *mux.Router, h *api.Handlers) {
	router.HandleFunc("/missions", h.GetAllMissions).Methods("GET")
	router.HandleFunc("/missions/{id}", h.GetMissionByID).Methods("GET")
	router.HandleFunc("/missions/{id}/complete", h.CompleteMission).Methods("POST")
	router.HandleFunc("/missions/recurring", h.GetRecurringMissions).Methods("GET")
	router.HandleFunc("/missions/last/{user_id}", h.GetLastMission).Methods("GET")
	router.HandleFunc("/missions/scheduled", h.GetScheduledMissions).Methods("GET")
	router.HandleFunc("/missions/user/{user_id}", h.GetMissionsByUser).Methods("GET")
	router.HandleFunc("/register-mission", h.SaveNewMission).Methods("POST")
	router.HandleFunc("/telemetry", h.SubmitBatchMissionLogs).Methods("POST")
	router.HandleFunc("/get-telemetry/{mission_id}", h.GetMissionTelemetry).Methods("GET")
}

func registerAssetRoutes(router *mux.Router, h *api.Handlers) {
	router.HandleFunc("/get-location/{type}/{id}", h.GetLatestLocation).Methods("GET")
	router.HandleFunc("/upload-footage", h.UploadFootage).Methods("POST")
	router.PathPrefix("/footages/").Handler(
		http.StripPrefix("/footages/", http.FileServer(http.Dir("./uploads/footages"))))
}

func registerRealtimeRoutes(router *mux.Router, h *api.Handlers, wsHandler http.Handler) {
	router.HandleFunc("/realtime/telemetry", h.SubmitRealtimeTelemetry).Methods("POST")
	router.Handle("/ws/telemetry", wsHandler).Methods("GET")
}

func registerAuthRoutes(router *mux.Router, h *api.Handlers) {
	router.HandleFunc("/auth/login", h.Login).Methods("POST")
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

func loadDotEnv(path string) {
	content, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		_ = os.Setenv(key, value)
	}
}
