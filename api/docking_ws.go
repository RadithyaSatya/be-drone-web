package api

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type DockingStatus struct {
	DockingID string    `json:"docking_id"`
	Online    bool      `json:"online"`
	Timestamp time.Time `json:"ts"`
}

type DockingHub struct {
	clients    map[*websocket.Conn]bool
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	broadcast  chan DockingStatus
	mu         sync.Mutex
	lastStatus *DockingStatus
}

const defaultDockingID = "dock-1"

func NewDockingHub() *DockingHub {
	return &DockingHub{
		clients:    make(map[*websocket.Conn]bool),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		broadcast:  make(chan DockingStatus, 16),
	}
}

func (h *DockingHub) Run() {
	for {
		select {
		case conn := <-h.register:
			h.clients[conn] = true
		case conn := <-h.unregister:
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
			}
		case status := <-h.broadcast:
			h.mu.Lock()
			h.lastStatus = &status
			h.mu.Unlock()
			for conn := range h.clients {
				if err := conn.WriteJSON(status); err != nil {
					delete(h.clients, conn)
					conn.Close()
				}
			}
		}
	}
}

func (h *DockingHub) last() *DockingStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.lastStatus == nil {
		return nil
	}
	copied := *h.lastStatus
	return &copied
}

var dockingUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (h *Handlers) DockingStatusWS(w http.ResponseWriter, r *http.Request) {
	conn, err := dockingUpgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "failed to upgrade websocket", http.StatusBadRequest)
		return
	}
	h.DockingHub.register <- conn

	if last := h.DockingHub.last(); last != nil {
		_ = conn.WriteJSON(last)
	} else {
		dockingID := r.URL.Query().Get("docking_id")
		if dockingID == "" {
			dockingID = defaultDockingID
		}
		_ = conn.WriteJSON(DockingStatus{
			DockingID: dockingID,
			Online:    false,
			Timestamp: time.Now().UTC(),
		})
	}

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	h.DockingHub.unregister <- conn
}

type dockingHeartbeatRequest struct {
	DockingID string `json:"docking_id"`
	Online    *bool  `json:"online"`
}

func (h *Handlers) DockingHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req dockingHeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
		return
	}
	if req.DockingID == "" {
		respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "docking_id is required"})
		return
	}
	online := true
	if req.Online != nil {
		online = *req.Online
	}
	status := DockingStatus{
		DockingID: req.DockingID,
		Online:    online,
		Timestamp: time.Now().UTC(),
	}
	h.DockingHub.broadcast <- status
	respondWithJSON(w, http.StatusOK, status)
}
