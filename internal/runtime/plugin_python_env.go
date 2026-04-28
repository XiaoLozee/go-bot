package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/XiaoLozee/go-bot/internal/plugin/externalexec"
)

const pythonRequirementsFile = "requirements.txt"

var (
	pythonEnvLookPath   = exec.LookPath
	pythonEnvRunCommand = runPythonEnvCommand
)

type pythonEnvCommandRunner func(ctx context.Context, workDir string, name string, args ...string) ([]byte, error)

func (s *Service) preparePythonPluginEnv(ctx context.Context, desc externalexec.Descriptor) (string, bool, error) {
	if !strings.EqualFold(desc.Manifest.Runtime, externalexec.RuntimePython) {
		return "", false, nil
	}

	requirementsPath := filepath.Join(desc.WorkDir, pythonRequirementsFile)
	hasRequirements, err := requirementsFileHasEntries(requirementsPath)
	if err != nil {
		return "", false, fmt.Errorf("检查插件依赖文件失败: %w", err)
	}
	if !hasRequirements {
		return "", false, nil
	}

	envRoot := strings.TrimSpace(s.pluginEnvRoot)
	if envRoot == "" {
		return "", false, fmt.Errorf("未配置插件依赖环境目录")
	}
	if err := os.MkdirAll(envRoot, 0o755); err != nil {
		return "", false, fmt.Errorf("创建插件依赖环境根目录失败: %w", err)
	}

	finalEnvDir := s.pythonPluginEnvDir(desc.Manifest.ID)
	if err := ensurePathWithinRoot(envRoot, finalEnvDir); err != nil {
		return "", false, err
	}

	stageEnvDir, err := os.MkdirTemp(envRoot, desc.Manifest.ID+"-env-*")
	if err != nil {
		return "", false, fmt.Errorf("创建插件依赖环境暂存目录失败: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(stageEnvDir)
		}
	}()

	if err := createPythonVenv(ctx, desc.WorkDir, stageEnvDir); err != nil {
		return "", false, err
	}
	if err := installPythonRequirements(ctx, desc.WorkDir, stageEnvDir, requirementsPath); err != nil {
		return "", false, err
	}
	if err := replacePythonPluginEnv(finalEnvDir, stageEnvDir); err != nil {
		return "", false, err
	}
	committed = true
	return finalEnvDir, true, nil
}

func (s *Service) pythonPluginEnvDir(pluginID string) string {
	return filepath.Join(s.pluginEnvRoot, pluginID)
}

func (s *Service) removePythonPluginEnv(pluginID string) error {
	envDir := s.pythonPluginEnvDir(pluginID)
	if err := ensurePathWithinRoot(s.pluginEnvRoot, envDir); err != nil {
		return err
	}
	return os.RemoveAll(envDir)
}

func createPythonVenv(ctx context.Context, workDir string, envDir string) error {
	if uv, err := pythonEnvLookPath("uv"); err == nil {
		output, runErr := pythonEnvRunCommand(ctx, workDir, uv, "venv", envDir)
		if runErr != nil {
			return fmt.Errorf("uv 创建插件依赖环境失败: %w%s", runErr, commandOutputSuffix(output))
		}
		return nil
	}

	python, args, err := resolveBasePythonCommand()
	if err != nil {
		return err
	}
	args = append(args, "-m", "venv", envDir)
	output, runErr := pythonEnvRunCommand(ctx, workDir, python, args...)
	if runErr != nil {
		return fmt.Errorf("创建插件依赖环境失败: %w%s", runErr, commandOutputSuffix(output))
	}
	return nil
}

func installPythonRequirements(ctx context.Context, workDir string, envDir string, requirementsPath string) error {
	venvPython := pythonExecutableInVenv(envDir)
	if uv, err := pythonEnvLookPath("uv"); err == nil {
		output, runErr := pythonEnvRunCommand(ctx, workDir, uv, "pip", "install", "--python", venvPython, "-r", requirementsPath)
		if runErr != nil {
			return fmt.Errorf("uv 安装插件依赖失败: %w%s", runErr, commandOutputSuffix(output))
		}
		return nil
	}

	if output, err := pythonEnvRunCommand(ctx, workDir, venvPython, "-m", "ensurepip", "--upgrade"); err != nil {
		return fmt.Errorf("初始化插件依赖环境 pip 失败: %w%s", err, commandOutputSuffix(output))
	}
	output, err := pythonEnvRunCommand(ctx, workDir, venvPython, "-m", "pip", "install", "-r", requirementsPath)
	if err != nil {
		return fmt.Errorf("安装插件依赖失败: %w%s", err, commandOutputSuffix(output))
	}
	return nil
}

func resolveBasePythonCommand() (string, []string, error) {
	if runtime.GOOS == "windows" {
		if launcher, err := pythonEnvLookPath("py"); err == nil {
			return launcher, []string{"-3"}, nil
		}
	}
	if launcher, err := pythonEnvLookPath("python3"); err == nil {
		return launcher, nil, nil
	}
	if launcher, err := pythonEnvLookPath("python"); err == nil {
		return launcher, nil, nil
	}
	return "", nil, fmt.Errorf("未找到可用的 Python 解释器（尝试过 python3、python%s）", map[bool]string{true: "、py", false: ""}[runtime.GOOS == "windows"])
}

func pythonExecutableInVenv(envDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(envDir, "Scripts", "python.exe")
	}
	return filepath.Join(envDir, "bin", "python")
}

func replacePythonPluginEnv(finalEnvDir string, stageEnvDir string) error {
	backupEnvDir := ""
	if exists, err := pathExists(finalEnvDir); err != nil {
		return fmt.Errorf("检查旧插件依赖环境失败: %w", err)
	} else if exists {
		backupEnvDir = finalEnvDir + ".bak-" + time.Now().Format("20060102-150405.000")
		if err := moveDir(finalEnvDir, backupEnvDir); err != nil {
			return fmt.Errorf("备份旧插件依赖环境失败: %w", err)
		}
	}

	if err := moveDir(stageEnvDir, finalEnvDir); err != nil {
		if backupEnvDir != "" {
			_ = restorePluginDirectory(finalEnvDir, backupEnvDir)
		}
		return fmt.Errorf("启用插件依赖环境失败: %w", err)
	}
	if backupEnvDir != "" {
		if err := os.RemoveAll(backupEnvDir); err != nil {
			return fmt.Errorf("清理旧插件依赖环境失败: %w", err)
		}
	}
	return nil
}

func runPythonEnvCommand(ctx context.Context, workDir string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(),
		"PYTHONUTF8=1",
		"PYTHONIOENCODING=utf-8",
	)
	return cmd.CombinedOutput()
}

func regularFileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		return !info.IsDir(), nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func requirementsFileHasEntries(path string) (bool, error) {
	exists, err := regularFileExists(path)
	if err != nil || !exists {
		return exists, err
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(payload), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		return true, nil
	}
	return false, nil
}

func ensurePathWithinRoot(root string, target string) error {
	resolvedRoot := root
	if abs, err := filepath.Abs(resolvedRoot); err == nil {
		resolvedRoot = abs
	}
	resolvedTarget := target
	if abs, err := filepath.Abs(resolvedTarget); err == nil {
		resolvedTarget = abs
	}
	if !pathWithinRoot(resolvedRoot, resolvedTarget) {
		return fmt.Errorf("插件依赖环境目录超出允许范围: %s", resolvedTarget)
	}
	return nil
}

func commandOutputSuffix(output []byte) string {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return ""
	}
	if len(text) > 4000 {
		text = text[len(text)-4000:]
	}
	return "；输出: " + text
}
