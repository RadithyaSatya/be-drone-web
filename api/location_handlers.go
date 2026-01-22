package api

import (
	"database/sql"
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

func (h *Handlers) GetLatestLocation(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	entityTypeStr := vars["type"]
	entityIDStr := vars["id"]

	entityType, err := strconv.Atoi(entityTypeStr)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid entity type"})
		return
	}

	entityID, err := strconv.Atoi(entityIDStr)
	if err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid entity ID"})
		return
	}

	query := `
        SELECT latitude, longitude, timestamp
        FROM locations
        WHERE entity_id = $1 AND type = $2
        ORDER BY timestamp DESC
        LIMIT 1`

	var location struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Timestamp string  `json:"timestamp"`
	}

	row := h.DB.QueryRow(query, entityID, entityType)
	err = row.Scan(&location.Latitude, &location.Longitude, &location.Timestamp)

	if err == sql.ErrNoRows {
		respondWithJSON(w, http.StatusNotFound, map[string]string{"message": "No location found for this entity"})
		return
	}
	if err != nil {
		log.Printf("Error querying location: %v", err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
		return
	}

	respondWithJSON(w, http.StatusOK, location)
}
