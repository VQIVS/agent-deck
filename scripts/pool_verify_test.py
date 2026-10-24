#!/usr/bin/env python3
"""Fast protocol checks for the pool demo fake (no model or live server)."""

import json
import threading
import unittest
import urllib.error
import urllib.request

from pool_verify import PoolHandler
from smoke_ui import FakeServer


class PoolProtocolTests(unittest.TestCase):
    def test_unique_sessions_directory_scope_and_one_failed_brief(self):
        fake = FakeServer("/scratch")
        fake.RequestHandlerClass = PoolHandler
        fake.sessions, fake.serial, fake.failed_brief, fake.delivered_briefs = [], 0, False, []
        threading.Thread(target=fake.serve_forever, daemon=True).start()
        base = f"http://127.0.0.1:{fake.server_port}"

        def post(path, body):
            request = urllib.request.Request(base + path, json.dumps(body).encode(),
                                             {"Content-Type": "application/json"})
            with urllib.request.urlopen(request, timeout=2) as response:
                return response.read()

        try:
            first = json.loads(post("/session?directory=%2Fone", {"title": "worker"}))
            fake.sessions.clear()
            second = json.loads(post("/session?directory=%2Ftwo", {"title": "worker"}))
            self.assertNotEqual(first["id"], second["id"])
            with urllib.request.urlopen(base + "/session?directory=%2Fone", timeout=2) as response:
                self.assertEqual(json.load(response), [])
            brief = {"parts": [{"text": "You are assigned task T-1"}]}
            with self.assertRaises(urllib.error.HTTPError) as failure:
                post("/session/new/prompt_async?directory=%2Ftwo", brief)
            self.assertEqual(failure.exception.code, 503)
            post("/session/new/prompt_async?directory=%2Ftwo", brief)
            self.assertEqual(len(fake.delivered_briefs), 1)
        finally:
            fake.shutdown()
            fake.server_close()


if __name__ == "__main__":
    unittest.main()
