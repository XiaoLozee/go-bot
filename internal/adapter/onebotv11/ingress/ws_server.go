package ingress

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/XiaoLozee/go-bot/internal/adapter"
	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/gorilla/websocket"
)

type WSServer struct {
	id          string
	listen      string
	path        string
	accessToken string
	logger      *slog.Logger
	events      chan event.Event

	mu               sync.RWMutex
	state            adapter.ConnectionState
	lastError        string
	connectedClients int
	observedEvents   int
	lastEventAt      time.Time
	updatedAt        time.Time
	server           *http.Server
	listener         net.Listener
	conns            map[*websocket.Conn]*wsActionSession
	primaryConn      *websocket.Conn
}

func NewWSServer(id, listen, path, accessToken string, logger *slog.Logger) *WSServer {
	if path == "" {
		path = "/ws"
	}
	return &WSServer{
		id:          id,
		listen:      listen,
		path:        path,
		accessToken: accessToken,
		logger:      logger.With("connection", id, "component", "ws_ingress"),
		events:      make(chan event.Event, 128),
		state:       adapter.ConnectionStopped,
		updatedAt:   time.Now(),
		conns:       make(map[*websocket.Conn]*wsActionSession),
	}
}

func (w *WSServer) ID() string {
	return w.id
}

func (w *WSServer) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.state == adapter.ConnectionRunning {
		w.mu.Unlock()
		return nil
	}
	w.mu.Unlock()

	mux := http.NewServeMux()
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	mux.HandleFunc(w.path, func(rw http.ResponseWriter, r *http.Request) {
		if !accessTokenMatches(w.accessToken, requestAccessToken(r)) {
			rw.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(rw, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			w.logger.Warn("拒绝未授权的 WebSocket 连接", "remote", r.RemoteAddr)
			return
		}
		conn, err := upgrader.Upgrade(rw, r, nil)
		if err != nil {
			w.setError(err)
			w.logger.Error("升级 WebSocket 失败", "error", err)
			return
		}
		w.handleConn(ctx, conn)
	})

	ln, err := net.Listen("tcp", w.listen)
	if err != nil {
		w.setError(err)
		return err
	}

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	w.mu.Lock()
	w.listener = ln
	w.server = server
	w.state = adapter.ConnectionRunning
	w.lastError = ""
	w.updatedAt = time.Now()
	w.mu.Unlock()

	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			w.setError(err)
			w.logger.Error("WebSocket ingress 运行失败", "error", err)
		}
	}()

	w.logger.Info("启动", "stage", "ws_ingress", "status", "listening", "listen", w.listen, "path", w.path)
	return nil
}

func (w *WSServer) Stop(ctx context.Context) error {
	w.mu.Lock()
	server := w.server
	conns := make([]*websocket.Conn, 0, len(w.conns))
	for conn := range w.conns {
		conns = append(conns, conn)
	}
	w.state = adapter.ConnectionStopped
	w.updatedAt = time.Now()
	w.mu.Unlock()

	for _, conn := range conns {
		_ = conn.Close()
	}

	if server != nil {
		if err := server.Shutdown(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (w *WSServer) Events() <-chan event.Event {
	return w.events
}

func (w *WSServer) BuildActionClient(timeout time.Duration) adapter.ActionClient {
	return newWSActionClient(w.id, timeout, w.logger, w.currentSession)
}

func (w *WSServer) Snapshot() adapter.IngressSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return adapter.IngressSnapshot{
		ID:               w.id,
		Type:             "ws_server",
		State:            w.state,
		Listen:           w.listen,
		Path:             w.path,
		ConnectedClients: w.connectedClients,
		ObservedEvents:   w.observedEvents,
		LastEventAt:      w.lastEventAt,
		LastError:        w.lastError,
		UpdatedAt:        w.updatedAt,
	}
}

func (w *WSServer) handleConn(ctx context.Context, conn *websocket.Conn) {
	session := newWSActionSession(conn, w.logger)
	w.mu.Lock()
	w.conns[conn] = session
	w.primaryConn = conn
	w.connectedClients = len(w.conns)
	w.updatedAt = time.Now()
	w.mu.Unlock()

	w.logger.Info("连接", "stage", "ws_ingress", "peer", "napcat", "status", "connected", "remote", conn.RemoteAddr().String())

	closeErr := fmt.Errorf("WebSocket ingress 连接已关闭")
	defer func() {
		session.closeWithError(closeErr)
		_ = conn.Close()
		w.mu.Lock()
		delete(w.conns, conn)
		if w.primaryConn == conn {
			w.primaryConn = nil
			for candidate := range w.conns {
				w.primaryConn = candidate
				break
			}
		}
		w.connectedClients = len(w.conns)
		w.updatedAt = time.Now()
		w.mu.Unlock()
		w.logger.Info("NapCat 已断开 WebSocket ingress", "remote", conn.RemoteAddr().String())
	}()

	for {
		select {
		case <-ctx.Done():
			closeErr = ctx.Err()
			return
		default:
		}

		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			closeErr = err
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				w.setError(err)
				w.logger.Warn("读取 WebSocket 消息失败", "error", err)
			}
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}
		if session.handleActionResponse(payload) {
			continue
		}

		evt, err := ParseEvent(w.id, payload)
		if err != nil {
			w.setError(err)
			w.logger.Warn("解析 OneBot 事件失败", "error", err)
			continue
		}

		w.mu.Lock()
		w.observedEvents++
		w.lastEventAt = time.Now()
		w.updatedAt = time.Now()
		w.mu.Unlock()

		w.events <- evt
	}
}

func (w *WSServer) setError(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = adapter.ConnectionFailed
	w.lastError = err.Error()
	w.updatedAt = time.Now()
}

func (w *WSServer) currentSession() *wsActionSession {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.primaryConn != nil {
		if session := w.conns[w.primaryConn]; session != nil {
			return session
		}
	}
	for _, session := range w.conns {
		return session
	}
	return nil
}
