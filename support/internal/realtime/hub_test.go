package realtime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestSlowClientClosesWithRetryCode(t *testing.T) {
	hub := NewHub()
	defer hub.Close()
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		client := &socketClient{conn: connection, queue: make(chan []byte, 64), done: make(chan struct{})}
		hub.mu.Lock()
		hub.clients[client] = true
		hub.mu.Unlock()
		_ = connection.WriteJSON(map[string]string{"type": "ready"})
	}))
	defer server.Close()
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := connection.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 65; index++ {
		hub.Publish(map[string]string{"type": "changed"})
	}
	if _, _, err := connection.ReadMessage(); !websocket.IsCloseError(err, websocket.CloseTryAgainLater) {
		t.Fatalf("expected overload close, got %v", err)
	}
}
