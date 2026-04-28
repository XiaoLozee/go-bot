# Go-bot

Go-bot 是一个 AI First 的自托管机器人宿主平台，面向 OneBot v11 / NapCat 场景，提供统一的运行时、AI 能力、插件系统、WebUI 控制台与配置管理。

当前主线是新宿主架构 v1：AI 作为内建核心能力，业务扩展统一走 `external_exec` 插件体系。

## 核心能力

- OneBot v11 / NapCat 接入
- `ws_server`、`ws_reverse`、`http_callback` 入站方式
- NapCat HTTP Action 消息发送与查询
- 内嵌 WebUI 管理后台
- 后台首次设置密码、登录、退出、修改密码
- 配置加载、校验、脱敏、保存与备份
- 多连接管理与探活
- 插件安装、升级、卸载、启停、重载、恢复
- Python-first `external_exec` 插件运行时
- AI 群聊回复、视觉摘要、记忆、反思、聊天记录与关系分析
- SQLite / MySQL / PostgreSQL 结构化存储
- local / Cloudflare R2 媒体存储

## 项目结构

| 路径 | 说明 |
| --- | --- |
| `main.go` | 程序入口 |
| `internal/app/botd/` | 启动编排与 CLI 子命令 |
| `internal/runtime/` | Runtime、连接、插件、AI、配置热应用协调 |
| `internal/config/` | 配置模型、默认值、校验、保存、脱敏 |
| `internal/admin/api/` | Admin API |
| `internal/admin/webui/` | 内嵌 WebUI 与前端资源 |
| `internal/adapter/` | OneBot / NapCat 适配层 |
| `internal/transport/messenger/` | 统一消息发送路由 |
| `internal/ai/` | AI 核心能力 |
| `internal/media/` | 媒体下载与存储 |
| `internal/plugin/host/` | 插件宿主与生命周期 |
| `internal/plugin/externalexec/` | 外部进程插件运行时 |
| `sdk/` | 插件 SDK |
| `templates/` | 插件模板 |
| `configs/` | 配置模板 |
| `docs/` | 专题文档 |
| `scripts/` | 发布打包与插件打包脚本 |

## 环境要求

运行预编译二进制时：

- NapCat 或其他 OneBot v11 实现
- Python 3，用于 Python `external_exec` 插件
- uv，可选但推荐，用于 Python 插件环境与依赖管理

自行编译时额外需要：

- Go 1.25+
- Node.js + npm，仅在需要重新构建 WebUI 前端时使用
- PowerShell 7，可选，用于执行多平台发布脚本

## 快速启动

### 使用预编译二进制

从 GitHub Releases 下载与你的系统匹配的压缩包：

```text
https://github.com/XiaoLozee/go-bot/releases
```

常见包名形态：

```text
go-bot-v0.1.0-windows-amd64.zip
go-bot-v0.1.0-linux-amd64.zip
go-bot-v0.1.0-linux-arm64.zip
go-bot-v0.1.0-darwin-arm64.zip
```

解压后复制配置模板：

```powershell
Copy-Item configs/config.example.yml configs/config.yml
```

启动：

```powershell
./go-bot --config configs/config.yml
```

Windows 下是：

```powershell
./go-bot.exe --config configs/config.yml
```

默认情况下，管理后台监听：

```text
http://127.0.0.1:8090/
```

### 使用源码运行

```powershell
git clone https://github.com/XiaoLozee/go-bot.git
Set-Location go-bot
Copy-Item configs/config.example.yml configs/config.yml
go run . --config configs/config.yml
```

如果未显式指定 `--config`，程序会依次尝试：

1. `configs/config.yml`
2. `configs/config.example.yml`

## 部署教程

### 1. Linux 二进制部署

推荐目录：

```bash
sudo mkdir -p /opt/go-bot
sudo unzip go-bot-v0.1.0-linux-amd64.zip -d /opt/go-bot
cd /opt/go-bot
sudo chmod +x ./go-bot
sudo cp configs/config.example.yml configs/config.yml
```

编辑配置：

```bash
sudo nano configs/config.yml
```

直接运行验证：

```bash
sudo ./go-bot --config configs/config.yml
```

确认可以启动后，可以交给 systemd 管理。发布包内包含 `go-bot.service` 示例文件，默认路径为 `/opt/go-bot`，默认用户为 `gobot`。

创建运行用户并安装服务：

```bash
id -u gobot >/dev/null 2>&1 || sudo useradd --system --home /opt/go-bot --shell /usr/sbin/nologin gobot
sudo chown -R gobot:gobot /opt/go-bot
sudo cp /opt/go-bot/go-bot.service /etc/systemd/system/go-bot.service
sudo systemctl daemon-reload
sudo systemctl enable --now go-bot
```

查看日志：

```bash
sudo journalctl -u go-bot -f
```

如果你的部署目录、用户或配置文件路径不同，需要同步修改 `go-bot.service` 里的：

```text
User=
Group=
WorkingDirectory=
ExecStart=
```

### 2. Windows 二进制部署

解压发布包：

```powershell
New-Item -ItemType Directory -Force -Path C:\go-bot | Out-Null
Expand-Archive .\go-bot-v0.1.0-windows-amd64.zip -DestinationPath C:\go-bot -Force
Copy-Item C:\go-bot\configs\config.example.yml C:\go-bot\configs\config.yml
```

启动：

```powershell
C:\go-bot\go-bot.exe --config C:\go-bot\configs\config.yml
```

如需常驻运行，可以使用 Windows 服务管理工具、计划任务，或你自己的进程守护方案。

### 3. Docker / 容器部署

当前仓库还没有内置 Dockerfile。生产部署优先使用预编译二进制或自行编译后的单机部署。

## 最小配置

`configs/config.example.yml` 只保留必要字段，其余字段由系统默认值补齐。完整展开示例见 `configs/config.full.example.yml`。

最小配置形态：

```yaml
app:
  owner_qq: ""

connections:
  - id: napcat-main
    enabled: true
    ingress:
      type: ws_server
      listen: ":8080"
    action:
      type: napcat_http
      base_url: http://127.0.0.1:3000

security:
  admin_auth:
    enabled: false
    password: ""
```

常用默认值：

- WebUI 默认开启，监听 `:8090`
- 存储默认使用 SQLite，路径 `./data/app.db`
- 媒体默认使用 local，路径 `./data/media`
- AI 默认关闭，需要在 WebUI 或配置文件中补充 provider
- 未配置插件不会写入配置文件

## WebUI

WebUI 是当前推荐的运维入口，默认访问：

```text
http://127.0.0.1:8090/
```

如果配置了 `server.webui.base_path`，例如 `/console`，则访问：

```text
http://127.0.0.1:8090/console/
```

当前页面包括：

- 概览
- AI 功能
- 连接
- 插件
- 审计
- 配置

常用能力：

- 首次设置后台密码
- 后台登录与退出
- 在线修改后台密码
- 查看运行时状态
- 管理 OneBot 连接
- 查看连接详情与探活
- 安装、升级、卸载、启停、重载插件
- 编辑结构化配置
- 查看 AI 记忆、聊天记录、群画像与关系图
- 触发 AI 反思与关系分析任务

## AI 能力

AI 是宿主内建核心能力，不作为插件实现。

当前支持：

- 群聊回复策略
- 私聊人格模板
- 上下文窗口
- 长期记忆与候选记忆
- 群画像、成员画像、关系边
- 聊天记录存储
- 图片媒体引用与视觉摘要
- 异步关系分析任务
- WebUI 观察与调试入口

结构化存储支持：

- SQLite
- MySQL
- PostgreSQL

媒体存储支持：

- local
- Cloudflare R2

## 插件系统

插件系统当前以 `external_exec` 为正式业务扩展主线。

特点：

- 插件与宿主进程隔离
- Python-first
- stdio JSON-RPC 协议
- 支持独立依赖环境
- 支持 WebUI 安装、升级、启停、重载、卸载
- 插件包支持 `.zip`、`.tar.gz`、`.tgz`
- Python 公共运行时 `_common` 会在启动和安装同步时自动检查与修复

仓库根目录的 `plugins/` 是本地运行时插件目录，默认不提交到 Git。发布二进制包默认也不内置本地插件目录；需要的业务插件建议通过 WebUI 上传插件包安装。

### 创建插件模板

```powershell
go run . scaffold external-plugin --id hello_echo --name "Hello Echo" --description "my first external_exec plugin"
```

默认输出到：

```text
plugins/hello_echo/
```

可用参数：

```text
--template python_echo
--output <dir>
--force
--list-templates
```

更多规范见：

- `docs/external-exec-plugin-package-v1.md`
- `docs/plugin-development-guide-v1.md`

## 构建、测试与发布打包

构建 Go 代码：

```powershell
go build ./...
```

运行测试：

```powershell
go test ./...
```

格式化 Go 代码：

```powershell
gofmt -w main.go
Get-ChildItem internal -Recurse -Filter *.go | ForEach-Object { gofmt -w $_.FullName }
Get-ChildItem sdk -Recurse -Filter *.go | ForEach-Object { gofmt -w $_.FullName }
```

构建 WebUI 前端：

```powershell
Set-Location internal/admin/webui/frontend
npm install
npm run build
```

### 自行编译单平台二进制

当前平台：

```powershell
go build -trimpath -ldflags "-s -w" -o build/go-bot .
```

Windows：

```powershell
go build -trimpath -ldflags "-s -w" -o build/go-bot.exe .
```

### 多平台发布包

仓库提供多平台发布脚本：

```text
scripts/build_release.ps1
```

默认构建以下平台：

- `windows/amd64`
- `windows/arm64`
- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`

生成发布包：

```powershell
pwsh ./scripts/build_release.ps1 -Version v0.1.0
```

只构建部分平台：

```powershell
pwsh ./scripts/build_release.ps1 -Version v0.1.0 -Platforms linux/amd64,linux/arm64
```

发布前顺便重新构建 WebUI：

```powershell
pwsh ./scripts/build_release.ps1 -Version v0.1.0 -BuildFrontend
```

输出目录：

```text
build/release/dist/       unpacked release directories
build/release/packages/   zip packages and SHA256SUMS.txt
```

发布包会包含：

- `go-bot` 或 `go-bot.exe`
- `configs/config.example.yml`
- `configs/config.full.example.yml`
- `README.md`
- `ARCHITECTURE.md`
- `SPEC.md`
- `contributing.md`
- `docs/`
- `go-bot.service`

## 本地数据与发布注意事项

以下内容属于本地运行数据或私有配置，不应提交到仓库：

- `configs/config.yml`
- `configs/.bak/`
- `data/`
- `plugins/`
- `*.db`
- Python venv 与缓存目录
- 前端 `node_modules/`

发布到 GitHub 前建议至少运行：

```powershell
go test ./...
go build ./...
```

如果修改了前端，还需要运行：

```powershell
Set-Location internal/admin/webui/frontend
npm run build
```

## 文档入口

- `SPEC.md`：产品范围与功能规格
- `ARCHITECTURE.md`：系统架构与模块边界
- `contributing.md`：开发约定
- `docs/config-schema-v1.md`：配置结构说明
- `docs/onebot-adapter-v1.md`：OneBot / NapCat 接入说明
- `docs/webui-backend-contract-v1.md`：WebUI 后端契约
- `docs/external-exec-plugin-package-v1.md`：插件包规范
- `docs/plugin-development-guide-v1.md`：插件开发说明
- `docs/ai-upgrade-v2.md`：AI 能力演进说明

## 当前边界

当前暂不覆盖：

- 多租户
- SaaS 化部署
- 分布式 Worker 集群
- 插件市场
- 视频 / 语音纳入 AI 主消息模型
- 私聊人格自动孵化

## License

本项目采用 GPL-3.0 协议发布，详见 `LICENSE`。
