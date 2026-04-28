package ai

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
)

const (
	mcpToolProviderPrefix = "mcp."
	mcpClientName         = "go-bot"
	mcpMaxListPages       = 32
	mcpSessionHeader      = "Mcp-Session-Id"
	mcpLastEventIDHeader  = "Last-Event-ID"
)

var nonToolNameChars = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
var errMCPSSEUnsupported = errors.New("mcp sse unsupported")

type MCPServerDebugView struct {
	ID              string             `json:"id"`
	Name            string             `json:"name,omitempty"`
	Enabled         bool               `json:"enabled"`
	Transport       string             `json:"transport"`
	State           string             `json:"state"`
	ProtocolVersion string             `json:"protocol_version,omitempty"`
	ServerName      string             `json:"server_name,omitempty"`
	ServerVersion   string             `json:"server_version,omitempty"`
	ToolCount       int                `json:"tool_count"`
	Tools           []MCPToolDebugView `json:"tools,omitempty"`
	LastError       string             `json:"last_error,omitempty"`
	ConnectedAt     time.Time          `json:"connected_at,omitempty"`
	SSEState        string             `json:"sse_state,omitempty"`
	LastSSEError    string             `json:"last_sse_error,omitempty"`
	LastSSEEventID  string             `json:"last_sse_event_id,omitempty"`
	LastSSEAt       time.Time          `json:"last_sse_at,omitempty"`
	LastRefreshAt   time.Time          `json:"last_refresh_at,omitempty"`
}

type MCPToolDebugView struct {
	Name        string `json:"name"`
	Original    string `json:"original"`
	Description string `json:"description,omitempty"`
}

type mcpToolManager struct {
	logger   *slog.Logger
	cfg      config.AIMCPConfig
	mu       sync.RWMutex
	servers  map[string]*mcpServerRuntime
	statuses []MCPServerDebugView
	onChange func(map[string][]sdk.AIToolDefinition) error
}

type mcpServerRuntime struct {
	cfg          config.AIMCPServerConfig
	client       mcpClient
	tools        []mcpToolRegistration
	toolNames    map[string]mcpToolRegistration
	status       MCPServerDebugView
	listenCancel context.CancelFunc
	listenWG     sync.WaitGroup
}

type mcpToolRegistration struct {
	ServerID     string
	OriginalName string
	ToolName     string
	Description  string
	InputSchema  map[string]any
}

type mcpClient interface {
	Initialize(ctx context.Context) (mcpInitializeResult, error)
	ListTools(ctx context.Context) ([]mcpRemoteTool, error)
	CallTool(ctx context.Context, name string, args json.RawMessage) (mcpCallToolResult, error)
	Close() error
}

type mcpNotificationListener interface {
	Listen(ctx context.Context, handler func(mcpJSONRPCNotification)) error
}

type mcpJSONRPCNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	EventID string          `json:"-"`
}

type mcpInitializeResult struct {
	ProtocolVersion string        `json:"protocolVersion"`
	ServerInfo      mcpServerInfo `json:"serverInfo"`
}

type mcpServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type mcpRemoteTool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpListToolsResult struct {
	Tools      []mcpRemoteTool `json:"tools"`
	NextCursor string          `json:"nextCursor"`
}

type mcpCallToolResult struct {
	Content []map[string]any `json:"content,omitempty"`
	IsError bool             `json:"isError,omitempty"`
	Raw     map[string]any   `json:"-"`
}

type mcpJSONRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type mcpJSONRPCResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      any              `json:"id,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *mcpJSONRPCError `json:"error,omitempty"`
}

type mcpJSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func newMCPToolManager(cfg config.AIMCPConfig, logger *slog.Logger, onChange func(map[string][]sdk.AIToolDefinition) error) *mcpToolManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &mcpToolManager{
		logger:   logger.With("component", "mcp_tools"),
		cfg:      config.NormalizeAIMCPConfig(cfg),
		servers:  make(map[string]*mcpServerRuntime),
		onChange: onChange,
	}
}

func (m *mcpToolManager) Refresh(ctx context.Context, aiEnabled bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	servers := make(map[string]*mcpServerRuntime)
	statuses := make([]MCPServerDebugView, 0, len(m.cfg.Servers))
	if !m.cfg.Enabled {
		for _, server := range m.cfg.Servers {
			statuses = append(statuses, mcpStatusFromConfig(server, "disabled", ""))
		}
		m.replace(servers, statuses)
		return
	}
	if !aiEnabled {
		for _, server := range m.cfg.Servers {
			statuses = append(statuses, mcpStatusFromConfig(server, "waiting_ai", ""))
		}
		m.replace(servers, statuses)
		return
	}

	usedToolNames := map[string]struct{}{}
	for _, server := range m.cfg.Servers {
		status := mcpStatusFromConfig(server, "disabled", "")
		if !server.Enabled {
			statuses = append(statuses, status)
			continue
		}
		status.State = "connecting"
		client, err := newMCPClient(server)
		if err != nil {
			status.State = "failed"
			status.LastError = err.Error()
			statuses = append(statuses, status)
			continue
		}
		runtime := &mcpServerRuntime{
			cfg:       server,
			client:    client,
			toolNames: make(map[string]mcpToolRegistration),
		}
		timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(server.TimeoutSeconds)*time.Second)
		initResult, err := client.Initialize(timeoutCtx)
		cancel()
		if err != nil {
			_ = client.Close()
			status.State = "failed"
			status.LastError = err.Error()
			statuses = append(statuses, status)
			m.logger.Warn("MCP server initialize failed", "server", server.ID, "error", err)
			continue
		}
		status.ProtocolVersion = strings.TrimSpace(initResult.ProtocolVersion)
		status.ServerName = strings.TrimSpace(initResult.ServerInfo.Name)
		status.ServerVersion = strings.TrimSpace(initResult.ServerInfo.Version)
		timeoutCtx, cancel = context.WithTimeout(ctx, time.Duration(server.TimeoutSeconds)*time.Second)
		tools, err := client.ListTools(timeoutCtx)
		cancel()
		if err != nil {
			_ = client.Close()
			status.State = "failed"
			status.LastError = err.Error()
			statuses = append(statuses, status)
			m.logger.Warn("MCP server list tools failed", "server", server.ID, "error", err)
			continue
		}
		registrations := buildMCPToolRegistrations(server, tools, usedToolNames)
		for _, item := range registrations {
			runtime.tools = append(runtime.tools, item)
			runtime.toolNames[item.ToolName] = item
			status.Tools = append(status.Tools, MCPToolDebugView{
				Name:        item.ToolName,
				Original:    item.OriginalName,
				Description: item.Description,
			})
		}
		status.State = "ready"
		status.ToolCount = len(runtime.tools)
		status.ConnectedAt = time.Now()
		status.LastRefreshAt = status.ConnectedAt
		runtime.status = status
		servers[server.ID] = runtime
		statuses = append(statuses, status)
	}
	m.replace(servers, statuses)
	m.startListeners()
}

func (m *mcpToolManager) replace(servers map[string]*mcpServerRuntime, statuses []MCPServerDebugView) {
	m.mu.Lock()
	oldServers := m.servers
	m.servers = servers
	m.statuses = append([]MCPServerDebugView(nil), statuses...)
	m.mu.Unlock()
	for _, server := range oldServers {
		if server != nil {
			_ = server.close()
		}
	}
}

func (m *mcpToolManager) Close() error {
	m.mu.Lock()
	servers := m.servers
	m.servers = map[string]*mcpServerRuntime{}
	m.statuses = nil
	m.mu.Unlock()
	var closeErr error
	for _, server := range servers {
		if server == nil {
			continue
		}
		if err := server.close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func (r *mcpServerRuntime) close() error {
	if r.listenCancel != nil {
		r.listenCancel()
		r.listenWG.Wait()
	}
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}

func (m *mcpToolManager) startListeners() {
	m.mu.RLock()
	runtimes := make([]*mcpServerRuntime, 0, len(m.servers))
	for _, runtime := range m.servers {
		if runtime != nil {
			runtimes = append(runtimes, runtime)
		}
	}
	m.mu.RUnlock()
	for _, runtime := range runtimes {
		listener, ok := runtime.client.(mcpNotificationListener)
		if !ok {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		runtime.listenCancel = cancel
		runtime.listenWG.Add(1)
		go m.runNotificationListener(ctx, runtime, listener)
	}
}

func (m *mcpToolManager) runNotificationListener(ctx context.Context, runtime *mcpServerRuntime, listener mcpNotificationListener) {
	defer runtime.listenWG.Done()
	delay := 2 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		m.setSSEState(runtime.cfg.ID, "connecting", "", "")
		err := listener.Listen(ctx, func(notification mcpJSONRPCNotification) {
			m.handleNotification(runtime.cfg.ID, notification)
		})
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errMCPSSEUnsupported) {
			m.setSSEState(runtime.cfg.ID, "unsupported", "", "")
			return
		}
		errText := ""
		if err != nil {
			errText = err.Error()
		}
		m.setSSEState(runtime.cfg.ID, "reconnecting", errText, "")
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
		if delay < 30*time.Second {
			delay *= 2
		}
	}
}

func (m *mcpToolManager) handleNotification(serverID string, notification mcpJSONRPCNotification) {
	method := strings.TrimSpace(notification.Method)
	if method == "" {
		return
	}
	m.setSSEState(serverID, "listening", "", notification.EventID)
	if method != "notifications/tools/list_changed" {
		return
	}
	refreshCtx, cancel := context.WithTimeout(context.Background(), m.serverTimeout(serverID))
	defer cancel()
	if err := m.refreshServerTools(refreshCtx, serverID); err != nil {
		m.setSSEState(serverID, "refresh_failed", err.Error(), notification.EventID)
		m.logger.Warn("MCP tools/list refresh failed", "server", serverID, "error", err)
		return
	}
	if m.onChange != nil {
		if err := m.onChange(m.ToolDefinitionsByProvider()); err != nil {
			m.setSSEState(serverID, "refresh_failed", err.Error(), notification.EventID)
			m.logger.Warn("MCP tool catalog apply failed", "server", serverID, "error", err)
			return
		}
	}
	m.setSSEState(serverID, "listening", "", notification.EventID)
}

func (m *mcpToolManager) serverTimeout(serverID string) time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if runtime := m.servers[serverID]; runtime != nil && runtime.cfg.TimeoutSeconds > 0 {
		return time.Duration(runtime.cfg.TimeoutSeconds) * time.Second
	}
	return 15 * time.Second
}

func (m *mcpToolManager) refreshServerTools(ctx context.Context, serverID string) error {
	m.mu.RLock()
	runtime := m.servers[serverID]
	usedToolNames := map[string]struct{}{}
	for id, server := range m.servers {
		if id == serverID || server == nil {
			continue
		}
		for _, item := range server.tools {
			usedToolNames[item.ToolName] = struct{}{}
		}
	}
	m.mu.RUnlock()
	if runtime == nil || runtime.client == nil {
		return fmt.Errorf("MCP server 不存在或未就绪: %s", serverID)
	}
	tools, err := runtime.client.ListTools(ctx)
	if err != nil {
		return err
	}
	registrations := buildMCPToolRegistrations(runtime.cfg, tools, usedToolNames)
	toolNames := make(map[string]mcpToolRegistration, len(registrations))
	toolViews := make([]MCPToolDebugView, 0, len(registrations))
	for _, item := range registrations {
		toolNames[item.ToolName] = item
		toolViews = append(toolViews, MCPToolDebugView{
			Name:        item.ToolName,
			Original:    item.OriginalName,
			Description: item.Description,
		})
	}
	now := time.Now()
	m.mu.Lock()
	if current := m.servers[serverID]; current != nil {
		current.tools = registrations
		current.toolNames = toolNames
		current.status.Tools = toolViews
		current.status.ToolCount = len(toolViews)
		current.status.LastError = ""
		current.status.LastRefreshAt = now
		m.updateStatusLocked(serverID, func(status *MCPServerDebugView) {
			status.Tools = toolViews
			status.ToolCount = len(toolViews)
			status.LastError = ""
			status.LastRefreshAt = now
		})
	}
	m.mu.Unlock()
	return nil
}

func (m *mcpToolManager) setSSEState(serverID string, state string, errText string, eventID string) {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateStatusLocked(serverID, func(status *MCPServerDebugView) {
		status.SSEState = state
		status.LastSSEError = errText
		status.LastSSEAt = now
		if eventID != "" {
			status.LastSSEEventID = eventID
		}
	})
	if runtime := m.servers[serverID]; runtime != nil {
		runtime.status.SSEState = state
		runtime.status.LastSSEError = errText
		runtime.status.LastSSEAt = now
		if eventID != "" {
			runtime.status.LastSSEEventID = eventID
		}
	}
}

func (m *mcpToolManager) updateStatusLocked(serverID string, update func(*MCPServerDebugView)) {
	for i := range m.statuses {
		if m.statuses[i].ID != serverID {
			continue
		}
		update(&m.statuses[i])
		return
	}
}

func (m *mcpToolManager) Statuses() []MCPServerDebugView {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MCPServerDebugView, 0, len(m.statuses))
	for _, status := range m.statuses {
		status.Tools = append([]MCPToolDebugView(nil), status.Tools...)
		out = append(out, status)
	}
	return out
}

func (m *mcpToolManager) ToolDefinitionsByProvider() map[string][]sdk.AIToolDefinition {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string][]sdk.AIToolDefinition, len(m.servers))
	for serverID, runtime := range m.servers {
		if runtime == nil || len(runtime.tools) == 0 {
			continue
		}
		defs := make([]sdk.AIToolDefinition, 0, len(runtime.tools))
		for _, item := range runtime.tools {
			registration := item
			defs = append(defs, sdk.AIToolDefinition{
				Name:        registration.ToolName,
				Description: buildMCPToolDescription(runtime.cfg, registration),
				InputSchema: registration.InputSchema,
				Handle: func(ctx context.Context, _ sdk.AIToolContext, args json.RawMessage) (any, error) {
					return m.CallTool(ctx, registration.ToolName, args)
				},
			})
		}
		out[mcpToolProviderPrefix+serverID] = defs
	}
	return out
}

func (m *mcpToolManager) CallTool(ctx context.Context, toolName string, args json.RawMessage) (any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.RLock()
	var runtime *mcpServerRuntime
	var registration mcpToolRegistration
	for _, server := range m.servers {
		if server == nil {
			continue
		}
		if item, ok := server.toolNames[toolName]; ok {
			itemCopy := item
			runtime = server
			registration = itemCopy
			break
		}
	}
	m.mu.RUnlock()
	if runtime == nil || runtime.client == nil {
		return nil, fmt.Errorf("MCP 工具未就绪: %s", toolName)
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(runtime.cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	result, err := runtime.client.CallTool(callCtx, registration.OriginalName, args)
	if err != nil {
		return nil, err
	}
	limited := limitMCPToolResult(result.toPayload(), runtime.cfg.MaxOutputBytes)
	if result.IsError {
		return limited, fmt.Errorf("MCP 工具返回错误")
	}
	return limited, nil
}

func mcpStatusFromConfig(server config.AIMCPServerConfig, state string, errText string) MCPServerDebugView {
	return MCPServerDebugView{
		ID:              server.ID,
		Name:            server.Name,
		Enabled:         server.Enabled,
		Transport:       server.Transport,
		State:           state,
		ProtocolVersion: server.ProtocolVersion,
		LastError:       errText,
	}
}

func newMCPClient(server config.AIMCPServerConfig) (mcpClient, error) {
	switch strings.TrimSpace(strings.ToLower(server.Transport)) {
	case "", "stdio":
		return newMCPStdioClient(server), nil
	case "http", "streamable_http":
		return newMCPHTTPClient(server), nil
	default:
		return nil, fmt.Errorf("不支持的 MCP transport: %s", server.Transport)
	}
}

func buildMCPToolRegistrations(server config.AIMCPServerConfig, tools []mcpRemoteTool, used map[string]struct{}) []mcpToolRegistration {
	allowed := map[string]struct{}{}
	for _, item := range server.AllowedTools {
		allowed[strings.TrimSpace(item)] = struct{}{}
	}
	out := make([]mcpToolRegistration, 0, len(tools))
	localUsed := map[string]int{}
	for _, tool := range tools {
		originalName := strings.TrimSpace(tool.Name)
		if originalName == "" {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[originalName]; !ok {
				continue
			}
		}
		safeName := safeMCPToolName(server.ID, originalName)
		if count := localUsed[safeName]; count > 0 {
			safeName = safeMCPToolName(server.ID, fmt.Sprintf("%s_%d", originalName, count+1))
		}
		localUsed[safeName]++
		if _, exists := used[safeName]; exists {
			safeName = safeMCPToolName(server.ID, originalName+"_"+shortHash(server.ID+":"+originalName))
		}
		used[safeName] = struct{}{}
		description := strings.TrimSpace(tool.Description)
		if description == "" {
			description = strings.TrimSpace(tool.Title)
		}
		out = append(out, mcpToolRegistration{
			ServerID:     server.ID,
			OriginalName: originalName,
			ToolName:     safeName,
			Description:  description,
			InputSchema:  normalizeMCPInputSchema(tool.InputSchema),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ToolName < out[j].ToolName })
	return out
}

func buildMCPToolDescription(server config.AIMCPServerConfig, tool mcpToolRegistration) string {
	serverName := firstNonEmpty(server.Name, server.ID)
	description := strings.TrimSpace(tool.Description)
	if description == "" {
		description = "MCP server tool."
	}
	return fmt.Sprintf("MCP server %s tool %s. %s", serverName, tool.OriginalName, description)
}

func safeMCPToolName(serverID string, remoteName string) string {
	serverPart := normalizeToolNamePart(serverID)
	toolPart := normalizeToolNamePart(remoteName)
	if serverPart == "" {
		serverPart = "server"
	}
	if toolPart == "" {
		toolPart = "tool"
	}
	name := "mcp_" + serverPart + "_" + toolPart
	if len(name) <= 64 {
		return name
	}
	suffix := "_" + shortHash(name)
	return strings.TrimRight(name[:64-len(suffix)], "_-") + suffix
}

func normalizeToolNamePart(value string) string {
	value = strings.TrimSpace(value)
	value = nonToolNameChars.ReplaceAllString(value, "_")
	value = strings.Trim(value, "_-")
	return value
}

func shortHash(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])[:8]
}

func normalizeMCPInputSchema(schema map[string]any) map[string]any {
	if len(schema) == 0 {
		return emptyToolInputSchema()
	}
	cloned := cloneToolInputSchema(schema)
	if cloned == nil {
		return emptyToolInputSchema()
	}
	if typ, _ := cloned["type"].(string); strings.TrimSpace(typ) == "" {
		cloned["type"] = "object"
	}
	return cloned
}

func limitMCPToolResult(value any, maxBytes int) any {
	if maxBytes <= 0 {
		maxBytes = 65536
	}
	body, err := json.Marshal(value)
	if err != nil || len(body) <= maxBytes {
		return value
	}
	return map[string]any{
		"truncated": true,
		"bytes":     len(body),
		"preview":   string(body[:maxBytes]),
	}
}

func (r mcpCallToolResult) toPayload() map[string]any {
	if len(r.Raw) > 0 {
		return r.Raw
	}
	return map[string]any{
		"content":  r.Content,
		"is_error": r.IsError,
	}
}

type mcpStdioClient struct {
	cfg      config.AIMCPServerConfig
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	scanner  *bufio.Scanner
	mu       sync.Mutex
	closeMu  sync.Mutex
	closed   bool
	nextID   int64
	stderrMu sync.Mutex
	stderr   []string
}

func newMCPStdioClient(cfg config.AIMCPServerConfig) *mcpStdioClient {
	return &mcpStdioClient{cfg: cfg}
}

func (c *mcpStdioClient) Initialize(ctx context.Context) (mcpInitializeResult, error) {
	if err := c.start(ctx); err != nil {
		return mcpInitializeResult{}, err
	}
	var result mcpInitializeResult
	if err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": c.cfg.ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    mcpClientName,
			"version": "0.1.0",
		},
	}, &result); err != nil {
		return mcpInitializeResult{}, err
	}
	if err := c.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		return mcpInitializeResult{}, err
	}
	return result, nil
}

func (c *mcpStdioClient) ListTools(ctx context.Context) ([]mcpRemoteTool, error) {
	var out []mcpRemoteTool
	cursor := ""
	for page := 0; page < mcpMaxListPages; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var result mcpListToolsResult
		if err := c.request(ctx, "tools/list", params, &result); err != nil {
			return nil, err
		}
		out = append(out, result.Tools...)
		cursor = strings.TrimSpace(result.NextCursor)
		if cursor == "" {
			return out, nil
		}
	}
	return out, fmt.Errorf("MCP tools/list 超过最大分页数 %d", mcpMaxListPages)
}

func (c *mcpStdioClient) CallTool(ctx context.Context, name string, args json.RawMessage) (mcpCallToolResult, error) {
	params := map[string]any{
		"name":      name,
		"arguments": decodeRawJSONObject(args),
	}
	var result map[string]any
	if err := c.request(ctx, "tools/call", params, &result); err != nil {
		return mcpCallToolResult{}, err
	}
	return decodeMCPCallToolResult(result), nil
}

func (c *mcpStdioClient) Close() error {
	c.closeMu.Lock()
	if c.closed {
		c.closeMu.Unlock()
		return nil
	}
	c.closed = true
	stdin := c.stdin
	cmd := c.cmd
	c.closeMu.Unlock()
	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
	return nil
}

func (c *mcpStdioClient) start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd != nil {
		return nil
	}
	if strings.TrimSpace(c.cfg.Command) == "" {
		return fmt.Errorf("MCP stdio command 不能为空")
	}
	cmd := exec.Command(c.cfg.Command, c.cfg.Args...)
	cmd.Env = os.Environ()
	for key, value := range c.cfg.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	bufferSize := mcpMaxInt(c.cfg.MaxOutputBytes+4096, 1024*1024)
	scanner.Buffer(make([]byte, 0, 64*1024), bufferSize)
	c.cmd = cmd
	c.stdin = stdin
	c.scanner = scanner
	go c.captureStderr(stderr)
	return nil
}

func (c *mcpStdioClient) request(ctx context.Context, method string, params any, target any) error {
	done := make(chan error, 1)
	go func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		id := c.nextID + 1
		c.nextID = id
		payload, err := json.Marshal(mcpJSONRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
		if err != nil {
			done <- err
			return
		}
		if _, err := c.stdin.Write(append(payload, '\n')); err != nil {
			done <- err
			return
		}
		for c.scanner.Scan() {
			line := bytes.TrimSpace(c.scanner.Bytes())
			if len(line) == 0 || line[0] != '{' {
				continue
			}
			var response mcpJSONRPCResponse
			if err := json.Unmarshal(line, &response); err != nil {
				continue
			}
			if fmt.Sprint(response.ID) != fmt.Sprint(id) {
				continue
			}
			if response.Error != nil {
				done <- fmt.Errorf("MCP %s 返回错误 %d: %s", method, response.Error.Code, response.Error.Message)
				return
			}
			if target != nil {
				done <- json.Unmarshal(response.Result, target)
				return
			}
			done <- nil
			return
		}
		if err := c.scanner.Err(); err != nil {
			done <- fmt.Errorf("读取 MCP stdio 响应失败: %w", err)
			return
		}
		done <- fmt.Errorf("MCP stdio 已关闭%s", c.stderrSuffix())
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = c.Close()
		return ctx.Err()
	}
}

func (c *mcpStdioClient) notify(ctx context.Context, method string, params any) error {
	done := make(chan error, 1)
	go func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		payload, err := json.Marshal(mcpJSONRPCRequest{JSONRPC: "2.0", Method: method, Params: params})
		if err != nil {
			done <- err
			return
		}
		_, err = c.stdin.Write(append(payload, '\n'))
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = c.Close()
		return ctx.Err()
	}
}

func (c *mcpStdioClient) captureStderr(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		c.stderrMu.Lock()
		if len(c.stderr) >= 8 {
			c.stderr = c.stderr[1:]
		}
		c.stderr = append(c.stderr, line)
		c.stderrMu.Unlock()
	}
}

func (c *mcpStdioClient) stderrSuffix() string {
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()
	if len(c.stderr) == 0 {
		return ""
	}
	return ": " + strings.Join(c.stderr, " | ")
}

type mcpHTTPClient struct {
	cfg         config.AIMCPServerConfig
	httpClient  *http.Client
	sessionID   string
	lastEventID string
	mu          sync.Mutex
	nextID      int64
}

func newMCPHTTPClient(cfg config.AIMCPServerConfig) *mcpHTTPClient {
	return &mcpHTTPClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
		},
	}
}

func (c *mcpHTTPClient) Initialize(ctx context.Context) (mcpInitializeResult, error) {
	var result mcpInitializeResult
	if err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": c.cfg.ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    mcpClientName,
			"version": "0.1.0",
		},
	}, &result); err != nil {
		return mcpInitializeResult{}, err
	}
	if err := c.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		return mcpInitializeResult{}, err
	}
	return result, nil
}

func (c *mcpHTTPClient) ListTools(ctx context.Context) ([]mcpRemoteTool, error) {
	var out []mcpRemoteTool
	cursor := ""
	for page := 0; page < mcpMaxListPages; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var result mcpListToolsResult
		if err := c.request(ctx, "tools/list", params, &result); err != nil {
			return nil, err
		}
		out = append(out, result.Tools...)
		cursor = strings.TrimSpace(result.NextCursor)
		if cursor == "" {
			return out, nil
		}
	}
	return out, fmt.Errorf("MCP tools/list 超过最大分页数 %d", mcpMaxListPages)
}

func (c *mcpHTTPClient) CallTool(ctx context.Context, name string, args json.RawMessage) (mcpCallToolResult, error) {
	params := map[string]any{
		"name":      name,
		"arguments": decodeRawJSONObject(args),
	}
	var result map[string]any
	if err := c.request(ctx, "tools/call", params, &result); err != nil {
		return mcpCallToolResult{}, err
	}
	return decodeMCPCallToolResult(result), nil
}

func (c *mcpHTTPClient) Close() error {
	return nil
}

func (c *mcpHTTPClient) request(ctx context.Context, method string, params any, target any) error {
	id := c.nextRequestID()
	body, err := json.Marshal(mcpJSONRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return err
	}
	response, err := c.doPost(ctx, body)
	if err != nil {
		return err
	}
	if fmt.Sprint(response.ID) != fmt.Sprint(id) {
		return fmt.Errorf("MCP HTTP 响应 id 不匹配")
	}
	if response.Error != nil {
		return fmt.Errorf("MCP %s 返回错误 %d: %s", method, response.Error.Code, response.Error.Message)
	}
	if target != nil {
		return json.Unmarshal(response.Result, target)
	}
	return nil
}

func (c *mcpHTTPClient) notify(ctx context.Context, method string, params any) error {
	body, err := json.Marshal(mcpJSONRPCRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	_, err = c.doPost(ctx, body)
	return err
}

func (c *mcpHTTPClient) Listen(ctx context.Context, handler func(mcpJSONRPCNotification)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.URL, nil)
	if err != nil {
		return err
	}
	c.applyHeaders(req, "text/event-stream")
	sessionID, lastEventID := c.sessionState()
	if sessionID != "" {
		req.Header.Set(mcpSessionHeader, sessionID)
	}
	if lastEventID != "" {
		req.Header.Set(mcpLastEventIDHeader, lastEventID)
	}
	client := *c.httpClient
	client.Timeout = 0
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if sessionID := strings.TrimSpace(resp.Header.Get(mcpSessionHeader)); sessionID != "" {
		c.setSessionID(sessionID)
	}
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotFound {
		return errMCPSSEUnsupported
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("MCP SSE GET 返回状态 %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "text/event-stream") {
		return errMCPSSEUnsupported
	}
	return c.readSSEStream(ctx, resp.Body, handler)
}

func (c *mcpHTTPClient) doPost(ctx context.Context, body []byte) (mcpJSONRPCResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return mcpJSONRPCResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyHeaders(req, "application/json, text/event-stream")
	if sessionID, _ := c.sessionState(); sessionID != "" {
		req.Header.Set(mcpSessionHeader, sessionID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return mcpJSONRPCResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if sessionID := strings.TrimSpace(resp.Header.Get(mcpSessionHeader)); sessionID != "" {
		c.setSessionID(sessionID)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, int64(mcpMaxInt(c.cfg.MaxOutputBytes+4096, 1024*1024))))
	if err != nil {
		return mcpJSONRPCResponse{}, err
	}
	if resp.StatusCode == http.StatusAccepted && len(bytes.TrimSpace(payload)) == 0 {
		return mcpJSONRPCResponse{}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mcpJSONRPCResponse{}, fmt.Errorf("MCP HTTP 返回状态 %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		payload = firstSSEData(payload)
	}
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return mcpJSONRPCResponse{}, nil
	}
	var decoded mcpJSONRPCResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return mcpJSONRPCResponse{}, err
	}
	return decoded, nil
}

func (c *mcpHTTPClient) nextRequestID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	return c.nextID
}

func (c *mcpHTTPClient) sessionState() (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionID, c.lastEventID
}

func (c *mcpHTTPClient) setSessionID(sessionID string) {
	c.mu.Lock()
	c.sessionID = strings.TrimSpace(sessionID)
	c.mu.Unlock()
}

func (c *mcpHTTPClient) setLastEventID(eventID string) {
	c.mu.Lock()
	c.lastEventID = strings.TrimSpace(eventID)
	c.mu.Unlock()
}

func (c *mcpHTTPClient) applyHeaders(req *http.Request, accept string) {
	req.Header.Set("Accept", accept)
	req.Header.Set("MCP-Protocol-Version", c.cfg.ProtocolVersion)
	for key, value := range c.cfg.Headers {
		req.Header.Set(key, value)
	}
}

func (c *mcpHTTPClient) readSSEStream(ctx context.Context, reader io.Reader, handler func(mcpJSONRPCNotification)) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 16*1024), mcpMaxInt(c.cfg.MaxOutputBytes+4096, 1024*1024))
	event := mcpSSEEvent{}
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			c.flushSSEEvent(event, handler)
			event = mcpSSEEvent{}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			event.ID = value
		case "event":
			event.Event = value
		case "data":
			event.Data = append(event.Data, value)
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	c.flushSSEEvent(event, handler)
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}

type mcpSSEEvent struct {
	ID    string
	Event string
	Data  []string
}

func (c *mcpHTTPClient) flushSSEEvent(event mcpSSEEvent, handler func(mcpJSONRPCNotification)) {
	if strings.TrimSpace(event.ID) != "" {
		c.setLastEventID(event.ID)
	}
	if len(event.Data) == 0 {
		return
	}
	body := strings.TrimSpace(strings.Join(event.Data, "\n"))
	if body == "" || !strings.HasPrefix(body, "{") {
		return
	}
	var notification mcpJSONRPCNotification
	if err := json.Unmarshal([]byte(body), &notification); err != nil {
		return
	}
	if strings.TrimSpace(notification.Method) == "" {
		return
	}
	notification.EventID = strings.TrimSpace(event.ID)
	handler(notification)
}

func decodeRawJSONObject(raw json.RawMessage) any {
	payload := bytes.TrimSpace(raw)
	if len(payload) == 0 {
		return map[string]any{}
	}
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return map[string]any{}
	}
	if decoded == nil {
		return map[string]any{}
	}
	return decoded
}

func decodeMCPCallToolResult(raw map[string]any) mcpCallToolResult {
	body, _ := json.Marshal(raw)
	var decoded struct {
		Content []map[string]any `json:"content"`
		IsError bool             `json:"isError"`
	}
	_ = json.Unmarshal(body, &decoded)
	return mcpCallToolResult{
		Content: decoded.Content,
		IsError: decoded.IsError,
		Raw:     raw,
	}
}

func firstSSEData(payload []byte) []byte {
	scanner := bufio.NewScanner(bytes.NewReader(payload))
	var data []string
	flush := func() []byte {
		if len(data) == 0 {
			return nil
		}
		joined := strings.Join(data, "\n")
		data = nil
		trimmed := strings.TrimSpace(joined)
		if strings.HasPrefix(trimmed, "{") {
			return []byte(trimmed)
		}
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			if found := flush(); len(found) > 0 {
				return found
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if found := flush(); len(found) > 0 {
		return found
	}
	return bytes.TrimSpace(payload)
}

func mcpMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
