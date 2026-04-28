# Go-bot 配置模型设计 v1

> 状态：Current / 持续对齐实现  
> 依据：
> - `ARCHITECTURE.md`
> - `SPEC.md`
> - `docs/onebot-adapter-v1.md`
> - 当前仓库中的 `configs/config.example.yml`

---

## 1. 文档目标

本文件用于定义 Go-bot 重构后的**统一配置模型**，解决当前项目中：

- 配置键风格不统一
- 插件配置与宿主配置混杂
- 敏感信息与普通配置混放
- 热重载边界不清晰
- 多连接扩展困难

的问题。

本文档的目标是让后续代码开发时，有一个稳定、可校验、可演进的配置基础。

---

## 2. 设计目标

新的配置系统需要满足：

1. **结构清晰**
   - 宿主、连接、插件、管理后台、存储分层

2. **易于扩展**
   - 支持多连接
   - 支持多插件
   - 支持未来外部插件

3. **易于校验**
   - 启动时能发现错误
   - WebUI 保存时能给出友好提示

4. **支持热重载**
   - 某些配置可热更新
   - 某些配置只能冷启动生效

5. **敏感信息可控**
   - token / password / api_key 不应和普通显示配置混为一谈

---

## 3. 配置分层

建议整体分为 6 层：

```yaml
app:          # 应用级配置
server:       # 宿主管理接口 / WebUI 服务
storage:      # 数据与日志存储
connections:  # 对外连接配置
plugins:      # 插件配置
security:     # 安全、认证、密钥等
```

---

## 4. 配置来源优先级

建议采用以下优先级：

1. **环境变量**
2. **本地配置文件**
3. **默认值**

即：

```text
ENV > configs/config.yml > defaults
```

### 原因

- 方便部署时注入敏感信息
- 避免把密钥硬编码进仓库
- 便于 Docker / systemd / CI 环境使用

---

## 5. 顶层结构

建议最终配置文件结构如下：

```yaml
app:
  name: go-bot
  env: dev
  owner_qq: ""
  data_dir: ./data
  log_level: info

server:
  admin:
    enabled: true
    listen: ":8090"
    enable_pprof: false
  webui:
    enabled: true
    base_path: /

storage:
  sqlite:
    path: ./data/app.db
  logs:
    dir: ./data/logs
    max_size_mb: 50
    max_backups: 7
    max_age_days: 30

connections:
  - id: napcat-main
    enabled: true
    platform: onebot_v11
    ingress:
      type: ws_server
      listen: ":8080"
    action:
      type: napcat_http
      base_url: http://127.0.0.1:3000
      timeout_ms: 10000
      access_token: ""

plugins:
  - id: menu_hint
    kind: external_exec
    enabled: true
    config: {}

  - id: video_parser
    kind: external_exec
    enabled: false
    config:
      video_max_size: 50

  - id: test
    kind: builtin
    enabled: false
    config: {}

security:
  admin_auth:
    enabled: false
    password: ""
```

---

## 6. 顶层字段说明

## 6.1 `app`

应用级基础配置。

```yaml
app:
  name: go-bot
  env: dev
  owner_qq: ""
  data_dir: ./data
  log_level: info
```

### 字段说明

- `name`
  - 机器人昵称
  - 默认：`go-bot`
  - 同时会作为 AI 默认 `bot_name` 的同步来源

- `env`
  - 运行环境
  - 枚举建议：`dev | test | prod`

- `owner_qq`
  - 机器人主人 QQ
  - 建议填写纯数字字符串，便于后续插件或管理逻辑读取

- `data_dir`
  - 运行时数据目录
  - 用于存放 DB、日志、缓存等

- `log_level`
  - 日志级别
  - 枚举建议：`debug | info | warn | error`

### 热重载策略

| 字段 | 是否可热重载 |
|---|---|
| `name` | 否 |
| `env` | 否 |
| `owner_qq` | 否 |
| `data_dir` | 否 |
| `log_level` | 是 |

---

## 6.2 `server`

用于配置宿主自己的管理接口与静态 UI 服务。

```yaml
server:
  admin:
    enabled: true
    listen: ":8090"
    enable_pprof: false
  webui:
    enabled: true
    base_path: /
```

### `server.admin`

- `enabled`
  - 是否启用管理 API

- `listen`
  - 监听地址
  - 例如：`:8090`

- `enable_pprof`
  - 是否暴露 pprof
  - 默认建议关闭

### `server.webui`

- `enabled`
  - 是否启用 WebUI 静态资源服务

- `base_path`
  - UI 根路径
  - 例如 `/` 或 `/console`

### 热重载策略

| 字段 | 是否可热重载 |
|---|---|
| `server.admin.enabled` | 否 |
| `server.admin.listen` | 否 |
| `server.admin.enable_pprof` | 否 |
| `server.webui.enabled` | 否 |
| `server.webui.base_path` | 否 |

原因：这些字段涉及 HTTP Server 生命周期，当前统一按**冷重启生效**处理。

---

## 6.3 `storage`

用于定义默认存储实现。

```yaml
storage:
  sqlite:
    path: ./data/app.db
  logs:
    dir: ./data/logs
    max_size_mb: 50
    max_backups: 7
    max_age_days: 30
```

### `storage.sqlite`

- `path`
  - SQLite 文件路径

### `storage.logs`

- `dir`
  - 日志目录

- `max_size_mb`
  - 单文件大小上限

- `max_backups`
  - 最多保留文件数

- `max_age_days`
  - 日志保留天数

### 热重载策略

| 字段 | 是否可热重载 |
|---|---|
| `storage.sqlite.path` | 否 |
| `storage.logs.*` | 建议否 |

当前不建议动态切换底层存储位置。

---

## 6.4 `connections`

这是新架构最关键的配置之一，用来支撑多连接。

```yaml
connections:
  - id: napcat-main
    enabled: true
    platform: onebot_v11
    ingress:
      type: ws_server
      listen: ":8080"
    action:
      type: napcat_http
      base_url: http://127.0.0.1:3000
      timeout_ms: 10000
      access_token: ""
```

### 公共字段

- `id`
  - 连接唯一标识
  - 必须唯一
  - 只能包含：字母、数字、`-`、`_`

- `enabled`
  - 是否启用该连接

- `platform`
  - 当前固定：`onebot_v11`

### `ingress`

定义事件输入端。

#### 当前允许的 `type`

- `ws_server`
- `ws_reverse`
- `http_callback`

#### `ws_server`

```yaml
ingress:
  type: ws_server
  listen: ":8080"
```

字段：

- `listen`: 监听地址

#### `ws_reverse`

```yaml
ingress:
  type: ws_reverse
  url: "ws://127.0.0.1:3001/ws"
  retry_interval_ms: 5000
```

字段：

- `url`
- `retry_interval_ms`

#### `http_callback`

```yaml
ingress:
  type: http_callback
  listen: ":8081"
  path: "/callback"
```

字段：

- `listen`
- `path`

### `action`

定义主动调用端。

#### 当前允许的 `type`

- `napcat_http`

```yaml
action:
  type: napcat_http
  base_url: http://127.0.0.1:3000
  timeout_ms: 10000
  access_token: ""
```

字段：

- `base_url`
- `timeout_ms`
- `access_token`

### 热重载策略

| 字段 | 是否可热重载 |
|---|---|
| `connections[].enabled` | 是 |
| `connections[].ingress.*` | 否（当前） |
| `connections[].action.base_url` | 否（当前） |
| `connections[].action.timeout_ms` | 是 |
| `connections[].action.access_token` | 是 |

#### 建议行为

- `enabled` 变更：
  - 允许触发连接启停

- `timeout_ms` / `access_token`：
  - 允许更新 client 配置

- `listen` / `url` / `base_url`：
  - 当前统一要求冷重启

---

## 6.5 `plugins`

新架构下插件配置统一改成列表，不再用散乱的 `bot.function.xxx`。

```yaml
plugins:
  - id: menu_hint
    kind: external_exec
    enabled: true
    config: {}

  - id: video_parser
    kind: external_exec
    enabled: false
    config:
      video_max_size: 50

  - id: test
    kind: builtin
    enabled: false
    config: {}
```

### 公共字段

- `id`
  - 插件唯一标识

- `kind`
  - 当前建议允许：
    - `builtin`
    - `external_exec`

- `enabled`
  - 是否启用该插件

- `config`
  - 插件私有配置对象

### 当前插件约束

- `id` 必须唯一
- `id` 必须能在插件注册表中找到
- `kind=builtin` 时必须存在匹配内建插件
- `kind=external_exec` 时必须存在匹配的 external descriptor / manifest

### 热重载策略

| 字段 | 是否可热重载 |
|---|---|
| `plugins[].enabled` | 是 |
| `plugins[].config` | 是（插件自行决定如何应用） |
| `plugins[].kind` | 否 |
| `plugins[].id` | 否 |

### 设计原因

这样后续实现插件启停、重载、WebUI 开关会很自然。

---

## 6.6 `security`

安全配置单独抽出，避免和普通业务配置混杂。

```yaml
security:
  admin_auth:
    enabled: false
    password: ""
```

### `security.admin_auth`

- `enabled`
  - 是否启用后台认证

- `password`
  - 后台密码
  - 可通过配置文件或环境变量输入明文
  - 宿主在保存配置时会自动转换为哈希；WebUI / API 只返回脱敏值

### 后续可扩展

- JWT 密钥
- session secret
- 多用户
- RBAC

### 热重载策略

| 字段 | 是否可热重载 |
|---|---|
| `security.admin_auth.enabled` | 否 |
| `security.admin_auth.password` | 否 |

---

## 7. 插件配置规范

为了避免未来插件配置继续失控，建议统一约束：

### 7.1 命名规范

- 插件 ID 全小写
- 使用下划线风格
- 如：`menu_hint`、`video_parser`

### 7.2 插件私有配置建议

插件配置一律放在：

```yaml
plugins:
  - id: xxx
    config:
      ...
```

不再允许在顶层随意新增：

```yaml
video_parser:
  ...
```

### 7.3 插件配置读取方式

插件不应再直接全局读 Viper 全量配置。  
而应通过 `PluginConfigReader` 读取自己的局部配置。

例如：

```go
cfg := env.Config.Unmarshal(&VideoParserConfig{})
```

这样更符合 DIP 和 SRP。

---

## 8. 敏感配置策略

当前项目里有：

- `api_key`
- `password`
- 外部接口地址

这些都属于敏感配置或半敏感配置。

### 8.1 建议原则

1. 仓库中默认只提交 `configs/config.example.yml`
2. `configs/config.yml` 保持在 `.gitignore`
3. 优先支持环境变量覆盖敏感字段

### 8.2 环境变量映射建议

例如：

```text
GOBOT_APP_ENV
GOBOT_SERVER_ADMIN_LISTEN
GOBOT_CONNECTIONS__0__ACTION__ACCESS_TOKEN
GOBOT_SECURITY__ADMIN_AUTH__PASSWORD
```

如果后续环境变量索引写法太难用，也可以支持：

```text
GOBOT_ADMIN_PASSWORD
GOBOT_NAPCAT_MAIN_ACCESS_TOKEN
```

通过自定义映射完成。

### 8.3 WebUI 展示规则

在 WebUI 或 API 返回中：

- `password`
- `access_token`
- `api_key`

默认返回脱敏值，不直接回传原文。

---

## 9. 校验规则

## 9.1 顶层校验

- `app` 必须存在
- `server` 可选，但默认需补齐
- `connections` 至少要有一个启用连接

## 9.2 连接校验

### 必须满足

- `id` 唯一
- `platform` 为支持值
- `ingress.type` 为支持值
- `action.type` 为支持值

### 条件校验

- `ingress.type=ws_server` 时必须有 `listen`
- `ingress.type=ws_reverse` 时必须有 `url`
- `ingress.type=http_callback` 时必须有 `listen` 和 `path`
- `action.type=napcat_http` 时必须有 `base_url`

## 9.3 插件校验

- `plugins[].id` 唯一
- `plugins[].kind` 必须为支持值
- `plugins[].enabled=true` 时，该插件必须已注册

## 9.4 服务端口冲突校验

建议启动前检查：

- `server.admin.listen`
- 各 `connections[].ingress.listen`

避免重复绑定相同端口。

---

## 10. 冷重载 / 热重载边界

## 10.1 冷启动生效字段

以下字段当前统一要求重启：

- `app.data_dir`
- `server.*`
- `storage.*`
- `connections[].ingress.listen/url/path`
- `connections[].action.base_url`
- `plugins[].id`
- `plugins[].kind`

## 10.2 热重载字段

以下字段当前允许热更新：

- `app.log_level`
- `connections[].enabled`
- `connections[].action.timeout_ms`
- `connections[].action.access_token`
- `plugins[].enabled`
- `plugins[].config`

### 热重载行为建议

- 连接 `enabled`：
  - 触发连接启动/停止

- 插件 `enabled`：
  - 触发插件 Start/Stop

- 插件 `config`：
  - 先由宿主替换配置快照
  - 插件是否立即生效由插件自己控制

---

## 11. 从旧配置迁移到新配置

当前旧配置大致结构：

```yaml
server:
bot:
  nickname:
  master:
  function:
    ...
video_parser:
  video_max_size:
```

建议迁移为：

### 旧 → 新映射

| 旧字段 | 新字段 |
|---|---|
| `server.host` + `server.port` | `connections[].ingress.listen` |
| `bot.nickname` | 暂不放全局，作为连接元信息或默认 bot profile |
| `bot.master` | 插件私有配置 |
| `bot.function.xxx` | `plugins[]` |
| `video_parser.video_max_size` | `plugins[id=video_parser].config.video_max_size` |

### 说明

#### `bot.master`

旧项目里“master”是业务概念，不是宿主概念。  
因此建议迁移到对应插件自己的配置里，例如：

```yaml
plugins:
  - id: freqtrade
    enabled: true
    config:
      master_user_id: "2686020087"
```

这更符合 SRP。

---

## 12. 配置文件建议

建议仓库中维护两个文件：

### 12.1 `configs/config.example.yml`

用途：

- 作为模板
- 用于文档示例
- 可提交到仓库

### 12.2 `configs/config.yml`

用途：

- 实际运行
- 包含环境私有信息
- 不提交

---

## 13. Go 结构体建议

建议宿主内部配置模型：

```go
type Config struct {
    App         AppConfig          `mapstructure:"app"`
    Server      ServerConfig       `mapstructure:"server"`
    Storage     StorageConfig      `mapstructure:"storage"`
    Connections []ConnectionConfig `mapstructure:"connections"`
    Plugins     []PluginConfig     `mapstructure:"plugins"`
    Security    SecurityConfig     `mapstructure:"security"`
}
```

子结构建议按模块拆文件：

- `app.go`
- `server.go`
- `storage.go`
- `connection.go`
- `plugin.go`
- `security.go`

---

## 14. 当前落地清单

### 必须完成

- [ ] 定义 `Config` 总结构
- [ ] 定义 `ConnectionConfig`
- [ ] 定义 `PluginConfig`
- [ ] 定义 `SecurityConfig`
- [ ] 加载默认值
- [ ] 做字段校验
- [ ] 支持环境变量覆盖

### 建议完成

- [ ] 热重载字段分类
- [ ] 配置变更 diff
- [ ] 输出配置校验错误列表

### 暂缓

- [ ] 配置版本历史
- [ ] 配置回滚
- [ ] 远程配置中心

---

## 15. 当前结论

新配置系统应采用：

1. **顶层分层**
2. **连接列表化**
3. **插件列表化**
4. **安全配置单独隔离**
5. **热重载边界明确**

这样才能支撑：

- 多连接
- WebUI 管理
- 插件启停
- 后续外部插件

---

## 16. 下一步建议

基于当前文档，下一步建议继续补：

1. `configs/config.example.yml`
2. `internal/config/model.go`
3. `internal/config/validator.go`

其中：

- `config.example.yml` 用来反向验证本配置文档是否可用
- `model.go` / `validator.go` 用来确认字段、默认值与校验规则是否与文档持续一致
