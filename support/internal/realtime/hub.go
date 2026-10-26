package realtime

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type socketClient struct {
	conn  *websocket.Conn
	queue chan []byte
	done  chan struct{}
	once  sync.Once
}

func (client *socketClient) close(code int) {
	client.once.Do(func() {
		close(client.done)
		_ = client.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, "reconnect and reload"), time.Now().Add(time.Second))
		_ = client.conn.Close()
	})
}

type Hub struct {
	mu      sync.Mutex
	clients map[*socketClient]bool
	closed  bool
}

func NewHub() *Hub { return &Hub{clients: map[*socketClient]bool{}} }
func (hub *Hub) Publish(frame any) {
	message, err := json.Marshal(frame)
	if err != nil {
		return
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for client := range hub.clients {
		select {
		case client.queue <- message:
		default:
			delete(hub.clients, client)
			go client.close(websocket.CloseTryAgainLater)
		}
	}
}
func (hub *Hub) Close() {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	hub.closed = true
	for client := range hub.clients {
		delete(hub.clients, client)
		go client.close(websocket.CloseGoingAway)
	}
}
func (hub *Hub) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	upgrader := websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 4096, CheckOrigin: func(*http.Request) bool { return true }}
	connection, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return
	}
	client := &socketClient{conn: connection, queue: make(chan []byte, 64), done: make(chan struct{})}
	hub.mu.Lock()
	if hub.closed {
		hub.mu.Unlock()
		client.close(websocket.CloseGoingAway)
		return
	}
	client.queue <- []byte(`{"type":"ready"}`)
	hub.clients[client] = true
	hub.mu.Unlock()
	defer func() {
		hub.mu.Lock()
		delete(hub.clients, client)
		hub.mu.Unlock()
		client.close(websocket.CloseNormalClosure)
	}()
	connection.SetReadLimit(1024)
	_ = connection.SetReadDeadline(time.Now().Add(60 * time.Second))
	connection.SetPongHandler(func(string) error { return connection.SetReadDeadline(time.Now().Add(60 * time.Second)) })
	go func() {
		defer client.close(websocket.CloseNormalClosure)
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
		}
	}()
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	lifetime := time.NewTimer(time.Hour)
	defer lifetime.Stop()
	for {
		select {
		case <-client.done:
			return
		case <-lifetime.C:
			return
		case message := <-client.queue:
			_ = connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := connection.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ping.C:
			if err := connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				return
			}
		}
	}
}
