# {{.PluginName}}

这是通过 `botd scaffold external-plugin` 生成的 Python-first external_exec 模板。

## 基础信息

- 插件 ID：`{{.PluginID}}`
- 模板：`{{.TemplateName}}`
- 模板说明：{{.TemplateSource}}

## 推荐流程

1. 按需修改 `config.schema.json`
2. 在 `main.py` 中实现你的业务逻辑
3. 安装开发期类型提示依赖：

```bash
pip install -r requirements-dev.txt
```

VS Code 可在插件目录加入 `.vscode/settings.json`：

```json
{
  "python.analysis.extraPaths": [
    "../../sdk/python",
    "../_common"
  ]
}
```

4. 先本地联调：

```bash
uv run python scripts/external_plugin_harness.py \
  --plugin-dir plugins/{{.PluginID}} \
  --chat-type private \
  --text /{{.PluginID}}
```

5. 再执行统一打包：

```powershell
./scripts/package_external_plugins.ps1
```

## 入口说明

当前模板 manifest 固定使用：

```yaml
runtime: python
entry: ./main.py
```

宿主会统一：

- 若插件已创建独立 venv，优先使用 venv Python；否则选择 `uv` / `python3` / `python`（Windows 下额外尝试 `py -3`）
- 注入 `_common/gobot_runtime.py` 和 `gobot_plugin` SDK 所需的 `PYTHONPATH`
- 打开 UTF-8 相关环境变量

如果 `requirements.txt` 中声明了外部依赖，WebUI 安装插件包时会为该插件创建独立 venv；只有注释或空行时不会创建。

