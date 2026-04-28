package ingress

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/XiaoLozee/go-bot/internal/adapter"
	"github.com/XiaoLozee/go-bot/internal/domain/event"
)

type HTTPCallback struct {
	id     string
	listen string
	path   string
	logger *slog.Logger
	events chan event.Event

	mu             sync.RWMutex
	state          adapter.ConnectionState
	lastError      string
	lastEventAt    time.Time
	updatedAt      time.Time
	server         *http.Server
	listener       net.Listener
	observedEvents int
}

func NewHTTPCallback(id, listen, path string, logger *slog.Logger) *HTTPCallback {
	if path == "" {
		path = "/callback"
	}
	return &HTTPCallback{
		id:        id,
		listen:    listen,
		path:      path,
		logger:    logger.With("connection", id, "component", "http_callback"),
		events:    make(chan event.Event, 128),
		state:     adapter.ConnectionStopped,
		updatedAt: time.Now(),
	}
}

func (h *HTTPCallback) ID() string {
	return h.id
}

func (h *HTTPCallback) Start(ctx context.Context) error {
	h.mu.Lock()
	if h.state == adapter.ConnectionRunning {
		h.mu.Unlock()
		return nil
	}
	h.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc(h.path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			h.setError(err)
			h.logger.Warn("读取回调请求体失败", "error", err)
			http.Error(w, "read body failed", http.StatusBadRequest)
			return
		}
		defer func() { _ = r.Body.Close() }()

		evt, err := ParseEvent(h.id, body)
		if err != nil {
			h.setError(err)
			h.logger.Warn("解析回调事件失败", "error", err)
			http.Error(w, "invalid event payload", http.StatusBadRequest)
			return
		}

		h.mu.Lock()
		h.observedEvents++
		h.lastEventAt = time.Now()
		h.updatedAt = time.Now()
		h.lastError = ""
		h.mu.Unlock()

		select {
		case h.events <- evt:
			w.WriteHeader(http.StatusNoContent)
		case <-ctx.Done():
			http.Error(w, "shutting down", http.StatusServiceUnavailable)
		}
	})

	ln, err := net.Listen("tcp", h.listen)
	if err != nil {
		h.setError(err)
		return err
	}

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	h.mu.Lock()
	h.listener = ln
	h.server = server
	h.state = adapter.ConnectionRunning
	h.lastError = ""
	h.updatedAt = time.Now()
	h.mu.Unlock()

	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			h.setError(err)
			h.logger.Error("HTTP callback ingress 运行失败", "error", err)
		}
	}()

	h.logger.Info("启动", "stage", "http_callback", "status", "listening", "listen", h.listen, "path", h.path)
	return nil
}

func (h *HTTPCallback) Stop(ctx context.Context) error {
	h.mu.Lock()
	server := h.server
	h.state = adapter.ConnectionStopped
	h.updatedAt = time.Now()
	h.mu.Unlock()

	if server != nil {
		if err := server.Shutdown(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (h *HTTPCallback) Events() <-chan event.Event {
	return h.events
}

func (h *HTTPCallback) Snapshot() adapter.IngressSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return adapter.IngressSnapshot{
		ID:             h.id,
		Type:           "http_callback",
		State:          h.state,
		Listen:         h.listen,
		Path:           h.path,
		ObservedEvents: h.observedEvents,
		LastEventAt:    h.lastEventAt,
		LastError:      h.lastError,
		UpdatedAt:      h.updatedAt,
	}
}

func (h *HTTPCallback) setError(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state = adapter.ConnectionFailed
	h.lastError = err.Error()
	h.updatedAt = time.Now()
}
