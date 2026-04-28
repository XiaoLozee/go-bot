import sys
import types
import unittest
from io import BytesIO
from unittest.mock import patch
from urllib.error import HTTPError


gobot_runtime = types.ModuleType("gobot_runtime")


class BasePlugin:
    def start(self, env):
        self.env = env


def _identity(value, *args, **kwargs):
    return value


def _null(*args, **kwargs):
    return None


gobot_runtime.BasePlugin = BasePlugin
gobot_runtime.build_target = _identity
gobot_runtime.image_segment = _identity
gobot_runtime.run_plugin = _null
gobot_runtime.text_segment = _identity
gobot_runtime.video_segment = _identity
sys.modules.setdefault("gobot_runtime", gobot_runtime)

import main


class FakeResponse:
    def __init__(self, status, headers):
        self.status = status
        self.headers = headers

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc, tb):
        return False


class GetRemoteFileSizeTests(unittest.TestCase):
    def setUp(self):
        self.plugin = main.VideoParserPlugin()
        self.plugin.request_timeout_ms = 1500

    def test_uses_content_length_from_head(self):
        with patch("main.urllib.request.urlopen", return_value=FakeResponse(200, {"Content-Length": "4096"})):
            self.assertEqual(self.plugin.get_remote_file_size("https://example.com/video.mp4"), 4096)

    def test_falls_back_to_range_get_when_head_is_rejected(self):
        def fake_urlopen(request, timeout):
            if request.get_method() == "HEAD":
                raise HTTPError(request.full_url, 405, "Method Not Allowed", hdrs=None, fp=BytesIO())
            self.assertEqual(request.get_header("Range"), "bytes=0-0")
            return FakeResponse(206, {"Content-Range": "bytes 0-0/8192"})

        with patch("main.urllib.request.urlopen", side_effect=fake_urlopen):
            self.assertEqual(self.plugin.get_remote_file_size("https://example.com/video.mp4"), 8192)

    def test_reports_all_probe_attempts_when_size_is_missing(self):
        def fake_urlopen(request, timeout):
            return FakeResponse(200, {})

        with patch("main.urllib.request.urlopen", side_effect=fake_urlopen):
            with self.assertRaisesRegex(RuntimeError, r"HEAD missing size headers; GET missing size headers"):
                self.plugin.get_remote_file_size("https://example.com/video.mp4")


if __name__ == "__main__":
    unittest.main()
