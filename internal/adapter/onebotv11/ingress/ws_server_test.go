package ingress

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWSServerRequiresAccessToken(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ingress := NewWSServer("napcat-main", "127.0.0.1:0", "/ws", "secret-token", logger)

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

	deadline := time.Now().Add(2 * time.Second)
	for ingress.listener == nil {
		if time.Now().After(deadline) {
			t.Fatal("listener was not initialized in time")
		}
		time.Sleep(10 * time.Millisecond)
	}

	wsURL := "ws://" + ingress.listener.Addr().String() + "/ws"
	unauthorizedConn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if unauthorizedConn != nil {
		_ = unauthorizedConn.Close()
	}
	if err == nil {
		t.Fatal("Dial() error = nil, want unauthorized handshake failure")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		if resp == nil {
			t.Fatalf("response = nil, want %d", http.StatusUnauthorized)
		}
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer secret-token")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		if resp == nil {
			t.Fatalf("authorized Dial() error = %v", err)
		}
		t.Fatalf("authorized Dial() error = %v, status=%d", err, resp.StatusCode)
	}
	defer func() { _ = conn.Close() }()
}
