package runtime

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/plugin/externalexec"
)

const (
	pluginPackageFormatZIP   = "zip"
	pluginPackageFormatTarGZ = "tar.gz"
)

func (s *Service) InstallPluginPackage(ctx context.Context, fileName string, payload []byte, overwrite bool) (PluginInstallResult, error) {
	if len(payload) == 0 {
		return PluginInstallResult{}, fmt.Errorf("上传文件为空")
	}

	format, err := detectPluginPackageFormat(fileName)
	if err != nil {
		return PluginInstallResult{}, err
	}
	if strings.TrimSpace(s.externalRoot) == "" {
		return PluginInstallResult{}, fmt.Errorf("未配置外部插件目录")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.syncExternalPlugins(); err != nil {
		return PluginInstallResult{}, err
	}
	if err := os.MkdirAll(s.externalRoot, 0o755); err != nil {
		return PluginInstallResult{}, fmt.Errorf("创建外部插件目录失败: %w", err)
	}

	stageRoot, err := os.MkdirTemp(filepath.Dir(s.externalRoot), ".gobot-plugin-upload-*")
	if err != nil {
		return PluginInstallResult{}, fmt.Errorf("创建插件上传暂存目录失败: %w", err)
	}
	defer func() { _ = os.RemoveAll(stageRoot) }()

	if err := extractPluginPackage(stageRoot, payload, format); err != nil {
		return PluginInstallResult{}, err
	}

	descriptors, err := externalexec.Discover(stageRoot)
	if err != nil {
		return PluginInstallResult{}, err
	}
	switch len(descriptors) {
	case 0:
		return PluginInstallResult{}, fmt.Errorf("压缩包中未发现 plugin.yaml 或 plugin.yml")
	case 1:
	default:
		return PluginInstallResult{}, fmt.Errorf("一个插件包只能包含一个外部插件，当前发现 %d 个", len(descriptors))
	}

	desc := descriptors[0]
	if manifest, ok := s.host.Manifest(desc.Manifest.ID); ok && manifest.Builtin {
		return PluginInstallResult{}, fmt.Errorf("插件 ID 与内置插件冲突: %s", desc.Manifest.ID)
	}

	s.mu.RLock()
	running := s.state == StateRunning
	currentCfg := s.cfg
	s.mu.RUnlock()

	pluginCfg, configured := findConfiguredPlugin(currentCfg.Plugins, desc.Manifest.ID)
	shouldRestart := running && configured && pluginCfg.Enabled

	targetDir := filepath.Join(s.externalRoot, desc.Manifest.ID)
	targetExists, err := pathExists(targetDir)
	if err != nil {
		return PluginInstallResult{}, fmt.Errorf("检查插件目录失败: %w", err)
	}
	if targetExists && !overwrite {
		return PluginInstallResult{}, fmt.Errorf("插件已存在: %s；如需升级请勾选覆盖安装", desc.Manifest.ID)
	}

	backupPath := ""
	replaced := false
	if targetExists {
		if shouldRestart {
			if err := s.host.StopPlugin(ctx, desc.Manifest.ID); err != nil {
				return PluginInstallResult{}, fmt.Errorf("停止运行中的旧插件失败: %w", err)
			}
		}

		backupPath = s.nextPluginBackupPath(desc.Manifest.ID)
		if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
			if shouldRestart {
				_ = s.host.StartPlugin(ctx, desc.Manifest.ID)
			}
			return PluginInstallResult{}, fmt.Errorf("创建插件备份目录失败: %w", err)
		}
		if err := moveDir(targetDir, backupPath); err != nil {
			if shouldRestart {
				_ = s.host.StartPlugin(ctx, desc.Manifest.ID)
			}
			return PluginInstallResult{}, fmt.Errorf("备份旧插件目录失败: %w", err)
		}
		replaced = true
	} else if manifest, ok := s.host.Manifest(desc.Manifest.ID); ok && !manifest.Builtin {
		return PluginInstallResult{}, fmt.Errorf("插件已存在但目录缺失: %s；请检查外部插件目录后重试", desc.Manifest.ID)
	}

	if err := moveDir(desc.WorkDir, targetDir); err != nil {
		if rollbackErr := restorePluginDirectory(targetDir, backupPath); rollbackErr != nil {
			return PluginInstallResult{}, fmt.Errorf("移动插件目录失败: %w；回滚失败: %v", err, rollbackErr)
		}
		if shouldRestart && backupPath != "" {
			_ = s.syncExternalPlugins()
			_ = s.host.StartPlugin(ctx, desc.Manifest.ID)
		}
		return PluginInstallResult{}, fmt.Errorf("移动插件目录失败: %w", err)
	}

	installedDesc := desc
	installedDesc.WorkDir = targetDir
	if strings.EqualFold(installedDesc.Manifest.Runtime, externalexec.RuntimePython) {
		if err := s.syncPythonPluginCommonRuntime(installedDesc.WorkDir); err != nil {
			if rollbackErr := restorePluginDirectory(targetDir, backupPath); rollbackErr != nil {
				return PluginInstallResult{}, fmt.Errorf("修复 Python 插件运行时目录失败: %w；回滚失败: %v", err, rollbackErr)
			}
			if shouldRestart && backupPath != "" {
				_ = s.syncExternalPlugins()
				_ = s.host.StartPlugin(ctx, desc.Manifest.ID)
			}
			return PluginInstallResult{}, fmt.Errorf("修复 Python 插件运行时目录失败: %w", err)
		}
	}
	dependencyEnvPath, dependenciesInstalled, err := s.preparePythonPluginEnv(ctx, installedDesc)
	if err != nil {
		if rollbackErr := restorePluginDirectory(targetDir, backupPath); rollbackErr != nil {
			return PluginInstallResult{}, fmt.Errorf("准备 Python 插件依赖环境失败: %w；回滚失败: %v", err, rollbackErr)
		}
		if shouldRestart && backupPath != "" {
			_ = s.syncExternalPlugins()
			_ = s.host.StartPlugin(ctx, desc.Manifest.ID)
		}
		return PluginInstallResult{}, fmt.Errorf("准备 Python 插件依赖环境失败: %w", err)
	}

	result := PluginInstallResult{
		PluginID:              desc.Manifest.ID,
		Kind:                  desc.Manifest.Kind,
		Format:                format,
		InstalledTo:           targetDir,
		ManifestPath:          filepath.Join(targetDir, filepath.Base(desc.ManifestPath)),
		BackupPath:            backupPath,
		DependencyEnvPath:     dependencyEnvPath,
		DependenciesInstalled: dependenciesInstalled,
		Replaced:              replaced,
		Reloaded:              false,
	}

	if err := s.syncExternalPlugins(); err != nil {
		if rollbackErr := restorePluginDirectory(targetDir, backupPath); rollbackErr != nil {
			return PluginInstallResult{}, fmt.Errorf("刷新外部插件清单失败: %w；回滚失败: %v", err, rollbackErr)
		}
		_ = s.syncExternalPlugins()
		if shouldRestart && backupPath != "" {
			_ = s.host.StartPlugin(ctx, desc.Manifest.ID)
		}
		return PluginInstallResult{}, fmt.Errorf("刷新外部插件清单失败: %w", err)
	}

	nextCfg, err := config.Clone(currentCfg)
	if err != nil {
		return PluginInstallResult{}, err
	}
	nextCfg.Plugins = s.normalizePluginConfigs(nextCfg.Plugins, currentCfg.Plugins)
	s.host.SetConfigured(nextCfg.Plugins)
	s.mu.Lock()
	s.cfg.Plugins = nextCfg.Plugins
	s.mu.Unlock()

	if shouldRestart {
		if err := s.host.StartPlugin(ctx, desc.Manifest.ID); err != nil {
			if rollbackErr := restorePluginDirectory(targetDir, backupPath); rollbackErr != nil {
				return PluginInstallResult{}, fmt.Errorf("重启升级后的插件失败: %w；回滚失败: %v", err, rollbackErr)
			}
			_ = s.syncExternalPlugins()
			if backupPath != "" {
				_ = s.host.StartPlugin(ctx, desc.Manifest.ID)
			}
			return PluginInstallResult{}, fmt.Errorf("重启升级后的插件失败: %w", err)
		}
	}

	result.Reloaded = true
	switch {
	case replaced && shouldRestart:
		result.Message = "插件包已覆盖安装，旧版本已备份，运行中的插件已自动重启"
	case replaced:
		result.Message = "插件包已覆盖安装，旧版本已备份，运行时已刷新插件清单"
	default:
		result.Message = "插件包已安装，运行时已刷新插件清单，可继续安装或启用该插件"
	}
	return result, nil
}

func detectPluginPackageFormat(fileName string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(fileName))
	switch {
	case strings.HasSuffix(name, ".zip"):
		return pluginPackageFormatZIP, nil
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
		return pluginPackageFormatTarGZ, nil
	default:
		return "", fmt.Errorf("当前仅支持上传 .zip / .tar.gz / .tgz 外部插件包")
	}
}

func extractPluginPackage(targetRoot string, payload []byte, format string) error {
	switch format {
	case pluginPackageFormatZIP:
		return unzipPluginPackage(targetRoot, payload)
	case pluginPackageFormatTarGZ:
		return untarGzipPluginPackage(targetRoot, payload)
	default:
		return fmt.Errorf("不支持的插件包格式: %s", format)
	}
}

func unzipPluginPackage(targetRoot string, payload []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return fmt.Errorf("解析 zip 插件包失败: %w", err)
	}

	for _, file := range reader.File {
		entryName, err := normalizeArchiveEntryName(file.Name)
		if err != nil {
			return err
		}
		if entryName == "" {
			continue
		}

		targetPath, err := archiveTargetPath(targetRoot, entryName)
		if err != nil {
			return err
		}

		info := file.FileInfo()
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("当前不支持带符号链接的插件包: %s", file.Name)
		}
		if info.IsDir() {
			if err := ensureArchiveDir(targetPath, info.Mode().Perm()); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("插件包包含不支持的文件类型: %s", file.Name)
		}

		src, err := file.Open()
		if err != nil {
			return fmt.Errorf("打开压缩包文件失败 %s: %w", file.Name, err)
		}
		if err := writeArchiveFile(targetPath, src, info.Mode().Perm(), file.Name); err != nil {
			_ = src.Close()
			return err
		}
		if err := src.Close(); err != nil {
			return fmt.Errorf("关闭压缩包文件失败 %s: %w", file.Name, err)
		}
	}

	return nil
}

func untarGzipPluginPackage(targetRoot string, payload []byte) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("解析 tar.gz 插件包失败: %w", err)
	}
	defer func() { _ = gzipReader.Close() }()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("读取 tar.gz 插件包失败: %w", err)
		}

		entryName, err := normalizeArchiveEntryName(header.Name)
		if err != nil {
			return err
		}
		if entryName == "" {
			continue
		}
		targetPath, err := archiveTargetPath(targetRoot, entryName)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := ensureArchiveDir(targetPath, os.FileMode(header.Mode).Perm()); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := writeArchiveFile(targetPath, tarReader, os.FileMode(header.Mode).Perm(), header.Name); err != nil {
				return err
			}
		case tar.TypeXHeader, tar.TypeXGlobalHeader, tar.TypeGNULongName, tar.TypeGNULongLink:
			continue
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("当前不支持带符号链接的 tar.gz 插件包: %s", header.Name)
		default:
			return fmt.Errorf("插件包包含不支持的 tar 条目类型: %s", header.Name)
		}
	}
}

func normalizeArchiveEntryName(name string) (string, error) {
	entryName := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	switch entryName {
	case ".", "/", "":
		return "", nil
	}
	if strings.HasPrefix(entryName, "../") || entryName == ".." || path.IsAbs(entryName) {
		return "", fmt.Errorf("插件包包含非法路径: %s", name)
	}
	return entryName, nil
}

func archiveTargetPath(root, entryName string) (string, error) {
	targetPath := filepath.Join(root, filepath.FromSlash(entryName))
	if !pathWithinRoot(root, targetPath) {
		return "", fmt.Errorf("插件包包含越界路径: %s", entryName)
	}
	return targetPath, nil
}

func ensureArchiveDir(targetPath string, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o755
	}
	if err := os.MkdirAll(targetPath, mode); err != nil {
		return fmt.Errorf("创建插件目录失败 %s: %w", targetPath, err)
	}
	return nil
}

func writeArchiveFile(targetPath string, src io.Reader, mode os.FileMode, displayName string) error {
	if mode == 0 {
		mode = 0o644
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("创建插件文件目录失败 %s: %w", targetPath, err)
	}
	dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("创建插件文件失败 %s: %w", targetPath, err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return fmt.Errorf("写入插件文件失败 %s: %w", targetPath, err)
	}
	if err := dst.Close(); err != nil {
		return fmt.Errorf("关闭插件文件失败 %s: %w", displayName, err)
	}
	return nil
}

func (s *Service) nextPluginBackupPath(pluginID string) string {
	stamp := time.Now().Format("20060102-150405.000")
	return filepath.Join(s.externalRoot, ".bak", pluginID+"-"+stamp)
}

func (s *Service) nextPluginDeleteStagePath(pluginID string) string {
	stamp := time.Now().Format("20060102-150405.000")
	return filepath.Join(s.externalRoot, ".trash", pluginID+"-"+stamp)
}

func (s *Service) resolveExternalPluginDirectory(pluginID string) (string, bool, error) {
	root := strings.TrimSpace(s.externalRoot)
	if root == "" {
		return "", false, nil
	}
	resolvedRoot := root
	if abs, err := filepath.Abs(root); err == nil {
		resolvedRoot = abs
	}

	descriptors, err := externalexec.Discover(root)
	if err != nil {
		return "", false, err
	}
	for _, desc := range descriptors {
		if desc.Manifest.ID != pluginID {
			continue
		}
		target := desc.WorkDir
		if abs, err := filepath.Abs(target); err == nil {
			target = abs
		}
		if !pathWithinRoot(resolvedRoot, target) {
			return "", false, fmt.Errorf("插件目录超出允许范围: %s", target)
		}
		exists, err := pathExists(target)
		return target, exists, err
	}

	target := filepath.Join(resolvedRoot, pluginID)
	if !pathWithinRoot(resolvedRoot, target) {
		return "", false, fmt.Errorf("插件目录超出允许范围: %s", target)
	}
	exists, err := pathExists(target)
	return target, exists, err
}

func restorePluginDirectory(targetDir, backupPath string) error {
	_ = os.RemoveAll(targetDir)
	if strings.TrimSpace(backupPath) == "" {
		return nil
	}
	exists, err := pathExists(backupPath)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return moveDir(backupPath, targetDir)
}

func moveDir(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isCrossDeviceError(err) {
		return err
	}

	if err := copyDir(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func isCrossDeviceError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "cross-device") || strings.Contains(text, "not same device")
}

func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("源路径不是目录: %s", src)
	}
	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}

	return filepath.Walk(src, func(current string, fileInfo os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, current)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		targetPath := filepath.Join(dst, rel)
		if fileInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("目录复制不支持符号链接: %s", current)
		}
		if fileInfo.IsDir() {
			return os.MkdirAll(targetPath, fileInfo.Mode().Perm())
		}
		return copyFile(current, targetPath, fileInfo.Mode().Perm())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func pathExists(target string) (bool, error) {
	_, err := os.Stat(target)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func findConfiguredPlugin(plugins []config.PluginConfig, id string) (config.PluginConfig, bool) {
	for _, plugin := range plugins {
		if plugin.ID != id {
			continue
		}
		return clonePluginConfig(plugin), true
	}
	return config.PluginConfig{}, false
}

func pathWithinRoot(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}
