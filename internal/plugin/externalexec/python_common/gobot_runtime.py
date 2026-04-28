from __future__ import annotations

import itertools
import json
import queue
import sys
import threading
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any, Callable, Dict, Iterable, List, Optional

CALL_BOT_GET_STRANGER_INFO = "bot.get_stranger_info"
CALL_BOT_GET_GROUP_INFO = "bot.get_group_info"
CALL_BOT_GET_GROUP_MEMBERS = "bot.get_group_member_list"
CALL_BOT_GET_GROUP_MEMBER = "bot.get_group_member_info"
CALL_BOT_GET_MESSAGE = "bot.get_message"
CALL_BOT_GET_FORWARD_MESSAGE = "bot.get_forward_message"
CALL_BOT_DELETE_MESSAGE = "bot.delete_message"
CALL_BOT_RESOLVE_MEDIA = "bot.resolve_media"
CALL_BOT_GET_LOGIN_INFO = "bot.get_login_info"
CALL_BOT_GET_STATUS = "bot.get_status"
CALL_BOT_SEND_GROUP_FORWARD = "bot.send_group_forward"


class RuntimeErrorResponse(RuntimeError):
    pass


@dataclass
class ForwardOptions:
    source: str = ""

    def to_payload(self) -> Dict[str, Any]:
        payload: Dict[str, Any] = {}
        if self.source:
            payload["source"] = self.source
        return payload


class Logger:
    def __init__(self, runtime: "PluginRuntime") -> None:
        self._runtime = runtime

    def debug(self, message: str) -> None:
        self._runtime.log("debug", message)

    def info(self, message: str) -> None:
        self._runtime.log("info", message)

    def warn(self, message: str) -> None:
        self._runtime.log("warn", message)

    def error(self, message: str) -> None:
        self._runtime.log("error", message)


class Messenger:
    def __init__(self, runtime: "PluginRuntime") -> None:
        self._runtime = runtime

    def send_text(self, target: Dict[str, Any], text: str) -> None:
        self._runtime.emit("send_text", {"target": target, "text": text})

    def send_segments(self, target: Dict[str, Any], segments: List[Dict[str, Any]]) -> None:
        self._runtime.emit("send_segments", {"target": target, "segments": segments})

    def reply_text(self, target: Dict[str, Any], reply_to: str, text: str) -> None:
        self._runtime.emit("reply_text", {"target": target, "reply_to": reply_to, "text": text})


class BotAPI:
    def __init__(self, runtime: "PluginRuntime") -> None:
        self._runtime = runtime

    def get_stranger_info(self, connection_id: str, user_id: str) -> Dict[str, Any]:
        result = self._runtime.call_host(
            CALL_BOT_GET_STRANGER_INFO,
            {"connection_id": connection_id, "user_id": user_id},
        )
        return ensure_dict(result)

    def get_group_info(self, connection_id: str, group_id: str) -> Dict[str, Any]:
        result = self._runtime.call_host(
            CALL_BOT_GET_GROUP_INFO,
            {"connection_id": connection_id, "group_id": group_id},
        )
        return ensure_dict(result)

    def get_group_member_list(self, connection_id: str, group_id: str) -> List[Dict[str, Any]]:
        result = self._runtime.call_host(
            CALL_BOT_GET_GROUP_MEMBERS,
            {"connection_id": connection_id, "group_id": group_id},
        )
        return ensure_list(result)

    def get_group_member_info(self, connection_id: str, group_id: str, user_id: str) -> Dict[str, Any]:
        result = self._runtime.call_host(
            CALL_BOT_GET_GROUP_MEMBER,
            {"connection_id": connection_id, "group_id": group_id, "user_id": user_id},
        )
        return ensure_dict(result)

    def get_message(self, connection_id: str, message_id: str) -> Dict[str, Any]:
        result = self._runtime.call_host(
            CALL_BOT_GET_MESSAGE,
            {"connection_id": connection_id, "message_id": message_id},
        )
        return ensure_dict(result)

    def get_forward_message(self, connection_id: str, forward_id: str) -> Dict[str, Any]:
        result = self._runtime.call_host(
            CALL_BOT_GET_FORWARD_MESSAGE,
            {"connection_id": connection_id, "forward_id": forward_id},
        )
        return ensure_dict(result)

    def delete_message(self, connection_id: str, message_id: str) -> None:
        self._runtime.call_host(
            CALL_BOT_DELETE_MESSAGE,
            {"connection_id": connection_id, "message_id": message_id},
        )

    def resolve_media(self, connection_id: str, segment_type: str, file: str) -> Dict[str, Any]:
        result = self._runtime.call_host(
            CALL_BOT_RESOLVE_MEDIA,
            {"connection_id": connection_id, "segment_type": segment_type, "file": file},
        )
        return ensure_dict(result)

    def get_login_info(self, connection_id: str) -> Dict[str, Any]:
        result = self._runtime.call_host(
            CALL_BOT_GET_LOGIN_INFO,
            {"connection_id": connection_id},
        )
        return ensure_dict(result)

    def get_status(self, connection_id: str) -> Dict[str, Any]:
        result = self._runtime.call_host(
            CALL_BOT_GET_STATUS,
            {"connection_id": connection_id},
        )
        return ensure_dict(result)

    def send_group_forward(
        self,
        connection_id: str,
        group_id: str,
        nodes: List[Dict[str, Any]],
        options: Optional[ForwardOptions] = None,
    ) -> None:
        payload: Dict[str, Any] = {
            "connection_id": connection_id,
            "group_id": group_id,
            "nodes": nodes,
        }
        if options is not None:
            options_payload = options.to_payload()
            if options_payload:
                payload["options"] = options_payload
        self._runtime.call_host(CALL_BOT_SEND_GROUP_FORWARD, payload)


class PluginCatalog:
    def __init__(self, items: Iterable[Dict[str, Any]]) -> None:
        self._items = [ensure_dict(item) for item in items]

    def list_plugins(self) -> List[Dict[str, Any]]:
        return [dict(item) for item in self._items]


@dataclass
class AIToolContext:
    event: Dict[str, Any]
    target: Dict[str, Any]
    reply_to: str = ""
    _scheduled: Optional[Dict[str, Any]] = None

    def schedule_current_send(self, text: str, reply: bool = False) -> None:
        if not str(text or "").strip():
            raise ValueError("scheduled text is empty")
        if self._scheduled is not None:
            raise ValueError("scheduled message already exists")
        self._scheduled = {
            "text": str(text),
            "reply": bool(reply),
        }

    def scheduled_payload(self) -> Optional[Dict[str, Any]]:
        if self._scheduled is None:
            return None
        return dict(self._scheduled)


AIToolHandler = Callable[[AIToolContext, Any], Any]
AIToolAvailable = Callable[[Dict[str, Any]], bool]


@dataclass
class AIToolDefinition:
    name: str
    description: str = ""
    input_schema: Dict[str, Any] = field(default_factory=dict)
    handler: Optional[AIToolHandler] = None
    available: Optional[AIToolAvailable] = None


class AIToolRegistrar:
    def __init__(self, runtime: "PluginRuntime") -> None:
        self._runtime = runtime

    def register_tools(self, namespace: str, tools: List[Any]) -> None:
        namespace = str(namespace or "").strip()
        namespace_key = normalize_ai_tool_namespace(namespace)
        local_tools: Dict[str, AIToolDefinition] = {}
        payload_tools: List[Dict[str, Any]] = []
        for item in tools:
            definition = normalize_ai_tool_definition(item)
            name = definition.name.strip()
            if not name:
                raise ValueError("AI tool name is required")
            if definition.handler is None:
                raise ValueError(f"AI tool {name} handler is required")
            if name in local_tools:
                raise ValueError(f"duplicate AI tool name: {name}")
            local_tools[name] = definition
            payload_tools.append(
                {
                    "name": name,
                    "description": str(definition.description or "").strip(),
                    "input_schema": ensure_dict(definition.input_schema or {}),
                }
            )
        response = self._runtime.request("ai_tools_register", {"namespace": namespace, "tools": payload_tools})
        if response.get("error"):
            raise RuntimeErrorResponse(str(response.get("error")))
        self._runtime.replace_ai_tools(namespace_key, local_tools)

    def unregister_tools(self, namespace: str) -> None:
        namespace = str(namespace or "").strip()
        namespace_key = normalize_ai_tool_namespace(namespace)
        self._runtime.remove_ai_tools(namespace_key)
        self._runtime.emit("ai_tools_unregister", {"namespace": namespace})


@dataclass
class Env:
    manifest: Dict[str, Any]
    config: Dict[str, Any]
    catalog: PluginCatalog
    app: Dict[str, Any]
    messenger: Messenger
    bot_api: BotAPI
    ai_tools: AIToolRegistrar
    logger: Logger


class BasePlugin:
    def start(self, env: Env) -> None:
        self.env = env

    def stop(self) -> None:
        return None

    def handle_event(self, event: Dict[str, Any]) -> None:
        return None


class PluginRuntime:
    def __init__(self, plugin: BasePlugin) -> None:
        self._plugin = plugin
        self._stdout_lock = threading.Lock()
        self._packet_queue: "queue.Queue[tuple[str, Dict[str, Any]]]" = queue.Queue()
        self._call_seq = itertools.count(1)
        self._pending_calls: Dict[str, "queue.Queue[Dict[str, Any]]"] = {}
        self._pending_lock = threading.Lock()
        self._logger = Logger(self)
        self._messenger = Messenger(self)
        self._bot_api = BotAPI(self)
        self._ai_tools = AIToolRegistrar(self)
        self._registered_ai_tools: Dict[str, AIToolDefinition] = {}
        self._registered_ai_namespaces: Dict[str, List[str]] = {}

    def emit(self, message_type: str, payload: Optional[Dict[str, Any]] = None) -> None:
        packet: Dict[str, Any] = {"type": message_type}
        if payload is not None:
            packet["payload"] = payload
        raw = json.dumps(packet, ensure_ascii=False)
        with self._stdout_lock:
            sys.stdout.write(raw + "\n")
            sys.stdout.flush()

    def log(self, level: str, message: str) -> None:
        self.emit("log", {"level": level, "message": message})

    def request(self, message_type: str, payload: Dict[str, Any]) -> Dict[str, Any]:
        call_id = str(next(self._call_seq))
        response_queue: "queue.Queue[Dict[str, Any]]" = queue.Queue(maxsize=1)
        with self._pending_lock:
            self._pending_calls[call_id] = response_queue
        try:
            request_payload = dict(payload)
            request_payload["id"] = call_id
            self.emit(message_type, request_payload)
            return ensure_dict(response_queue.get())
        finally:
            with self._pending_lock:
                self._pending_calls.pop(call_id, None)

    def call_host(self, method: str, payload: Dict[str, Any]) -> Any:
        response = self.request("call", {"method": method, "payload": payload})
        if response.get("error"):
            raise RuntimeErrorResponse(str(response.get("error")))
        return response.get("result")

    def replace_ai_tools(self, namespace_key: str, tools: Dict[str, AIToolDefinition]) -> None:
        old_names = self._registered_ai_namespaces.get(namespace_key, [])
        for name in old_names:
            self._registered_ai_tools.pop(name, None)
        self._registered_ai_namespaces[namespace_key] = list(tools.keys())
        self._registered_ai_tools.update(tools)

    def remove_ai_tools(self, namespace_key: str) -> None:
        old_names = self._registered_ai_namespaces.pop(namespace_key, [])
        for name in old_names:
            self._registered_ai_tools.pop(name, None)

    def _reader_loop(self) -> None:
        try:
            for raw_line in sys.stdin:
                line = raw_line.strip()
                if not line:
                    continue
                try:
                    packet = json.loads(line)
                except json.JSONDecodeError as exc:
                    self.log("error", f"invalid host packet: {exc}")
                    continue

                message_type = str(packet.get("type") or "")
                payload = ensure_dict(packet.get("payload") or {})
                if message_type == "response":
                    call_id = str(payload.get("id") or "")
                    if not call_id:
                        continue
                    with self._pending_lock:
                        response_queue = self._pending_calls.get(call_id)
                    if response_queue is not None:
                        response_queue.put(payload)
                    continue

                self._packet_queue.put((message_type, payload))
        finally:
            self._packet_queue.put(("stop", {}))

    def run(self) -> int:
        reader = threading.Thread(target=self._reader_loop, name="gobot-plugin-reader", daemon=True)
        reader.start()

        message_type, payload = self._packet_queue.get()
        if message_type != "start":
            self.log("error", f"first host message must be start, got {message_type}")
            return 1

        env = Env(
            manifest=ensure_dict(payload.get("plugin") or {}),
            config=ensure_dict(payload.get("config") or {}),
            catalog=PluginCatalog(payload.get("catalog") or []),
            app=ensure_dict(payload.get("app") or {}),
            messenger=self._messenger,
            bot_api=self._bot_api,
            ai_tools=self._ai_tools,
            logger=self._logger,
        )

        try:
            self._plugin.start(env)
        except Exception as exc:
            self.log("error", f"plugin start failed: {exc}")
            return 1

        self.emit("ready", {"message": "python plugin ready"})

        while True:
            message_type, payload = self._packet_queue.get()
            if message_type == "stop":
                break
            if message_type == "event":
                event = ensure_dict(payload.get("event") or {})
                try:
                    self._plugin.handle_event(event)
                except Exception as exc:
                    self.log("error", f"handle event failed: {exc}")
                continue
            if message_type == "ai_tool_call":
                worker = threading.Thread(
                    target=self._handle_ai_tool_call,
                    args=(payload,),
                    name="gobot-ai-tool-call",
                    daemon=True,
                )
                worker.start()
                continue
            if message_type != "event":
                self.log("warn", f"ignore unsupported host message: {message_type}")
                continue

        try:
            self._plugin.stop()
        except Exception as exc:
            self.log("error", f"plugin stop failed: {exc}")
            return 1
        return 0

    def _handle_ai_tool_call(self, payload: Dict[str, Any]) -> None:
        call_id = str(payload.get("id") or "")
        tool_name = str(payload.get("tool_name") or "").strip()
        if not call_id:
            return

        tool = self._registered_ai_tools.get(tool_name)
        if tool is None or tool.handler is None:
            self.emit("ai_tool_result", {"id": call_id, "error": f"unknown AI tool: {tool_name}"})
            return

        context_payload = ensure_dict(payload.get("context") or {})
        tool_context = AIToolContext(
            event=ensure_dict(context_payload.get("event") or {}),
            target=ensure_dict(context_payload.get("target") or {}),
            reply_to=str(context_payload.get("reply_to") or ""),
        )
        try:
            if tool.available is not None and not tool.available(tool_context.event):
                raise RuntimeErrorResponse(f"AI tool {tool_name} is unavailable for current event")
            result = tool.handler(tool_context, payload.get("arguments"))
            response: Dict[str, Any] = {"id": call_id, "result": result}
            scheduled = tool_context.scheduled_payload()
            if scheduled is not None:
                response["scheduled"] = scheduled
            self.emit("ai_tool_result", response)
        except Exception as exc:
            self.emit("ai_tool_result", {"id": call_id, "error": str(exc)})


def ensure_dict(value: Any) -> Dict[str, Any]:
    if isinstance(value, dict):
        return value
    return {}


def ensure_list(value: Any) -> List[Dict[str, Any]]:
    if not isinstance(value, list):
        return []
    return [ensure_dict(item) for item in value]


def normalize_ai_tool_namespace(namespace: str) -> str:
    namespace = namespace.strip()
    return namespace or "default"


def normalize_ai_tool_definition(value: Any) -> AIToolDefinition:
    if isinstance(value, AIToolDefinition):
        return value
    raw = ensure_dict(value)
    return AIToolDefinition(
        name=str(raw.get("name") or "").strip(),
        description=str(raw.get("description") or "").strip(),
        input_schema=ensure_dict(raw.get("input_schema") or {}),
        handler=raw.get("handler") if callable(raw.get("handler")) else None,
        available=raw.get("available") if callable(raw.get("available")) else None,
    )


def parse_event_timestamp(event: Dict[str, Any]) -> datetime:
    raw = event.get("timestamp")
    if isinstance(raw, str) and raw:
        try:
            if raw.endswith("Z"):
                return datetime.fromisoformat(raw.replace("Z", "+00:00"))
            return datetime.fromisoformat(raw)
        except ValueError:
            pass
    return datetime.now(timezone.utc)


def build_target(event: Dict[str, Any], chat_type: Optional[str] = None) -> Dict[str, Any]:
    return {
        "connection_id": str(event.get("connection_id") or ""),
        "chat_type": chat_type or str(event.get("chat_type") or "private"),
        "user_id": str(event.get("user_id") or ""),
        "group_id": str(event.get("group_id") or ""),
    }


def text_segment(text: str) -> Dict[str, Any]:
    return {"type": "text", "data": {"text": text}}


def at_segment(user_id: str) -> Dict[str, Any]:
    return {"type": "at", "data": {"qq": user_id}}


def image_segment(file_url: str) -> Dict[str, Any]:
    return {"type": "image", "data": {"file": file_url}}


def video_segment(file_url: str) -> Dict[str, Any]:
    return {"type": "video", "data": {"file": file_url}}


def file_segment(file_url: str, name: str = "") -> Dict[str, Any]:
    data: Dict[str, Any] = {"file": file_url}
    if name:
        data["name"] = name
    return {"type": "file", "data": data}


def music_custom_segment(url: str, audio: str, title: str, content: str, image: str) -> Dict[str, Any]:
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


def text_node(user_id: str, nickname: str, text: str) -> Dict[str, Any]:
    return {
        "user_id": user_id,
        "nickname": nickname,
        "content": [text_segment(text)],
    }


def clone_segment(segment: Dict[str, Any]) -> Dict[str, Any]:
    return {
        "type": str(segment.get("type") or ""),
        "data": dict(ensure_dict(segment.get("data") or {})),
    }


def segment_text(segment: Dict[str, Any]) -> str:
    if str(segment.get("type") or "") != "text":
        return ""
    data = ensure_dict(segment.get("data") or {})
    return str(data.get("text") or "")


def run_plugin(plugin: BasePlugin) -> int:
    runtime = PluginRuntime(plugin)
    return runtime.run()
