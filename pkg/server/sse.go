package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (s *AppServer) broadcastSSE(eventType string, data any) {
	bytes, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(bytes))

	s.eventsMu.RLock()
	defer s.eventsMu.RUnlock()
	for ch := range s.sseClients {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (s *AppServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan string, 100)
	s.eventsMu.Lock()
	s.sseClients[ch] = true
	s.eventsMu.Unlock()

	defer func() {
		s.eventsMu.Lock()
		delete(s.sseClients, ch)
		s.eventsMu.Unlock()
		close(ch)
	}()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case msg := <-ch:
			_, _ = fmt.Fprint(w, msg)
			flusher.Flush()
		}
	}
}
