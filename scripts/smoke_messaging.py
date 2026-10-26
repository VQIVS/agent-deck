#!/usr/bin/env python3
"""Verify pinned composer, draft retention and unknown delivery in a real PTY."""

import http.server
import json
import os
import pathlib
import tempfile
import threading
import urllib.parse

from smoke_ui import Handler, Terminal
from smoke_git_tui import plain


class MessageHandler(Handler):
    def do_POST(self):
        path = urllib.parse.urlparse(self.path).path
        if not path.endswith("/prompt_async"):
            return super().do_POST()
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))) or b"{}")
        with self.server.lock:
            self.server.calls.append(("POST", self.path, body))
        if self.server.fail:
            self.connection.shutdown(2)
            self.connection.close()
            return
        self.reply(None, 204)


def main():
    root = pathlib.Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory(prefix="agent-deck-messaging-") as temporary:
        directory = pathlib.Path(temporary)
        fake = http.server.ThreadingHTTPServer(("127.0.0.1", 0), MessageHandler)
        fake.daemon_threads = True
        fake.directory, fake.calls, fake.lock, fake.fail = str(directory), [], threading.Lock(), False
        fake.sessions = [{"id": "ses_master", "title": "Master", "directory": str(directory)}]
        fake.permission, fake.approved, fake.created = False, False, False
        threading.Thread(target=fake.serve_forever, daemon=True).start()
        teams = directory / "config" / "agent-deck" / "teams"
        teams.mkdir(parents=True)
        team = {"version": 1, "name": "smoke", "repo": str(directory), "base": "main", "capacity": 1,
                "server": f"http://127.0.0.1:{fake.server_port}",
                "members": [{"id": "message-master", "name": "Master", "role": "master", "mode": "shared",
                             "dir": str(directory), "session": "ses_master", "permissions": {
                                 "read": "allow", "edit": "allow", "bash": "ask",
                                 "webfetch": "allow", "external_directory": "ask"}}]}
        (teams / "smoke.yaml").write_text(json.dumps(team))
        env = dict(os.environ, TERM="xterm-256color", XDG_CONFIG_HOME=str(directory / "config"),
                   XDG_STATE_HOME=str(directory / "state"), AGENT_DECK_SERVER=team["server"])
        terminal = Terminal(root, env)
        try:
            terminal.wait(lambda: "Send message" in plain(terminal.output), "visible Send message")
            terminal.send(b"m")
            terminal.wait(lambda: "pinned recipient" in plain(terminal.output), "pinned composer")
            terminal.send(b"draft-keep")
            terminal.send(b"\t")
            terminal.wait(lambda: "API directory:" in plain(terminal.output) and
                          "j/k scroll" in plain(terminal.output), "raw Tab identity")
            terminal.send(b"\x1b")
            terminal.settle()
            terminal.send(b"\x1b")
            terminal.settle()
            terminal.output.clear()
            terminal.send(b"m")
            terminal.wait(lambda: "draft-keep" in plain(terminal.output), "retained per-recipient draft")
            terminal.send(b"\r")
            terminal.wait(lambda: any(c[0] == "POST" for c in fake.calls), "pinned send endpoint")
            terminal.settle()
            sent = [c for c in fake.calls if c[0] == "POST"]
            if len(sent) != 1 or "/session/ses_master/prompt_async" not in sent[0][1]:
                raise RuntimeError("send targeted wrong session or duplicated")
            fake.fail = True
            terminal.output.clear()
            terminal.send(b"m")
            terminal.settle()
            terminal.send(b"uncertain-delivery\r")
            terminal.wait(lambda: "Outcome unknown" in plain(terminal.output), "unknown outcome warning")
            count = len([c for c in fake.calls if c[0] == "POST"])
            terminal.send(b"\r\r")
            terminal.settle(1)
            if len([c for c in fake.calls if c[0] == "POST"]) != count:
                raise RuntimeError("uncertain send allowed duplicate retry")
            print("Messaging PTY PASS: visible composer, Tab identity, draft retention, pinned send, unknown/no duplicate")
        finally:
            terminal.close()
            fake.shutdown()
            fake.server_close()


if __name__ == "__main__":
    main()
