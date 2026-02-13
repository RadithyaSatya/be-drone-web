package ws

import (
	"encoding/json"
	"log"
	"sync"

	"xflight-backend/internal/telemetry"
)

type Hub struct {
	clients    map[*Client]struct{}
	register   chan *Client
	unregister chan *Client
	broadcast  chan *telemetry.Message
	subs       map[*Client]map[string]struct{}
	mu         sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]struct{}),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan *telemetry.Message, 256),
		subs:       make(map[*Client]map[string]struct{}),
	}
}

func (h *Hub) Run() {
	for {
		select {
	case client := <-h.register:
		h.clients[client] = struct{}{}
		h.mu.Lock()
		h.subs[client] = make(map[string]struct{})
		h.mu.Unlock()
	case client := <-h.unregister:
		if _, ok := h.clients[client]; ok {
			delete(h.clients, client)
			h.mu.Lock()
			delete(h.subs, client)
			h.mu.Unlock()
			close(client.send)
			_ = client.conn.Close()
		}
	case msg := <-h.broadcast:
		h.mu.Lock()
		for client := range h.clients {
			if !h.clientSubscribedTo(client, msg.DroneID) {
				continue
			}
				data, err := json.Marshal(msg)
				if err != nil {
					log.Printf("ws: marshal telemetry: %v", err)
					continue
				}
			select {
			case client.send <- data:
			default:
				delete(h.clients, client)
				delete(h.subs, client)
				close(client.send)
				_ = client.conn.Close()
			}
		}
		h.mu.Unlock()
	}
}
}

func (h *Hub) clientSubscribedTo(client *Client, droneID string) bool {
	set, ok := h.subs[client]
	if !ok {
		return false
	}
	_, subscribed := set[droneID]
	return subscribed
}

func (h *Hub) SetSubscriptions(client *Client, droneIDs []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set := make(map[string]struct{}, len(droneIDs))
	for _, id := range droneIDs {
		if id == "" {
			continue
		}
		set[id] = struct{}{}
	}
	h.subs[client] = set
}

func (h *Hub) Broadcast(msg *telemetry.Message) {
	h.broadcast <- msg
}
