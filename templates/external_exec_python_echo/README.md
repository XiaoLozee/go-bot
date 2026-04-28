# external_exec Python Echo 模板

这是一个对齐当前 Python-first 主线的最小模板。

## 目录

- `plugin.yaml`：插件清单
- `config.schema.json`：WebUI 配置模型
- `main.py`：基于 `gobot_plugin` SDK 的最小插件
- `requirements.txt`：插件运行时依赖
- `requirements-dev.txt`：开发期 IDE 类型提示依赖

## IDE 代码提示

开发时建议创建插件自己的 venv，然后安装开发依赖：

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

模板默认从 `gobot_plugin` 导入 `BasePlugin`、`PluginEnv`、`MessageEvent` 等类型。
宿主运行时会通过 `_common/` 注入实际 runtime，所以插件包不需要把开发 venv 打进去。

## 推荐开发方式

1. 复制此目录
2. 修改 `plugin.yaml`
3. 按需修改 `config.schema.json`
4. 在 `main.py` 中补业务逻辑
5. 先用 harness 本地联调：

```bash
uv run python scripts/external_plugin_harness.py \
  --plugin-dir plugins/template_python_echo \
  --chat-type private \
  --text /echo-demo
```

6. 再执行打包：

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

