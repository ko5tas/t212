package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ko5tas/t212/internal/hub"
	"github.com/ko5tas/t212/internal/server"
)

func TestServer_HealthEndpoint(t *testing.T) {
	h := hub.New()
	srv := server.New(h, ":0")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
}

func TestServer_SecureHeaders(t *testing.T) {
	h := hub.New()
	srv := server.New(h, ":0")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}

	checks := map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
	}
	for header, want := range checks {
		if got := resp.Header.Get(header); got != want {
			t.Errorf("header %s: got %q, want %q", header, got, want)
		}
	}
}

func TestServer_WebSocketReceivesBroadcast(t *testing.T) {
	h := hub.New()
	srv := server.New(h, ":0")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	// Give handler time to register subscriber.
	time.Sleep(50 * time.Millisecond)

	payload := map[string]string{"test": "hello"}
	b, _ := json.Marshal(payload)
	h.Broadcast(b)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read ws message: %v", err)
	}
	if string(msg) != string(b) {
		t.Errorf("got %q, want %q", msg, b)
	}
}

func TestServer_WebSocketMaxConnections(t *testing.T) {
	h := hub.New()
	srv := server.New(h, ":0")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	conns := make([]*websocket.Conn, 5)
	for i := range conns {
		c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("connection %d: %v", i, err)
		}
		conns[i] = c
		defer c.Close()
	}
	time.Sleep(50 * time.Millisecond)

	// 6th connection should be rejected.
	c, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		c.Close()
		t.Error("expected 6th connection to be rejected")
	}
	if resp != nil && resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", resp.StatusCode)
	}
}
