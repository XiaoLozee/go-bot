package botd

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

//go:embed scaffold_templates/external_exec_python_echo/*
var scaffoldTemplates embed.FS

var pluginIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

type scaffoldData struct {
	PluginID       string
	PluginName     string
	Description    string
	TemplateName   string
	TemplateSource string
}

type scaffoldTemplateSpec struct {
	Name        string
	Description string
	Root        string
}

var externalPluginScaffoldTemplates = map[string]scaffoldTemplateSpec{
	"python_echo": {
		Name:        "python_echo",
		Description: "Python + uv + stdio_jsonrpc 示例模板",
		Root:        "scaffold_templates/external_exec_python_echo",
	},
}

func runScaffold(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("缺少 scaffold 子命令，当前支持: external-plugin")
	}

	switch args[0] {
	case "external-plugin":
		return runExternalPluginScaffold(args[1:])
	default:
		return fmt.Errorf("未知 scaffold 子命令: %s", args[0])
	}
}

func runExternalPluginScaffold(args []string) error {
	fs := flag.NewFlagSet("scaffold external-plugin", flag.ContinueOnError)
	pluginID := fs.String("id", "", "插件 ID，例如 hello_world")
	pluginName := fs.String("name", "", "插件名称，默认与 id 相同")
	description := fs.String("description", "", "插件描述")
	output := fs.String("output", "plugins", "输出目录，默认生成到 plugins/<id>")
	templateName := fs.String("template", "python_echo", "模板名称，当前支持 python_echo")
	force := fs.Bool("force", false, "若目标目录已存在，是否覆盖")
	listTemplates := fs.Bool("list-templates", false, "列出可用模板")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *listTemplates {
		for _, item := range listAvailableExternalPluginTemplates() {
			fmt.Fprintf(os.Stdout, "%s\t%s\n", item.Name, item.Description)
		}
		return nil
	}

	spec, ok := externalPluginScaffoldTemplates[strings.TrimSpace(*templateName)]
	if !ok {
		return fmt.Errorf("未知模板: %s", *templateName)
	}

	id := strings.TrimSpace(*pluginID)
	if id == "" {
		return fmt.Errorf("插件 ID 不能为空")
	}
	if !pluginIDPattern.MatchString(id) {
		return fmt.Errorf("插件 ID 格式非法，仅支持小写字母、数字、下划线和中划线，且必须以字母或数字开头: %s", id)
	}

	name := strings.TrimSpace(*pluginName)
	if name == "" {
		name = id
	}

	desc := strings.TrimSpace(*description)
	if desc == "" {
		desc = "A new external_exec plugin scaffold"
	}

	outputRoot := strings.TrimSpace(*output)
	if outputRoot == "" {
		outputRoot = "plugins"
	}
	targetDir := filepath.Join(outputRoot, id)
	exists, err := scaffoldPathExists(targetDir)
	if err != nil {
		return err
	}
	if exists {
		if !*force {
			return fmt.Errorf("目标目录已存在: %s", targetDir)
		}
		if err := os.RemoveAll(targetDir); err != nil {
			return fmt.Errorf("删除旧模板目录失败: %w", err)
		}
	}

	data := scaffoldData{
		PluginID:       id,
		PluginName:     name,
		Description:    desc,
		TemplateName:   spec.Name,
		TemplateSource: spec.Description,
	}
	if err := renderScaffoldTemplate(spec, targetDir, data); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "已生成 external_exec 插件模板: %s\n", targetDir)
	return nil
}

func listAvailableExternalPluginTemplates() []scaffoldTemplateSpec {
	out := make([]scaffoldTemplateSpec, 0, len(externalPluginScaffoldTemplates))
	for _, item := range externalPluginScaffoldTemplates {
		out = append(out, item)
	}
	return out
}

func renderScaffoldTemplate(spec scaffoldTemplateSpec, targetDir string, data scaffoldData) error {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("创建模板输出目录失败: %w", err)
	}

	return fs.WalkDir(scaffoldTemplates, spec.Root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == spec.Root {
			return nil
		}

		rel, err := filepath.Rel(spec.Root, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(targetDir, rel)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		payload, err := scaffoldTemplates.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取模板文件失败 %s: %w", path, err)
		}

		rendered, err := renderTemplateFile(path, string(payload), data)
		if err != nil {
			return err
		}

		mode := os.FileMode(0o644)
		if strings.HasSuffix(strings.ToLower(targetPath), ".sh") {
			mode = 0o755
		}
		if err := os.WriteFile(targetPath, []byte(rendered), mode); err != nil {
			return fmt.Errorf("写入模板文件失败 %s: %w", targetPath, err)
		}
		return nil
	})
}

func renderTemplateFile(name, content string, data scaffoldData) (string, error) {
	tpl, err := template.New(filepath.Base(name)).Parse(content)
	if err != nil {
		return "", fmt.Errorf("解析模板失败 %s: %w", name, err)
	}
	var builder strings.Builder
	if err := tpl.Execute(&builder, data); err != nil {
		return "", fmt.Errorf("渲染模板失败 %s: %w", name, err)
	}
	return builder.String(), nil
}

func scaffoldPathExists(target string) (bool, error) {
	_, err := os.Stat(target)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("检查目标目录失败: %w", err)
}
