# external_exec 插件打包规范 v1

本文档描述 Go-bot 当前 external_exec 插件包的最小约定。

当前正式主线已经切到：

- **Python-first 脚本插件**
- 宿主协议：`stdio_jsonrpc`
- 入口约定：`runtime: python` + `entry: ./main.py`

## 1. 支持的插件包格式

- `.zip`
- `.tar.gz`
- `.tgz`

其中 `.zip` 是当前内置打包脚本的默认输出格式。

## 2. 包内目录结构

推荐一个插件一个目录：

```text
your_plugin/
  plugin.yaml
  config.schema.json
  main.py
  requirements.txt
  requirements-dev.txt
  _common/
    gobot_runtime.py
    gobot_plugin/
```

说明：

- `main.py`：插件主逻辑入口
- `requirements.txt`：可选，声明插件第三方 Python 依赖
- `requirements-dev.txt`：可选，声明开发期 IDE 类型提示依赖，不参与运行时依赖安装
- `_common/`：共享 Python runtime，打包时建议随插件一起嵌入，避免依赖宿主机器上的额外仓库目录

也允许 `plugin.yaml` 直接放在压缩包根目录，但仍建议使用单独目录，结构更清晰。

## 3. 必需文件

### 3.1 plugin.yaml

最小示例：

```yaml
id: template_python_echo
name: Python Echo Template
version: 0.1.0
description: An external_exec Python example plugin
author: Go-bot
runtime: python
entry: ./main.py
protocol: stdio_jsonrpc
config_schema: ./config.schema.json
```

字段说明：

- `id`: 插件唯一 ID，必须全局唯一
- `name`: 展示名
- `version`: 版本号
- `runtime`: Python 插件固定为 `python`
- `entry`: Python 插件主文件，推荐 `./main.py`
- `protocol`: 当前固定为 `stdio_jsonrpc`
- `config_schema`: WebUI 配置表单 schema

### 3.2 main.py

`main.py` 负责：

- 读取宿主通过 `stdin` 发送的 `start / event / stop`
- 输出 `ready / log / send_text / send_segments / call`
- 通过 `_common/gobot_runtime.py` 复用协议循环与 BotAPI 调用能力
- 通过 `_common/gobot_plugin/` 或 `sdk/python/gobot_plugin/` 获得 IDE 类型提示

### 3.3 Python runtime

当 `runtime: python` 时，启动策略由宿主统一处理：

- 若插件已创建独立 venv，优先使用 venv Python；否则选择 `uv` → Windows `py -3` → `python3` → `python`
- 统一设置 UTF-8 环境变量
- 统一把 `<plugin>/_common` 或 `<plugin>/../_common` 加入 `PYTHONPATH`
- 统一执行 `entry` 指向的 Python 主文件

如果插件包内的 `requirements.txt` 至少包含一条非空、非注释依赖项，WebUI 安装时会为该插件创建独立 venv，并在后续启动时优先使用该 venv 的 Python。

## 4. 宿主上下文

启动 external_exec 插件时，宿主会通过 `start` 消息附带：

- `plugin`: 当前插件 manifest
- `config`: 当前插件配置
- `catalog`: 已安装插件目录
- `app`: 宿主应用信息

其中 `app` 当前包含：

- `name`: 应用 / 机器人名称
- `environment`: 运行环境
- `owner_qq`: 机器人主人 QQ

## 5. entry 约束

当前运行时会将相对路径解析为 **插件目录内文件**。

Python 插件推荐：

```yaml
runtime: python
entry: ./main.py
```

非 Python 的 external_exec 插件仍可不设置 `runtime`，继续使用可执行文件或平台 launcher 作为 `entry`。

## 6. 覆盖升级行为

后台上传同 ID 插件时：

- 不勾选“覆盖同 ID 插件” → 拒绝安装
- 勾选后：
  - 旧版本目录先移动到 `plugins/.bak/<plugin_id>-<timestamp>/`
  - 新版本安装到 `plugins/<plugin_id>/`
  - 若 `requirements.txt` 声明了依赖，重新创建 `data/plugin-envs/<plugin_id>/`
  - 运行时刷新 external plugin 清单
  - 若该插件当前已启用且正在运行，则自动重启

## 7. 安全限制

当前上传解包会拒绝：

- 路径穿越（Zip Slip / Tar Slip）
- 符号链接
- 多插件混装包（一个包里多个 `plugin.yaml`）

## 8. 仓库内置打包脚本

仓库提供：

```powershell
./scripts/package_external_plugins.ps1
```

该脚本会：

1. 遍历 `plugins/*` 下带 manifest 的插件目录
2. 跳过 `_common/` 与 `.bak/`
3. 将插件目录复制到 stage
4. 清理本地 venv 与 Python 缓存文件
5. 把 `plugins/_common/` 嵌入到每个 stage 插件目录的 `_common/`
6. 生成 `build/plugin-packages/<plugin_id>.zip`

## 9. 模板

可直接参考：

```text
templates/external_exec_python_echo/
```

该模板展示了：

- `plugin.yaml`
- `config.schema.json`
- `main.py`
- `requirements.txt`
- `requirements-dev.txt`

## 10. 脚手架命令

可以直接用 CLI 生成模板：

```bash
botd scaffold external-plugin \
  --id hello_echo \
  --name "Hello Echo" \
  --description "my first external_exec plugin"
```

默认会输出到：

```text
plugins/hello_echo/
```

可选参数：

- `--template python_echo`
- `--output <dir>`
- `--force`
- `--list-templates`

