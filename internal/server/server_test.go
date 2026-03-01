package server_test

import (
	"context"
	"encoding/json"
	"net"
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
		"X-Frame-Options":         "DENY",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Content-Security-Policy": "default-src 'self'",
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

func TestServer_GracefulShutdown(t *testing.T) {
	h := hub.New()
	_ = server.New(h, "127.0.0.1:0") // ensure New compiles with unused var

	ctx, cancel := context.WithCancel(context.Background())

	started := make(chan struct{})
	done := make(chan error, 1)

	// Find a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	srv2 := server.New(h, addr)
	go func() {
		close(started)
		done <- srv2.Start(ctx)
	}()

	<-started
	time.Sleep(50 * time.Millisecond) // let server bind

	// Cancel context — should trigger graceful shutdown
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start returned error on graceful shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within 5 seconds")
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

func TestServer_WebSocketRefreshMessage(t *testing.T) {
	refreshCh := make(chan string, 8)
	h := hub.New()
	srv := server.New(h, ":0", server.WithRefreshChan(refreshCh))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	conn.WriteMessage(websocket.TextMessage, []byte(`{"action":"refresh","ticker":"AAPL_US_EQ"}`))

	select {
	case ticker := <-refreshCh:
		if ticker != "AAPL_US_EQ" {
			t.Errorf("got ticker %q, want AAPL_US_EQ", ticker)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for refresh request")
	}
}

func TestServer_WebSocketRefreshAll(t *testing.T) {
	refreshCh := make(chan string, 8)
	h := hub.New()
	srv := server.New(h, ":0", server.WithRefreshChan(refreshCh))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	conn.WriteMessage(websocket.TextMessage, []byte(`{"action":"refresh_all"}`))

	select {
	case ticker := <-refreshCh:
		if ticker != "" {
			t.Errorf("got ticker %q, want empty string for refresh_all", ticker)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for refresh_all")
	}
}
