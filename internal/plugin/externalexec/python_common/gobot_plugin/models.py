"""Go-bot 插件常用数据结构类型。

这里使用 TypedDict，是为了让插件代码在 PyCharm、VS Code/Pylance 等 IDE 中获得补全。
这些类型不会改变运行时协议，真实数据仍然是普通 dict/list。
"""

from __future__ import annotations

from typing import Any, Dict, List, Literal, TypedDict, Union

JSONValue = Union[None, bool, int, float, str, "JSONArray", "JSONObject"]
JSONArray = List[JSONValue]
JSONObject = Dict[str, JSONValue]

ChatType = Literal["private", "group"]
MessageSegmentType = Literal["text", "at", "image", "video", "file", "music", "reply", "forward"]


class MessageTarget(TypedDict, total=False):
    """消息发送目标。"""

    # 连接 ID，对应后台连接配置 ID。
    connection_id: str
    # 聊天类型：private 或 group。
    chat_type: ChatType
    # 私聊目标 QQ。
    user_id: str
    # 群聊目标群号。
    group_id: str


class MessageSegment(TypedDict, total=False):
    """OneBot 风格消息段。"""

    # 消息段类型，例如 text、image、at。
    type: str
    # 消息段数据，不同 type 字段不同。
    data: Dict[str, Any]


class MessageEvent(TypedDict, total=False):
    """宿主推送给插件的消息事件。"""

    # 事件类型，消息事件固定为 message。
    kind: str
    # 连接 ID。
    connection_id: str
    # 消息 ID。
    message_id: str
    # private 或 group。
    chat_type: ChatType
    # 发送者 QQ。
    user_id: str
    # 群号，私聊时通常为空。
    group_id: str
    # 纯文本内容，宿主会尽量从消息段中提取。
    raw_text: str
    # 原始消息段列表。
    segments: List[MessageSegment]
    # 事件时间戳，通常是 ISO-8601 字符串。
    timestamp: str
    # 发送者昵称。
    sender_nickname: str
    # 群名。
    group_name: str


class PluginManifest(TypedDict, total=False):
    """插件清单信息，对应 plugin.yaml。"""

    id: str
    name: str
    version: str
    description: str
    author: str
    kind: str
    runtime: str
    entry: str
    protocol: str


class PluginInfo(TypedDict, total=False):
    """插件目录里的单个插件摘要。"""

    id: str
    name: str
    version: str
    description: str
    kind: str
    enabled: bool
    builtin: bool


class AppInfo(TypedDict, total=False):
    """宿主应用信息。"""

    name: str
    env: str
    owner_qq: str


class UserInfo(TypedDict, total=False):
    """QQ 用户信息。"""

    user_id: str
    nickname: str
    remark: str
    sex: str
    age: int


class GroupInfo(TypedDict, total=False):
    """群信息。"""

    group_id: str
    group_name: str
    member_count: int
    max_member_count: int


class GroupMemberInfo(TypedDict, total=False):
    """群成员信息。"""

    group_id: str
    user_id: str
    nickname: str
    card: str
    role: str
    title: str
    join_time: int
    last_sent_time: int


class MessageDetail(TypedDict, total=False):
    """消息详情。"""

    message_id: str
    real_id: str
    sender: Dict[str, Any]
    message: List[MessageSegment]
    raw_message: str
    time: int


class ForwardNode(TypedDict, total=False):
    """合并转发节点。"""

    user_id: str
    nickname: str
    content: List[MessageSegment]


class ForwardMessage(TypedDict, total=False):
    """合并转发消息详情。"""

    id: str
    messages: List[ForwardNode]


class ResolvedMedia(TypedDict, total=False):
    """媒体解析结果。"""

    url: str
    file: str
    file_name: str
    mime_type: str
    size_bytes: int


class LoginInfo(TypedDict, total=False):
    """机器人登录信息。"""

    user_id: str
    nickname: str


class BotStatus(TypedDict, total=False):
    """机器人连接状态。"""

    online: bool
    good: bool
    status: Dict[str, Any]
