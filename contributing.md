# Go-bot 贡献与开发规范

> 适用范围：整个仓库  
> 当前目标：保持 **AI First 宿主平台** 的一致性、可维护性与可扩展性

---

## 1. 项目定位

Go-bot 是一个面向 **OneBot v11 / NapCat** 的 Go 机器人宿主项目。

当前主线原则：

- **AI 是核心能力，不做插件**
- **插件是第一扩展系统**
- **WebUI 是统一控制台**
- **部署以 Linux 单机自托管优先**

在任何改动中，请优先遵守：

- **KISS**：实现简单、路径直白
- **YAGNI**：只做当前明确需要的能力
- **DRY**：避免重复实现同类逻辑
- **SOLID**：模块边界清晰，职责单一，依赖抽象

---

## 2. 开发前先了解什么

开始开发前，建议至少先看：

- `README.md`
- `ARCHITECTURE.md`
- `SPEC.md`
- `docs/webui-backend-contract-v1.md`
- `docs/config-schema-v1.md`

如果本次开发涉及 AI、插件或协议适配，再补充阅读：

- `docs/ai-upgrade-v2.md`
- `docs/plugin-development-guide-v1.md`
- `docs/external-exec-plugin-package-v1.md`
- `docs/onebot-adapter-v1.md`

---

## 3. 目录与职责约定

### 3.1 主目录

```text
main.go                  程序入口
internal/                宿主内部实现
plugins/                 插件目录
configs/                 配置模板与实际配置
docs/                    设计文档、契约文档、迁移文档
```

### 3.2 internal 目录职责

```text
internal/app/            启动编排
internal/runtime/        运行时协调、热应用、快照
internal/config/         配置加载、校验、保存、脱敏
internal/admin/          Admin API + WebUI
internal/adapter/        OneBot/NapCat 协议接入
internal/transport/      消息发送路由
internal/plugin/         插件宿主、external_exec 生态
internal/ai/             AI 核心、记忆、反思、消息存储
internal/media/          媒体下载、落盘与 R2 存储
```

### 3.3 不要再向这些路径加新主逻辑

```text
legacy/
cmd/botd/
根目录 config.yml
```

这些历史路径已经移除，不要重新引入新的兼容层或旧目录。

---

## 4. 命名规范

本项目同时包含 Go、YAML/JSON 配置、WebUI、插件元数据，因此命名需要统一。

## 4.1 Go 包名

规则：

- 全小写
- 不使用驼峰
- 尽量短、明确、语义稳定

推荐：

- `runtime`
- `config`
- `externalexec`
- `webui`

避免：

- `RunTime`
- `go_bot_runtime`
- `utils2`

## 4.2 Go 文件名

规则：

- 使用 `snake_case`
- 文件名体现职责，不体现实现细节噪声
- 测试文件必须为 `*_test.go`

推荐：

- `service.go`
- `service_messages.go`
- `reflection_govern.go`
- `router_test.go`

避免：

- `newServiceFinal.go`
- `tmp_fix.go`
- `misc.go`

## 4.3 Go 标识符

### 导出标识符

- 使用 `CamelCase`
- 名词/动词清晰

推荐：

- `Runtime`
- `SaveAIConfig`
- `ListMessageLogs`

### 非导出标识符

- 使用 `camelCase`

推荐：

- `loadBootstrap`
- `renderAIView`
- `buildInboundMessageLog`

### 构造函数

- 统一使用 `NewXxx`

推荐：

- `NewRouter`
- `NewHandler`
- `NewService`

## 4.4 接口命名

规则：

- 接口必须体现抽象角色
- 小接口优先
- 不使用“万能接口”

推荐：

- `Provider`
- `Store`
- `ActionClient`
- `ConnectionManager`

避免：

- `ManagerInterface`
- `EverythingProvider`

## 4.5 常量与枚举

规则：

- 类型常量优先配套自定义类型
- 状态值集中定义

推荐：

- `StateRunning`
- `StateStopped`
- `ActionTypeNapCatHTTP`

## 4.6 布尔字段命名

Go 结构体字段推荐：

- `Enabled`
- `Ready`
- `Configured`
- `RequiresSetup`

JSON / YAML 字段推荐：

- `enabled`
- `ready`
- `configured`
- `requires_setup`

避免：

- `EnableFlag`
- `IsEnable`
- `switch_on`

## 4.7 JSON / YAML / DB 字段命名

统一使用：

- **lower_snake_case**

推荐：

- `private_active_persona_id`
- `message_status`
- `vision_summary`
- `group_policies`

数据库表名也统一使用：

- `lower_snake_case`

推荐：

- `ai_message_log`
- `ai_message_image`
- `raw_message_log`

## 4.8 插件与配置 ID 命名

### 插件 ID

统一使用：

- **lower_snake_case**

推荐：

- `menu_hint`
- `choose_a_song`
- `video_parser`

### 连接 ID

统一使用：

- **lower-kebab-case**

推荐：

- `napcat-main`
- `group-bot-1`

原因：

- 连接 ID 更偏运维标识，适合直接展示在 WebUI / URL / 日志中

### 人格模板 ID

统一使用：

- **lower_snake_case**

推荐：

- `private_gentle`
- `private_playful`

---

## 5. 开发规则

## 5.1 新功能应该放在哪

### AI 相关

放到：

- `internal/ai/`

不要做成插件，除非它只是：

- AI 的一个工具提供者
- AI 的一个上下文提供者

### 连接相关

放到：

- `internal/adapter/`
- `internal/runtime/`

### 插件相关

主线放到：

- `internal/plugin/externalexec/`
- `plugins/`

仅保留极少量 builtin 示例能力。

### WebUI / Admin API

放到：

- `internal/admin/api/`
- `internal/admin/webui/`

不要把后端业务逻辑直接堆进模板或前端脚本里。

## 5.2 什么时候该拆文件

满足以下任一情况应考虑拆分：

- 一个文件开始承担多个职责
- 一个文件超过一个稳定子领域
- 同类逻辑已经有明显分组

例如：

- `service.go` 拆出 `service_messages.go`
- `sqlstore.go` 拆出 `sqlstore_messages.go`

## 5.3 什么时候不该拆

如果只是为了“看起来模块化”而拆出大量薄文件，不建议这么做。

判断标准：

- 是否真的提升了职责清晰度
- 是否减少了修改时的认知负担
- 是否避免了重复

---

## 6. 配置规范

### 6.1 配置来源

实际运行配置建议使用：

```text
configs/config.yml
```

模板配置：

```text
configs/config.example.yml
```

### 6.2 配置改动要求

- 修改配置模型时，必须同步更新：
  - `internal/config/model.go`
  - `internal/config/loader.go`
  - `internal/config/validator.go`
- 如果 WebUI 可编辑，还要同步更新：
  - `internal/admin/webui/assets/html/*.tmpl`
  - `internal/admin/webui/assets/js/*.js`
  - `internal/admin/webui/assets/css/*.css`
  - 相关 API / 测试

### 6.3 配置字段原则

- 配置字段必须可解释
- 配置默认值必须明确
- 配置校验必须尽量前置
- WebUI 优先走结构化表单，不鼓励用户直接改 JSON

---

## 7. AI 开发规范

## 7.1 AI 是核心，不是插件

以下能力必须留在 `internal/ai/`：

- 回复决策
- 记忆系统
- 群画像
- 关系边
- 反思整理
- 私聊人格模板
- AI 聊天记录存储

## 7.2 群聊与私聊边界

### 群聊

- 允许学习群画像、群梗、关系
- 是当前 AI 主线

### 私聊

- 不做孵化
- 采用多模板预设人格
- 同时只允许一套人格生效

## 7.3 聊天数据存储

当前主线只纳入：

- 文本
- 图片

不作为 AI 主消息存储主线的内容：

- 视频
- 语音

图片策略：

- 存媒体引用
- 存视觉摘要
- 后台可直接预览
- 不依赖 QQ 临时 URL

---

## 8. WebUI 开发规范

## 8.1 风格要求

- 更像正式控制台，不像个人工具页
- 中文产品化表达优先
- 结构化表单优先
- 不直接暴露内部变量名给用户

## 8.2 前端实现原则

- 尽量复用已有渲染 helper
- 优先局部刷新，不做无意义全量重载
- 高风险操作必须有明确反馈
- 新增状态必须有：
  - 空状态
  - 加载状态
  - 错误状态

## 8.3 WebUI 改动必须同步

- `assets/html/*.tmpl`
- `assets/js/*.js`
- `assets/css/*.css`
- `handler_test.go`

---

## 9. 测试要求

## 9.1 至少要跑什么

提交前至少运行：

- 对改动过的前端脚本执行 `node --check <file>`
- `go test ./...`
- `go build ./...`

## 9.2 新增能力必须补什么测试

### 配置字段

至少补：

- validator 测试

### Admin API

至少补：

- router 测试

### WebUI 模板 / 前端关键逻辑

至少补：

- handler 测试

### AI / Store / Runtime 逻辑

至少补对应包测试：

- `service_test.go`
- `sqlstore_test.go`
- `runtime_test.go`

---

## 10. 提交规范

## 10.1 Commit 信息

沿用当前项目风格：

- 简短
- 直接
- 中文优先
- 祈使句

推荐：

- `补齐 AI 聊天记录观察页`
- `新增私聊人格模板配置校验`
- `收口 external_exec 插件安装链路`

避免：

- `fix`
- `update files`
- `临时改一下`

## 10.2 PR / 合并说明建议

至少写清：

- 改了什么
- 为什么改
- 是否影响配置
- 是否影响 WebUI / API / 数据结构
- 如何验证

---

## 11. 文件编码与文本规范

所有代码与文档必须：

- 使用 **UTF-8（无 BOM）**
- 不允许 GBK / ANSI
- 不允许提交乱码

日志、错误消息、命令保留原语言即可；说明文字优先中文。

---

## 12. 不推荐的做法

- 重新引入 `legacy/` 旧目录并继续堆新主逻辑
- 为了“可能以后会用到”提前加复杂抽象
- 在前端硬编码后端内部字段语义
- 在插件里直接接管 AI 最终人格
- 同时维护长期双轨实现而不清理旧体系

---

## 13. 建议的开发流程

1. 先读相关文档
2. 明确本次边界
3. 优先改最小必要模块
4. 补测试
5. 跑格式化、前端校验、Go 测试、Go 构建
6. 更新文档

---

## 14. 文档维护要求

如果你的改动影响以下任一内容，请同步更新根文档或 `docs/`：

- 架构边界
- 配置结构
- 插件协议
- AI 数据模型
- WebUI 行为
- Admin API 契约

> 文档不是补充项，而是交付的一部分。

