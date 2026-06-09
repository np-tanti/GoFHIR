package triage

import (
	"fmt"
	"log"
	"net/http"
	"sync"
)

type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type SSEHub struct {
	mu      sync.RWMutex
	clients map[string]chan []byte
	nextID  int
}

func NewSSEHub() *SSEHub {
	return &SSEHub{clients: make(map[string]chan []byte)}
}

func (h *SSEHub) Subscribe() (string, <-chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	id := fmt.Sprintf("client-%d", h.nextID)
	ch := make(chan []byte, 64)
	h.clients[id] = ch
	return id, ch
}

func (h *SSEHub) Unsubscribe(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok := h.clients[id]; ok {
		close(ch)
		delete(h.clients, id)
	}
}

func (h *SSEHub) Broadcast(eventType string, payload []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	data := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(payload)))
	for id, ch := range h.clients {
		select {
		case ch <- data:
		default:
			log.Printf("sse: dropping slow client %s", id)
		}
	}
}

func (h *SSEHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	id, ch := h.Subscribe()
	defer h.Unsubscribe(id)

	_, _ = fmt.Fprintf(w, "event: connected\ndata: {\"client\": %q}\n\n", id)
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			_, _ = w.Write(msg)
			flusher.Flush()
		}
	}
}