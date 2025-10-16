package websocket

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/Maruqes/512SvMan/logger"
	"github.com/gorilla/websocket"
)

type Message struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

var connections []*websocket.Conn
var connsMu sync.Mutex

// BroadcastMessage sends a message to all connected WebSocket clients
func BroadcastMessage(msg Message) {
	connsMu.Lock()
	defer connsMu.Unlock()

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		logger.Error("WebSocket marshal error:", err)
		return
	}

	for i := 0; i < len(connections); i++ {
		conn := connections[i]

		if err := conn.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
			logger.Error("WebSocket set write deadline error:", err)
		}

		if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
			logger.Error("WebSocket write error:", err)
			connections = append(connections[:i], connections[i+1:]...)
			i--
		}
	}
}

// RegisterConnection adds a new WebSocket connection to the broker
func RegisterConnection(conn *websocket.Conn) {
	connsMu.Lock()
	defer connsMu.Unlock()
	connections = append(connections, conn)
}
