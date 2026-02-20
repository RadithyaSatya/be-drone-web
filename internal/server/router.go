package server

import (
	"net/http"

	"xflight-backend/internal/handlers"
	"xflight-backend/internal/middleware"

	"github.com/gorilla/mux"
)

func SetupRouter(h *handlers.Handlers, wsHandler http.Handler) *mux.Router {
	router := mux.NewRouter()

	registerAuthRoutes(router, h)
	registerRealtimeWebsocketRoutes(router, wsHandler)

	protected := router.PathPrefix("/").Subrouter()
	protected.Use(middleware.AuthMiddleware)

	// Mission management and mission telemetry history
	registerMissionRoutes(protected, h)
	// Location and media assets
	registerAssetRoutes(protected, h)
	// Realtime ingestion
	registerRealtimeRoutes(protected, h)
	// User management
	registerUserRoutes(protected, h)
	// Auth-protected helper endpoints
	registerAuthProtectedRoutes(protected, h)

	return router
}

func registerMissionRoutes(router *mux.Router, h *handlers.Handlers) {
	router.HandleFunc("/missions", h.GetAllMissions).Methods("GET")
	router.HandleFunc("/missions/me", h.GetMissionsForCurrentUser).Methods("GET")
	router.HandleFunc("/missions/{id}", h.GetMissionByID).Methods("GET")
	router.HandleFunc("/missions/{id}/start", h.StartMission).Methods("POST")
	router.HandleFunc("/mission-history/{history_id}/complete", h.CompleteMissionByHistoryID).Methods("POST")
	router.HandleFunc("/mission-history", h.ListMissionHistory).Methods("GET")
	router.HandleFunc("/missions/{id}/history", h.GetMissionHistory).Methods("GET")
	router.HandleFunc("/missions/next/{user_id}", h.GetLastMission).Methods("GET")
	router.HandleFunc("/missions/user/{user_id}", h.GetMissionsByUser).Methods("GET")
	router.HandleFunc("/register-mission", h.SaveNewMission).Methods("POST")
	router.HandleFunc("/telemetry", h.SubmitBatchMissionLogs).Methods("POST")
	router.HandleFunc("/get-telemetry/{mission_id}", h.GetMissionTelemetry).Methods("GET")
}

func registerAssetRoutes(router *mux.Router, h *handlers.Handlers) {
	router.HandleFunc("/get-location/{type}/{id}", h.GetLatestLocation).Methods("GET")
	router.HandleFunc("/upload-footage", h.UploadFootage).Methods("POST")
	router.PathPrefix("/footages/").Handler(
		http.StripPrefix("/footages/", http.FileServer(http.Dir("./uploads/footages"))))
}

func registerRealtimeRoutes(router *mux.Router, h *handlers.Handlers) {
	router.HandleFunc("/realtime/telemetry", h.SubmitRealtimeTelemetry).Methods("POST")
}

func registerRealtimeWebsocketRoutes(router *mux.Router, wsHandler http.Handler) {
	router.Handle("/ws/telemetry", wsHandler).Methods("GET")
}

func registerAuthRoutes(router *mux.Router, h *handlers.Handlers) {
	router.HandleFunc("/auth/login", h.Login).Methods("POST")
}

func registerUserRoutes(router *mux.Router, h *handlers.Handlers) {
	router.HandleFunc("/register-user", h.CreateUser).Methods("POST")
	router.HandleFunc("/users", h.ListUsers).Methods("GET")
}

func registerAuthProtectedRoutes(router *mux.Router, h *handlers.Handlers) {
	router.HandleFunc("/auth/ws-token", h.GenerateWSToken).Methods("POST")
}
