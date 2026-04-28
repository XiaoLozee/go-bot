package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
)

func TestMCPToolManagerStdioDiscoversAndCallsTools(t *testing.T) {
	if os.Getenv("GO_BOT_MCP_HELPER") == "1" {
		runMCPHelperProcess()
		return
	}

	manager := newMCPToolManager(config.AIMCPConfig{
		Enabled: true,
		Servers: []config.AIMCPServerConfig{
			{
				ID:              "demo",
				Name:            "Demo MCP",
				Enabled:         true,
				Transport:       "stdio",
				Command:         os.Args[0],
				Args:            []string{"-test.run=TestMCPToolManagerStdioDiscoversAndCallsTools"},
				Env:             map[string]string{"GO_BOT_MCP_HELPER": "1"},
				ProtocolVersion: config.DefaultMCPProtocolVersion,
				TimeoutSeconds:  5,
				MaxOutputBytes:  65536,
			},
		},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	defer func() { _ = manager.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	manager.Refresh(ctx, true)

	statuses := manager.Statuses()
	if len(statuses) != 1 {
		t.Fatalf("Statuses length = %d, want 1", len(statuses))
	}
	if statuses[0].State != "ready" {
		t.Fatalf("Status state = %q, want ready, error = %q", statuses[0].State, statuses[0].LastError)
	}
	if statuses[0].ToolCount != 1 {
		t.Fatalf("ToolCount = %d, want 1", statuses[0].ToolCount)
	}

	defsByProvider := manager.ToolDefinitionsByProvider()
	defs := defsByProvider["mcp.demo"]
	if len(defs) != 1 {
		t.Fatalf("mcp.demo tool count = %d, want 1", len(defs))
	}
	if defs[0].Name != "mcp_demo_echo" {
		t.Fatalf("tool name = %q, want mcp_demo_echo", defs[0].Name)
	}

	result, err := defs[0].Handle(ctx, nil, json.RawMessage(`{"text":"hello"}`))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("Handle() result = %T, want map", result)
	}
	body, _ := json.Marshal(payload["content"])
	if !strings.Contains(string(body), "echo: hello") {
		t.Fatalf("Handle() content = %s, want echo text", body)
	}
}

func TestMCPToolManagerHTTPRefreshesToolsOnListChangedNotification(t *testing.T) {
	var toolName atomic.Value
	toolName.Store("echo")
	listenerReady := make(chan struct{})
	sendEvent := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set(mcpSessionHeader, "session-1")
			flusher, _ := w.(http.Flusher)
			select {
			case <-listenerReady:
			default:
				close(listenerReady)
			}
			if flusher != nil {
				flusher.Flush()
			}
			select {
			case <-sendEvent:
				_, _ = io.WriteString(w, "id: 1\n")
				_, _ = io.WriteString(w, `data: {"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`+"\n\n")
				if flusher != nil {
					flusher.Flush()
				}
				<-r.Context().Done()
			case <-r.Context().Done():
			}
		case http.MethodPost:
			var request struct {
				ID     any    `json:"id"`
				Method string `json:"method"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set(mcpSessionHeader, "session-1")
			if request.ID == nil {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
			switch request.Method {
			case "initialize":
				response["result"] = map[string]any{
					"protocolVersion": config.DefaultMCPProtocolVersion,
					"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}},
					"serverInfo":      map[string]any{"name": "http-helper", "version": "1.0.0"},
				}
			case "tools/list":
				response["result"] = map[string]any{
					"tools": []map[string]any{{
						"name":        toolName.Load().(string),
						"description": "Echo input text.",
						"inputSchema": map[string]any{"type": "object"},
					}},
				}
			default:
				response["error"] = map[string]any{"code": -32601, "message": "method not found"}
			}
			_ = json.NewEncoder(w).Encode(response)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	applied := make(chan map[string][]sdk.AIToolDefinition, 1)
	manager := newMCPToolManager(config.AIMCPConfig{
		Enabled: true,
		Servers: []config.AIMCPServerConfig{
			{
				ID:              "http_demo",
				Name:            "HTTP Demo MCP",
				Enabled:         true,
				Transport:       "streamable_http",
				URL:             server.URL,
				ProtocolVersion: config.DefaultMCPProtocolVersion,
				TimeoutSeconds:  5,
				MaxOutputBytes:  65536,
			},
		},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), func(defs map[string][]sdk.AIToolDefinition) error {
		applied <- defs
		return nil
	})
	defer func() { _ = manager.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	manager.Refresh(ctx, true)

	select {
	case <-listenerReady:
	case <-ctx.Done():
		t.Fatal("SSE listener did not connect")
	}
	toolName.Store("echo_changed")
	close(sendEvent)

	var defs map[string][]sdk.AIToolDefinition
	select {
	case defs = <-applied:
	case <-ctx.Done():
		t.Fatal("MCP tool catalog was not refreshed")
	}
	tools := defs["mcp.http_demo"]
	if len(tools) != 1 {
		t.Fatalf("mcp.http_demo tool count = %d, want 1", len(tools))
	}
	if tools[0].Name != "mcp_http_demo_echo_changed" {
		t.Fatalf("tool name = %q, want mcp_http_demo_echo_changed", tools[0].Name)
	}
}

func runMCPHelperProcess() {
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			JSONRPC string         `json:"jsonrpc"`
			ID      any            `json:"id"`
			Method  string         `json:"method"`
			Params  map[string]any `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			continue
		}
		if request.ID == nil {
			continue
		}
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
		}
		switch request.Method {
		case "initialize":
			response["result"] = map[string]any{
				"protocolVersion": config.DefaultMCPProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "demo-helper", "version": "1.0.0"},
			}
		case "tools/list":
			response["result"] = map[string]any{
				"tools": []map[string]any{
					{
						"name":        "echo",
						"description": "Echo input text.",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"text": map[string]any{"type": "string"},
							},
							"required": []string{"text"},
						},
					},
				},
			}
		case "tools/call":
			arguments, _ := request.Params["arguments"].(map[string]any)
			text, _ := arguments["text"].(string)
			response["result"] = map[string]any{
				"content": []map[string]any{{"type": "text", "text": "echo: " + text}},
			}
		default:
			response["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		_ = encoder.Encode(response)
	}
	os.Exit(0)
}
