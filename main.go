package main

import (
    "database/sql"
    "fmt"
    "log"
    "net/http"

    "github.com/gorilla/mux"
    _ "github.com/lib/pq"
    "xflight-backend/api"
)

var db *sql.DB

func main() {
    connStr := "user=aldy dbname=xflight sslmode=disable password=!Gajah17#!"
    var err error
    db, err = sql.Open("postgres", connStr)
    if err != nil {
        log.Fatal(err)
    }
    err = db.Ping()
    if err != nil {
        log.Fatal(fmt.Errorf("error connecting to database: %w", err))
    }

    log.Println("Successfully connected to PostgreSQL database!")
    
    router := mux.NewRouter()
    h := api.NewHandlers(db)
    
    router.HandleFunc("/missions", h.GetAllMissions).Methods("GET")
    router.HandleFunc("/missions/{id}", h.GetMissionByID).Methods("GET")
    router.HandleFunc("/missions/recurring", h.GetRecurringMissions).Methods("GET")
    router.HandleFunc("/missions/last/{user_id}", h.GetLastMission).Methods("GET")
    router.HandleFunc("/missions/scheduled", h.GetScheduledMissions).Methods("GET")
    // router.HandleFunc("/missions/user/{user_id}", h.GetMissionsByUserAndUAV).Methods("GET")
    router.HandleFunc("/missions/user/{user_id}", h.GetMissionsByUser).Methods("GET")
    
    router.HandleFunc("/register-mission", h.SaveNewMission).Methods("POST")
    router.HandleFunc("/get-location/{type}/{id}", h.GetLatestLocation).Methods("GET")
    router.HandleFunc("/upload-footage", h.UploadFootage).Methods("POST")
    router.PathPrefix("/footages/").Handler(
    http.StripPrefix("/footages/", http.FileServer(http.Dir("./uploads/footages"))),)
    router.HandleFunc("/telemetry", h.SubmitBatchMissionLogs).Methods("POST")
    router.HandleFunc("/get-telemetry/{mission_id}", h.GetMissionTelemetry).Methods("GET")
    // router.HandleFunc("/api/missions/{mission_id}/telemetry", h.GetMissionTelemetry).Methods("GET")
    
    port := "0.0.0.0:8080"
    log.Printf("Server starting on port %s...", port)
    log.Fatal(http.ListenAndServe(port, router))
}
