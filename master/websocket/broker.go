package websocket

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Message struct {
	Type  string `json:"type"`
	Data  string `json:"data"`
	Extra string `json:"extra"`
}

type wsClient struct {
	conn *websocket.Conn
	mu   sync.Mutex // serialize writes per connection
}

var connections []*wsClient
var connsMu sync.Mutex

// BroadcastMessage sends a message to all connected WebSocket clients
func BroadcastMessage(msg Message) {
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		log.Printf("websocket: failed to marshal message: %v", err)
		return
	}

	// Copy current connections so that slow/broken clients can't block the mutex.
	connsMu.Lock()
	snapshot := make([]*wsClient, len(connections))
	copy(snapshot, connections)
	connsMu.Unlock()

	var dead []*wsClient

	for _, client := range snapshot {

		client.mu.Lock()
		if err := client.conn.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
			log.Printf("websocket: failed to set write deadline: %v", err)
		}

		if err := client.conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
			dead = append(dead, client)
		}
		client.mu.Unlock()
	}

	if len(dead) > 0 {
		// Remove failed connections.
		toDrop := make(map[*wsClient]struct{}, len(dead))
		for _, c := range dead {
			toDrop[c] = struct{}{}
		}

		connsMu.Lock()
		filtered := make([]*wsClient, 0, len(connections))
		for _, c := range connections {
			if _, drop := toDrop[c]; drop {
				_ = c.conn.Close()
				continue
			}
			filtered = append(filtered, c)
		}
		connections = filtered
		remaining := len(connections)
		connsMu.Unlock()

		log.Printf("websocket: pruned %d stale connections, %d remain", len(dead), remaining)
	}
}

// RegisterConnection adds a new WebSocket connection to the broker
func RegisterConnection(conn *websocket.Conn) {
	connsMu.Lock()
	defer connsMu.Unlock()
	connections = append(connections, &wsClient{conn: conn})
}
