package externalexec

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
	"gopkg.in/yaml.v3"
)

const (
	KindExternalExec    = "external_exec"
	ProtocolStdioJSONRP = "stdio_jsonrpc"
	RuntimePython       = "python"
)

type Descriptor struct {
	Manifest     sdk.Manifest
	ManifestPath string
	WorkDir      string
}

type rawManifest struct {
	ID           string   `yaml:"id"`
	Name         string   `yaml:"name"`
	Version      string   `yaml:"version"`
	Description  string   `yaml:"description"`
	Author       string   `yaml:"author"`
	Runtime      string   `yaml:"runtime"`
	Entry        string   `yaml:"entry"`
	Args         []string `yaml:"args"`
	Protocol     string   `yaml:"protocol"`
	ConfigSchema string   `yaml:"config_schema"`
}

func DefaultPluginRoot(configPath string) string {
	resolved := configPath
	if abs, err := filepath.Abs(resolved); err == nil {
		resolved = abs
	}
	dir := filepath.Dir(resolved)
	if strings.EqualFold(filepath.Base(dir), "configs") {
		dir = filepath.Dir(dir)
	}
	return filepath.Join(dir, "plugins")
}

func Discover(root string) ([]Descriptor, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, nil
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("外部插件目录不是文件夹: %s", root)
	}

	items := make(map[string]Descriptor)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		name := strings.ToLower(d.Name())
		if name != "plugin.yaml" && name != "plugin.yml" {
			return nil
		}
		desc, err := loadDescriptor(path)
		if err != nil {
			return fmt.Errorf("读取外部插件清单失败 %s: %w", path, err)
		}
		if _, exists := items[desc.Manifest.ID]; exists {
			return fmt.Errorf("发现重复的外部插件 ID: %s", desc.Manifest.ID)
		}
		items[desc.Manifest.ID] = desc
		return nil
	})
	if err != nil {
		return nil, err
	}

	out := make([]Descriptor, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.ID < out[j].Manifest.ID })
	return out, nil
}

func loadDescriptor(manifestPath string) (Descriptor, error) {
	payload, err := os.ReadFile(manifestPath)
	if err != nil {
		return Descriptor{}, err
	}
	var raw rawManifest
	if err := yaml.Unmarshal(payload, &raw); err != nil {
		return Descriptor{}, err
	}
	if strings.TrimSpace(raw.ID) == "" {
		return Descriptor{}, fmt.Errorf("id 不能为空")
	}
	if strings.TrimSpace(raw.Name) == "" {
		raw.Name = raw.ID
	}
	if strings.TrimSpace(raw.Version) == "" {
		raw.Version = "0.0.0"
	}
	if strings.TrimSpace(raw.Entry) == "" {
		return Descriptor{}, fmt.Errorf("entry 不能为空")
	}
	runtimeKind := strings.ToLower(strings.TrimSpace(raw.Runtime))
	switch runtimeKind {
	case "", RuntimePython:
	default:
		return Descriptor{}, fmt.Errorf("当前不支持 runtime: %s", raw.Runtime)
	}
	protocol := strings.TrimSpace(raw.Protocol)
	if protocol == "" {
		protocol = ProtocolStdioJSONRP
	}
	if protocol != ProtocolStdioJSONRP {
		return Descriptor{}, fmt.Errorf("当前仅支持协议 %s", ProtocolStdioJSONRP)
	}

	workDir := filepath.Dir(manifestPath)
	entry := expandManifestValue(strings.TrimSpace(raw.Entry), workDir)
	if !filepath.IsAbs(entry) {
		entry = filepath.Join(workDir, entry)
	}
	if abs, err := filepath.Abs(entry); err == nil {
		entry = abs
	}
	entry, err = resolveEntryPath(entry, runtimeKind)
	if err != nil {
		return Descriptor{}, err
	}

	configSchema := expandManifestValue(strings.TrimSpace(raw.ConfigSchema), workDir)
	if configSchema != "" && !filepath.IsAbs(configSchema) {
		configSchema = filepath.Join(workDir, configSchema)
	}
	if configSchema != "" {
		if abs, err := filepath.Abs(configSchema); err == nil {
			configSchema = abs
		}
	}

	args := make([]string, 0, len(raw.Args))
	for _, arg := range raw.Args {
		args = append(args, expandManifestValue(arg, workDir))
	}

	manifest := sdk.Manifest{
		ID:           strings.TrimSpace(raw.ID),
		Name:         strings.TrimSpace(raw.Name),
		Version:      strings.TrimSpace(raw.Version),
		Description:  strings.TrimSpace(raw.Description),
		Author:       strings.TrimSpace(raw.Author),
		Kind:         KindExternalExec,
		Builtin:      false,
		Runtime:      runtimeKind,
		Entry:        entry,
		Args:         args,
		Protocol:     protocol,
		Source:       workDir,
		ConfigSchema: configSchema,
	}
	return Descriptor{
		Manifest:     manifest,
		ManifestPath: manifestPath,
		WorkDir:      workDir,
	}, nil
}

func resolveEntryPath(entry string, runtimeKind string) (string, error) {
	info, err := os.Stat(entry)
	switch {
	case err == nil:
		if info.IsDir() {
			return "", fmt.Errorf("entry 不是文件: %s", entry)
		}
		return entry, nil
	case !os.IsNotExist(err):
		return "", err
	}

	if runtimeKind == RuntimePython {
		return "", fmt.Errorf("entry 不存在: %s", entry)
	}

	if filepath.Ext(entry) != "" {
		return "", fmt.Errorf("entry 不存在: %s", entry)
	}

	for _, candidate := range platformEntryCandidates(entry) {
		info, candidateErr := os.Stat(candidate)
		switch {
		case candidateErr == nil:
			if info.IsDir() {
				continue
			}
			return candidate, nil
		case os.IsNotExist(candidateErr):
			continue
		default:
			return "", candidateErr
		}
	}
	return "", fmt.Errorf("entry 不存在: %s", entry)
}

func platformEntryCandidates(entry string) []string {
	if runtime.GOOS == "windows" {
		return []string{entry + ".exe", entry + ".cmd", entry + ".bat"}
	}
	return []string{entry + ".sh"}
}

func expandManifestValue(value, workDir string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	executablePath, _ := os.Executable()
	return os.Expand(value, func(key string) string {
		switch key {
		case "GOBOT_EXECUTABLE":
			return executablePath
		case "PLUGIN_DIR":
			return workDir
		default:
			return os.Getenv(key)
		}
	})
}
