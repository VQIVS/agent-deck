#!/usr/bin/env python3
"""Exercise Summary routing in a real PTY with isolated fake endpoints."""

import json
import os
import pathlib
import tempfile
import threading
import re

from smoke_ui import FakeServer, Terminal


def plain(output):
    return re.sub(r"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x1b]*(?:\x1b\\|\a))", "", output.decode(errors="replace"))


def main():
    root = pathlib.Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory(prefix="agent-deck-summary-") as temporary:
        directory = pathlib.Path(temporary)
        fake = FakeServer(str(directory))
        threading.Thread(target=fake.serve_forever, daemon=True).start()
        teams = directory / "config" / "agent-deck" / "teams"
        teams.mkdir(parents=True)
        team = {
            "version": 1, "name": "smoke", "repo": str(directory),
            "base": "main", "capacity": 1,
            "server": f"http://127.0.0.1:{fake.server_port}",
            "members": [{"id": "summary-master", "name": "Master", "role": "master",
                         "mode": "shared", "dir": str(directory), "session": "ses_master",
                         "capability": "coder", "permissions": {
                             "read": "allow", "edit": "allow", "bash": "ask",
                             "webfetch": "allow", "external_directory": "ask"}}],
        }
        (teams / "smoke.yaml").write_text(json.dumps(team))
        env = dict(os.environ, TERM="xterm-256color",
                   XDG_CONFIG_HOME=str(directory / "config"),
                   XDG_STATE_HOME=str(directory / "state"),
                   AGENT_DECK_SERVER=team["server"])
        terminal = Terminal(root, env)
        try:
            terminal.wait(lambda: "3 Summary" in plain(terminal.output), "workspace tabs")
            terminal.send(b"3")
            terminal.wait(lambda: "Coverage / LIMITED" in plain(terminal.output),
                          "Summary coverage")
            terminal.output.clear()
            terminal.send(b"m")
            terminal.settle()
            terminal.send(b"2")
            terminal.settle()
            if "Git TUI" in plain(terminal.output):
                raise RuntimeError("numeric member search switched workspace")
            terminal.send(b"\x1b")
            terminal.settle()
            terminal.send(b"c")
            terminal.wait(lambda: b"Refresh scope" in terminal.output,
                          "coverage details")
            terminal.send(b"\x1b")
            terminal.settle()
            terminal.send(b"r")
            terminal.settle()
            terminal.send(b"\x1b")
            terminal.settle()
            if any(method == "POST" for method, _, _ in fake.calls):
                raise RuntimeError("Summary issued a mutating fake request")
            print("Summary PTY PASS: open, member search, coverage, refresh, return; no mutations")
        finally:
            terminal.close()
            fake.shutdown()
            fake.server_close()


if __name__ == "__main__":
    main()
