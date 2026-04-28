"""Go-bot Python 插件运行时类型入口。

SDK 会优先导入宿主注入的 gobot_runtime。
如果 IDE 只安装了 SDK 而没有 gobot_runtime，也会保留一组可补全的 fallback 类型。
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any, Callable, Dict, Iterable, List, Optional, Protocol

from .models import (
    AppInfo,
    BotStatus,
    ForwardMessage,
    ForwardNode,
    GroupInfo,
    GroupMemberInfo,
    LoginInfo,
    MessageDetail,
    MessageEvent,
    MessageSegment,
    MessageTarget,
    PluginInfo,
    PluginManifest,
    ResolvedMedia,
    UserInfo,
)


class RuntimeErrorResponse(RuntimeError):
    """宿主调用返回 error 时抛出的异常。"""


class Logger(Protocol):
    """插件日志接口。日志会回传给宿主。"""

    def debug(self, message: str) -> None: ...
    def info(self, message: str) -> None: ...
    def warn(self, message: str) -> None: ...
    def error(self, message: str) -> None: ...


class Messenger(Protocol):
    """发消息接口。"""

    def send_text(self, target: MessageTarget, text: str) -> None: ...
    def send_segments(self, target: MessageTarget, segments: List[MessageSegment]) -> None: ...
    def reply_text(self, target: MessageTarget, reply_to: str, text: str) -> None: ...


class BotAPI(Protocol):
    """宿主 Bot API。这里的方法会同步请求宿主，再返回结果。"""

    def get_stranger_info(self, connection_id: str, user_id: str) -> UserInfo: ...
    def get_group_info(self, connection_id: str, group_id: str) -> GroupInfo: ...
    def get_group_member_list(self, connection_id: str, group_id: str) -> List[GroupMemberInfo]: ...
    def get_group_member_info(self, connection_id: str, group_id: str, user_id: str) -> GroupMemberInfo: ...
    def get_message(self, connection_id: str, message_id: str) -> MessageDetail: ...
    def get_forward_message(self, connection_id: str, forward_id: str) -> ForwardMessage: ...
    def delete_message(self, connection_id: str, message_id: str) -> None: ...
    def resolve_media(self, connection_id: str, segment_type: str, file: str) -> ResolvedMedia: ...
    def get_login_info(self, connection_id: str) -> LoginInfo: ...
    def get_status(self, connection_id: str) -> BotStatus: ...
    def send_group_forward(
        self,
        connection_id: str,
        group_id: str,
        nodes: List[ForwardNode],
        options: Optional["ForwardOptions"] = None,
    ) -> None: ...


class PluginCatalog(Protocol):
    """插件目录接口。"""

    def list_plugins(self) -> List[PluginInfo]: ...


class AIToolContext(Protocol):
    """AI Tool 调用上下文。"""

    event: MessageEvent
    target: MessageTarget
    reply_to: str

    def schedule_current_send(self, text: str, reply: bool = False) -> None: ...
    def scheduled_payload(self) -> Optional[Dict[str, Any]]: ...


AIToolHandler = Callable[[AIToolContext, Any], Any]
AIToolAvailable = Callable[[MessageEvent], bool]


@dataclass
class AIToolDefinition:
    """AI Tool 定义。"""

    name: str
    description: str = ""
    input_schema: Dict[str, Any] = None  # type: ignore[assignment]
    handler: Optional[AIToolHandler] = None
    available: Optional[AIToolAvailable] = None

    def __post_init__(self) -> None:
        if self.input_schema is None:
            self.input_schema = {}


class AIToolRegistrar(Protocol):
    """AI Tool 注册接口。"""

    def register_tools(self, namespace: str, tools: List[Any]) -> None: ...
    def unregister_tools(self, namespace: str) -> None: ...


@dataclass
class PluginEnv:
    """插件启动上下文。"""

    # 当前插件的 plugin.yaml 清单。
    manifest: PluginManifest
    # 当前插件配置，来源于后台配置和 config.schema.json 默认值。
    config: Dict[str, Any]
    # 已安装插件目录。
    catalog: PluginCatalog
    # 宿主应用信息。
    app: AppInfo
    # 消息发送接口。
    messenger: Messenger
    # 宿主 Bot API。
    bot_api: BotAPI
    # AI Tool 注册接口。
    ai_tools: AIToolRegistrar
    # 插件日志接口。
    logger: Logger


@dataclass
class ForwardOptions:
    """合并转发选项。"""

    source: str = ""

    def to_payload(self) -> Dict[str, Any]:
        payload: Dict[str, Any] = {}
        if self.source:
            payload["source"] = self.source
        return payload


class BasePlugin:
    """插件基类。

    插件通常继承它，然后覆写 start、stop、handle_event。
    """

    env: PluginEnv

    def start(self, env: PluginEnv) -> None:
        self.env = env

    def stop(self) -> None:
        return None

    def handle_event(self, event: MessageEvent) -> None:
        return None


def build_target(event: MessageEvent, chat_type: Optional[str] = None) -> MessageTarget:
    """根据消息事件构造回复目标。"""

    return {
        "connection_id": str(event.get("connection_id") or ""),
        "chat_type": str(chat_type or event.get("chat_type") or "private"),
        "user_id": str(event.get("user_id") or ""),
        "group_id": str(event.get("group_id") or ""),
    }


def text_segment(text: str) -> MessageSegment:
    """构造 text 消息段。"""

    return {"type": "text", "data": {"text": text}}


def at_segment(user_id: str) -> MessageSegment:
    """构造 at 消息段。"""

    return {"type": "at", "data": {"qq": user_id}}


def image_segment(file_url: str) -> MessageSegment:
    """构造 image 消息段。"""

    return {"type": "image", "data": {"file": file_url}}


def video_segment(file_url: str) -> MessageSegment:
    """构造 video 消息段。"""

    return {"type": "video", "data": {"file": file_url}}


def file_segment(file_url: str, name: str = "") -> MessageSegment:
    """构造 file 消息段。"""

    data: Dict[str, Any] = {"file": file_url}
    if name:
        data["name"] = name
    return {"type": "file", "data": data}


def music_custom_segment(url: str, audio: str, title: str, content: str, image: str) -> MessageSegment:
    """构造自定义 music 消息段。"""

    data: Dict[str, Any] = {"type": "custom"}
    if url:
        data["url"] = url
    if audio:
        data["audio"] = audio
    if title:
        data["title"] = title
    if content:
        data["content"] = content
    if image:
        data["image"] = image
    return {"type": "music", "data": data}


def text_node(user_id: str, nickname: str, text: str) -> ForwardNode:
    """构造合并转发文本节点。"""

    return {"user_id": user_id, "nickname": nickname, "content": [text_segment(text)]}


def clone_segment(segment: MessageSegment) -> MessageSegment:
    """复制消息段，避免直接修改原始事件。"""

    return {
        "type": str(segment.get("type") or ""),
        "data": dict(segment.get("data") or {}),
    }


def segment_text(segment: MessageSegment) -> str:
    """读取 text 消息段里的文本。"""

    if str(segment.get("type") or "") != "text":
        return ""
    data = segment.get("data") or {}
    return str(data.get("text") or "")


def parse_event_timestamp(event: MessageEvent) -> datetime:
    """解析事件时间戳。解析失败时返回当前 UTC 时间。"""

    raw = event.get("timestamp")
    if isinstance(raw, str) and raw:
        try:
            if raw.endswith("Z"):
                return datetime.fromisoformat(raw.replace("Z", "+00:00"))
            return datetime.fromisoformat(raw)
        except ValueError:
            pass
    return datetime.now(timezone.utc)


def run_plugin(plugin: BasePlugin) -> int:
    """运行插件。只有宿主注入 gobot_runtime 后才可真正运行。"""

    raise RuntimeError("gobot_runtime is not available; run this plugin inside Go-bot or add plugins/_common to PYTHONPATH")


try:
    from gobot_runtime import (  # type: ignore[assignment]
        AIToolContext as AIToolContext,
        AIToolDefinition as AIToolDefinition,
        AIToolRegistrar as AIToolRegistrar,
        BasePlugin as BasePlugin,
        BotAPI as BotAPI,
        ForwardOptions as ForwardOptions,
        Logger as Logger,
        Messenger as Messenger,
        PluginCatalog as PluginCatalog,
        RuntimeErrorResponse as RuntimeErrorResponse,
        at_segment as at_segment,
        build_target as build_target,
        clone_segment as clone_segment,
        file_segment as file_segment,
        image_segment as image_segment,
        music_custom_segment as music_custom_segment,
        parse_event_timestamp as parse_event_timestamp,
        run_plugin as run_plugin,
        segment_text as segment_text,
        text_node as text_node,
        text_segment as text_segment,
        video_segment as video_segment,
    )
    from gobot_runtime import Env as PluginEnv  # type: ignore[assignment]
except ImportError:
    # IDE 或离线单元测试环境没有 gobot_runtime 时，使用上面的 fallback 类型。
    pass


__all__ = [
    "BasePlugin",
    "AIToolContext",
    "AIToolDefinition",
    "AIToolRegistrar",
    "BotAPI",
    "ForwardOptions",
    "Logger",
    "Messenger",
    "PluginCatalog",
    "PluginEnv",
    "RuntimeErrorResponse",
    "at_segment",
    "build_target",
    "clone_segment",
    "file_segment",
    "image_segment",
    "music_custom_segment",
    "parse_event_timestamp",
    "run_plugin",
    "segment_text",
    "text_node",
    "text_segment",
    "video_segment",
]
