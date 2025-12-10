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

var connections []*websocket.Conn
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
	snapshot := make([]*websocket.Conn, len(connections))
	copy(snapshot, connections)
	connsMu.Unlock()

	var dead []*websocket.Conn

	for _, conn := range snapshot {

		if err := conn.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
			log.Printf("websocket: failed to set write deadline: %v", err)
		}

		if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
			dead = append(dead, conn)
		}
	}

	if len(dead) > 0 {
		// Remove failed connections.
		toDrop := make(map[*websocket.Conn]struct{}, len(dead))
		for _, c := range dead {
			toDrop[c] = struct{}{}
		}

		connsMu.Lock()
		filtered := make([]*websocket.Conn, 0, len(connections))
		for _, c := range connections {
			if _, drop := toDrop[c]; drop {
				_ = c.Close()
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
	connections = append(connections, conn)
}
