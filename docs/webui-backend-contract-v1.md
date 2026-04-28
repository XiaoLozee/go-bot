# WebUI Backend Contract v1

状态：Implemented / 持续收口中  
目标：为当前 WebUI 提供稳定的后端读取契约，避免前端直接耦合运行时内部实现。

---

## 1. 设计原则

1. WebUI 只消费 Admin API
2. 统一覆盖运行状态、配置、AI 与插件管理接口
3. 配置返回默认脱敏
4. 列表接口与详情接口分离
5. 管理后台支持首次初始化密码、登录态校验与后台密码修改

---

## 2. 第一版核心接口

### 2.1 Meta

- `GET /api/admin/meta`

返回：

- 机器人昵称（字段仍为 `app_name`）
- 环境
- 机器人主人 QQ
- Admin/WebUI 开关
- WebUI 基础路径
- WebUI 可用能力（管理元信息，前端可按需展示）

### 2.2 Runtime

- `GET /api/admin/runtime`

返回：

- 宿主状态
- 启动时间
- 连接数
- 插件数

### 2.3 Connections

- `GET /api/admin/connections`
- `GET /api/admin/connections/{id}`

列表页返回运行快照；详情页额外返回该连接的脱敏配置。

### 2.4 Plugins

- `GET /api/admin/plugins`
- `GET /api/admin/plugins/{id}`
- `POST /api/admin/plugins/{id}/install`
- `POST /api/admin/plugins/{id}/start`
- `POST /api/admin/plugins/{id}/reload`
- `POST /api/admin/plugins/{id}/recover`
- `POST /api/admin/plugins/{id}/stop`
- `POST /api/admin/plugins/{id}/uninstall`

列表页返回安装/配置/运行状态；详情页额外返回插件脱敏配置，以及 external_exec 插件的运行时信息（PID、最近退出信息、最近日志等），并支持安装 / 卸载 / 重载 / 手动恢复等动作。

### 2.5 Audit

- `GET /api/admin/audit?limit=50`

支持查询参数：

- `limit`
- `category`
- `action`
- `result`
- `target`
- `q`

返回最近的后台操作审计日志，当前为**内存保留型**，默认返回最新 50 条，最多 200 条；当带过滤参数时会先在最近保留窗口内筛选，再返回命中项。

每条审计日志包含：

- `at`
- `category`
- `action`
- `target`
- `result`
- `summary`
- `detail`
- `username`
- `remote_addr`
- `method`
- `path`

### 2.6 Config

- `GET /api/admin/config`

返回整份脱敏后的当前配置，供 WebUI 配置页只读展示。

### 2.7 Bootstrap

- `GET /api/admin/webui/bootstrap`

用于 WebUI 首屏初始化，一次返回：

- `meta`
- `runtime`
- `connections`
- `plugins`
- `config`
- `generated_at`

### 2.8 Admin Auth

- `GET /api/admin/auth/state`
- `POST /api/admin/auth/setup`
- `POST /api/admin/auth/login`
- `POST /api/admin/auth/logout`
- `POST /api/admin/auth/password`

用于 WebUI 首次进入的初始化设置、密码登录、退出登录与后台密码修改。

---

## 3. 返回约定

### 3.1 脱敏字段

以下字段默认不回传明文：

- `password`
- `access_token`
- `api_key`

统一返回：

```json
"******"
```

### 3.2 插件列表字段

建议前端使用以下字段：

- `id`
- `name`
- `version`
- `description`
- `author`
- `kind`
- `builtin`
- `configured`
- `enabled`
- `state`
- `last_error`

### 3.2.1 插件详情运行时字段

`GET /api/admin/plugins/{id}` 在详情页额外返回：

- `runtime.running`
- `runtime.restarting`
- `runtime.auto_restart`
- `runtime.restart_count`
- `runtime.consecutive_failures`
- `runtime.next_restart_at`
- `runtime.circuit_open`
- `runtime.circuit_reason`
- `runtime.pid`
- `runtime.started_at`
- `runtime.stopped_at`
- `runtime.exit_code`
- `runtime.last_error`
- `runtime.recent_logs`

其中 `runtime.recent_logs` 主要用于 external_exec 生命周期排障，当前会保留最近一小段宿主侧观察到的日志：

- 生命周期日志
- 插件主动上报的 `log` 消息
- `stderr` 输出

### 3.3 连接列表字段

建议前端使用以下字段：

- `id`
- `platform`
- `enabled`
- `ingress_type`
- `action_type`
- `state`
- `ingress_state`
- `online`
- `good`
- `connected_clients`
- `observed_events`
- `last_event_at`
- `self_id`
- `self_nickname`
- `last_error`
- `updated_at`

---

## 4. Bootstrap 示例

```json
{
  "generated_at": "2026-04-18T12:00:00+08:00",
  "meta": {
    "app_name": "go-bot",
    "environment": "dev",
    "owner_qq": "123456789",
    "admin_enabled": true,
    "webui_enabled": true,
    "webui_base_path": "/",
    "capabilities": {
      "plugin_control": true,
      "plugin_recover": true,
      "audit_log": true,
      "connection_inspect": true,
      "connection_probe": true,
      "config_view": true,
      "config_validate": true,
      "config_save": true,
      "admin_auth": true,
      "webui_bootstrap": true
    }
  },
  "runtime": {
    "state": "running",
    "app_name": "go-bot",
    "environment": "dev",
    "connections": 1,
    "plugins": 8
  },
  "connections": [],
  "plugins": [],
  "config": {}
}
```

---

## 5. 前端页面映射建议

### 5.1 Dashboard

使用：

- `/api/admin/webui/bootstrap`

首次进入前先调用：

- `/api/admin/auth/state`

### 5.2 连接页

使用：

- `/api/admin/connections`
- `/api/admin/connections/{id}`
- `/api/admin/connections/{id}/probe`

`POST /api/admin/connections/{id}/probe` 会立即重新拉取该连接的登录信息与在线状态，并返回最新的连接详情。

### 5.3 插件页

使用：

- `/api/admin/plugins`
- `/api/admin/plugins/{id}`
- `/api/admin/plugins/{id}/install`
- `/api/admin/plugins/{id}/start`
- `/api/admin/plugins/{id}/reload`
- `/api/admin/plugins/{id}/recover`
- `/api/admin/plugins/{id}/stop`
- `/api/admin/plugins/{id}/uninstall`

### 5.4 审计页

使用：

- `/api/admin/audit`

建议前端按 `category / action / result / username / at` 维度展示最近操作流水，优先突出失败项与鉴权相关事件。

当前 WebUI 第一版已落地：

- 分类筛选
- 结果筛选
- 目标 / 摘要 / 账号 / 来源地址关键字搜索
- 最近操作概览卡片

### 5.5 配置页

使用：

- `/api/admin/config`
- `/api/admin/config/validate`
- `/api/admin/config/save`

建议前端在配置页同时提供：

- 原始 JSON 草稿编辑
- 结构化插件编辑器（按插件维度修改 `plugins` 列表）

#### `/api/admin/config/save` v1 响应

```json
{
  "accepted": true,
  "persisted": true,
  "restart_required": true,
  "plugin_changed": true,
  "non_plugin_changed": false,
  "hot_apply_attempted": true,
  "hot_applied": false,
  "hot_apply_error": "启动插件 menu_hint 失败: boom",
  "source_path": "configs/config.example.yml",
  "path": "configs/config.yml",
  "backup_path": "configs/.bak/config-20260418-120000.000.yml",
  "saved_at": "2026-04-18T12:00:00+08:00",
  "message": "插件配置已保存，但热应用失败，请重启实例后生效",
  "normalized_config": {}
}
```

说明：

- 保存前会先做与 `validate` 一致的规范化校验
- 若目标文件已存在，会先在同目录 `.bak/` 下生成时间戳备份
- 若仅 `plugins` 段发生变化，则会尝试对插件做**热应用**
- 若同时修改了插件以外的配置，则插件部分热应用，其余配置仍需重启后生效
- `hot_apply_attempted` / `hot_applied` / `hot_apply_error` 用于前端精确区分热应用成功、未尝试、失败三种状态

### 5.5.1 插件热插拔说明

- `POST /api/admin/plugins/{id}/install`
- `POST /api/admin/plugins/{id}/start`
- `POST /api/admin/plugins/{id}/reload`
- `POST /api/admin/plugins/{id}/recover`
- `POST /api/admin/plugins/{id}/stop`
- `POST /api/admin/plugins/{id}/uninstall`

当前第一版支持：

- external_exec 插件上传安装 / 覆盖升级 / 卸载
- 未配置但已发现的 external_exec 插件可直接通过 `start` 完成“安装并启用”
- builtin 插件的运行时启停（当前主要用于 testplugin）
- 已启用插件的运行时重载
- external_exec 插件熔断后的“清除熔断并立即恢复”
- 启停结果同步写回配置文件
- 通过 `POST /api/admin/config/save` 保存插件配置时，对插件新增 / 移除 / 启停 / 配置变更做热应用

### 5.6 登录 / 首次设置页

使用：

- `/api/admin/auth/state`
- `/api/admin/auth/setup`
- `/api/admin/auth/login`
- `/api/admin/auth/logout`
- `/api/admin/auth/password`

建议流程：

1. 首屏先请求 `/api/admin/auth/state`
2. 若 `requires_setup=true`，显示首次设置密码页
3. 若 `authenticated=false`，显示登录页
4. 登录成功后再请求 `/api/admin/webui/bootstrap`
5. 进入概览页后，可在“后台安全”面板调用 `/api/admin/auth/password` 修改密码

`POST /api/admin/auth/password` 请求体：

```json
{
  "current_password": "old-password",
  "new_password": "new-password"
}
```

---

## 6. 当前不在第一版内

- 非插件配置的全量热更新
- 连接重连按钮
- 日志查询
- 完整多用户权限模型

这些后续在 v2 再补。
