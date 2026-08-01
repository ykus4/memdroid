// Package wswatch manages WebSocket clients that receive real-time watch events.
package wswatch

import (
	"encoding/json"
	"sync"

	"golang.org/x/net/websocket"
)

// Event is sent to the browser when a watched address changes or an alert
// fires.
type Event struct {
	// Kind is "change" or "alert".
	Kind string `json:"kind"`
	Addr string `json:"addr"`
	Prev string `json:"prev,omitempty"`
	Cur  string `json:"cur"`
	// Condition and Triggered are only set for alert events.
	Condition string `json:"condition,omitempty"`
	Triggered bool   `json:"triggered,omitempty"`
}

// Hub tracks the connected WebSocket clients for one server. It is a value
// rather than package state so tests (and a second server) can run in
// isolation.
type Hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*websocket.Conn]struct{})}
}

// Register adds a WebSocket connection to the broadcast list and blocks until
// the connection closes. It is shaped to be used directly as a
// websocket.Handler.
func (h *Hub) Register(ws *websocket.Conn) {
	h.mu.Lock()
	h.clients[ws] = struct{}{}
	h.mu.Unlock()

	// The feed is push-only; reading just waits for the peer to hang up.
	buf := make([]byte, 128)
	for {
		if _, err := ws.Read(buf); err != nil {
			break
		}
	}

	h.mu.Lock()
	delete(h.clients, ws)
	h.mu.Unlock()
}

// Broadcast sends an Event to all connected clients. Writes happen outside the
// lock so one slow client can't stall the hub, and clients whose write fails
// are dropped.
func (h *Hub) Broadcast(e Event) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}

	var dead []*websocket.Conn
	for _, ws := range h.snapshot() {
		if _, err := ws.Write(data); err != nil {
			dead = append(dead, ws)
		}
	}
	if len(dead) == 0 {
		return
	}

	h.mu.Lock()
	for _, ws := range dead {
		delete(h.clients, ws)
	}
	h.mu.Unlock()
}

// CloseAll disconnects every client, unblocking their Register calls.
func (h *Hub) CloseAll() {
	for _, ws := range h.snapshot() {
		_ = ws.Close()
	}
}

// Count returns the number of connected clients.
func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (h *Hub) snapshot() []*websocket.Conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*websocket.Conn, 0, len(h.clients))
	for ws := range h.clients {
		out = append(out, ws)
	}
	return out
}
