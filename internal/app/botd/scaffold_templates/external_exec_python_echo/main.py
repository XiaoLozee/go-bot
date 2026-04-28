#!/usr/bin/env python3
from __future__ import annotations

from gobot_plugin import BasePlugin, MessageEvent, PluginEnv, build_target, run_plugin


class GeneratedPlugin(BasePlugin):
    def start(self, env: PluginEnv) -> None:
        super().start(env)
        self.keyword = str(env.config.get("keyword") or "/{{.PluginID}}").strip()
        self.response = str(env.config.get("response") or "{{.PluginName}} is alive").strip()
        self.env.logger.info("{{.PluginName}} started")

    def stop(self) -> None:
        self.env.logger.info("{{.PluginName}} stopped")

    def handle_event(self, event: MessageEvent) -> None:
        if str(event.get("kind") or "") != "message":
            return

        text = str(event.get("raw_text") or "").strip()
        if not self.keyword or text != self.keyword:
            return

        target = build_target(event)
        reply_to = str(event.get("message_id") or "")
        self.env.messenger.reply_text(target, reply_to, self.response)


if __name__ == "__main__":
    raise SystemExit(run_plugin(GeneratedPlugin()))
