# OneBot / NapCat 适配层设计 v1

> 状态：Implemented / 持续补强中  
> 依据：
> - `napcat_onebot_11_http_api.yaml`
> - NapCat 网页文档：https://napcat.apifox.cn/395455119e0
> - `ARCHITECTURE.md`
> - `SPEC.md`

---

## 1. 目标

本文档用于细化 Go-bot 新架构中的 **OneBot / NapCat 适配层**，明确：

1. 宿主如何对接 NapCat
2. 哪些能力属于 Action 调用
3. 哪些能力属于 Event Ingress
4. 宿主内部模型如何映射到 OneBot v11
5. 当前支持范围与后续补强点是什么

---

## 2. 设计结论

当前实现已经将适配层拆成两个独立方向：

### 2.1 Action 方向

负责主动调用 NapCat OneBot v11 接口：

- 发送消息
- 撤回消息
- 查询状态
- 查询登录信息
- 查询用户/群成员信息

### 2.2 Event 方向

负责接收 OneBot 推送事件：

- 私聊消息
- 群聊消息
- notice
- request
- meta event

---

## 3. 为什么要拆成两层

当前你手里的 NapCat 文档对 **HTTP Action** 描述非常完整，但对事件推送协议并不完整。

所以分层后：

- **Action Client** 已经作为主动调用路径落地
- **Ingress** 已经覆盖 `ws_server` / `ws_reverse` / `http_callback`
- 后续增强可以继续围绕真实接口能力逐步补齐，而不必回退到旧耦合结构

这符合：

- **KISS**：先把最确定的部分做出来
- **YAGNI**：先不提前实现所有接入方式
- **SOLID**：输入输出职责分离

---

## 4. 适配层总体结构

```text
internal/adapter/
  types.go
  onebotv11/
    httpclient/
      client.go
    ingress/
      http_callback.go
      parse.go
      ws_action_client.go
      ws_reverse.go
      ws_server.go
```

---

## 5. Action Client 设计

## 5.1 当前支持范围

### 必做接口

- `/send_msg`
- `/send_group_msg`
- `/send_private_msg`
- `/delete_msg`
- `/get_msg`
- `/get_login_info`
- `/get_status`

### 建议同时支持

- `/get_stranger_info`
- `/get_group_member_list`
- `/get_group_member_info`

### 暂缓

- 文件上传/下载
- 合并转发增强字段全量支持
- AI 扩展
- 频道接口
- 系统扩展

---

## 5.2 宿主内部接口

```go
type ActionClient interface {
    ID() string

    SendMessage(ctx context.Context, req SendMessageRequest) (*SendMessageResult, error)
    DeleteMessage(ctx context.Context, messageID string) error
    GetMessage(ctx context.Context, messageID string) (*MessageDetail, error)

    GetLoginInfo(ctx context.Context) (*LoginInfo, error)
    GetStatus(ctx context.Context) (*BotStatus, error)

    GetStrangerInfo(ctx context.Context, userID string) (*UserInfo, error)
    GetGroupMemberList(ctx context.Context, groupID string) ([]GroupMemberInfo, error)
    GetGroupMemberInfo(ctx context.Context, groupID, userID string) (*GroupMemberInfo, error)
}
```

---

## 5.3 NapCat HTTP Client 配置

```yaml
connections:
  - id: napcat-main
    platform: onebot_v11
    action:
      type: napcat_http
      base_url: http://127.0.0.1:3000
      timeout_ms: 10000
      access_token: ""
```

### 字段说明

- `id`: 连接标识
- `platform`: 当前固定为 `onebot_v11`
- `base_url`: NapCat HTTP API 地址
- `timeout_ms`: 单次请求超时
- `access_token`: 如 NapCat 启用了鉴权，则附带

---

## 5.4 通用响应模型

根据 NapCat 文档，统一响应可抽象为：

```go
type BaseResponse[T any] struct {
    Status  string `json:"status"`
    RetCode int64  `json:"retcode"`
    Data    T      `json:"data"`
    Message string `json:"message"`
    Wording string `json:"wording"`
    Stream  string `json:"stream"`
}
```

### 统一判断规则

- `status != "ok"` 视为失败
- `retcode != 0` 视为业务失败
- `data` 结构按接口分别解析

---

## 5.5 发送消息映射

### 宿主内部请求

```go
type SendMessageRequest struct {
    ConnectionID string
    ChatType     string // group/private
    UserID       string
    GroupID      string
    Segments     []message.Segment
    AutoEscape   bool
}
```

### NapCat 映射策略

#### 群聊

- 优先调用 `/send_group_msg`

请求体：

```json
{
  "group_id": "123456",
  "message": [...]
}
```

#### 私聊

- 优先调用 `/send_private_msg`

请求体：

```json
{
  "user_id": "123456789",
  "message": [...]
}
```

#### 通用发送

宿主内部可保留 `/send_msg` 作为 fallback：

- 用于未来更通用的路由发送
- 用于某些目标类型不方便在高层区分时兜底

---

## 5.6 消息段模型映射

NapCat 文档明确支持 `OB11MessageMixType`：

- string
- 单消息段对象
- 消息段数组

为了保持宿主统一性，建议：

> **宿主内部统一使用 `[]Segment`，落到适配层时再转成 OneBot 消息段数组。**

### 宿主建议模型

```go
type Segment struct {
    Type string
    Data map[string]any
}
```

### 当前建议支持的消息段

- `text`
- `at`
- `reply`
- `image`
- `face`
- `record`
- `video`
- `json`
- `music`
- `node`（合并转发）

### 当前最小集合

如果要先快跑，至少支持：

- `text`
- `at`
- `reply`
- `image`

---

## 5.7 查询类接口映射

### `/get_login_info`

用于：

- WebUI 显示当前 bot 账号信息
- 连接初始化时写入元信息

宿主映射：

```go
type LoginInfo struct {
    UserID   string
    Nickname string
}
```

### `/get_status`

用于：

- 健康检查
- WebUI 仪表盘
- 自动重连判断

宿主映射：

```go
type BotStatus struct {
    Online bool
    Good   bool
    Stat   map[string]any
}
```

### `/get_msg`

用于：

- 回复链路增强
- 调试消息详情
- 审计与问题排查

### `/get_stranger_info`

用于：

- 插件获取昵称
- 菜单/AI/欢迎语场景

### `/get_group_member_list` / `/get_group_member_info`

用于：

- 权限判断
- 群成员信息展示
- 群管理类插件

---

## 5.8 错误处理策略

适配层不要把 NapCat 的原始错误散落到业务插件。

建议统一包装：

```go
type ActionError struct {
    Endpoint string
    RetCode  int64
    Message  string
    Wording  string
    Cause    error
}
```

### 重试策略

当前建议：

- 默认 **不自动重试业务错误**
- 网络超时可进行一次短重试
- `get_status` 可由健康检查周期性调用

### 日志策略

记录：

- endpoint
- connection_id
- retcode
- latency
- request id

避免：

- 打全量敏感参数
- 打 access token

---

## 6. Event Ingress 设计

## 6.1 当前策略

由于当前掌握的网页文档主要覆盖 Action 接口，Event Ingress 先抽象，不写死具体实现。

建议接口：

```go
type EventIngress interface {
    ID() string
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Events() <-chan event.Event
    Health() IngressHealth
}
```

---

## 6.2 后续计划支持的 Ingress 类型

### A. 正向 WebSocket

适用于：

- Bot 主程序暴露 WS
- NapCat 主动连接过来

### B. 反向 WebSocket

适用于：

- Bot 主程序主动连接 NapCat

### C. HTTP 回调

适用于：

- NapCat 通过 HTTP POST 上报事件

---

## 6.3 统一事件转换目标

无论事件来自 WS 还是 HTTP，都应转成宿主统一事件模型：

```go
type Event struct {
    ID           string
    ConnectionID string
    Platform     string
    Kind         string
    ChatType     string
    UserID       string
    GroupID      string
    MessageID    string
    RawText      string
    Segments     []Segment
    Timestamp    time.Time
    RawPayload   json.RawMessage
    Meta         map[string]string
}
```

---

## 6.4 当前强支持事件

当前建议只把以下事件纳入“强类型支持”：

- 私聊消息
- 群聊消息

其他类型先保留：

- notice
- request
- meta event

先以原始 `RawPayload + Kind + Meta` 透传。

这样做的好处：

- 快速支撑插件迁移
- 避免一开始穷举所有 OneBot 事件

---

## 7. Action 与 Ingress 的组合关系

每个连接配置可以由两部分组成：

```yaml
connections:
  - id: napcat-main
    platform: onebot_v11
    ingress:
      type: ws_server
      listen: ":8080"
    action:
      type: napcat_http
      base_url: http://127.0.0.1:3000
      timeout_ms: 10000
```

即：

- `ingress` 负责收事件
- `action` 负责发动作

这样一来，未来可以出现以下组合：

1. `ws_server + http_action`
2. `ws_reverse + http_action`
3. `http_callback + http_action`

---

## 8. 适配层与宿主其他模块的关系

```text
Ingress --> EventBus --> PluginHost
                          |
                          v
                      Messenger
                          |
                          v
                     ActionClient
                          |
                          v
                        NapCat
```

### 流程说明

1. Ingress 收到 OneBot 事件
2. 转成统一 `Event`
3. EventBus 分发给 PluginHost
4. 插件通过 Messenger 发送消息
5. Messenger 选择对应 ActionClient
6. ActionClient 调 NapCat HTTP API

---

## 9. 当前实现与待补强

### 9.1 已落地

- NapCat HTTP Action client 已作为当前主动调用路径接入宿主
- Ingress 已支持 `ws_server`、`ws_reverse`、`http_callback`
- 事件解析、连接探测与登录信息回填已经进入 runtime 主流程
- 当前消息发送链路已经覆盖宿主、插件与 Messenger 的主路径

### 9.2 待补强

- 将文档中的接口清单与 `httpclient/client.go` 的真实实现逐项对表
- 继续补齐更完整的消息段映射说明与边界行为
- 针对非核心 Action 接口补更多测试和契约说明
- 如后续需要，再扩展文件接口、频道接口与更细粒度事件类型

---

## 10. 设计取舍

## 10.1 为什么不直接复用旧 `botapi`

因为旧 `botapi`：

- 同时承担连接、发送、同步响应、消息构造等多重职责
- 强依赖全局单例
- 不适合多连接与可测试设计

新适配层应拆开：

- `Ingress`
- `ActionClient`
- `Messenger`
- `Mapper`

---

## 10.2 为什么内部不继续用 OneBot 原生结构体

因为那会导致：

- 插件强绑定 OneBot
- 后续难以扩展其他协议
- WebUI 和日志层也要理解原始结构

统一模型更利于演进。

---

## 10.3 为什么当前只把少量消息段作为强保障

因为当前核心目标是：

- 宿主主路径稳定运行
- 现有业务插件持续可迁移、可维护
- WebUI / Runtime 能稳定观察状态

而不是立即追求“旧系统所有消息段全量兼容”。

---

## 11. 对齐建议

基于当前适配层实现，建议继续做三件事：

1. 让 `docs/config-schema-v1.md` 与连接模型保持一致
2. 让 `internal/adapter/onebotv11/ingress` 的事件路径补齐更明确的契约说明
3. 让 `internal/adapter/onebotv11/httpclient` 的接口覆盖与测试清单持续完善
