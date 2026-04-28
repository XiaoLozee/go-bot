package ai

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/XiaoLozee/go-bot/internal/config"
)

const (
	defaultCLITimeoutSeconds = 10
	defaultCLIMaxOutputBytes = 8192
	maxCLIArgCount           = 32
	maxCLICommandLength      = 1024
	maxCLIArgLength          = 2048
)

var blockedCLIShellNames = map[string]struct{}{
	"bash":           {},
	"bash.exe":       {},
	"cmd":            {},
	"cmd.exe":        {},
	"fish":           {},
	"fish.exe":       {},
	"powershell":     {},
	"powershell.exe": {},
	"pwsh":           {},
	"pwsh.exe":       {},
	"sh":             {},
	"sh.exe":         {},
	"zsh":            {},
	"zsh.exe":        {},
}

type cliCommandResult struct {
	Command   string   `json:"command"`
	Args      []string `json:"args,omitempty"`
	ExitCode  int      `json:"exit_code"`
	Stdout    string   `json:"stdout,omitempty"`
	Stderr    string   `json:"stderr,omitempty"`
	TimedOut  bool     `json:"timed_out,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
}

type cliCommandRunner interface {
	Run(ctx context.Context, name string, args []string, maxOutputBytes int) (cliCommandResult, error)
}

type execCLICommandRunner struct{}

type cappedOutputBuffer struct {
	mu        sync.Mutex
	limit     int
	buf       []byte
	truncated bool
}

func newCappedOutputBuffer(limit int) *cappedOutputBuffer {
	if limit <= 0 {
		limit = defaultCLIMaxOutputBytes
	}
	return &cappedOutputBuffer{limit: limit}
}

func (b *cappedOutputBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	remaining := b.limit - len(b.buf)
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		b.buf = append(b.buf, p[:remaining]...)
		b.truncated = true
		return len(p), nil
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *cappedOutputBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.ToValidUTF8(string(b.buf), "�")
}

func (b *cappedOutputBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

func (execCLICommandRunner) Run(ctx context.Context, name string, args []string, maxOutputBytes int) (cliCommandResult, error) {
	result := cliCommandResult{
		Command: name,
		Args:    append([]string(nil), args...),
	}
	stdout := newCappedOutputBuffer(maxOutputBytes)
	stderr := newCappedOutputBuffer(maxOutputBytes)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.Truncated = stdout.Truncated() || stderr.Truncated()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		result.ExitCode = -1
		result.TimedOut = true
		return result, nil
	}
	if err == nil {
		result.ExitCode = 0
		return result, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}

	return cliCommandResult{}, err
}

func (s *Service) cliConfigSnapshot() config.AICLIConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return config.NormalizeAICLIConfig(s.cfg.CLI)
}

func normalizeCLIExecutableName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	base := filepath.Base(trimmed)
	if runtime.GOOS == "windows" {
		base = strings.TrimSuffix(strings.ToLower(base), ".exe")
		return base
	}
	return base
}

func hasCLIPathSeparator(value string) bool {
	return strings.Contains(value, "/") || strings.Contains(value, `\`)
}

func isBlockedCLICommand(name string) bool {
	_, blocked := blockedCLIShellNames[normalizeCLIExecutableName(name)]
	return blocked
}

func matchesAllowedCLICommand(command string, allowed string) bool {
	command = strings.TrimSpace(command)
	allowed = strings.TrimSpace(allowed)
	if command == "" || allowed == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		if strings.EqualFold(command, allowed) {
			return true
		}
	} else if command == allowed {
		return true
	}
	if hasCLIPathSeparator(command) || hasCLIPathSeparator(allowed) {
		return false
	}
	return normalizeCLIExecutableName(command) == normalizeCLIExecutableName(allowed)
}

func isAllowedCLICommand(command string, allowed []string) bool {
	for _, item := range allowed {
		if matchesAllowedCLICommand(command, item) {
			return true
		}
	}
	return false
}

func normalizeCLIInvocation(command string, args []string) (string, []string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", nil, fmt.Errorf("command 不能为空")
	}
	if len(command) > maxCLICommandLength {
		return "", nil, fmt.Errorf("command 过长")
	}
	if isBlockedCLICommand(command) {
		return "", nil, fmt.Errorf("不允许调用 shell 解释器，请改为白名单中的具体可执行文件")
	}
	if len(args) > maxCLIArgCount {
		return "", nil, fmt.Errorf("args 数量不能超过 %d", maxCLIArgCount)
	}
	cleanArgs := make([]string, 0, len(args))
	for i, item := range args {
		if len(item) > maxCLIArgLength {
			return "", nil, fmt.Errorf("args[%d] 过长", i)
		}
		if strings.ContainsRune(item, '\x00') {
			return "", nil, fmt.Errorf("args[%d] 包含非法空字符", i)
		}
		cleanArgs = append(cleanArgs, item)
	}
	return command, cleanArgs, nil
}

func cliTimeoutDuration(timeoutSeconds int) time.Duration {
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultCLITimeoutSeconds
	}
	return time.Duration(timeoutSeconds) * time.Second
}
