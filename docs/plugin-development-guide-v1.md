# Go-bot 插件开发文档 v1

本文档面向当前 Go-bot 的 **Python-first external_exec 插件主线**。

当前结论很简单：

- 正式业务插件：`plugins/*`
- 宿主协议层：`internal/plugin/externalexec`
- builtin：仅保留 `internal/plugin/builtin/testplugin`

如果你要写新业务插件，默认直接写 Python 脚本插件，不再走旧 Go external plugin 路线。

---

## 1. 当前插件模型

### 1.1 builtin

适合：

- 宿主自检
- 测试插件
- 极少量必须随主程序编译的能力

当前实际保留：

```text
internal/plugin/builtin/testplugin
```

### 1.2 external_exec（主线）

适合：

- 所有业务插件
- 热插拔 / 独立升级 / 独立打包
- WebUI 上传安装

当前实际目录：

```text
plugins/<plugin_id>/
```

---

## 2. 推荐目录结构

一个标准 Python-first 插件目录如下：

```text
your_plugin/
  plugin.yaml
  config.schema.json
  main.py
  requirements.txt
  requirements-dev.txt
```

运行时共享协议 helper 位于：

```text
plugins/_common/gobot_runtime.py
plugins/_common/gobot_plugin/
sdk/python/gobot_plugin/
```

仓库内开发态：

- 宿主按 `runtime: python` 统一启动 `main.py`
- 宿主会从同级 `_common/` 或上级 `../_common/` 找 `gobot_runtime.py` 和 `gobot_plugin`
- 宿主会显式开启 UTF-8 stdin/stdout，避免 Windows 下中文消息解码出错

打包态：

- 打包脚本会把 `_common/` 一起嵌入每个插件包
- 打包脚本会忽略 `.venv` / `venv`、`__pycache__`、`*.pyc`

### 2.1 IDE 代码提示

插件开发时推荐安装开发期 SDK：

```text
requirements-dev.txt
```

内容：

```text
-e ../../sdk/python
```

VS Code 可在插件目录添加：

```json
{
  "python.analysis.extraPaths": [
    "../../sdk/python",
    "../_common"
  ]
}
```

PyCharm 可把以下目录标记为 Sources Root：

```text
sdk/python
plugins/_common
```

这样 `from gobot_plugin import BasePlugin, PluginEnv, MessageEvent` 会有补全。
运行时仍由宿主注入 `_common`，开发 venv 不需要打进插件包。

---

## 3. `plugin.yaml`

推荐写法：

```yaml
id: hello_echo
name: Hello Echo
version: 0.1.0
description: A minimal Python external_exec plugin
author: Go-bot
runtime: python
entry: ./main.py
protocol: stdio_jsonrpc
config_schema: ./config.schema.json
```

关键点：

- `runtime` 固定推荐 `python`
- `entry` 固定推荐 `./main.py`
- 若插件已创建独立 venv，宿主优先使用 venv Python；否则选择 `uv` → Windows `py -3` → `python3` → `python`
- 宿主统一注入 `_common` 到 `PYTHONPATH`

---

## 4. 外部依赖

如果插件需要第三方 Python 包，统一写在：

```text
requirements.txt
```

示例：

```text
requests==2.32.3
pydantic==2.11.0
```

当 `requirements.txt` 至少包含一条非空、非注释依赖项时，WebUI 安装插件包会：

1. 检测插件目录下的 `requirements.txt` 是否声明了依赖
2. 为该插件创建独立 venv：`data/plugin-envs/<plugin_id>/`
3. 优先使用 `uv` 创建环境并安装依赖
4. 没有 `uv` 时回退到 `python -m venv` + `pip install -r requirements.txt`
5. 插件启动时优先使用该 venv 中的 Python

不要把 `.venv` 或本机 site-packages 打进插件包。

---

## 5. Python 插件最小示例

当前推荐直接基于 `gobot_plugin` SDK 写：

```python
from __future__ import annotations

from gobot_plugin import BasePlugin, MessageEvent, PluginEnv, build_target, run_plugin


class HelloPlugin(BasePlugin):
    def start(self, env: PluginEnv) -> None:
        super().start(env)
        self.keyword = str(env.config.get("keyword") or "/hello").strip()
        self.response = str(env.config.get("response") or "hello").strip()

    def handle_event(self, event: MessageEvent) -> None:
        if str(event.get("kind") or "") != "message":
            return
        if str(event.get("raw_text") or "").strip() != self.keyword:
            return

        target = build_target(event)
        reply_to = str(event.get("message_id") or "")
        self.env.messenger.reply_text(target, reply_to, self.response)


if __name__ == "__main__":
    raise SystemExit(run_plugin(HelloPlugin()))
```

这已经够覆盖大部分简单插件。

---

## 6. 宿主注入给插件的上下文

`gobot_runtime` 在 `start` 时会给你一个 `env`，常用能力包括：

- `env.config`
- `env.catalog`
- `env.app`
- `env.logger`
- `env.messenger`
- `env.bot_api`

### 6.1 `env.config`

当前插件配置，类型是普通 `dict`：

```python
keyword = str(env.config.get("keyword") or "/hello").strip()
```

### 6.2 `env.catalog`

已安装插件目录，可用于菜单类插件：

```python
plugins = env.catalog.list_plugins()
```

### 6.3 `env.app`

宿主应用信息：

```python
app_name = str(env.app.get("name") or "go-bot")
owner_qq = str(env.app.get("owner_qq") or "")
```

### 6.4 `env.messenger`

发消息：

```python
env.messenger.send_text(target, "hello")
env.messenger.reply_text(target, reply_to, "pong")
env.messenger.send_segments(target, segments)
```

### 6.5 `env.bot_api`

当前支持：

- `get_stranger_info`
- `get_group_member_list`
- `send_group_forward`

示例：

```python
info = env.bot_api.get_stranger_info(connection_id, user_id)
members = env.bot_api.get_group_member_list(connection_id, group_id)
env.bot_api.send_group_forward(connection_id, group_id, nodes)
```

---

## 7. 本地调试：统一 harness

仓库现在提供统一本地调试脚本：

```text
scripts/external_plugin_harness.py
```

它会：

1. 启动目标插件的 `main.py`
2. 向插件发送 `start / event / stop`
3. 自动模拟 `BotAPI` 调用返回
4. 打印插件发回的：
   - `ready`
   - `log`
   - `send_text`
   - `reply_text`
   - `send_segments`
   - `call`

### 7.1 最简单用法

例如调试 `menu_hint`：

```bash
uv run python scripts/external_plugin_harness.py \
  --plugin-dir plugins/menu_hint \
  --chat-type group \
  --text 菜单
```

### 7.2 带配置调试

```bash
uv run python scripts/external_plugin_harness.py \
  --plugin-dir plugins/choose_a_song \
  --config-json '{"search_limit": 3, "api_base_url": "http://127.0.0.1:17997"}' \
  --chat-type group \
  --text '点歌 晴天'
```

### 7.3 用事件文件调试

```bash
uv run python scripts/external_plugin_harness.py \
  --plugin-dir plugins/forging_message \
  --event-file ./tmp/forge_event.json
```

`--event-file` 支持：

- 单个事件对象
- 事件数组
- `{ "events": [...] }`

### 7.4 自定义 BotAPI mock

可选传入：

```bash
--bot-api-fixture ./tmp/bot_api_fixture.json
```

格式示例：

```json
{
  "stranger_info": {
    "10001": { "user_id": "10001", "nickname": "Alice" }
  },
  "group_members": {
    "123456": [
      { "group_id": "123456", "user_id": "10001", "nickname": "Alice", "card": "Alice" }
    ]
  },
  "call_results": {
    "bot.send_group_forward": { "ok": true }
  }
}
```

---

## 8. `config.schema.json`

如果你希望 WebUI 为插件生成结构化表单，而不是裸 JSON，需要提供：

```json
{
  "type": "object",
  "properties": {
    "keyword": {
      "type": "string",
      "title": "触发指令",
      "default": "/hello"
    },
    "response": {
      "type": "string",
      "title": "回复内容",
      "default": "hello"
    }
  }
}
```

建议：

- `title` 用中文产品化名称
- 能写 `default` 就写
- 不要把内部实现细节直接暴露给最终用户

---

## 9. 打包

当前统一使用：

```powershell
./scripts/package_external_plugins.ps1
```

它会：

1. 遍历 `plugins/*`
2. 跳过 `_common/` 和 `.bak/`
3. 复制插件目录到 stage
4. 清理本地 venv 与 Python 缓存文件
5. 把 `plugins/_common/` 一起拷进每个插件目录
6. 输出：

```text
build/plugin-packages/<plugin_id>.zip
```

如果你的部署环境是 Linux，也可以自己打：

```bash
tar -czf your_plugin.tar.gz your_plugin/
```

---

## 10. 脚手架

可直接生成 Python-first 模板：

```bash
botd scaffold external-plugin \
  --id hello_echo \
  --name "Hello Echo" \
  --description "my first external_exec plugin"
```

默认输出到：

```text
plugins/hello_echo/
```

---

## 11. 推荐开发流程

### 第一步：从模板起步

- 用脚手架生成
- 或复制 `templates/external_exec_python_echo/`

### 第二步：先做最小闭环

先只做：

- 一个命令
- 一条回复
- 一个配置项

不要一开始就塞状态机、外部 API、数据库。

### 第三步：先本地 harness 跑通

至少验证：

1. `start` 能收到
2. `ready` 能发回
3. `event` 能触发业务
4. `send_text` / `reply_text` / `send_segments` 输出符合预期

### 第四步：再打包上传

- 先本地 harness 联调
- 再打 zip / tar.gz
- 最后进 WebUI 安装验证

---

## 12. 模板与相关文档

- `templates/external_exec_python_echo/`
- `docs/external-exec-plugin-package-v1.md`
- `scripts/external_plugin_harness.py`

如果你只是要快速开始，建议顺序：

1. 先复制模板或用 scaffold 生成
2. 用 harness 跑通一个命令
3. 再接真实外部 API
4. 最后打包并上传安装

