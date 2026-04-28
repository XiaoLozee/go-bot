package externalexec

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
)

type Plugin struct {
	desc      Descriptor
	mu        sync.Mutex
	logger    *slog.Logger
	messenger sdk.Messenger
	botAPI    sdk.BotAPI
	aiTools   sdk.AIToolRegistrar
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	encoder   *json.Encoder
	exitCh    chan struct{}
	readyCh   chan readyPayload
	waitErr   error
	running   bool
	lastErr   string
	startedAt time.Time
	stoppedAt time.Time
	pid       int
	exitCode  *int

	stopRequested bool
	recentLogs    []sdk.RuntimeLog

	pendingAIToolMu sync.Mutex
	pendingAITools  map[string]chan aiToolResultPayload
	aiToolSeq       uint64
}

const maxRuntimeLogEntries = 64

func New(desc Descriptor) sdk.Plugin {
	return &Plugin{desc: desc}
}

func (p *Plugin) Manifest() sdk.Manifest {
	return p.desc.Manifest
}

func (p *Plugin) Start(ctx context.Context, env sdk.Env) error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return nil
	}

	cmd, extraEnv, err := newPluginCommand(p.desc.Manifest, p.desc.WorkDir)
	if err != nil {
		p.mu.Unlock()
		return err
	}
	cmd.Dir = p.desc.WorkDir
	cmd.Env = append(os.Environ(),
		"GOBOT_PLUGIN_ID="+p.desc.Manifest.ID,
		"GOBOT_PLUGIN_KIND="+KindExternalExec,
		"GOBOT_PLUGIN_PROTOCOL="+p.desc.Manifest.Protocol,
	)
	cmd.Env = append(cmd.Env, extraEnv...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		p.mu.Unlock()
		return fmt.Errorf("创建 stdin pipe 失败: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		p.mu.Unlock()
		_ = stdin.Close()
		return fmt.Errorf("创建 stdout pipe 失败: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		p.mu.Unlock()
		_ = stdin.Close()
		return fmt.Errorf("创建 stderr pipe 失败: %w", err)
	}
	if err := cmd.Start(); err != nil {
		p.mu.Unlock()
		_ = stdin.Close()
		return fmt.Errorf("启动外部插件进程失败: %w", err)
	}

	logger := env.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	p.logger = logger.With("plugin", p.desc.Manifest.ID, "kind", KindExternalExec)
	p.messenger = env.Messenger
	p.botAPI = env.BotAPI
	p.aiTools = env.AITools
	p.cmd = cmd
	p.stdin = stdin
	p.encoder = json.NewEncoder(stdin)
	p.exitCh = make(chan struct{})
	p.readyCh = make(chan readyPayload, 1)
	p.waitErr = nil
	p.running = true
	p.lastErr = ""
	p.startedAt = time.Now()
	p.stoppedAt = time.Time{}
	p.pid = cmd.Process.Pid
	p.exitCode = nil
	p.stopRequested = false
	p.recentLogs = nil
	p.pendingAITools = make(map[string]chan aiToolResultPayload)
	p.mu.Unlock()

	p.recordRuntimeLog("info", "lifecycle", fmt.Sprintf("external_exec 进程已启动 pid=%d", cmd.Process.Pid))

	go p.readStdout(stdout)
	go p.readStderr(stderr)
	go p.waitProcess()

	config := map[string]any{}
	if env.Config != nil {
		config = env.Config.Raw()
	}
	catalog := []sdk.PluginInfo{}
	if env.PluginCatalog != nil {
		catalog = env.PluginCatalog.ListPlugins()
	}

	if err := p.send(hostMessage{
		Type: "start",
		Payload: startPayload{
			Plugin:  p.desc.Manifest,
			Config:  config,
			Catalog: catalog,
			App:     env.App,
		},
	}); err != nil {
		p.logError("发送 start 消息失败", err)
		_ = p.Stop(context.Background())
		return fmt.Errorf("发送 start 消息失败: %w", err)
	}

	return p.waitReady(ctx)
}

func newPluginCommand(manifest sdk.Manifest, workDir string) (*exec.Cmd, []string, error) {
	runtimeKind := strings.ToLower(strings.TrimSpace(manifest.Runtime))
	if runtimeKind == RuntimePython {
		launcher, args, venvEnv, err := resolvePythonCommand(manifest, workDir, exec.LookPath)
		if err != nil {
			return nil, nil, err
		}
		commonPath, err := resolvePythonCommonPath(workDir)
		if err != nil {
			return nil, nil, err
		}
		env := []string{
			"PYTHONUTF8=1",
			"PYTHONIOENCODING=utf-8",
		}
		if commonPath != "" {
			env = append(env, "PYTHONPATH="+prependEnvPath(commonPath, os.Getenv("PYTHONPATH")))
		}
		env = append(env, venvEnv...)
		return exec.Command(launcher, args...), env, nil
	}
	return newExecutablePluginCommand(manifest.Entry, manifest.Args), nil, nil
}

func newExecutablePluginCommand(entry string, args []string) *exec.Cmd {
	lowerEntry := strings.ToLower(entry)
	if runtime.GOOS == "windows" && (strings.HasSuffix(lowerEntry, ".cmd") || strings.HasSuffix(lowerEntry, ".bat")) {
		shellArgs := append([]string{"/c", entry}, args...)
		return exec.Command("cmd", shellArgs...)
	}
	if runtime.GOOS != "windows" && strings.HasSuffix(lowerEntry, ".sh") {
		shellArgs := append([]string{entry}, args...)
		return exec.Command("/bin/sh", shellArgs...)
	}
	return exec.Command(entry, args...)
}

func resolvePythonCommand(manifest sdk.Manifest, workDir string, lookPath func(string) (string, error)) (string, []string, []string, error) {
	if venv, ok, err := resolvePythonVenv(manifest, workDir); err != nil {
		return "", nil, nil, err
	} else if ok {
		args := append([]string{"-X", "utf8", manifest.Entry}, manifest.Args...)
		env := []string{
			"VIRTUAL_ENV=" + venv.Root,
			"PATH=" + prependEnvPath(venv.BinDir, os.Getenv("PATH")),
		}
		return venv.Python, args, env, nil
	}

	if launcher, err := lookPath("uv"); err == nil {
		return launcher, append([]string{"run", "python", "-X", "utf8", manifest.Entry}, manifest.Args...), nil, nil
	}
	if runtime.GOOS == "windows" {
		if launcher, err := lookPath("py"); err == nil {
			return launcher, append([]string{"-3", "-X", "utf8", manifest.Entry}, manifest.Args...), nil, nil
		}
	}
	if launcher, err := lookPath("python3"); err == nil {
		return launcher, append([]string{"-X", "utf8", manifest.Entry}, manifest.Args...), nil, nil
	}
	if launcher, err := lookPath("python"); err == nil {
		return launcher, append([]string{"-X", "utf8", manifest.Entry}, manifest.Args...), nil, nil
	}
	return "", nil, nil, fmt.Errorf("未找到可用的 Python 解释器（尝试过 uv、python3、python%s）", map[bool]string{true: "、py", false: ""}[runtime.GOOS == "windows"])
}

type pythonVenv struct {
	Root   string
	BinDir string
	Python string
}

func resolvePythonVenv(manifest sdk.Manifest, workDir string) (pythonVenv, bool, error) {
	root := strings.TrimSpace(manifest.PythonEnv)
	if root == "" {
		return pythonVenv{}, false, nil
	}
	if !filepath.IsAbs(root) {
		root = filepath.Join(workDir, root)
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	info, err := os.Stat(root)
	switch {
	case err == nil:
		if !info.IsDir() {
			return pythonVenv{}, false, fmt.Errorf("Python 依赖环境不是目录: %s", root)
		}
	case os.IsNotExist(err):
		return pythonVenv{}, false, nil
	default:
		return pythonVenv{}, false, err
	}

	binDir := filepath.Join(root, "bin")
	pythonPath := filepath.Join(binDir, "python")
	if runtime.GOOS == "windows" {
		binDir = filepath.Join(root, "Scripts")
		pythonPath = filepath.Join(binDir, "python.exe")
	}
	info, err = os.Stat(pythonPath)
	switch {
	case err == nil:
		if info.IsDir() {
			return pythonVenv{}, false, fmt.Errorf("Python 依赖环境解释器不是文件: %s", pythonPath)
		}
		return pythonVenv{Root: root, BinDir: binDir, Python: pythonPath}, true, nil
	case os.IsNotExist(err):
		return pythonVenv{}, false, nil
	default:
		return pythonVenv{}, false, err
	}
}

func resolvePythonCommonPath(workDir string) (string, error) {
	candidates := pythonCommonPathCandidates(workDir)
	incomplete := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			commonDir := filepath.Dir(candidate)
			complete, err := HasCompletePythonCommonDir(commonDir)
			if err != nil {
				return "", err
			}
			if complete {
				return commonDir, nil
			}
			incomplete = append(incomplete, commonDir)
		}
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	if len(incomplete) > 0 {
		return "", fmt.Errorf("未找到完整的 Python 插件运行时目录；以下目录仅包含部分文件: %s；需要同时包含 gobot_runtime.py 与 gobot_plugin/runtime.py", strings.Join(dedupeCleanPaths(incomplete), ", "))
	}
	return "", fmt.Errorf("未找到 Python 插件运行时 gobot_runtime.py，请确认宿主 plugins/_common 存在，或通过 GOBOT_PLUGIN_COMMON_DIR 指定运行时目录")
}

var requiredPythonCommonFiles = []string{
	"gobot_runtime.py",
	filepath.Join("gobot_plugin", "__init__.py"),
	filepath.Join("gobot_plugin", "models.py"),
	filepath.Join("gobot_plugin", "runtime.py"),
}

func HasCompletePythonCommonDir(commonDir string) (bool, error) {
	info, err := os.Stat(commonDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("Python 插件运行时目录不是目录: %s", commonDir)
	}
	for _, name := range requiredPythonCommonFiles {
		itemInfo, err := os.Stat(filepath.Join(commonDir, name))
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
		if itemInfo.IsDir() {
			return false, fmt.Errorf("Python 插件运行时文件不是普通文件: %s", filepath.Join(commonDir, name))
		}
	}
	return true, nil
}

func pythonCommonPathCandidates(workDir string) []string {
	dirs := make([]string, 0, 16)
	if override := strings.TrimSpace(os.Getenv("GOBOT_PLUGIN_COMMON_DIR")); override != "" {
		dirs = append(dirs, override)
	}
	dirs = append(dirs,
		filepath.Join(workDir, "_common"),
		filepath.Join(workDir, "..", "_common"),
	)

	for _, ancestor := range pathAncestors(workDir) {
		dirs = append(dirs,
			filepath.Join(ancestor, "_common"),
			filepath.Join(ancestor, "plugins", "_common"),
		)
	}

	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs,
			filepath.Join(cwd, "_common"),
			filepath.Join(cwd, "plugins", "_common"),
		)
	}
	if executable, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executable)
		dirs = append(dirs,
			filepath.Join(executableDir, "_common"),
			filepath.Join(executableDir, "plugins", "_common"),
		)
	}

	return runtimeFileCandidates(dedupeCleanPaths(dirs))
}

func pathAncestors(value string) []string {
	if abs, err := filepath.Abs(value); err == nil {
		value = abs
	}
	value = filepath.Clean(value)
	out := make([]string, 0, 8)
	for {
		parent := filepath.Dir(value)
		if parent == value {
			break
		}
		out = append(out, parent)
		value = parent
	}
	return out
}

func runtimeFileCandidates(dirs []string) []string {
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		out = append(out, filepath.Join(dir, "gobot_runtime.py"))
	}
	return out
}

func dedupeCleanPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, item := range paths {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		cleaned := filepath.Clean(item)
		key := cleaned
		if abs, err := filepath.Abs(cleaned); err == nil {
			key = abs
		}
		key = strings.ToLower(key)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}

func prependEnvPath(value, existing string) string {
	value = strings.TrimSpace(value)
	existing = strings.TrimSpace(existing)
	if value == "" {
		return existing
	}
	if existing == "" {
		return value
	}
	return value + string(os.PathListSeparator) + existing
}

func (p *Plugin) HandleEvent(_ context.Context, evt event.Event) error {
	return p.send(hostMessage{
		Type:    "event",
		Payload: eventPayload{Event: evt},
	})
}

func (p *Plugin) Stop(ctx context.Context) error {
	p.mu.Lock()
	if p.exitCh == nil {
		p.mu.Unlock()
		return nil
	}
	cmd := p.cmd
	stdin := p.stdin
	exitCh := p.exitCh
	p.stopRequested = true
	p.mu.Unlock()

	p.recordRuntimeLog("info", "lifecycle", "收到停止请求，准备关闭 external_exec 进程")

	_ = p.send(hostMessage{Type: "stop"})
	if stdin != nil {
		_ = stdin.Close()
	}

	if ctx == nil {
		ctx = context.Background()
	}
	stopCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	select {
	case <-exitCh:
		p.mu.Lock()
		err := p.waitErr
		p.mu.Unlock()
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "killed") {
			return nil
		}
		return err
	case <-stopCtx.Done():
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case <-exitCh:
				p.mu.Lock()
				err := p.waitErr
				p.mu.Unlock()
				if err != nil && strings.Contains(strings.ToLower(err.Error()), "killed") {
					return nil
				}
				return err
			case <-time.After(500 * time.Millisecond):
			}
			_ = cmd.Process.Kill()
		}
		p.recordRuntimeLog("warn", "lifecycle", "停止 external_exec 进程超时，已执行强制终止")
		<-exitCh
		return stopCtx.Err()
	}
}

func (p *Plugin) send(msg hostMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running || p.encoder == nil {
		if p.lastErr != "" {
			return fmt.Errorf("外部插件未运行: %s", p.lastErr)
		}
		return fmt.Errorf("外部插件未运行")
	}
	if err := p.encoder.Encode(msg); err != nil {
		p.lastErr = err.Error()
		p.running = false
		return fmt.Errorf("向外部插件发送消息失败: %w", err)
	}
	return nil
}

func (p *Plugin) waitProcess() {
	cmd := p.cmd
	err := cmd.Wait()
	processState := cmd.ProcessState
	now := time.Now()

	p.mu.Lock()
	defer p.mu.Unlock()
	p.waitErr = err
	p.running = false
	p.stoppedAt = now
	p.pid = 0
	if processState != nil {
		exitCode := processState.ExitCode()
		p.exitCode = &exitCode
	} else {
		p.exitCode = nil
	}
	switch {
	case p.stopRequested:
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "killed") {
			p.lastErr = err.Error()
		}
	case err != nil:
		p.lastErr = fmt.Sprintf("external_exec 进程退出: %v", err)
	default:
		p.lastErr = "external_exec 进程意外退出"
	}
	exitCh := p.exitCh
	p.stdin = nil
	p.encoder = nil
	p.cmd = nil
	p.exitCh = nil
	p.readyCh = nil
	p.aiTools = nil
	if p.lastErr != "" {
		p.appendRuntimeLogLocked("error", "lifecycle", p.lastErr, now)
	} else {
		p.appendRuntimeLogLocked("info", "lifecycle", "external_exec 进程已退出", now)
	}
	exitMessage := strings.TrimSpace(p.lastErr)
	if exitMessage == "" {
		if err != nil {
			exitMessage = err.Error()
		} else {
			exitMessage = "external_exec 进程已退出"
		}
	}
	p.mu.Unlock()
	p.failPendingAIToolCalls(errors.New(exitMessage))
	p.mu.Lock()
	if exitCh != nil {
		close(exitCh)
	}
}

func (p *Plugin) readStdout(stdout io.Reader) {
	decoder := json.NewDecoder(stdout)
	for {
		var msg pluginMessage
		if err := decoder.Decode(&msg); err != nil {
			if err != io.EOF {
				p.logError("读取外部插件 stdout 失败", err)
			}
			return
		}
		p.handlePluginMessage(msg)
	}
}

func (p *Plugin) readStderr(stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		p.recordRuntimeLog("warn", "stderr", line)
		p.logWarn("外部插件 stderr", "line", line)
	}
	if err := scanner.Err(); err != nil {
		p.logError("读取外部插件 stderr 失败", err)
	}
}

func (p *Plugin) handlePluginMessage(msg pluginMessage) {
	switch msg.Type {
	case "ready":
		var payload readyPayload
		_ = json.Unmarshal(msg.Payload, &payload)
		p.signalReady(payload)
	case "log":
		var payload logPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			p.logError("解析外部插件 log 消息失败", err)
			return
		}
		p.logByLevel(payload.Level, payload.Message)
	case "send_text":
		var payload sendTextPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			p.logError("解析 send_text 消息失败", err)
			return
		}
		if p.messenger == nil {
			p.logWarn("当前未配置 Messenger，忽略 send_text")
			return
		}
		if err := p.messenger.SendText(context.Background(), payload.Target, payload.Text); err != nil {
			p.logError("执行 send_text 失败", err)
		}
	case "reply_text":
		var payload replyTextPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			p.logError("解析 reply_text 消息失败", err)
			return
		}
		if p.messenger == nil {
			p.logWarn("当前未配置 Messenger，忽略 reply_text")
			return
		}
		if err := p.messenger.ReplyText(context.Background(), payload.Target, payload.ReplyTo, payload.Text); err != nil {
			p.logError("执行 reply_text 失败", err)
		}
	case "send_segments":
		var payload sendSegmentsPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			p.logError("解析 send_segments 消息失败", err)
			return
		}
		if p.messenger == nil {
			p.logWarn("当前未配置 Messenger，忽略 send_segments")
			return
		}
		if err := p.messenger.SendSegments(context.Background(), payload.Target, payload.Segments); err != nil {
			p.logError("执行 send_segments 失败", err)
		}
	case "call":
		var payload callPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			p.logError("解析 call 消息失败", err)
			return
		}
		p.handleCall(payload)
	case "ai_tools_register":
		var payload registerAIToolsPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			p.logError("解析 ai_tools_register 消息失败", err)
			return
		}
		p.handleAIToolsRegister(payload)
	case "ai_tools_unregister":
		var payload unregisterAIToolsPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			p.logError("解析 ai_tools_unregister 消息失败", err)
			return
		}
		p.handleAIToolsUnregister(payload)
	case "ai_tool_result":
		var payload aiToolResultPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			p.logError("解析 ai_tool_result 消息失败", err)
			return
		}
		p.handleAIToolResult(payload)
	default:
		p.logWarn("收到未知的外部插件消息", "type", msg.Type)
	}
}

func (p *Plugin) waitReady(ctx context.Context) error {
	p.mu.Lock()
	readyCh := p.readyCh
	exitCh := p.exitCh
	p.mu.Unlock()

	if readyCh == nil || exitCh == nil {
		return fmt.Errorf("外部插件启动状态异常")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	select {
	case payload := <-readyCh:
		p.recordRuntimeLog("info", "lifecycle", "external_exec 插件已就绪")
		if strings.TrimSpace(payload.Message) == "" {
			p.logInfo("外部插件已就绪")
		} else {
			p.logInfo("外部插件已就绪", "message", payload.Message)
		}
		return nil
	case <-exitCh:
		p.mu.Lock()
		err := p.waitErr
		lastErr := strings.TrimSpace(p.lastErr)
		logSummary := p.recentFailureLogSummaryLocked(4)
		p.mu.Unlock()
		if err != nil {
			if logSummary != "" {
				return fmt.Errorf("外部插件在 ready 前退出: %w; %s", err, logSummary)
			}
			if lastErr != "" && lastErr != err.Error() {
				return fmt.Errorf("外部插件在 ready 前退出: %w; %s", err, lastErr)
			}
			return fmt.Errorf("外部插件在 ready 前退出: %w", err)
		}
		if lastErr != "" {
			if logSummary != "" && !strings.Contains(logSummary, lastErr) {
				return fmt.Errorf("外部插件在 ready 前退出: %s; %s", lastErr, logSummary)
			}
			return fmt.Errorf("外部插件在 ready 前退出: %s", lastErr)
		}
		if logSummary != "" {
			return fmt.Errorf("外部插件在 ready 前退出: %s", logSummary)
		}
		return fmt.Errorf("外部插件在 ready 前退出")
	case <-waitCtx.Done():
		_ = p.Stop(context.Background())
		return fmt.Errorf("等待外部插件 ready 超时: %w", waitCtx.Err())
	}
}

func (p *Plugin) recentFailureLogSummaryLocked(limit int) string {
	if limit <= 0 || len(p.recentLogs) == 0 {
		return ""
	}
	entries := make([]string, 0, limit)
	for i := len(p.recentLogs) - 1; i >= 0 && len(entries) < limit; i-- {
		item := p.recentLogs[i]
		message := strings.TrimSpace(item.Message)
		if message == "" {
			continue
		}
		level := strings.ToLower(strings.TrimSpace(item.Level))
		source := strings.TrimSpace(item.Source)
		if level != "error" && level != "warn" && source != "stderr" {
			continue
		}
		prefix := strings.Trim(strings.Join([]string{source, level}, "/"), "/")
		if prefix != "" {
			message = prefix + ": " + message
		}
		entries = append(entries, message)
	}
	if len(entries) == 0 {
		return ""
	}
	for left, right := 0, len(entries)-1; left < right; left, right = left+1, right-1 {
		entries[left], entries[right] = entries[right], entries[left]
	}
	return "recent logs: " + strings.Join(entries, " | ")
}

func (p *Plugin) signalReady(payload readyPayload) {
	p.mu.Lock()
	readyCh := p.readyCh
	p.mu.Unlock()
	if readyCh == nil {
		return
	}
	select {
	case readyCh <- payload:
	default:
	}
}

func (p *Plugin) handleCall(payload callPayload) {
	var (
		result any
		err    error
	)

	switch payload.Method {
	case CallBotGetStrangerInfo:
		var req getStrangerInfoPayload
		if err = json.Unmarshal(payload.Payload, &req); err == nil {
			if p.botAPI == nil {
				err = fmt.Errorf("当前未配置 BotAPI")
			} else {
				result, err = p.botAPI.GetStrangerInfo(context.Background(), req.ConnectionID, req.UserID)
			}
		}
	case CallBotGetGroupInfo:
		var req getGroupInfoPayload
		if err = json.Unmarshal(payload.Payload, &req); err == nil {
			if p.botAPI == nil {
				err = fmt.Errorf("当前未配置 BotAPI")
			} else {
				result, err = p.botAPI.GetGroupInfo(context.Background(), req.ConnectionID, req.GroupID)
			}
		}
	case CallBotGetGroupMembers:
		var req getGroupMemberListPayload
		if err = json.Unmarshal(payload.Payload, &req); err == nil {
			if p.botAPI == nil {
				err = fmt.Errorf("当前未配置 BotAPI")
			} else {
				result, err = p.botAPI.GetGroupMemberList(context.Background(), req.ConnectionID, req.GroupID)
			}
		}
	case CallBotGetGroupMember:
		var req getGroupMemberInfoPayload
		if err = json.Unmarshal(payload.Payload, &req); err == nil {
			if p.botAPI == nil {
				err = fmt.Errorf("当前未配置 BotAPI")
			} else {
				result, err = p.botAPI.GetGroupMemberInfo(context.Background(), req.ConnectionID, req.GroupID, req.UserID)
			}
		}
	case CallBotGetMessage:
		var req getMessagePayload
		if err = json.Unmarshal(payload.Payload, &req); err == nil {
			if p.botAPI == nil {
				err = fmt.Errorf("当前未配置 BotAPI")
			} else {
				result, err = p.botAPI.GetMessage(context.Background(), req.ConnectionID, req.MessageID)
			}
		}
	case CallBotGetForwardMessage:
		var req getForwardMessagePayload
		if err = json.Unmarshal(payload.Payload, &req); err == nil {
			if p.botAPI == nil {
				err = fmt.Errorf("当前未配置 BotAPI")
			} else {
				result, err = p.botAPI.GetForwardMessage(context.Background(), req.ConnectionID, req.ForwardID)
			}
		}
	case CallBotDeleteMessage:
		var req deleteMessagePayload
		if err = json.Unmarshal(payload.Payload, &req); err == nil {
			if p.botAPI == nil {
				err = fmt.Errorf("当前未配置 BotAPI")
			} else {
				err = p.botAPI.DeleteMessage(context.Background(), req.ConnectionID, req.MessageID)
			}
		}
	case CallBotResolveMedia:
		var req resolveMediaPayload
		if err = json.Unmarshal(payload.Payload, &req); err == nil {
			if p.botAPI == nil {
				err = fmt.Errorf("当前未配置 BotAPI")
			} else {
				result, err = p.botAPI.ResolveMedia(context.Background(), req.ConnectionID, req.SegmentType, req.File)
			}
		}
	case CallBotGetLoginInfo:
		var req getLoginInfoPayload
		if err = json.Unmarshal(payload.Payload, &req); err == nil {
			if p.botAPI == nil {
				err = fmt.Errorf("当前未配置 BotAPI")
			} else {
				result, err = p.botAPI.GetLoginInfo(context.Background(), req.ConnectionID)
			}
		}
	case CallBotGetStatus:
		var req getStatusPayload
		if err = json.Unmarshal(payload.Payload, &req); err == nil {
			if p.botAPI == nil {
				err = fmt.Errorf("当前未配置 BotAPI")
			} else {
				result, err = p.botAPI.GetStatus(context.Background(), req.ConnectionID)
			}
		}
	case CallBotSendGroupForward:
		var req sendGroupForwardPayload
		if err = json.Unmarshal(payload.Payload, &req); err == nil {
			if p.botAPI == nil {
				err = fmt.Errorf("当前未配置 BotAPI")
			} else {
				err = p.botAPI.SendGroupForward(context.Background(), req.ConnectionID, req.GroupID, req.Nodes, req.Options)
			}
		}
	default:
		err = fmt.Errorf("未知调用方法: %s", payload.Method)
	}

	if sendErr := p.sendResponse(payload.ID, result, err); sendErr != nil {
		p.logError("发送 call 响应失败", sendErr, "method", payload.Method, "call_id", payload.ID)
	}
}

func (p *Plugin) sendResponse(id string, result any, err error) error {
	response := hostResponsePayload{ID: id}
	if err != nil {
		response.Error = err.Error()
	} else {
		raw, marshalErr := marshalPayload(result)
		if marshalErr != nil {
			response.Error = marshalErr.Error()
		} else {
			response.Result = raw
		}
	}
	return p.send(hostMessage{Type: "response", Payload: response})
}

func (p *Plugin) handleAIToolsRegister(payload registerAIToolsPayload) {
	var err error
	if p.aiTools == nil {
		err = fmt.Errorf("当前未配置 AITools")
	} else {
		tools := make([]sdk.AIToolDefinition, 0, len(payload.Tools))
		for _, item := range payload.Tools {
			name := strings.TrimSpace(item.Name)
			if name == "" {
				err = fmt.Errorf("AI tool name 不能为空")
				break
			}
			schema := cloneAnyMap(item.InputSchema)
			toolName := name
			tools = append(tools, sdk.AIToolDefinition{
				Name:        toolName,
				Description: strings.TrimSpace(item.Description),
				InputSchema: schema,
				Handle: func(ctx context.Context, toolCtx sdk.AIToolContext, args json.RawMessage) (any, error) {
					return p.callRemoteAITool(ctx, toolName, toolCtx, args)
				},
			})
		}
		if err == nil {
			err = p.aiTools.RegisterTools(payload.Namespace, tools)
		}
	}
	if payload.ID != "" {
		if sendErr := p.sendResponse(payload.ID, nil, err); sendErr != nil {
			p.logError("发送 ai_tools_register 响应失败", sendErr, "namespace", payload.Namespace)
		}
	}
	if err != nil {
		p.logError("注册外部 AI tools 失败", err, "namespace", payload.Namespace)
	}
}

func (p *Plugin) handleAIToolsUnregister(payload unregisterAIToolsPayload) {
	var err error
	if p.aiTools == nil {
		err = fmt.Errorf("当前未配置 AITools")
	} else {
		p.aiTools.UnregisterTools(payload.Namespace)
	}
	if payload.ID != "" {
		if sendErr := p.sendResponse(payload.ID, nil, err); sendErr != nil {
			p.logError("发送 ai_tools_unregister 响应失败", sendErr, "namespace", payload.Namespace)
		}
	}
	if err != nil {
		p.logError("注销外部 AI tools 失败", err, "namespace", payload.Namespace)
	}
}

func (p *Plugin) handleAIToolResult(payload aiToolResultPayload) {
	p.pendingAIToolMu.Lock()
	waitCh := p.pendingAITools[payload.ID]
	p.pendingAIToolMu.Unlock()
	if waitCh == nil {
		return
	}
	select {
	case waitCh <- payload:
	default:
	}
}

func (p *Plugin) callRemoteAITool(ctx context.Context, toolName string, toolCtx sdk.AIToolContext, args json.RawMessage) (any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	callID := strconv.FormatUint(atomic.AddUint64(&p.aiToolSeq, 1), 10)
	waitCh := make(chan aiToolResultPayload, 1)

	p.pendingAIToolMu.Lock()
	if p.pendingAITools == nil {
		p.pendingAITools = make(map[string]chan aiToolResultPayload)
	}
	p.pendingAITools[callID] = waitCh
	p.pendingAIToolMu.Unlock()
	defer func() {
		p.pendingAIToolMu.Lock()
		delete(p.pendingAITools, callID)
		p.pendingAIToolMu.Unlock()
	}()

	exitCh := p.currentExitCh()
	if err := p.send(hostMessage{
		Type: "ai_tool_call",
		Payload: aiToolCallPayload{
			ID:        callID,
			ToolName:  toolName,
			Arguments: args,
			Context: aiToolContextPayload{
				Event:   toolCtx.Event(),
				Target:  toolCtx.Target(),
				ReplyTo: toolCtx.ReplyTo(),
			},
		},
	}); err != nil {
		return nil, err
	}

	select {
	case resp := <-waitCh:
		if resp.Error != "" {
			return nil, errors.New(resp.Error)
		}
		if resp.Scheduled != nil {
			if err := toolCtx.ScheduleCurrentSend(resp.Scheduled.Text, resp.Scheduled.Reply); err != nil {
				return nil, err
			}
		}
		if len(resp.Result) == 0 {
			return nil, nil
		}
		var result any
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return nil, fmt.Errorf("解析远程 AI tool 响应失败: %w", err)
		}
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-exitCh:
		message := p.currentLastError()
		if message == "" {
			message = "外部插件已退出"
		}
		return nil, errors.New(message)
	}
}

func (p *Plugin) currentExitCh() chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitCh
}

func (p *Plugin) currentLastError() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.TrimSpace(p.lastErr)
}

func (p *Plugin) failPendingAIToolCalls(err error) {
	if err == nil {
		return
	}
	pending := make([]chan aiToolResultPayload, 0)
	p.pendingAIToolMu.Lock()
	for id, waitCh := range p.pendingAITools {
		pending = append(pending, waitCh)
		delete(p.pendingAITools, id)
	}
	p.pendingAIToolMu.Unlock()
	for _, waitCh := range pending {
		select {
		case waitCh <- aiToolResultPayload{Error: err.Error()}:
		default:
		}
	}
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func (p *Plugin) logByLevel(level, message string) {
	level = strings.ToLower(strings.TrimSpace(level))
	p.recordRuntimeLog(level, "plugin", message)
	switch level {
	case "debug":
		p.logDebug(message)
	case "warn", "warning":
		p.logWarn(message)
	case "error":
		p.logError(message, nil)
	default:
		p.logInfo(message)
	}
}

func (p *Plugin) logDebug(message string, args ...any) {
	p.withLogger(func(logger *slog.Logger) { logger.Debug(message, args...) })
}

func (p *Plugin) logInfo(message string, args ...any) {
	p.withLogger(func(logger *slog.Logger) { logger.Info(message, args...) })
}

func (p *Plugin) logWarn(message string, args ...any) {
	p.withLogger(func(logger *slog.Logger) { logger.Warn(message, args...) })
}

func (p *Plugin) logError(message string, err error, args ...any) {
	if err != nil {
		args = append(args, "error", err)
	}
	p.withLogger(func(logger *slog.Logger) { logger.Error(message, args...) })
}

func (p *Plugin) withLogger(fn func(*slog.Logger)) {
	p.mu.Lock()
	logger := p.logger
	p.mu.Unlock()
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	fn(logger)
}

func (p *Plugin) RuntimeStatus() sdk.RuntimeStatus {
	p.mu.Lock()
	defer p.mu.Unlock()

	status := sdk.RuntimeStatus{
		Running:   p.running,
		PID:       p.pid,
		StartedAt: p.startedAt,
		StoppedAt: p.stoppedAt,
		LastError: p.lastErr,
	}
	if p.exitCode != nil {
		exitCode := *p.exitCode
		status.ExitCode = &exitCode
	}
	if len(p.recentLogs) > 0 {
		status.RecentLogs = append([]sdk.RuntimeLog(nil), p.recentLogs...)
	}
	return status
}

func (p *Plugin) recordRuntimeLog(level, source, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.appendRuntimeLogLocked(level, source, message, time.Now())
}

func (p *Plugin) appendRuntimeLogLocked(level, source, message string, at time.Time) {
	p.recentLogs = append(p.recentLogs, sdk.RuntimeLog{
		At:      at,
		Level:   strings.TrimSpace(level),
		Source:  strings.TrimSpace(source),
		Message: message,
	})
	if len(p.recentLogs) > maxRuntimeLogEntries {
		p.recentLogs = append([]sdk.RuntimeLog(nil), p.recentLogs[len(p.recentLogs)-maxRuntimeLogEntries:]...)
	}
}
