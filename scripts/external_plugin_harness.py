#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import queue
import subprocess
import sys
import threading
import time
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional

CALL_BOT_GET_STRANGER_INFO = "bot.get_stranger_info"
CALL_BOT_GET_GROUP_INFO = "bot.get_group_info"
CALL_BOT_GET_GROUP_MEMBERS = "bot.get_group_member_list"
CALL_BOT_GET_FORWARD_MESSAGE = "bot.get_forward_message"
CALL_BOT_SEND_GROUP_FORWARD = "bot.send_group_forward"

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(errors="replace")
if hasattr(sys.stderr, "reconfigure"):
    sys.stderr.reconfigure(errors="replace")


class HarnessError(RuntimeError):
    pass


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run a Python-first external plugin with a local stdio harness.")
    parser.add_argument("--plugin-dir", required=True, help="Plugin directory, for example plugins/menu_hint")
    parser.add_argument("--config-file", help="JSON file for plugin config")
    parser.add_argument("--config-json", help="Inline JSON object for plugin config")
    parser.add_argument("--catalog-file", help="JSON file for plugin catalog list")
    parser.add_argument("--bot-api-fixture", help="JSON file for mocked BotAPI responses")
    parser.add_argument("--event-file", action="append", default=[], help="JSON file containing one event or an array of events")
    parser.add_argument("--text", help="Inline raw_text for a generated message event")
    parser.add_argument("--chat-type", choices=["private", "group"], default="group")
    parser.add_argument("--connection-id", default="gobot-dev")
    parser.add_argument("--user-id", default="10001")
    parser.add_argument("--group-id", default="123456")
    parser.add_argument("--message-id", default="msg-1")
    parser.add_argument("--self-id", default="bot-self")
    parser.add_argument("--platform", default="onebot")
    parser.add_argument("--app-name", default="go-bot-dev")
    parser.add_argument("--environment", default="dev")
    parser.add_argument("--owner-qq", default="10001")
    parser.add_argument("--event-timeout", type=float, default=2.0, help="How long to wait for plugin output after each event")
    parser.add_argument("--ready-timeout", type=float, default=5.0, help="How long to wait for plugin ready")
    parser.add_argument("--raw", action="store_true", help="Print raw packets from plugin stdout")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    plugin_dir = Path(args.plugin_dir).resolve()
    if not plugin_dir.is_dir():
        raise SystemExit(f"plugin directory not found: {plugin_dir}")

    manifest = load_manifest(plugin_dir)
    config = merge_dicts(load_json_object(args.config_file), parse_json_object(args.config_json))
    catalog = load_catalog(args.catalog_file, manifest)
    bot_api_fixture = load_json_object(args.bot_api_fixture)
    events = load_events(args)

    env = os.environ.copy()
    env.setdefault("PYTHONUTF8", "1")
    env.setdefault("PYTHONIOENCODING", "utf-8")
    common_paths = resolve_common_paths(plugin_dir)
    if common_paths:
        env["PYTHONPATH"] = os.pathsep.join(common_paths + ([env["PYTHONPATH"]] if env.get("PYTHONPATH") else []))

    command = [sys.executable, "-X", "utf8", str(plugin_dir / "main.py")]
    process = subprocess.Popen(
        command,
        cwd=str(plugin_dir),
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        encoding="utf-8",
        env=env,
    )
    if process.stdin is None or process.stdout is None or process.stderr is None:
        raise SystemExit("failed to create stdio pipes")

    stdout_queue: "queue.Queue[tuple[str, str]]" = queue.Queue()
    stderr_thread = threading.Thread(target=stream_stderr, args=(process.stderr,), daemon=True)
    stdout_thread = threading.Thread(target=stream_stdout, args=(process.stdout, stdout_queue), daemon=True)
    stderr_thread.start()
    stdout_thread.start()

    try:
        send_packet(
            process.stdin,
            {
                "type": "start",
                "payload": {
                    "plugin": manifest,
                    "config": config,
                    "catalog": catalog,
                    "app": {
                        "name": args.app_name,
                        "environment": args.environment,
                        "owner_qq": args.owner_qq,
                    },
                },
            },
        )

        wait_for_ready(process, process.stdin, stdout_queue, args.ready_timeout, bot_api_fixture, args.raw)

        for event in events:
            send_packet(process.stdin, {"type": "event", "payload": {"event": event}})
            pump_until_idle(process, process.stdin, stdout_queue, args.event_timeout, bot_api_fixture, args.raw)

        send_packet(process.stdin, {"type": "stop", "payload": {}})
        process.stdin.close()
        pump_until_exit(process, stdout_queue, bot_api_fixture, args.raw)
    finally:
        if process.poll() is None:
            process.kill()

    return int(process.returncode or 0)


def load_manifest(plugin_dir: Path) -> Dict[str, Any]:
    for file_name in ("plugin.yaml", "plugin.yml"):
        path = plugin_dir / file_name
        if path.exists():
            manifest = parse_simple_yaml(path.read_text(encoding="utf-8"))
            manifest.setdefault("kind", "external_exec")
            return manifest
    raise HarnessError(f"manifest not found under {plugin_dir}")


def parse_simple_yaml(raw: str) -> Dict[str, Any]:
    data: Dict[str, Any] = {}
    for line in raw.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if line.startswith((" ", "\t")):
            continue
        key, sep, value = line.partition(":")
        if not sep:
            continue
        data[key.strip()] = strip_yaml_scalar(value.strip())
    return data


def strip_yaml_scalar(value: str) -> str:
    if len(value) >= 2 and value[0] == value[-1] and value[0] in {'"', "'"}:
        return value[1:-1]
    return value


def resolve_common_paths(plugin_dir: Path) -> List[str]:
    candidates = [plugin_dir / "_common", plugin_dir.parent / "_common"]
    resolved: List[str] = []
    for candidate in candidates:
        if (candidate / "gobot_runtime.py").exists():
            resolved.append(str(candidate))
    return resolved


def load_json_object(path_value: Optional[str]) -> Dict[str, Any]:
    if not path_value:
        return {}
    path = Path(path_value).resolve()
    payload = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise HarnessError(f"expected JSON object: {path}")
    return payload


def parse_json_object(raw: Optional[str]) -> Dict[str, Any]:
    if not raw:
        return {}
    payload = json.loads(raw)
    if not isinstance(payload, dict):
        raise HarnessError("--config-json must be a JSON object")
    return payload


def merge_dicts(*items: Dict[str, Any]) -> Dict[str, Any]:
    merged: Dict[str, Any] = {}
    for item in items:
        merged.update(item)
    return merged


def load_catalog(path_value: Optional[str], manifest: Dict[str, Any]) -> List[Dict[str, Any]]:
    if path_value:
        path = Path(path_value).resolve()
        payload = json.loads(path.read_text(encoding="utf-8"))
        if not isinstance(payload, list):
            raise HarnessError(f"expected JSON array: {path}")
        return [ensure_dict(item) for item in payload]
    return [
        {
            "id": str(manifest.get("id") or "plugin-under-test"),
            "name": str(manifest.get("name") or manifest.get("id") or "plugin-under-test"),
            "version": str(manifest.get("version") or "0.1.0"),
            "description": str(manifest.get("description") or ""),
            "kind": "external_exec",
            "enabled": True,
            "builtin": False,
        }
    ]


def load_events(args: argparse.Namespace) -> List[Dict[str, Any]]:
    events: List[Dict[str, Any]] = []
    if args.event_file:
        for index, file_name in enumerate(args.event_file, start=1):
            payload = json.loads(Path(file_name).resolve().read_text(encoding="utf-8"))
            if isinstance(payload, list):
                for item_offset, item in enumerate(payload, start=1):
                    events.append(apply_event_defaults(args, ensure_dict(item), len(events) + 1, f"evt-{index}-{item_offset}"))
            elif isinstance(payload, dict) and isinstance(payload.get("events"), list):
                for item_offset, item in enumerate(payload["events"], start=1):
                    events.append(apply_event_defaults(args, ensure_dict(item), len(events) + 1, f"evt-{index}-{item_offset}"))
            elif isinstance(payload, dict):
                events.append(apply_event_defaults(args, payload, len(events) + 1, f"evt-{index}"))
            else:
                raise HarnessError(f"unsupported event payload type in {file_name}")
    elif args.text is not None:
        events.append(
            build_message_event(
                args,
                raw_text=args.text,
                event_id="evt-1",
            )
        )
    else:
        raise HarnessError("provide --text or at least one --event-file")
    return events


def apply_event_defaults(args: argparse.Namespace, event: Dict[str, Any], ordinal: int, event_id: str) -> Dict[str, Any]:
    if str(event.get("kind") or "message") != "message":
        event.setdefault("kind", "message")
    raw_text = str(event.get("raw_text") or "")
    default_event = build_message_event(args, raw_text=raw_text, event_id=event_id)
    merged = dict(default_event)
    merged.update(event)
    if "segments" not in merged or not isinstance(merged.get("segments"), list):
        merged["segments"] = [{"type": "text", "data": {"text": raw_text}}]
    meta = ensure_dict(merged.get("meta"))
    meta.setdefault("self_id", args.self_id)
    merged["meta"] = meta
    merged.setdefault("id", f"evt-{ordinal}")
    return merged


def build_message_event(args: argparse.Namespace, *, raw_text: str, event_id: str) -> Dict[str, Any]:
    timestamp = time.strftime("%Y-%m-%dT%H:%M:%S%z")
    return {
        "id": event_id,
        "connection_id": args.connection_id,
        "platform": args.platform,
        "kind": "message",
        "chat_type": args.chat_type,
        "user_id": args.user_id,
        "group_id": args.group_id if args.chat_type == "group" else "",
        "message_id": args.message_id,
        "raw_text": raw_text,
        "segments": [{"type": "text", "data": {"text": raw_text}}],
        "timestamp": timestamp,
        "meta": {"self_id": args.self_id},
    }


def wait_for_ready(
    process: subprocess.Popen[str],
    stdin: Any,
    stdout_queue: "queue.Queue[tuple[str, str]]",
    timeout: float,
    fixture: Dict[str, Any],
    raw: bool,
) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise HarnessError(f"plugin exited before ready, code={process.returncode}")
        try:
            kind, line = stdout_queue.get(timeout=0.1)
        except queue.Empty:
            continue
        if kind != "stdout":
            continue
        packet = parse_packet(line, raw)
        if packet is None:
            continue
        if handle_packet(stdin, packet, fixture):
            return
    raise HarnessError(f"plugin did not send ready within {timeout:.1f}s")


def pump_until_idle(
    process: subprocess.Popen[str],
    stdin: Any,
    stdout_queue: "queue.Queue[tuple[str, str]]",
    timeout: float,
    fixture: Dict[str, Any],
    raw: bool,
) -> None:
    deadline = time.monotonic() + timeout
    while True:
        if process.poll() is not None:
            return
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            return
        try:
            kind, line = stdout_queue.get(timeout=min(0.1, remaining))
        except queue.Empty:
            continue
        if kind != "stdout":
            continue
        packet = parse_packet(line, raw)
        if packet is None:
            continue
        handle_packet(stdin, packet, fixture)
        deadline = time.monotonic() + timeout


def pump_until_exit(
    process: subprocess.Popen[str],
    stdout_queue: "queue.Queue[tuple[str, str]]",
    fixture: Dict[str, Any],
    raw: bool,
) -> None:
    while True:
        try:
            kind, line = stdout_queue.get(timeout=0.1)
        except queue.Empty:
            if process.poll() is not None:
                break
            continue
        if kind != "stdout":
            continue
        packet = parse_packet(line, raw)
        if packet is None:
            continue
        handle_packet(None, packet, fixture)
        if process.poll() is not None and stdout_queue.empty():
            break
    process.wait(timeout=3.0)


def parse_packet(line: str, raw: bool) -> Optional[Dict[str, Any]]:
    if raw:
        print(f"[plugin][raw] {line}")
    try:
        payload = json.loads(line)
    except json.JSONDecodeError:
        print(f"[plugin][stdout] {line}")
        return None
    return ensure_dict(payload)


def handle_packet(stdin: Any, packet: Dict[str, Any], fixture: Dict[str, Any]) -> bool:
    packet_type = str(packet.get("type") or "")
    payload = ensure_dict(packet.get("payload"))

    if packet_type == "ready":
        print(f"[plugin][ready] {str(payload.get('message') or 'ready')}")
        return True
    if packet_type == "log":
        print(f"[plugin][log][{str(payload.get('level') or 'info')}] {str(payload.get('message') or '')}")
        return False
    if packet_type == "send_text":
        print(f"[plugin][send_text] target={format_target(payload.get('target'))} text={str(payload.get('text') or '')}")
        return False
    if packet_type == "reply_text":
        print(
            f"[plugin][reply_text] target={format_target(payload.get('target'))} reply_to={str(payload.get('reply_to') or '')} text={str(payload.get('text') or '')}"
        )
        return False
    if packet_type == "send_segments":
        print(
            "[plugin][send_segments] "
            f"target={format_target(payload.get('target'))} segments={json.dumps(payload.get('segments') or [], ensure_ascii=False)}"
        )
        return False
    if packet_type == "call":
        response = build_call_response(payload, fixture)
        print(f"[plugin][call] method={str(payload.get('method') or '')} mocked")
        if stdin is not None:
            send_packet(stdin, {"type": "response", "payload": response})
        return False

    print(f"[plugin][packet] type={packet_type} payload={json.dumps(payload, ensure_ascii=False)}")
    return False


def build_call_response(payload: Dict[str, Any], fixture: Dict[str, Any]) -> Dict[str, Any]:
    call_id = str(payload.get("id") or "")
    method = str(payload.get("method") or "")
    call_payload = ensure_dict(payload.get("payload"))

    call_results = ensure_dict(fixture.get("call_results"))
    if method in call_results:
        return {"id": call_id, "result": call_results[method]}

    if method == CALL_BOT_GET_STRANGER_INFO:
        stranger_info = ensure_dict(ensure_dict(fixture.get("stranger_info")).get(str(call_payload.get("user_id") or "")))
        if not stranger_info:
            user_id = str(call_payload.get("user_id") or "")
            stranger_info = {
                "user_id": user_id,
                "nickname": f"User {user_id}" if user_id else "Harness User",
            }
        return {"id": call_id, "result": stranger_info}

    if method == CALL_BOT_GET_GROUP_INFO:
        group_id = str(call_payload.get("group_id") or "")
        group_info = ensure_dict(ensure_dict(fixture.get("group_info")).get(group_id))
        if not group_info:
            group_info = {
                "group_id": group_id,
                "group_name": f"Group {group_id}" if group_id else "Harness Group",
                "member_count": 3,
                "max_member_count": 200,
            }
        return {"id": call_id, "result": group_info}

    if method == CALL_BOT_GET_GROUP_MEMBERS:
        group_id = str(call_payload.get("group_id") or "")
        group_members = ensure_dict(fixture.get("group_members")).get(group_id)
        if not isinstance(group_members, list):
            group_members = default_group_members(group_id)
        return {"id": call_id, "result": group_members}

    if method == CALL_BOT_GET_FORWARD_MESSAGE:
        forward_id = str(call_payload.get("forward_id") or "")
        forward_messages = ensure_dict(fixture.get("forward_messages"))
        forward_message = ensure_dict(forward_messages.get(forward_id))
        if not forward_message:
            forward_message = {
                "id": forward_id,
                "nodes": [
                    {
                        "user_id": "10001",
                        "nickname": "Alice",
                        "content": [{"type": "text", "data": {"text": "hello from forward message"}}],
                    }
                ],
            }
        return {"id": call_id, "result": forward_message}

    if method == CALL_BOT_SEND_GROUP_FORWARD:
        nodes = payload.get("payload")
        node_count = len(ensure_dict({"nodes": ensure_dict(call_payload).get("nodes")}).get("nodes") or [])
        return {"id": call_id, "result": {"ok": True, "node_count": node_count, "echo": nodes}}

    return {"id": call_id, "error": f"unsupported mock method: {method}"}


def default_group_members(group_id: str) -> List[Dict[str, Any]]:
    return [
        {"group_id": group_id, "user_id": "10001", "nickname": "Alice", "card": "Alice"},
        {"group_id": group_id, "user_id": "10002", "nickname": "Bob", "card": "Bob"},
        {"group_id": group_id, "user_id": "10003", "nickname": "Carol", "card": "Carol"},
    ]


def send_packet(stdin: Any, packet: Dict[str, Any]) -> None:
    stdin.write(json.dumps(packet, ensure_ascii=False) + "\n")
    stdin.flush()


def stream_stdout(stdout: Any, stdout_queue: "queue.Queue[tuple[str, str]]") -> None:
    for line in stdout:
        stdout_queue.put(("stdout", line.rstrip("\r\n")))


def stream_stderr(stderr: Any) -> None:
    for line in stderr:
        print(f"[plugin][stderr] {line.rstrip()}" )


def format_target(target: Any) -> str:
    payload = ensure_dict(target)
    chat_type = str(payload.get("chat_type") or "")
    connection_id = str(payload.get("connection_id") or "")
    user_id = str(payload.get("user_id") or "")
    group_id = str(payload.get("group_id") or "")
    suffix = group_id if chat_type == "group" else user_id
    return f"{connection_id}/{chat_type}/{suffix}"


def ensure_dict(value: Any) -> Dict[str, Any]:
    if isinstance(value, dict):
        return value
    return {}


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except HarnessError as exc:
        raise SystemExit(f"harness error: {exc}")
