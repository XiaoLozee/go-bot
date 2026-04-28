package externalexec

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed all:python_common
var embeddedPythonCommonFS embed.FS

const embeddedPythonCommonRoot = "python_common"

func EnsureEmbeddedPythonCommonRuntime(commonDir string) error {
	commonDir = strings.TrimSpace(commonDir)
	if commonDir == "" {
		return fmt.Errorf("Python 插件运行时目录为空")
	}
	complete, err := HasCompletePythonCommonDir(commonDir)
	if err != nil {
		return err
	}
	if complete {
		return nil
	}
	return writeEmbeddedPythonCommonRuntime(commonDir)
}

func writeEmbeddedPythonCommonRuntime(commonDir string) error {
	return fs.WalkDir(embeddedPythonCommonFS, embeddedPythonCommonRoot, func(sourcePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(embeddedPythonCommonRoot, filepath.FromSlash(sourcePath))
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(commonDir, 0o755)
		}

		targetPath := filepath.Join(commonDir, rel)
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("内置 Python 插件运行时包含不支持的文件类型: %s", sourcePath)
		}

		payload, err := embeddedPythonCommonFS.ReadFile(sourcePath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		return os.WriteFile(targetPath, payload, mode)
	})
}
