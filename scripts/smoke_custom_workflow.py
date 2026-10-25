#!/usr/bin/env python3
"""Exercise custom stages through production CLI and PTY with isolated fake HTTP."""

import argparse
import json
import os
import pathlib
import subprocess
import tempfile
import threading
import uuid

from pool_verify import PoolTerminal
from smoke_ui import FakeServer


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=pathlib.Path, required=True)
    parser.add_argument("--timeout", type=int, default=90)
    args = parser.parse_args()
    binary = args.binary.resolve()
    if not binary.is_file():
        raise RuntimeError("Provide a freshly built production binary")
    with tempfile.TemporaryDirectory(prefix="custom-workflow-") as temporary:
        root = pathlib.Path(temporary)
        fake = FakeServer(str(root))
        threading.Thread(target=fake.serve_forever, daemon=True).start()
        terminal = None
        try:
            name = "workflow-" + uuid.uuid4().hex[:10]
            teams = root / "config" / "agent-deck" / "teams"
            teams.mkdir(parents=True)
            stages = [("Ideas", "todo"), ("Queue", "ready"), ("Building", "active"),
                      ("Checking", "review"), ("Accepted", "done"), ("Delivered", "shipped"),
                      ("Waiting", "blocked"), ("Dropped", "cancelled")]
            permissions = {"read": "allow", "edit": "allow", "bash": "ask",
                           "webfetch": "allow", "external_directory": "ask"}
            team = {"version": 1, "name": name, "repo": str(root), "base": "main", "capacity": 3,
                    "server": f"http://127.0.0.1:{fake.server_port}", "model": "fake/test",
                    "workflow": {"stages": [{"name": stage, "color": "#89C9A0", "category": category}
                                             for stage, category in stages],
                                 "transitions": {"Delivered": {"from": ["Accepted"],
                                                                "by": ["release-manager", "user"]}}},
                    "members": [{"id": "fixture-" + member.lower(), "name": member,
                                 "role": "master" if member == "Master" else "worker", "mode": "shared",
                                 "dir": str(root), "session": "ses_master" if member == "Master" else "",
                                 "capability": "coder", "permissions": permissions}
                                for member in ("Master", "Builder", "Reviewer")]}
            (teams / (name + ".yaml")).write_text(json.dumps(team))
            env = dict(os.environ, HOME=str(root), XDG_CONFIG_HOME=str(root / "config"),
                       XDG_STATE_HOME=str(root / "state"), AGENT_DECK_SERVER=team["server"], TERM="xterm-256color")

            def cli(action, *flags, success=True):
                result = subprocess.run([str(binary), "task", action, "--team", name, *flags],
                                        env=env, capture_output=True, text=True, timeout=10)
                if (result.returncode == 0) != success:
                    raise RuntimeError(f"task {action}: exit={result.returncode}\n{result.stdout}\n{result.stderr}")
                return result

            def task():
                return json.loads(cli("show", "--id", "custom-smoke", "--json").stdout)

            cli("add", "--id", "custom-smoke", "--title", "Custom workflow PTY fixture",
                "--path", "fixture/**", "--criteria", "Observe fixture", "--assignee", "Builder",
                "--reviewer", "Reviewer")
            before_discovery = task()
            discovery = cli("workflow", "--id", "custom-smoke", "--json")
            if not all(stage in discovery.stdout for stage in ("Queue", "Building", "Delivered")):
                raise RuntimeError("Workflow discovery omitted configured stages")
            if task() != before_discovery:
                raise RuntimeError("Read-only workflow discovery mutated task")
            for stage in ("Queue", "Building", "Checking"):
                cli("move", "--id", "custom-smoke", "--state", stage)
                if task()["state"] != stage:
                    raise RuntimeError("CLI did not persist configured stage " + stage)
            cli("review", "--id", "custom-smoke", "--decision", "accept", "--comment", "Fixture review")
            accepted = task()
            if accepted["state"] != "Accepted":
                raise RuntimeError("Review did not use configured done destination")
            if not any(item["id"] == "custom-smoke" for item in json.loads(cli("pending-release", "--json").stdout)):
                raise RuntimeError("Accepted task absent from pending release queue")
            cli("move", "--id", "custom-smoke", "--state", "Delivered", success=False)
            if task() != accepted:
                raise RuntimeError("Denied versionless shipment changed persisted task")
            cli("move", "--id", "custom-smoke", "--state", "Delivered", "--version", "v-fixture")
            shipped = task()
            if shipped.get("release_version") != "v-fixture" or shipped.get("completed_at") != accepted.get("completed_at"):
                raise RuntimeError("Shipment lost version or double-counted completion time")
            if any(item["id"] == "custom-smoke" for item in json.loads(cli("pending-release", "--json").stdout)):
                raise RuntimeError("Shipped task remained pending release")
            cli("move", "--id", "custom-smoke", "--state", "Queue", success=False)
            terminal = PoolTerminal(binary, name, env, args.timeout)
            terminal.wait(lambda: b"5 Board" in terminal.output, "workspace")
            terminal.send(b"5")
            terminal.wait(lambda: b"Delivered" in terminal.output and b"Ideas" in terminal.output,
                          "custom Kanban stage columns")
            terminal.send(b"\x1b1")
            terminal.settle()
            terminal.send(b"\x1b[<0;3;3M\x1b[<0;3;3m")
            terminal.wait(lambda: b"Workflow" in terminal.output, "Team controls")

            def open_editor():
                offset = len(terminal.output)
                terminal.send(b"W")
                terminal.wait(lambda: b"Ctrl+S save" in terminal.output[offset:], "workflow editor")

            def rename_first(value):
                terminal.send(b"e")
                terminal.settle(0.1)
                terminal.send(b"\x7f" * len("Ideas") + value.encode() + b"\x13")
                terminal.settle(0.1)

            open_editor()
            rename_first("Cancelled draft")
            terminal.send(b"\x1b")
            terminal.settle()
            if "Cancelled draft" in cli("workflow", "--json").stdout:
                raise RuntimeError("Cancelled editor draft persisted")
            open_editor()
            rename_first("Queue")
            offset = len(terminal.output)
            terminal.send(b"\x13")
            terminal.wait(lambda: b"unique" in terminal.output[offset:], "invalid duplicate stage feedback")
            terminal.send(b"\x1b")
            terminal.settle()
            cli("add", "--id", "occupied-editor", "--title", "Occupied rename fixture", "--path", "editor/**")
            open_editor()
            rename_first("Research")
            # Concurrent real mutation invalidates the editor's loaded board revision.
            cli("comment", "--id", "occupied-editor", "--text", "Concurrent real board change")
            offset = len(terminal.output)
            terminal.send(b"\x13")
            terminal.wait(lambda: b"reload" in terminal.output[offset:], "stale editor feedback")
            if "Research" in cli("workflow", "--json").stdout:
                raise RuntimeError("Stale editor persisted draft")
            terminal.send(b"\x1b")
            terminal.settle()
            # Reload dashboard/board evidence before opening a new editor snapshot.
            terminal.send(b"\x1b")
            terminal.settle(1)
            terminal.send(b"5")
            terminal.settle(1)
            terminal.send(b"\x1b1")
            terminal.settle()
            terminal.send(b"\x1b[<0;3;3M\x1b[<0;3;3m")
            terminal.settle()
            open_editor()
            rename_first("Research")
            terminal.send(b"J")
            terminal.send(b"\x13")
            terminal.wait(lambda: "Research" in cli("workflow", "--json").stdout, "durable editor rename")
            occupied = json.loads(cli("show", "--id", "occupied-editor", "--json").stdout)
            if occupied["state"] != "Research" or len(occupied.get("comments", [])) != 1:
                raise RuntimeError("Editor rename lost occupied placement/evidence")
            definition = json.loads(cli("workflow", "--json").stdout)["workflow"]
            if [stage["name"] for stage in definition["stages"]][:2] != ["Queue", "Research"]:
                raise RuntimeError("Editor reorder was not persisted")
            terminal.close()
            terminal = None
            if task() != shipped:
                raise RuntimeError("Restart/read changed shipment evidence")
            print("PASS: custom CLI lifecycle/version/terminal/restart; PTY columns and editor cancel/invalid/stale/occupied save")
        finally:
            if terminal is not None:
                terminal.close()
            fake.shutdown()
            fake.server_close()


if __name__ == "__main__":
    main()
