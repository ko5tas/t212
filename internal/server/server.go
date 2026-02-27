package server

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/gorilla/websocket"
	"github.com/ko5tas/t212/internal/hub"
)

//go:embed web
var webFS embed.FS

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" || origin == "http://"+r.Host || origin == "https://"+r.Host
	},
}

const maxConnections = 5

// Server serves the web UI and WebSocket endpoint.
type Server struct {
	hub         *hub.Hub
	addr        string
	activeConns atomic.Int32
}

// New creates a Server.
func New(h *hub.Hub, addr string) *Server {
	return &Server{hub: h, addr: addr}
}

// Handler returns the HTTP handler. Used directly in tests via httptest.NewServer.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ws", s.handleWS)

	return secureHeaders(mux)
}

// Start binds and serves. Blocks until the server errors.
func (s *Server) Start() error {
	slog.Info("web server starting", "addr", s.addr)
	return http.ListenAndServe(s.addr, s.Handler())
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if s.activeConns.Add(1) > maxConnections {
		s.activeConns.Add(-1)
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}
	defer s.activeConns.Add(-1)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("ws upgrade failed", "err", err)
		return
	}
	defer conn.Close()

	ch, unsub := s.hub.Subscribe()
	defer unsub()

	// Drain incoming messages (keep-alives, browser pings).
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for msg := range ch {
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		next.ServeHTTP(w, r)
	})
}
