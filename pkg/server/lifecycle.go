package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"scanfile/pkg/privileges"
)

// Handler builds the complete HTTP handler (routes + middlewares) without binding a listener.
// It is used by Start and by tests (httptest).
func (s *AppServer) Handler() http.Handler {
	mux := http.NewServeMux()

	s.registerScanRoutes(mux)
	s.registerFileRoutes(mux)
	s.registerLifecycleRoutes(mux)

	// Static UI assets
	if s.uiFS != nil {
		mux.Handle("/", http.FileServer(http.FS(s.uiFS)))
	}

	return s.authMiddleware(s.debugMiddleware(mux))
}

// registerLifecycleRoutes registers routes owned by the lifecycle (instance, elevation, presence).
func (s *AppServer) registerLifecycleRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/system/elevate", s.handleElevateProcess)
}

// Start launches the local HTTP/SSE server on an ephemeral or designated port.
func (s *AppServer) Start(port int) (string, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Fallback to any available port
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", err
		}
	}

	s.listener = ln
	s.httpServer = &http.Server{
		Handler:      s.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	go func() {
		_ = s.httpServer.Serve(ln)
	}()

	return fmt.Sprintf("http://%s", ln.Addr().String()), nil
}

// Stop gracefully stops the server.
func (s *AppServer) Stop() {
	if s.Watcher != nil {
		s.Watcher.Stop()
	}
	if s.httpServer != nil {
		_ = s.httpServer.Shutdown(context.Background())
	}
}

func (s *AppServer) handleElevateProcess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := privileges.RelaunchAsAdmin()
	if err != nil {
		http.Error(w, fmt.Sprintf("Falha ao solicitar elevação UAC: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "elevating",
		"message": "Nova janela solicitada como Administrador com permissões completas.",
	})
}
