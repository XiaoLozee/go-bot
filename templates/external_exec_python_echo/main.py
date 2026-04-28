#!/usr/bin/env python3
from __future__ import annotations

from gobot_plugin import BasePlugin, MessageEvent, PluginEnv, build_target, run_plugin


class TemplatePythonEchoPlugin(BasePlugin):
    def start(self, env: PluginEnv) -> None:
        super().start(env)
        self.keyword = str(env.config.get("keyword") or "/echo-demo").strip()
        self.response = str(env.config.get("response") or "external_exec python template is alive").strip()
        self.env.logger.info("template plugin started")

    def stop(self) -> None:
        self.env.logger.info("template plugin stopped")

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
    raise SystemExit(run_plugin(TemplatePythonEchoPlugin()))
