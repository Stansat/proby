package web

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// hub tracks connected WebSocket clients and broadcasts JSON snapshots to them.
type hub struct {
	mu      sync.Mutex
	clients map[*wsClient]bool
}

type wsClient struct {
	ch chan []byte
}

func newHub() *hub { return &hub{clients: map[*wsClient]bool{}} }

func (h *hub) add() *wsClient {
	c := &wsClient{ch: make(chan []byte, 8)}
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
	return c
}

func (h *hub) remove(c *wsClient) {
	h.mu.Lock()
	if h.clients[c] {
		delete(h.clients, c)
		close(c.ch)
	}
	h.mu.Unlock()
}

func (h *hub) broadcast(msg []byte) {
	h.mu.Lock()
	for c := range h.clients {
		select {
		case c.ch <- msg:
		default: // slow client: drop this update
		}
	}
	h.mu.Unlock()
}

// broadcaster periodically snapshots the store and pushes it to all clients.
func (s *Server) broadcaster(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			msg, err := json.Marshal(map[string]any{
				"type":    "snapshot",
				"targets": s.deps.Store.Snapshot(),
			})
			if err == nil {
				s.hub.broadcast(msg)
			}
		}
	}
}

// handleStream upgrades to a WebSocket and streams live snapshots.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return
	}
	defer c.CloseNow()

	ctx := c.CloseRead(r.Context()) // handles pings and detects client close
	client := s.hub.add()
	defer s.hub.remove(client)

	// Send an immediate snapshot so the UI populates without waiting a tick.
	if msg, err := json.Marshal(map[string]any{"type": "snapshot", "targets": s.deps.Store.Snapshot()}); err == nil {
		_ = c.Write(ctx, websocket.MessageText, msg)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-client.ch:
			if !ok {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.Write(writeCtx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		}
	}
}
