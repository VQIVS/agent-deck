#!/usr/bin/env python3
"""Verify real ExecProcess terminal handoff using a disposable fake Git client."""

import fcntl
import json
import os
import pathlib
import pty
import struct
import subprocess
import tempfile
import threading
import time

from smoke_ui import FakeServer, Terminal
from smoke_summary import plain


class GitTerminal(Terminal):
    def __init__(self, root, env):
        self.master, slave = pty.openpty()
        fcntl.ioctl(slave, 0x80087467, struct.pack("HHHH", 24, 80, 0, 0))
        self.process = subprocess.Popen(
            [str(root / "agent-deck"), "--team", "smoke", "--notify=false", "--theme=dark"],
            stdin=slave, stdout=slave, stderr=slave, env=env, start_new_session=True,
        )
        os.close(slave)
        self.output = bytearray()
        self.deadline = time.monotonic() + 40


def main():
    root = pathlib.Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory(prefix="agent-deck-git-tui-") as temporary:
        directory = pathlib.Path(temporary) / "checkout"
        directory.mkdir()
        subprocess.run(["git", "init", "-q", str(directory)], check=True, timeout=10)
        binary_dir = pathlib.Path(temporary) / "bin"
        binary_dir.mkdir()
        client = binary_dir / "lazygit"
        client.write_text("#!/bin/sh\nprintf 'FAKE_GIT_CLIENT_READY\\n'\nread answer\nexit 0\n")
        client.chmod(0o755)
        fake = FakeServer(str(directory))
        threading.Thread(target=fake.serve_forever, daemon=True).start()
        teams = directory / "config" / "agent-deck" / "teams"
        teams.mkdir(parents=True)
        team = {
            "version": 1, "name": "smoke", "repo": str(directory), "base": "main", "capacity": 1,
            "server": f"http://127.0.0.1:{fake.server_port}",
            "members": [{"id": "git-master", "name": "Master", "role": "master", "mode": "shared",
                         "dir": str(directory), "session": "ses_master", "permissions": {
                             "read": "allow", "edit": "allow", "bash": "ask",
                             "webfetch": "allow", "external_directory": "ask"}}],
        }
        (teams / "smoke.yaml").write_text(json.dumps(team))
        env = dict(os.environ, TERM="xterm-256color", PATH=str(binary_dir) + os.pathsep + os.environ["PATH"],
                   XDG_CONFIG_HOME=str(directory / "config"), XDG_STATE_HOME=str(directory / "state"),
                   AGENT_DECK_SERVER=team["server"])
        terminal = GitTerminal(root, env)
        try:
            terminal.wait(lambda: "3 Summary" in plain(terminal.output), "workspace tabs")
            terminal.send(b"2")
            terminal.wait(lambda: "Open Git TUI" in plain(terminal.output), "built-in Git view")
            terminal.settle(1)
            terminal.send(b"t")
            terminal.wait(lambda: "Enter Open lazygit" in plain(terminal.output), "explicit launch chooser")
            if "FAKE_GIT_CLIENT_READY" in plain(terminal.output):
                raise RuntimeError("client ran without explicit launch")
            terminal.send(b"\r")
            terminal.wait(lambda: "FAKE_GIT_CLIENT_READY" in plain(terminal.output), "fake child owns terminal")
            terminal.output.clear()
            terminal.send(b"q\r")
            terminal.wait(lambda: "Open Git TUI" in plain(terminal.output), "dashboard restored after child exit")
            terminal.send(b"3")
            terminal.wait(lambda: "Summary /" in plain(terminal.output), "post-return keyboard routing")
            if terminal.process.poll() is not None:
                raise RuntimeError("dashboard did not survive handoff")
            print("Git TUI PTY PASS: explicit fake-child handoff, exit, terminal restore, keyboard routing")
        finally:
            terminal.close()
            fake.shutdown()
            fake.server_close()


if __name__ == "__main__":
    main()
