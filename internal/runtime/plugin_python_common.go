package runtime

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/XiaoLozee/go-bot/internal/plugin/externalexec"
)

func (s *Service) syncPythonPluginCommonRuntime(pluginDir string) error {
	pluginDir = strings.TrimSpace(pluginDir)
	if pluginDir == "" {
		return fmt.Errorf("Python 插件目录为空")
	}

	targetCommonDir := filepath.Join(pluginDir, "_common")
	targetComplete, err := externalexec.HasCompletePythonCommonDir(targetCommonDir)
	if err != nil {
		return err
	}
	if targetComplete {
		return nil
	}

	sourceCommonDir := filepath.Join(strings.TrimSpace(s.externalRoot), "_common")
	if err := externalexec.EnsureEmbeddedPythonCommonRuntime(sourceCommonDir); err != nil {
		return err
	}
	sourceComplete, err := externalexec.HasCompletePythonCommonDir(sourceCommonDir)
	if err != nil {
		return err
	}
	if !sourceComplete {
		targetExists, err := pathExists(targetCommonDir)
		if err != nil {
			return err
		}
		if targetExists {
			return fmt.Errorf("插件 _common 不完整，且宿主共享运行时目录不可用: plugin=%s source=%s", targetCommonDir, sourceCommonDir)
		}
		return nil
	}

	if err := copyDir(sourceCommonDir, targetCommonDir); err != nil {
		return fmt.Errorf("同步 Python 插件 _common 失败: %w", err)
	}
	return nil
}
