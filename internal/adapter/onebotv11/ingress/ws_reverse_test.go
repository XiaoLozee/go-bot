package ingress

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWSReverseReceivesEvent(t *testing.T) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	connCh := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Upgrade() error = %v", err)
			return
		}
		connCh <- conn
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	ingress := NewWSReverse("napcat-main", wsURL, "", 50*time.Millisecond, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ingress.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		if err := ingress.Stop(context.Background()); err != nil {
			t.Fatalf("Stop() error = %v", err)
		}
	}()

	var conn *websocket.Conn
	select {
	case conn = <-connCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected reverse ws connection")
	}
	defer func() { _ = conn.Close() }()

	payload := []byte(`{
		"time": 1710000000,
		"self_id": 123456,
		"post_type": "message",
		"message_type": "group",
		"sub_type": "normal",
		"message_id": 10001,
		"group_id": 20001,
		"user_id": 30001,
		"raw_message": "菜单",
		"message": [{"type":"text","data":{"text":"菜单"}}]
	}`)
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}

	select {
	case evt := <-ingress.Events():
		if evt.ConnectionID != "napcat-main" {
			t.Fatalf("ConnectionID = %s, want napcat-main", evt.ConnectionID)
		}
		if evt.RawText != "菜单" {
			t.Fatalf("RawText = %s, want 菜单", evt.RawText)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected event from reverse ws ingress")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		snapshot := ingress.Snapshot()
		if snapshot.ObservedEvents == 1 && snapshot.ConnectedClients == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("snapshot = %+v, want observed_events=1 and connected_clients=1", snapshot)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWSReverseSendsAuthorizationHeader(t *testing.T) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	authCh := make(chan string, 1)
	connCh := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}
		authCh <- r.Header.Get("Authorization")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Upgrade() error = %v", err)
			return
		}
		connCh <- conn
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	ingress := NewWSReverse("napcat-main", wsURL, "secret-token", 50*time.Millisecond, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ingress.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		if err := ingress.Stop(context.Background()); err != nil {
			t.Fatalf("Stop() error = %v", err)
		}
	}()

	select {
	case auth := <-authCh:
		if auth != "Bearer secret-token" {
			t.Fatalf("Authorization header = %q, want Bearer secret-token", auth)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected reverse ws authorization header")
	}

	select {
	case conn := <-connCh:
		defer func() { _ = conn.Close() }()
	case <-time.After(2 * time.Second):
		t.Fatal("expected reverse ws connection")
	}
}
