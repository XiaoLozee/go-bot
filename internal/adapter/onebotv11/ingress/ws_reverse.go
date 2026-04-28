package ingress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/XiaoLozee/go-bot/internal/adapter"
	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/gorilla/websocket"
)

type WSReverse struct {
	id            string
	url           string
	accessToken   string
	retryInterval time.Duration
	logger        *slog.Logger
	dialer        *websocket.Dialer
	events        chan event.Event

	mu               sync.RWMutex
	state            adapter.ConnectionState
	lastError        string
	connectedClients int
	observedEvents   int
	lastEventAt      time.Time
	updatedAt        time.Time
	session          *wsActionSession
	cancel           context.CancelFunc
	wg               sync.WaitGroup
}

func NewWSReverse(id, url, accessToken string, retryInterval time.Duration, logger *slog.Logger) *WSReverse {
	if retryInterval <= 0 {
		retryInterval = 5 * time.Second
	}
	return &WSReverse{
		id:            id,
		url:           url,
		accessToken:   accessToken,
		retryInterval: retryInterval,
		logger:        logger.With("connection", id, "component", "ws_reverse"),
		dialer: &websocket.Dialer{
			Proxy:            http.ProxyFromEnvironment,
			HandshakeTimeout: 10 * time.Second,
		},
		events:    make(chan event.Event, 128),
		state:     adapter.ConnectionStopped,
		updatedAt: time.Now(),
	}
}

func (w *WSReverse) ID() string {
	return w.id
}

func (w *WSReverse) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.state == adapter.ConnectionRunning {
		w.mu.Unlock()
		return nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.state = adapter.ConnectionRunning
	w.lastError = ""
	w.updatedAt = time.Now()
	w.mu.Unlock()

	w.wg.Add(1)
	go w.run(runCtx)

	w.logger.Info("启动", "stage", "ws_reverse", "status", "dialing", "url", w.url, "retry_interval", w.retryInterval)
	return nil
}

func (w *WSReverse) Stop(_ context.Context) error {
	w.mu.Lock()
	cancel := w.cancel
	session := w.session
	w.cancel = nil
	w.session = nil
	w.connectedClients = 0
	w.state = adapter.ConnectionStopped
	w.updatedAt = time.Now()
	w.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if session != nil {
		session.closeWithError(context.Canceled)
		_ = session.conn.Close()
	}
	w.wg.Wait()
	return nil
}

func (w *WSReverse) Events() <-chan event.Event {
	return w.events
}

func (w *WSReverse) BuildActionClient(timeout time.Duration) adapter.ActionClient {
	return newWSActionClient(w.id, timeout, w.logger, w.currentSession)
}

func (w *WSReverse) Snapshot() adapter.IngressSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return adapter.IngressSnapshot{
		ID:               w.id,
		Type:             "ws_reverse",
		ConnectedClients: w.connectedClients,
		ObservedEvents:   w.observedEvents,
		LastEventAt:      w.lastEventAt,
		LastError:        w.lastError,
		State:            w.state,
		UpdatedAt:        w.updatedAt,
	}
}

func (w *WSReverse) run(ctx context.Context) {
	defer w.wg.Done()

	for {
		if ctx.Err() != nil {
			return
		}

		err := w.connectAndServe(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			w.recordError(err)
			w.logger.Warn("反向 WebSocket 连接中断，等待重连", "error", err, "retry_interval", w.retryInterval)
		}

		timer := time.NewTimer(w.retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (w *WSReverse) connectAndServe(ctx context.Context) error {
	header := http.Header{}
	applyAccessTokenHeader(header, w.accessToken)
	conn, resp, err := w.dialer.DialContext(ctx, w.url, header)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		if resp != nil && resp.StatusCode != 0 {
			body, _ := io.ReadAll(resp.Body)
			if len(body) > 0 {
				return errors.New(resp.Status + ": " + string(body))
			}
			return errors.New(resp.Status)
		}
		return err
	}

	session := newWSActionSession(conn, w.logger)
	w.setConnected(session)
	closeErr := fmt.Errorf("反向 WebSocket 连接已关闭")
	defer w.clearConn(session, closeErr)

	w.logger.Info("连接", "stage", "ws_reverse", "status", "connected", "url", w.url)

	for {
		select {
		case <-ctx.Done():
			closeErr = ctx.Err()
			return nil
		default:
		}

		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			closeErr = err
			if ctx.Err() != nil {
				closeErr = ctx.Err()
				return nil
			}
			return err
		}
		if messageType != websocket.TextMessage {
			continue
		}
		if session.handleActionResponse(payload) {
			continue
		}

		evt, err := ParseEvent(w.id, payload)
		if err != nil {
			w.recordError(err)
			w.logger.Warn("解析反向 WebSocket 事件失败", "error", err)
			continue
		}

		w.mu.Lock()
		w.observedEvents++
		w.lastEventAt = time.Now()
		w.lastError = ""
		w.updatedAt = time.Now()
		w.mu.Unlock()

		select {
		case w.events <- evt:
		case <-ctx.Done():
			return nil
		}
	}
}

func (w *WSReverse) setConnected(session *wsActionSession) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.session = session
	w.connectedClients = 1
	w.lastError = ""
	w.updatedAt = time.Now()
}

func (w *WSReverse) clearConn(session *wsActionSession, err error) {
	if session != nil {
		session.closeWithError(err)
		_ = session.conn.Close()
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.session == session {
		w.session = nil
	}
	w.connectedClients = 0
	w.updatedAt = time.Now()
}

func (w *WSReverse) recordError(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state == adapter.ConnectionStopped {
		return
	}
	w.lastError = err.Error()
	w.updatedAt = time.Now()
}

func (w *WSReverse) currentSession() *wsActionSession {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.session
}
