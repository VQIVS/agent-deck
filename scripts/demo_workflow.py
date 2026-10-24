#!/usr/bin/env python3
"""Scripted fake master drives real MCP/task persistence and a real PTY board.

This demonstrates integration, not model planning or independently passing tests.
Build ./agent-deck first. All HTTP, configs, credentials and sessions are scratch-only.
"""

import argparse
import json
import os
import pathlib
import re
import subprocess
import threading
import time
import urllib.parse
import urllib.request

from smoke_ui import FakeServer, Handler, Terminal
import tempfile


class DemoHandler(Handler):
    def reply(self, data, status=200):
        try:
            super().reply(data, status)
        except (BrokenPipeError, ConnectionResetError):
            pass  # Dashboard shutdown cancels in-flight fake requests.

    def do_PATCH(self):
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))) or b"{}")
        with self.server.lock:
            self.server.calls.append(("PATCH", self.path, body))
        self.reply(True)

    def do_POST(self):
        if urllib.parse.urlparse(self.path).path != "/session":
            return super().do_POST()
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))))
        directory = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query)["directory"][0]
        with self.server.lock:
            session = {"id": f"ses_demo_{len(self.server.sessions)}", "title": body["title"],
                       "directory": directory}
            self.server.sessions.append(session)
            self.server.calls.append(("POST", self.path, body))
        self.reply(session)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=pathlib.Path, help="write state snapshots and raw PTY frames here")
    parser.add_argument("--explicit-assignment", action="store_true",
                        help="use policy-gated assign_task instead of requiring the runtime auto tick")
    args = parser.parse_args()
    root = pathlib.Path(__file__).resolve().parent.parent
    if not (root / "agent-deck").is_file():
        raise RuntimeError("Build the workspace binary first: make build")
    with tempfile.TemporaryDirectory(prefix="agent-deck-demo-") as temporary:
        directory = pathlib.Path(temporary).resolve()
        repo = directory / "repo"
        repo.mkdir()

        git_env = {key: value for key, value in os.environ.items()
                   if not key.startswith("GIT_")}
        git_env.update(GIT_AUTHOR_NAME="Workflow Demo", GIT_AUTHOR_EMAIL="demo@example.invalid",
                       GIT_COMMITTER_NAME="Workflow Demo", GIT_COMMITTER_EMAIL="demo@example.invalid",
                       GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=os.devnull)

        def scratch_git(*arguments):
            subprocess.run(["git", "-c", "core.hooksPath=" + os.devnull, "-c", "commit.gpgsign=false",
                            "-C", str(repo), *arguments], env=git_env, check=True,
                           stdout=subprocess.PIPE, stderr=subprocess.PIPE)

        scratch_git("init", "-b", "main")
        (repo / "greeting").mkdir()
        (repo / "greeting" / "README.md").write_text("Scratch workflow demonstration; no software implementation claimed.\n")
        scratch_git("add", "--", "greeting/README.md")
        scratch_git("commit", "-m", "chore(demo): initialize scratch greeting fixture")
        fake = FakeServer(str(repo))
        fake.RequestHandlerClass = DemoHandler
        fake.sessions = [{"id": "ses_master", "title": "Master", "directory": str(repo)},
                         {"id": "ses_worker", "title": "Worker", "directory": str(repo)},
                         {"id": "ses_reviewer", "title": "Reviewer", "directory": str(repo)}]
        threading.Thread(target=fake.serve_forever, daemon=True).start()
        teams = directory / "config" / "agent-deck" / "teams"
        teams.mkdir(parents=True)
        permissions = {"read": "allow", "edit": "allow", "bash": "ask", "webfetch": "allow",
                       "external_directory": "ask"}
        members = [{"id": name.lower() + "-id", "name": name, "role": "master" if name == "Master" else "worker",
                    "mode": "shared", "dir": str(repo), "session": "ses_" + name.lower(),
                    "capability": "coder", "permissions": permissions} for name in ("Master", "Worker", "Reviewer")]
        team = {"version": 1, "name": "smoke", "repo": str(repo), "base": "main", "capacity": 3,
                "server": f"http://127.0.0.1:{fake.server_port}", "model": "fake/test", "members": members}
        team_path = teams / "smoke.yaml"
        team_path.write_text(json.dumps(team))
        env = dict(git_env, TERM="xterm-256color", XDG_CONFIG_HOME=str(directory / "config"),
                   XDG_STATE_HOME=str(directory / "state"), AGENT_DECK_SERVER=team["server"],
                   PATH=os.environ["PATH"])
        terminal = Terminal(root, env)
        terminal.deadline += 100
        captures = []
        try:
            terminal.wait(lambda: b"+ Worker" in terminal.output, "dashboard")
            state_root = directory / "state" / "agent-deck"
            discovery = None
            terminal.wait(lambda: bool(list(state_root.rglob("*.runtime.json"))), "runtime discovery")
            discovery = json.loads(next(state_root.rglob("*.runtime.json")).read_text())

            def http(path, token, body):
                request = urllib.request.Request(path, json.dumps(body).encode(),
                                                 {"Authorization": "Bearer " + token,
                                                  "Content-Type": "application/json"})
                with urllib.request.urlopen(request, timeout=15) as response:
                    return json.loads(response.read())

            tokens = {}
            for member in members:
                http(discovery["endpoint"].replace("/mcp", "/user"), discovery["token"],
                     {"action": "setup_workflow", "member": member["name"]})
                # The source file is YAML after Save; locate only private generated configs.
                for config in state_root.rglob("opencode.json"):
                    if config.parent.name == member["id"]:
                        entry = next(iter(json.loads(config.read_text())["mcp"].values()))
                        tokens[member["name"]] = entry["headers"]["Authorization"].removeprefix("Bearer ")
            if len(tokens) != 3:
                raise RuntimeError("distinct member controls were not installed")
            call_id = 0

            def call(member, tool, arguments):
                nonlocal call_id
                call_id += 1
                response = http(discovery["endpoint"], tokens[member],
                                {"jsonrpc": "2.0", "id": call_id, "method": "tools/call",
                                 "params": {"name": tool, "arguments": arguments}})
                result = response.get("result", {})
                if response.get("error") or result.get("isError"):
                    raise RuntimeError(f"{member}/{tool}: {response}")
                return json.loads(result["content"][0]["text"])

            goal = "Add a greeting endpoint with acceptance evidence and independent review"
            call("Master", "create_task", {"title": "Greeting endpoint", "description": goal,
                           "criteria": ["Greeting output matches specification"], "paths": ["greeting/**"],
                           "reviewer": "Reviewer", "estimate": "10m"})
            board_path = state_root / "boards" / "smoke" / "board.json"

            def task():
                return json.loads(board_path.read_text())["board"]["tasks"][0]

            task_id = task()["id"]
            def capture(expected):
                terminal.wait(lambda: task()["state"] == expected, expected + " persisted")
                offset = len(terminal.output)
                terminal.send(b"b")
                terminal.settle(0.8)
                frame = bytes(terminal.output[offset:]).decode(errors="replace")
                plain = re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", frame)
                if "Greeting endpoint" not in plain:
                    raise RuntimeError("PTY did not render the persisted task in " + expected)
                captures.append({"state": expected, "task": task(), "pty": frame})
                terminal.send(b"\x1b")
                terminal.settle(0.2)
                print(f"{expected}: {task_id} · {task()['title']}")

            capture("backlog")
            call("Master", "set_assignee", {"id": task_id, "assignee": "Worker", "reviewer": "Reviewer"})
            call("Master", "move_task", {"id": task_id, "state": "ready"})
            capture("ready")
            if args.explicit_assignment:
                call("Master", "assign_task", {"task": task_id, "worker": "Worker"})
            capture("in_progress")
            criterion = task()["criteria"][0]["id"]
            call("Worker", "update_my_task", {"id": task_id, "checked": [criterion], "state": "review",
                                              "comment": "Scripted demonstration claim; no software test executed"})
            capture("review")
            call("Reviewer", "review_task", {"id": task_id, "decision": "accept",
                                              "comment": "Scripted reviewer accepts demo evidence"})
            if task()["state"] != "done":
                call("Master", "move_task", {"id": task_id, "state": "done"})
            capture("done")
            saved = task()
            if not saved.get("review", {}).get("accepted"):
                raise RuntimeError("done task lacks accepted reviewer evidence")
            history_states = [item.get("to") for item in saved.get("history", []) if item.get("field") == "state"]
            if not all(state in history_states for state in ("ready", "in_progress", "review")):
                raise RuntimeError("workflow history did not record all transitions: " + repr(history_states))
            if not any(item.get("field") == "review" for item in saved.get("history", [])):
                raise RuntimeError("review acceptance was not attributed in task history")
            notices = []
            for journal in teams.glob("*.channel.jsonl"):
                notices.extend(json.loads(line) for line in journal.read_text().splitlines() if line)
            task_notices = [notice for notice in notices if task_id in notice.get("text", "")]
            if not task_notices:
                raise RuntimeError("no durable task notification observed")
            expected = ["backlog", "ready", "in_progress", "review", "done"]
            if [capture["state"] for capture in captures] != expected:
                raise RuntimeError("five-state capture order differs")
            if not any(call[0] == "PATCH" for call in fake.calls):
                raise RuntimeError("automatic assignment did not install task lease rules")
            transcript = ["Driver: scripted fake master (not model planning)", "Goal: " + goal]
            for capture in captures:
                item = capture["task"]
                transcript.append(f"{capture['state']}: {item['id']} | {item['title']} | "
                                  f"assignee={item.get('assignee', '-')} | reviewer={item.get('reviewer', '-')}")
            transcript.append("Verified: actual scratch Git main commit, persisted history, accepted review, "
                              "private notices, task lease PATCH, PTY board")
            if args.output:
                args.output.mkdir(parents=True, exist_ok=True)
                (args.output / "transcript.txt").write_text("\n".join(transcript) + "\n")
                (args.output / "workflow.json").write_text(json.dumps({"driver": "scripted fake master",
                    "goal": goal, "captures": captures, "notices": task_notices}, indent=2))
            print("PASS: real MCP HTTP + private controls + persisted five-state board + PTY capture; "
                  "scripted actors, not model planning or passing software tests")
        finally:
            terminal.close()
            fake.shutdown()
            fake.server_close()


if __name__ == "__main__":
    main()
