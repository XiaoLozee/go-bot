# gobot-plugin-sdk

这是 Go-bot Python external_exec 插件的开发期 SDK。

用途：

- 让 IDE 识别 `BasePlugin`、`PluginEnv`、`MessageEvent`、`BotAPI` 等类型；
- 给插件开发提供自动补全、参数提示和返回值提示；
- 运行时仍优先使用宿主注入的 `plugins/_common/gobot_runtime.py`。

开发插件时可以在插件目录创建 `requirements-dev.txt`：

```text
-e ../../sdk/python
```

然后让 IDE 使用插件自己的虚拟环境，并安装开发依赖。

VS Code 可以添加：

```json
{
  "python.analysis.extraPaths": [
    "../../sdk/python",
    "../../plugins/_common"
  ]
}
```
