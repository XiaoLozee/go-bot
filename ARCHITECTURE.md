# Go-bot 架构说明（ARCHITECTURE）

> 状态：当前实现架构 + 持续重构中的稳定边界说明

---

## 1. 文档目标

本文档描述 Go-bot 当前的系统架构、模块边界和关键数据流。

它重点回答：

- 系统由哪些层组成
- 各层职责是什么
- 消息、配置、插件、AI 数据是如何流动的
- 后续扩展应该优先挂在哪一层

---

## 2. 总体架构

Go-bot 当前采用 **宿主中心架构**：

```text
                +----------------------+
                |       WebUI          |
                |   Admin Frontend     |
                +----------+-----------+
                           |
                           v
                +----------------------+
                |      Admin API       |
                +----------+-----------+
                           |
                           v
 +------------------------------------------------------+
 |                      Runtime                         |
 |------------------------------------------------------|
 | Config | Connection Mgmt | Plugin Host | AI Service  |
 | Audit  | Snapshot        | Hot Apply   | Media       |
 +-----------+--------------------+---------------------+
             |                    |                     |
             v                    v                     v
       +-----------+       +-------------+       +-------------+
       | Adapter   |       | Plugins     |       | Storage     |
       | OneBot    |       | builtin/ext |       | SQL + Media |
       +-----------+       +-------------+       +-------------+
```

核心思想：

- **Runtime 是中心协调器**
- **AI 是内建核心服务**
- **插件是扩展系统**
- **Admin API / WebUI 是统一运维入口**

---

## 3. 代码结构映射

```text
main.go                                程序入口
internal/app/botd/                     启动编排
internal/runtime/                      运行时
internal/config/                       配置系统
internal/admin/api/                    后台 API
internal/admin/webui/                  内嵌 WebUI
internal/adapter/                      协议适配层
internal/transport/messenger/          消息发送路由
internal/ai/                           AI 核心
internal/media/                        媒体存储与下载
internal/plugin/host/                  插件宿主
internal/plugin/externalexec/          external_exec 主线
internal/plugin/builtin/               builtin 注册与 testplugin
plugins/                      已安装外部插件
```

---

## 4. 启动流程

## 4.1 程序入口

主入口：

```text
main.go
```

## 4.2 启动阶段

推荐理解为以下顺序：

1. 读取配置
2. 校验配置
3. 初始化日志与基础依赖
4. 初始化存储与媒体服务
5. 初始化 AI 服务
6. 初始化连接管理
7. 初始化插件宿主
8. 初始化 Admin API / WebUI
9. 启动 runtime

## 4.3 关闭顺序

关闭时遵循：

1. 停止后台写入口
2. 停止接收新事件
3. 停止插件处理
4. 停止连接
5. 刷新状态并退出

---

## 5. 配置架构

配置由 `internal/config/` 统一负责。

主要职责：

- 模型定义
- 默认值
- 加载
- 校验
- 脱敏
- 保存
- 备份

配置不应由各模块各自散写。

### 5.1 配置原则

- 模型集中定义
- 校验集中处理
- 保存统一入口
- WebUI 编辑优先走结构化表单

### 5.2 热应用边界

当前支持的热应用主要集中在：

- 插件配置与生命周期
- WebUI 主题等局部设置
- AI 配置部分链路

非插件的大部分宿主级配置仍可能需要重启后完全生效。

---

## 6. 连接与协议架构

连接系统主要位于：

- `internal/adapter/`
- `internal/runtime/`

### 6.1 接入层职责

接入层负责：

- 接收 OneBot 事件
- 协议解析
- 转成统一事件模型

### 6.2 Action 输出

发送能力通过 NapCat / OneBot HTTP Action 客户端完成。

发送层负责：

- 发消息
- 查询消息
- 查询登录信息
- 查询运行状态

### 6.3 多连接管理

Runtime 持有连接快照与配置，并向后台提供：

- 列表
- 单连接详情
- 探活结果
- 保存结果

---

## 7. 插件架构

## 7.1 插件系统定位

插件不是宿主本体的替代，而是扩展层。

当前插件架构分为两类：

### builtin

- 数量极少
- 仅保留宿主测试 / 样例职责

### external_exec

- 正式主线
- 支持独立打包
- 支持安装、升级、卸载、启停、重载

## 7.2 external_exec 运行方式

宿主负责：

- 安装包落盘
- 读取描述符
- 生命周期管理
- 健康与日志接入
- 配置写入

插件自身负责：

- 具体业务逻辑
- 按协议响应宿主调用

## 7.3 插件与 AI 的边界

插件可以增强 AI，但不应替代 AI 内核。

推荐关系：

- 插件提供工具
- 插件提供上下文
- AI 决定是否使用
- AI 生成最终回复

---

## 8. AI 架构

AI 主代码位于：

```text
internal/ai/
```

这是当前项目的核心内建能力层。

## 8.1 AI 子模块

当前 AI 大致分为：

- 配置与 prompt 处理
- 回复决策
- 会话状态
- 候选记忆 / 长期记忆
- 群画像 / 成员画像 / 关系边
- 离线反思
- 聊天记录存储
- 私聊人格模板

## 8.2 群聊主线

群聊 AI 当前是主线：

- 进行上下文组织
- 支持群级策略覆盖
- 支持视觉摘要参与回复
- 支持群理解与后台整理

## 8.3 私聊人格模板

私聊不走孵化人格路线，而走模板化路线。

架构上：

- 模板保存在 AI 配置内
- 当前生效模板单独记录
- 私聊回复链路读取当前模板

## 8.4 AI 观察中心

WebUI 中的 AI 观察中心展示：

- 运行概况
- 记忆与会话
- 聊天记录
- 群画像与关系

它消费的是：

- AI Snapshot
- AI DebugView
- AI Message APIs

---

## 9. AI 聊天数据架构

这是最近重点增强的部分。

## 9.1 为什么要单独建模

历史上的 `raw_message_log` 只能支撑轻量反思回放，不足以作为正式 AI 聊天数据底座。

因此当前架构采用：

- 兼容保留 `raw_message_log`
- 新增正式消息模型：
  - `ai_message_log`
  - `ai_message_image`

## 9.2 数据分层

### `ai_message_log`

存储：

- 消息归属
- 群 / 私聊范围
- 发送者
- 回复关系
- 文本全文
- 时间

### `ai_message_image`

存储：

- 图片关联
- 来源引用
- 视觉摘要
- 视觉状态
- 与媒体资产的关联信息

### `media_asset`

由媒体层维护：

- 本地或 R2 中的真实资源
- 公网地址 / 本地路径 / 存储键等

## 9.3 架构原则

- AI 消息表不直接保存大块二进制
- 图片通过媒体系统落盘/落桶
- 聊天层只保存引用与摘要
- 后台预览统一走宿主控制的预览地址

---

## 10. 媒体架构

媒体模块位于：

```text
internal/media/
```

职责：

- 下载外部图片
- 写入 local / R2
- 返回媒体资产信息

AI 层与媒体层关系：

- AI 不直接管理二进制资源
- AI 只消费媒体资产引用

---

## 11. Admin API 与 WebUI 架构

## 11.1 Admin API

Admin API 负责把 runtime / AI / plugin / config 的能力统一暴露给后台。

职责：

- 认证与会话
- bootstrap 聚合
- 连接操作
- 插件操作
- AI 配置与观察
- 聊天记录查看
- 配置保存与校验
- 审计输出

## 11.2 WebUI

WebUI 为内嵌静态资源，源码按模块拆分为：

- `assets/html/*.tmpl`
- `assets/js/*.js`
- `assets/css/*.css`

运行时由 `internal/admin/webui/handler.go` 聚合输出 `/assets/app.js` 与 `/assets/style.css`。

前端特点：

- 控制台式布局
- 结构化表单
- 多主题
- 懒加载连接/插件详情
- 逐步增强 AI 页与插件页

### 当前前后端契约来源

主要参考：

- `docs/webui-backend-contract-v1.md`

---

## 12. Snapshot 架构

Runtime 对外展示大量状态时，不直接暴露内部对象，而统一通过 Snapshot / View 输出。

例如：

- `runtime.Snapshot`
- `runtime.Metadata`
- `runtime.AIView`
- `runtime.ConnectionDetail`
- `runtime.PluginDetail`
- `runtime.AIMessageListView`
- `runtime.AIMessageDetailView`

这样做的好处：

- 稳定前后端契约
- 降低 UI 对内部实现的耦合
- 便于测试

---

## 13. 数据流

## 13.1 入站消息流

```text
Adapter Event
  -> Runtime
  -> AI Service
  -> 会话/记忆更新
  -> 聊天记录写入
  -> 必要时触发视觉理解
  -> 生成回复计划
  -> Messenger / Action Client
```

## 13.2 图片消息流

```text
收到图片事件
  -> Media Service 下载并存储
  -> AI 记录消息主表
  -> AI 记录图片关联表
  -> 若开启视觉识别，则写入 vision_summary
  -> WebUI 可通过预览接口查看图片
```

## 13.3 后台配置流

```text
WebUI 表单
  -> Admin API
  -> Config Validate / Save
  -> Runtime Hot Apply（若支持）
  -> Snapshot 更新
  -> WebUI 再渲染
```

## 13.4 插件生命周期流

```text
安装/启用/停止/重载/卸载请求
  -> Admin API
  -> Runtime
  -> Plugin Host
  -> external_exec / builtin
  -> Snapshot 与审计更新
```

---

## 14. 扩展点

未来扩展优先挂在这些点：

### 新连接类型

- `internal/adapter/`
- `internal/runtime/`

### 新 external_exec 能力

- `internal/plugin/externalexec/`
- `plugins/`

### AI 新观察能力 / 存储能力

- `internal/ai/`

### WebUI 新控制台能力

- `internal/admin/api/`
- `internal/admin/webui/`

---

## 15. 当前架构边界总结

可以把当前 Go-bot 理解为：

### 核心内建层

- runtime
- config
- admin
- ai
- media
- adapter

### 扩展层

- external_exec 插件
- 少量 builtin

### 产品化层

- WebUI
- Admin API
- 审计
- 配置管理

---

## 16. 架构演进原则

后续演进中请持续坚持：

1. AI 内核不要被插件化稀释
2. 插件继续主打 external_exec
3. 新能力优先走宿主统一治理
4. Snapshot / View 契约优先稳定
5. legacy 逐步清理，不长期双轨

---

## 17. 相关阅读

- `README.md`
- `SPEC.md`
- `contributing.md`
- `docs/config-schema-v1.md`
- `docs/onebot-adapter-v1.md`
- `docs/webui-backend-contract-v1.md`
- `docs/plugin-development-guide-v1.md`
- `docs/ai-upgrade-v2.md`

