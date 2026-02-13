package ws

import (
	"net/http"
)

type Authenticator interface {
	Authenticate(r *http.Request) error
}

type Handler struct {
	hub  *Hub
	auth Authenticator
}

func NewHandler(hub *Hub, auth Authenticator) *Handler {
	return &Handler{hub: hub, auth: auth}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.auth != nil {
		if err := h.auth.Authenticate(r); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "failed to upgrade websocket", http.StatusBadRequest)
		return
	}
	client := &Client{
		hub:  h.hub,
		conn: conn,
		send: make(chan []byte, 64),
	}
	h.hub.register <- client
	go client.writePump()
	client.readPump()
}
