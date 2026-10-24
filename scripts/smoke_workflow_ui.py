#!/usr/bin/env python3
"""Create and progress a task through the real UI using isolated fake HTTP and XDG state."""

import json
import os
import pathlib
import tempfile
import threading

from smoke_ui import FakeServer, Terminal


def main():
    root = pathlib.Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory(prefix="agent-deck-workflow-ui-") as temporary:
        directory = pathlib.Path(temporary)
        fake = FakeServer(str(directory))
        threading.Thread(target=fake.serve_forever, daemon=True).start()
        teams = directory / "config" / "agent-deck" / "teams"
        teams.mkdir(parents=True)
        permissions = {"read": "allow", "edit": "allow", "bash": "ask",
                       "webfetch": "allow", "external_directory": "ask"}
        team = {
            "version": 1, "name": "smoke", "repo": str(directory), "base": "main", "capacity": 2,
            "server": f"http://127.0.0.1:{fake.server_port}", "model": "fake/test",
            "members": [
                {"id": "master-id", "name": "Master", "role": "master", "mode": "shared",
                 "dir": str(directory), "session": "ses_master", "capability": "coder", "permissions": permissions},
                {"id": "worker-id", "name": "Worker", "role": "worker", "mode": "shared",
                 "dir": str(directory), "capability": "coder", "permissions": permissions},
            ],
        }
        (teams / "smoke.yaml").write_text(json.dumps(team))
        env = dict(os.environ, TERM="xterm-256color", XDG_CONFIG_HOME=str(directory / "config"),
                   XDG_STATE_HOME=str(directory / "state"), AGENT_DECK_SERVER=team["server"])
        board_path = directory / "state" / "agent-deck" / "boards" / "smoke" / "board.json"

        def board():
            try:
                return json.loads(board_path.read_text())
            except (FileNotFoundError, json.JSONDecodeError):
                return {}

        def task():
            return next((item for item in board().get("board", {}).get("tasks", [])
                         if item["id"] == "smoke-task"), {})

        terminal = Terminal(root, env)
        try:
            terminal.deadline += 20
            terminal.wait(lambda: b"1 Dashboard" in terminal.output and b"5 Board" in terminal.output,
                          "persistent workspace tabs")
            terminal.wait(lambda: b"+ Worker" in terminal.output, "team dashboard")
            terminal.send(b"b")
            terminal.settle()
            terminal.send(b"n")
            terminal.wait(lambda: b"Task create" in terminal.output, "task creation form")
            values = ["smoke-task", "PTY workflow task", "Created through the actual task form",
                      "tests: Run fake verification", "internal/smoke/**", "", "high", "smoke", "", "Master", "10m"]
            for index, value in enumerate(values):
                if value:
                    terminal.send(value.encode())
                if index != len(values) - 1:
                    terminal.send(b"\t")
            terminal.send(b"\x13")
            terminal.wait(lambda: task().get("state") == "backlog", "persisted PTY task creation")
            terminal.settle()
            terminal.send(b"a")
            terminal.settle()
            terminal.send(b"Worker\x13")
            terminal.wait(lambda: task().get("assignee") == "Worker", "persisted assignee")
            terminal.settle()
            terminal.send(b"m")
            terminal.settle(0.15)
            terminal.send(b"2")
            terminal.wait(lambda: task().get("state") == "ready", "ready transition")
            terminal.settle()
            terminal.send(b"m")
            terminal.settle(0.15)
            terminal.send(b"3")
            terminal.wait(lambda: task().get("state") == "in_progress", "in-progress transition")
            terminal.settle()
            terminal.send(b"c")
            terminal.settle()
            terminal.send(b"PTY comment evidence\x13")
            terminal.wait(lambda: any(item.get("text") == "PTY comment evidence"
                                     for item in task().get("comments", [])), "persisted comment")
            terminal.settle()
            offset = len(terminal.output)
            terminal.send(b"\r")
            terminal.wait(lambda: b"PTY workflow task" in terminal.output[offset:]
                          and b"PTY comment evidence" in terminal.output[offset:], "task detail")
            saved = task()
            if saved.get("estimate", {}).get("duration") != "10m0s":
                raise RuntimeError("human duration did not round-trip")
            if len(saved.get("history", [])) < 4:
                raise RuntimeError("task history was not persisted")
            notices = []
            for path in directory.rglob("*.jsonl"):
                for line in path.read_text().splitlines():
                    try:
                        notices.append(json.loads(line))
                    except json.JSONDecodeError:
                        continue
            notice_calls = [call for call in fake.calls if call[0] == "POST" and "/prompt_async" in call[1]
                            and "smoke-task" in json.dumps(call[2])]
            # W2 ChangeTasks persists durable notice intent even when delivery runtime is unavailable.
            notice_files = [path for path in directory.rglob("*")
                            if path.is_file() and "notice" in path.name.lower()
                            and "smoke-task" in path.read_text(errors="replace")]
            task_notices = [notice for notice in notices if "smoke-task" in json.dumps(notice)]
            if not task_notices and not notice_calls and not notice_files:
                raise RuntimeError("no durable runtime notice or fake notification endpoint observed")
            print("Workflow PTY: Board opened; task created, assigned, moved ready/in_progress, commented, "
                  "detail opened; private state/history/duration and notification intent verified")
        finally:
            terminal.close()
            fake.shutdown()
            fake.server_close()


if __name__ == "__main__":
    main()
