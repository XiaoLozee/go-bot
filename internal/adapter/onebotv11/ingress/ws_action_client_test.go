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

func TestWSReverseActionClientGetStatus(t *testing.T) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Upgrade() error = %v", err)
		}
		defer conn.Close()

		var req struct {
			Action string `json:"action"`
			Echo   string `json:"echo"`
		}
		if err := conn.ReadJSON(&req); err != nil {
			t.Fatalf("ReadJSON() error = %v", err)
		}
		if req.Action != "get_status" {
			t.Fatalf("action = %q, want get_status", req.Action)
		}
		if err := conn.WriteJSON(map[string]any{
			"status":  "ok",
			"retcode": 0,
			"data": map[string]any{
				"online": true,
				"good":   true,
			},
			"echo": req.Echo,
		}); err != nil {
			t.Fatalf("WriteJSON() error = %v", err)
		}
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	ingress := NewWSReverse("napcat-main", wsURL, "", 20*time.Millisecond, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ingress.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = ingress.Stop(context.Background()) }()

	client := ingress.BuildActionClient(2 * time.Second)
	status, err := client.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}
	if status == nil || !status.Online || !status.Good {
		t.Fatalf("status = %+v, want online=true good=true", status)
	}
}

func TestWSActionClientReadiness(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	notReady := newWSActionClient("napcat-main", time.Second, logger, func() *wsActionSession {
		return nil
	})
	if notReady.Ready() {
		t.Fatalf("Ready() = true, want false")
	}
	if strings.TrimSpace(notReady.ReadinessReason()) == "" {
		t.Fatalf("ReadinessReason() = empty, want message")
	}

	ready := newWSActionClient("napcat-main", time.Second, logger, func() *wsActionSession {
		return &wsActionSession{}
	})
	if !ready.Ready() {
		t.Fatalf("Ready() = false, want true")
	}
}
