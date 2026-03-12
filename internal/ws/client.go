package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"xflight-backend/internal/telemetry"

	"github.com/gorilla/websocket"
)

type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

type subscribeMessage struct {
	Type    string                 `json:"type"`
	UavIDs  []int                  `json:"uav_ids"`
	UavID   int                    `json:"uav_id"`
	Kind    string                 `json:"kind"`
	Metric  string                 `json:"metric"`
	Payload map[string]interface{} `json:"payload"`
}

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
	maxSize    = 64 * 1024
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
	}()
	c.conn.SetReadLimit(maxSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var req subscribeMessage
		if err := json.Unmarshal(message, &req); err != nil {
			log.Printf("ws: invalid message: %v", err)
			continue
		}
		switch strings.ToLower(strings.TrimSpace(req.Type)) {
		case "subscribe":
			c.hub.SetSubscriptions(c, req.UavIDs)
		case "publish":
			c.publishTelemetry(req)
		default:
			log.Printf("ws: unsupported message type %q", req.Type)
		}
	}
}

func (c *Client) publishTelemetry(req subscribeMessage) {
	metric := strings.TrimSpace(req.Metric)
	kind := strings.TrimSpace(req.Kind)

	if req.UavID <= 0 {
		log.Printf("ws: publish rejected: missing uav_id")
		return
	}
	if metric == "" {
		log.Printf("ws: publish rejected: missing metric")
		return
	}
	if req.Payload == nil {
		log.Printf("ws: publish rejected: missing payload")
		return
	}
	if kind == "" {
		kind = telemetry.KindTelemetry
	}
	if kind != telemetry.KindTelemetry {
		log.Printf("ws: publish rejected: unsupported kind %q", kind)
		return
	}

	c.hub.Broadcast(&telemetry.Message{
		UavID:     req.UavID,
		Kind:      telemetry.KindTelemetry,
		Metric:    metric,
		Timestamp: time.Now().UTC(),
		Payload:   req.Payload,
	})
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
