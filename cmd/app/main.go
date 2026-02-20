package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"

	"xflight-backend/internal/handlers"
	"xflight-backend/internal/middleware"
	"xflight-backend/internal/server"
	"xflight-backend/internal/ws"

	_ "github.com/lib/pq"
)

func main() {
	server.LoadDotEnv(".env")
	cfg := server.LoadConfig()

	db, err := sql.Open("postgres", cfg.DBConn)
	if err != nil {
		log.Fatal(err)
	}
	err = db.Ping()
	if err != nil {
		log.Fatal(fmt.Errorf("error connecting to database: %w", err))
	}

	log.Println("Successfully connected to PostgreSQL database!")

	h := handlers.NewHandlers(db)
	wsHub := ws.NewHub()
	go wsHub.Run()
	h.RealtimeHub = wsHub

	wsAuth := ws.NewJWTAuthenticatorFromEnv()
	wsHandler := ws.NewHandler(wsHub, wsAuth)

	router := server.SetupRouter(h, wsHandler)

	log.Printf("Server starting on port %s...", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, middleware.CorsMiddleware(router)))
}
