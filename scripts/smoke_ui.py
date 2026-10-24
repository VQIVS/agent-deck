#!/usr/bin/env python3
"""Exercise worker creation, permission reply and composer in an isolated real PTY."""

import fcntl
import http.server
import json
import os
import pathlib
import pty
import select
import struct
import subprocess
import tempfile
import threading
import time
import urllib.parse


class FakeServer(http.server.ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self, directory):
        super().__init__(("127.0.0.1", 0), Handler)
        self.directory = directory
        self.calls = []
        self.sessions = [{"id": "ses_master", "title": "Master", "directory": directory}]
        self.permission = False
        self.approved = False
        self.created = False
        self.lock = threading.Lock()


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def reply(self, data, status=200):
        raw = json.dumps(data).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        path = urllib.parse.urlparse(self.path).path
        with self.server.lock:
            self.server.calls.append(("GET", self.path, None))
            if path == "/global/health":
                return self.reply({"healthy": True, "version": "1.19.0"})
            if path == "/config/providers":
                return self.reply({"providers": [{"id": "fake", "models": {"test": {"name": "Test"}}}]})
            if path == "/agent":
                return self.reply([{"name": "build"}])
            if path == "/session":
                return self.reply(self.server.sessions)
            if path == "/session/status":
                return self.reply({})
            if path == "/permission":
                permissions = []
                if self.server.permission and not self.server.approved:
                    permissions = [{"id": "per_smoke", "sessionID": "ses_master", "permission": "edit",
                                    "patterns": ["smoke.txt"], "metadata": {}}]
                return self.reply(permissions)
            if path == "/question" or path.endswith("/message") or path == "/project":
                return self.reply([])
            if path == "/event":
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.end_headers()
                self.wfile.write(b'data: {"type":"server.connected","properties":{}}\n\n')
                self.wfile.flush()
                return
            return self.reply({})

    def do_POST(self):
        path = urllib.parse.urlparse(self.path).path
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))) or b"{}")
        with self.server.lock:
            self.server.calls.append(("POST", self.path, body))
            if path == "/session":
                self.server.created = True
                session = {"id": "ses_worker", "title": body["title"], "directory": self.server.directory}
                self.server.sessions.append(session)
                return self.reply(session)
            if path == "/permission/per_smoke/reply":
                self.server.approved = body.get("reply") == "once"
                return self.reply(True)
            if path.endswith("/prompt_async"):
                return self.reply(None, 204)
            return self.reply({"error": "unexpected endpoint"}, 404)


class Terminal:
    def __init__(self, root, env):
        self.master, slave = pty.openpty()
        fcntl.ioctl(slave, 0x80087467, struct.pack("HHHH", 48, 180, 0, 0))
        self.process = subprocess.Popen(
            [str(root / "agent-deck"), "--team", "smoke", "--no-git", "--notify=false", "--theme=dark"],
            stdin=slave, stdout=slave, stderr=slave, env=env, start_new_session=True,
        )
        os.close(slave)
        self.output = bytearray()
        self.deadline = time.monotonic() + 40

    def pump(self):
        if time.monotonic() > self.deadline:
            raise RuntimeError("UI smoke exceeded 40 seconds")
        if select.select([self.master], [], [], 0.05)[0]:
            try:
                data = os.read(self.master, 65536)
            except OSError:
                data = b""
            self.output.extend(data)
            if b"\x1b]11;?" in data:
                self.send(b"\x1b]11;rgb:1414/1616/1919\x1b\\")
            if b"\x1b[6n" in data:
                self.send(b"\x1b[1;1R")
        if self.process.poll() is not None:
            raise RuntimeError("UI exited early: " + self.output[-6000:].decode(errors="replace"))

    def wait(self, predicate, description):
        end = min(self.deadline, time.monotonic() + 10)
        while time.monotonic() < end:
            self.pump()
            if predicate():
                return
        raise RuntimeError("Timed out waiting for " + description + "\n" + self.output[-9000:].decode(errors="replace"))

    def send(self, data):
        os.write(self.master, data)

    def settle(self, duration=0.5):
        until = time.monotonic() + duration
        while time.monotonic() < until:
            self.pump()

    def close(self):
        if self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait()
        os.close(self.master)


def main():
    root = pathlib.Path(__file__).resolve().parent.parent
    if not (root / "agent-deck").is_file():
        raise RuntimeError("Build the workspace binary first: make build")
    with tempfile.TemporaryDirectory(prefix="agent-deck-ui-") as temporary:
        directory = pathlib.Path(temporary)
        fake = FakeServer(str(directory))
        threading.Thread(target=fake.serve_forever, daemon=True).start()
        teams = directory / "config" / "agent-deck" / "teams"
        teams.mkdir(parents=True)
        team = {
            "version": 1, "name": "smoke", "repo": str(directory), "base": "main", "capacity": 2,
            "server": f"http://127.0.0.1:{fake.server_port}", "model": "fake/test",
            "members": [{"id": "smoke-master", "name": "Master", "role": "master", "mode": "shared",
                         "dir": str(directory), "session": "ses_master", "capability": "coder",
                         "permissions": {"read": "allow", "edit": "allow", "bash": "ask",
                                         "webfetch": "allow", "external_directory": "ask"}}],
        }
        (teams / "smoke.yaml").write_text(json.dumps(team))
        env = dict(os.environ, TERM="xterm-256color", XDG_CONFIG_HOME=str(directory / "config"),
                   XDG_STATE_HOME=str(directory / "state"), AGENT_DECK_SERVER=team["server"])
        terminal = Terminal(root, env)
        try:
            terminal.wait(lambda: b"+ Worker" in terminal.output, "team dashboard")
            terminal.send(b"\x1b[<0;12;2M\x1b[<0;12;2m")
            terminal.wait(lambda: b"Member name" in terminal.output, "worker form")
            terminal.wait(lambda: any("/config/providers" in call[1] for call in fake.calls), "configured provider catalog")
            terminal.send(b"\x15Smoke Worker")
            terminal.send(b"\x13")
            terminal.wait(lambda: fake.created, "fake session creation")
            terminal.wait(lambda: any(call[0] == "POST" and "/prompt_async" in call[1] for call in fake.calls),
                          "initial worker prompt")
            terminal.settle(1)
            with fake.lock:
                fake.permission = True
            terminal.send(b"r")
            terminal.wait(lambda: b"smoke.txt" in terminal.output, "pending permission")
            terminal.send(b"y")
            terminal.wait(lambda: fake.approved, "approve once endpoint")
            terminal.send(b"m")
            terminal.settle()
            terminal.send(b"smoke-composer-message\r")
            terminal.wait(lambda: any(call[0] == "POST" and "/prompt_async" in call[1]
                                     and any(part.get("text") == "smoke-composer-message"
                                             for part in (call[2] or {}).get("parts", []))
                                     for call in fake.calls), "composer prompt endpoint")
            for method, path, _ in fake.calls:
                if method == "POST" or urllib.parse.urlparse(path).path in ("/permission", "/config/providers"):
                    query = urllib.parse.parse_qs(urllib.parse.urlparse(path).query)
                    if query.get("directory") != [str(directory)]:
                        raise RuntimeError("Unscoped fake endpoint: " + path)
            print("UI PTY: team opened; configured providers loaded; worker session created; "
                  "permission approved once; composer message sent (isolated fake endpoints verified)")
        finally:
            terminal.close()
            fake.shutdown()
            fake.server_close()


if __name__ == "__main__":
    main()
