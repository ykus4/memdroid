// Package wswatch manages WebSocket clients that receive real-time watch events.
package wswatch

import (
	"encoding/json"
	"sync"

	"golang.org/x/net/websocket"
)

// Event is sent to the browser when a watched address changes.
type Event struct {
	Addr string `json:"addr"`
	Prev string `json:"prev"`
	Cur  string `json:"cur"`
}

var (
	mu      sync.Mutex
	clients = map[*websocket.Conn]struct{}{}
)

// Register adds a WebSocket connection to the broadcast list and blocks until
// the connection closes.
func Register(ws *websocket.Conn) {
	mu.Lock()
	clients[ws] = struct{}{}
	mu.Unlock()

	buf := make([]byte, 128)
	for {
		if _, err := ws.Read(buf); err != nil {
			break
		}
	}

	mu.Lock()
	delete(clients, ws)
	mu.Unlock()
}

// Broadcast sends an Event to all connected WebSocket clients.
func Broadcast(e Event) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	for ws := range clients {
		_, _ = ws.Write(data)
	}
}
