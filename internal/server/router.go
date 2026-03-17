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
	registerPublicAssetRoutes(router, h)

	protected := router.PathPrefix("/").Subrouter()
	protected.Use(middleware.AuthMiddleware(h.DB))

	// Mission management and mission telemetry history
	registerMissionRoutes(protected, h)
	// Location and media assets
	registerAssetRoutes(protected, h)
	// Docking management
	registerDockingRoutes(protected, h)
	// Realtime ingestion
	registerRealtimeRoutes(protected, h)
	// User management
	registerUserRoutes(protected, h)
	// UAV management
	registerUavRoutes(protected, h)
	// Auth-protected helper endpoints
	registerAuthProtectedRoutes(protected, h)

	return router
}

func registerMissionRoutes(router *mux.Router, h *handlers.Handlers) {
	router.HandleFunc("/missions", h.GetAllMissions).Methods("GET")
	router.HandleFunc("/missions/me", h.GetMissionsForCurrentUser).Methods("GET")
	router.HandleFunc("/missions/{id}", h.GetMissionByID).Methods("GET")
	router.HandleFunc("/missions/{id}", h.UpdateMission).Methods("PATCH")
	router.HandleFunc("/missions/{id}", h.DeleteMission).Methods("DELETE")
	router.HandleFunc("/missions/{id}/start", h.StartMission).Methods("POST")
	router.HandleFunc("/mission-history/{history_id}/state", h.GetMissionHistoryState).Methods("GET")
	router.HandleFunc("/mission-history/{history_id}/state", h.UpdateMissionHistoryState).Methods("PATCH")
	router.HandleFunc("/mission-history/{history_id}/complete", h.CompleteMissionByHistoryID).Methods("POST")
	router.HandleFunc("/mission-history/{history_id}/events", h.ListMissionHistoryEvents).Methods("GET")
	router.HandleFunc("/mission-history/{history_id}/media", h.ListMissionMedia).Methods("GET")
	router.HandleFunc("/mission-history/{history_id}/media", h.UploadMissionMedia).Methods("POST")
	router.HandleFunc("/mission-history", h.ListMissionHistory).Methods("GET")
	router.HandleFunc("/mission-history/me", h.ListMissionHistory).Methods("GET")
	router.HandleFunc("/missions/waiting/device", h.GetNextWaitingMissionForDevice).Methods("GET")
	router.HandleFunc("/missions/safe-to-fly/device", h.GetSafeToFlyMissionForDevice).Methods("GET")
	router.HandleFunc("/missions/next/{user_id}", h.GetLastMission).Methods("GET")
	router.HandleFunc("/missions/user/{user_id}", h.GetMissionsByUser).Methods("GET")
	router.HandleFunc("/register-mission", h.SaveNewMission).Methods("POST")
	router.HandleFunc("/telemetry", h.SubmitBatchMissionLogs).Methods("POST")
	router.HandleFunc("/get-telemetry/{mission_id}", h.GetMissionTelemetry).Methods("GET")
}

func registerAssetRoutes(router *mux.Router, h *handlers.Handlers) {
	router.HandleFunc("/get-location/{type}/{id}", h.GetLatestLocation).Methods("GET")
}

func registerDockingRoutes(router *mux.Router, h *handlers.Handlers) {
	router.HandleFunc("/dockings", h.CreateDocking).Methods("POST")
	router.HandleFunc("/dockings/{id}", h.UpdateDocking).Methods("PATCH")
	router.HandleFunc("/dockings/{id}", h.DeleteDocking).Methods("DELETE")
}

func registerPublicAssetRoutes(router *mux.Router, h *handlers.Handlers) {
	router.PathPrefix("/uav-images/").Handler(
		http.StripPrefix("/uav-images/", http.FileServer(http.Dir("./uploads/uav_images"))))
	router.PathPrefix("/footages/").Handler(
		http.StripPrefix("/footages/", http.FileServer(http.Dir("./uploads/footages"))))
	router.HandleFunc("/mission-media/{media_id}/download", h.DownloadMissionMedia).Methods("GET")
}

func registerRealtimeRoutes(router *mux.Router, h *handlers.Handlers) {
	router.HandleFunc("/realtime/telemetry", h.SubmitRealtimeTelemetry).Methods("POST")
}

func registerRealtimeWebsocketRoutes(router *mux.Router, wsHandler http.Handler) {
	router.Handle("/ws/telemetry", wsHandler).Methods("GET")
}

func registerAuthRoutes(router *mux.Router, h *handlers.Handlers) {
	router.HandleFunc("/auth/login", h.Login).Methods("POST")
	router.HandleFunc("/bootstrap/default-user", h.CreateDefaultUser).Methods("POST")
}

func registerUserRoutes(router *mux.Router, h *handlers.Handlers) {
	router.HandleFunc("/register-user", h.CreateUser).Methods("POST")
	router.HandleFunc("/users", h.ListUsers).Methods("GET")
	router.HandleFunc("/users/me", h.GetMyProfile).Methods("GET")
	router.HandleFunc("/users/me", h.UpdateMyProfile).Methods("PATCH")
	router.HandleFunc("/users/me/password", h.ChangeMyPassword).Methods("PATCH")
}

func registerUavRoutes(router *mux.Router, h *handlers.Handlers) {
	router.HandleFunc("/uavs", h.CreateUAV).Methods("POST")
	router.HandleFunc("/uavs", h.ListUAVs).Methods("GET")
	router.HandleFunc("/uavs/me", h.ListMyUAVs).Methods("GET")
	router.HandleFunc("/uavs/me/dropdown", h.ListMyUAVDropdown).Methods("GET")
	router.HandleFunc("/uavs/assign", h.AssignUAVToUser).Methods("POST")
	router.HandleFunc("/uavs/{id}", h.GetUAVByID).Methods("GET")
	router.HandleFunc("/uavs/{id}", h.UpdateUAV).Methods("PATCH")
	router.HandleFunc("/uavs/{id}/image", h.UploadUAVImage).Methods("POST")
	router.HandleFunc("/uavs/{id}", h.DeleteUAV).Methods("DELETE")
}

func registerAuthProtectedRoutes(router *mux.Router, h *handlers.Handlers) {
	router.HandleFunc("/auth/ws-token", h.GenerateWSToken).Methods("POST")
	router.HandleFunc("/auth/logout", h.Logout).Methods("POST")
	router.HandleFunc("/device-context", h.GetDeviceContext).Methods("GET")
	router.HandleFunc("/device-tokens/uav/{uav_id}", h.CreateUavDeviceToken).Methods("POST")
	router.HandleFunc("/device-tokens/docking/{docking_id}", h.CreateDockingDeviceToken).Methods("POST")
}
