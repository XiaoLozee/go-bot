package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type SaveResult struct {
	SourcePath string
	TargetPath string
	BackupPath string
	SavedAt    time.Time
}

func ResolveWritablePath(sourcePath string) string {
	if sourcePath == "" {
		return ""
	}

	dir := filepath.Dir(sourcePath)
	base := filepath.Base(sourcePath)
	if strings.Contains(base, ".example.") {
		base = strings.Replace(base, ".example.", ".", 1)
	}
	return filepath.Join(dir, base)
}

func Save(sourcePath string, cfg *Config) (SaveResult, error) {
	if cfg == nil {
		return SaveResult{}, fmt.Errorf("配置为空")
	}
	normalizedCfg, err := Clone(cfg)
	if err != nil {
		return SaveResult{}, err
	}
	NormalizeConfig(normalizedCfg)
	if err := Validate(normalizedCfg); err != nil {
		return SaveResult{}, err
	}

	targetPath := ResolveWritablePath(sourcePath)
	if targetPath == "" {
		return SaveResult{}, fmt.Errorf("未配置可写入的配置文件路径")
	}

	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return SaveResult{}, fmt.Errorf("解析配置文件路径失败: %w", err)
	}

	payload, err := marshalYAML(normalizedCfg)
	if err != nil {
		return SaveResult{}, err
	}

	if err := os.MkdirAll(filepath.Dir(absTarget), 0o755); err != nil {
		return SaveResult{}, fmt.Errorf("创建配置目录失败: %w", err)
	}

	fileMode := os.FileMode(0o644)
	if info, statErr := os.Stat(absTarget); statErr == nil {
		fileMode = info.Mode().Perm()
	} else if !os.IsNotExist(statErr) {
		return SaveResult{}, fmt.Errorf("读取配置文件状态失败: %w", statErr)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(absTarget), filepath.Base(absTarget)+".tmp-*")
	if err != nil {
		return SaveResult{}, fmt.Errorf("创建临时配置文件失败: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(payload); err != nil {
		_ = tmpFile.Close()
		return SaveResult{}, fmt.Errorf("写入临时配置文件失败: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return SaveResult{}, fmt.Errorf("同步临时配置文件失败: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return SaveResult{}, fmt.Errorf("关闭临时配置文件失败: %w", err)
	}
	if err := os.Chmod(tmpPath, fileMode); err != nil {
		return SaveResult{}, fmt.Errorf("设置临时配置文件权限失败: %w", err)
	}

	backupPath, err := createBackup(absTarget)
	if err != nil {
		return SaveResult{}, err
	}

	if err := atomicReplaceFile(tmpPath, absTarget); err != nil {
		return SaveResult{}, fmt.Errorf("替换配置文件失败: %w", err)
	}
	cleanupTemp = false

	return SaveResult{
		SourcePath: sourcePath,
		TargetPath: absTarget,
		BackupPath: backupPath,
		SavedAt:    time.Now(),
	}, nil
}

func marshalYAML(cfg *Config) ([]byte, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(cfg); err != nil {
		return nil, fmt.Errorf("编码配置失败: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("关闭 YAML 编码器失败: %w", err)
	}
	return buf.Bytes(), nil
}

func createBackup(targetPath string) (string, error) {
	info, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("读取待备份配置失败: %w", err)
	}

	backupDir := filepath.Join(filepath.Dir(targetPath), ".bak")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", fmt.Errorf("创建配置备份目录失败: %w", err)
	}

	ext := filepath.Ext(targetPath)
	name := strings.TrimSuffix(filepath.Base(targetPath), ext)
	stamp := time.Now().Format("20060102-150405.000")
	backupPath := filepath.Join(backupDir, name+"-"+stamp+ext)

	src, err := os.Open(targetPath)
	if err != nil {
		return "", fmt.Errorf("打开待备份配置失败: %w", err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.OpenFile(backupPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return "", fmt.Errorf("创建配置备份失败: %w", err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return "", fmt.Errorf("写入配置备份失败: %w", err)
	}
	if err := dst.Sync(); err != nil {
		_ = dst.Close()
		return "", fmt.Errorf("同步配置备份失败: %w", err)
	}
	if err := dst.Close(); err != nil {
		return "", fmt.Errorf("关闭配置备份失败: %w", err)
	}
	return backupPath, nil
}
