#!/usr/bin/env python3
"""Isolated production-runtime autoscaling demo with deterministic HTTP actors and PTY evidence."""

import argparse
import fcntl
import json
import os
import pathlib
import pty
import re
import struct
import subprocess
import tempfile
import threading
import time
import urllib.parse
import urllib.request
import uuid

from demo_workflow import DemoHandler
from smoke_ui import FakeServer, Terminal


class PoolHandler(DemoHandler):
    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        if parsed.path == "/mcp":
            return self.reply({"agent-deck": {"status": "connected"}})
        if parsed.path == "/session":
            directory = urllib.parse.parse_qs(parsed.query).get("directory", [""])[0]
            with self.server.lock:
                return self.reply([s for s in self.server.sessions if s["directory"] == directory])
        try:
            return super().do_GET()
        except (BrokenPipeError, ConnectionResetError):
            pass

    def do_POST(self):
        path = urllib.parse.urlparse(self.path).path
        if path == "/session":
            body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))) or b"{}")
            directory = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query)["directory"][0]
            with self.server.lock:
                self.server.serial += 1
                session = {"id": f"ses_pool_{self.server.serial}", "title": body["title"], "directory": directory}
                self.server.sessions.append(session)
                self.server.calls.append(("POST", self.path, body))
            return self.reply(session)
        if path in ("/mcp", "/mcp/agent-deck/connect") or path.endswith(("/abort", "/summarize")):
            body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))) or b"{}")
            with self.server.lock:
                self.server.calls.append(("POST", self.path, body))
            return self.reply(True)
        if path.endswith("/prompt_async"):
            body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))) or b"{}")
            text = "\n".join(p.get("text", "") for p in body.get("parts", []))
            with self.server.lock:
                self.server.calls.append(("POST", self.path, body))
                if "You are assigned task" in text and not self.server.failed_brief:
                    self.server.failed_brief = True
                    return self.reply({"error": "injected first brief failure"}, 503)
                if "You are assigned task" in text:
                    self.server.delivered_briefs.append(text)
            return self.reply(None, 204)
        return super().do_POST()


class PoolTerminal(Terminal):
    def __init__(self, binary, team, env, timeout):
        self.master, slave = pty.openpty()
        fcntl.ioctl(slave, 0x80087467, struct.pack("HHHH", 48, 180, 0, 0))
        self.process = subprocess.Popen([str(binary), "--team", team, "--no-git", "--notify=false", "--theme=dark"],
                                        stdin=slave, stdout=slave, stderr=slave, env=env, start_new_session=True)
        os.close(slave)
        self.output = bytearray()
        self.deadline = time.monotonic() + timeout

    def wait(self, predicate, description):
        while time.monotonic() < self.deadline:
            self.pump()
            if predicate():
                return
        raise RuntimeError("Timed out: " + description + "\n" + self.output[-9000:].decode(errors="replace"))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=pathlib.Path, default=pathlib.Path(__file__).resolve().parent.parent / "agent-deck")
    parser.add_argument("--output", type=pathlib.Path, help="retain sanitized evidence (no credentials)")
    parser.add_argument("--timeout", type=int, default=90)
    parser.add_argument("--repo", type=pathlib.Path, required=True, help="pre-provisioned disposable Git fixture (read-only)")
    parser.add_argument("--skip-offline", action="store_true", help="run baseline only; does not satisfy recovery gate")
    args = parser.parse_args()
    if not args.binary.is_file():
        raise RuntimeError("Provide a freshly built --binary")
    with tempfile.TemporaryDirectory(prefix="agent-deck-pool-") as temporary:
        root = pathlib.Path(temporary).resolve()
        repo = args.repo.resolve()
        if not (repo / ".git").is_dir():
            raise RuntimeError("--repo must be a pre-provisioned disposable Git repository")
        team_name = "pool-verify-" + uuid.uuid4().hex[:10]
        fake = FakeServer(str(repo))
        fake.RequestHandlerClass = PoolHandler
        fake.failed_brief, fake.delivered_briefs = False, []
        fake.serial = 0
        fake.sessions = [{"id": "ses_master", "title": "Master", "directory": str(repo)}]
        threading.Thread(target=fake.serve_forever, daemon=True).start()
        config = root / "config"
        state_root = root / "state" / "agent-deck"
        teams = config / "agent-deck" / "teams"
        teams.mkdir(parents=True)
        team = {"version": 1, "name": team_name, "repo": str(repo), "base": "main", "capacity": 3,
                "server": f"http://127.0.0.1:{fake.server_port}", "model": "fake/test", "lease_pool": True,
                "members": [{"id": "demo-master", "name": "Master", "role": "master", "mode": "shared",
                             "dir": str(repo), "session": "ses_master", "capability": "coder",
                             "permissions": {"read": "allow", "edit": "allow", "bash": "ask",
                                             "webfetch": "allow", "external_directory": "ask"}}]}
        (teams / (team_name + ".yaml")).write_text(json.dumps(team))
        board_dir = state_root / "boards" / team_name
        board_dir.mkdir(parents=True)
        board_path = board_dir / "board.json"
        board_path.write_text(json.dumps({"version": 2, "revision": 1, "board": {"version": 2, "tasks": []},
                                        "policy": {"min_workers": 0, "max_workers": 2, "approval": "auto",
                                                   "cooldown": 2000000000}, "approved": {}}))
        env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
        env.update(TERM="xterm-256color", XDG_CONFIG_HOME=str(config), XDG_STATE_HOME=str(root / "state"),
                   HOME=str(root), AGENT_DECK_SERVER=team["server"], GIT_CONFIG_NOSYSTEM="1",
                   GIT_CONFIG_GLOBAL=os.devnull)
        terminal = PoolTerminal(args.binary.resolve(), team_name, env, args.timeout)
        evidence = {"driver": "deterministic fake actors over real MCP/runtime", "captures": []}
        try:
            terminal.wait(lambda: bool(list(state_root.rglob("*.runtime.json"))), "runtime discovery")
            discovery = json.loads(next(state_root.rglob("*.runtime.json")).read_text())

            def http(url, token, body):
                request = urllib.request.Request(url, json.dumps(body).encode(),
                                                 {"Authorization": "Bearer " + token, "Content-Type": "application/json"})
                with urllib.request.urlopen(request, timeout=8) as response:
                    return json.loads(response.read())

            http(discovery["endpoint"].replace("/mcp", "/user"), discovery["token"],
                 {"action": "setup_workflow", "member": "Master"})

            def tokens():
                result = {}
                for file in state_root.rglob("opencode.json"):
                    for entry in json.loads(file.read_text()).get("mcp", {}).values():
                        result[file.parent.name] = entry["headers"]["Authorization"].removeprefix("Bearer ")
                return result

            call_id = 0

            def call(token, tool, arguments):
                nonlocal call_id
                call_id += 1
                response = http(discovery["endpoint"], token, {"jsonrpc": "2.0", "id": call_id,
                                "method": "tools/call", "params": {"name": tool, "arguments": arguments}})
                result = response.get("result", {})
                if response.get("error") or result.get("isError"):
                    raise RuntimeError(f"{tool}: {response}")
                return json.loads(result["content"][0]["text"])

            master_token = tokens()["demo-master"]
            for index in range(3):
                call(master_token, "create_task", {"title": f"Pool verification {index}",
                     "description": "Scripted lifecycle fixture, not model-written software", "criteria": ["Demo observed"],
                     "paths": [f"fixture-{index}/**"], "reviewer": "Master", "parallel": True,
                     "estimate": "1m", "ready": True})

            def tasks():
                return json.loads(board_path.read_text())["board"]["tasks"]

            completed = set()
            seen_workers = set()
            recovered_offline = False
            while len(completed) < 3:
                terminal.wait(lambda: any(t["state"] == "in_progress" and t["id"] not in completed for t in tasks()),
                              "automatic assignment")
                active = next(t for t in tasks() if t["state"] == "in_progress" and t["id"] not in completed)
                roster = call(master_token, "team_members", {})
                members = roster if isinstance(roster, list) else roster.get("members", [])
                member = next(m for m in members if m["name"] == active["assignee"])
                worker_token = tokens()[member["id"]]
                own = call(worker_token, "my_tasks", {})
                if active["id"] not in json.dumps(own):
                    raise RuntimeError("new worker cannot access assigned board task")
                seen_workers.add(member["id"])
                terminal.wait(lambda: any(active["id"] in text for text in fake.delivered_briefs), "recovered assignment brief")
                if not args.skip_offline and not recovered_offline and len(completed) == 2:
                    controls = [file.parent for file in state_root.rglob("opencode.json")
                                if file.parent.name == member["id"]]
                    old_control = str(controls[0])
                    with fake.lock:
                        missing = [s for s in fake.sessions if s["directory"] == old_control]
                        if len(missing) != 1:
                            raise RuntimeError("cannot identify scratch worker session for offline injection")
                        old_session = missing[0]["id"]
                        fake.sessions.remove(missing[0])
                    terminal.wait(lambda: any(s["title"].startswith(member["name"]) and s["id"] != old_session
                                             and s["directory"] != old_control for s in fake.sessions),
                                  "confirmed offline replacement identity")
                    terminal.wait(lambda: any("Prior pool session is missing" in
                                             " ".join(p.get("text", "") for p in (call[2] or {}).get("parts", []))
                                             for call in fake.calls if call[0] == "POST" and "/prompt_async" in call[1]),
                                  "durable offline handoff")
                    evidence["captures"].append({"stage": "offline-recovered", "task": active["id"],
                                                 "fresh_control": True})
                    recovered_offline = True
                    continue
                terminal.send(b"5")
                terminal.settle(0.3)
                if active["title"] not in terminal.output.decode(errors="replace"):
                    raise RuntimeError("PTY board missing active task")
                evidence["captures"].append({"stage": "assigned", "task": active["id"], "state": active["state"]})
                call(worker_token, "update_my_task", {"id": active["id"], "checked": [active["criteria"][0]["id"]],
                     "state": "review", "comment": "Scripted observation; no implementation claim"})
                call(master_token, "review_task", {"id": active["id"], "decision": "accept", "comment": "Demo evidence accepted"})
                completed.add(active["id"])
                evidence["captures"].append({"stage": "completed", "task": active["id"], "state": "done"})
                terminal.send(b"1")
            if len(seen_workers) < 2 or not fake.failed_brief:
                raise RuntimeError("scale-up or injected missing-brief recovery was not exercised")
            terminal.wait(lambda: len(call(master_token, "team_members", {})) == 1, "automatic scale-down")
            evidence.update(scale_up_workers=len(seen_workers), completed=len(completed),
                            failed_brief_recovered=fake.failed_brief, scale_down_workers=0,
                            offline_recovered=recovered_offline)
            evidence["pty"] = re.sub(r"Bearer [A-Za-z0-9._~-]+", "Bearer [redacted]", terminal.output.decode(errors="replace"))
            if args.output:
                args.output.mkdir(parents=True, exist_ok=True)
                (args.output / "pool-evidence.json").write_text(json.dumps(evidence, indent=2))
            print("PASS: scale-up -> board access -> recovered brief -> assigned -> reviewed done -> scale-down; real runtime + PTY")
        except Exception:
            effects = {str(file.relative_to(root)): json.loads(file.read_text())
                       for file in (root / "state").rglob("effect-*.json")}
            journals = {str(file.relative_to(root)): json.loads(file.read_text())
                        for file in (root / "state").rglob("actions.json")}
            diagnostics = {}
            for command in ("status", "requests"):
                result = subprocess.run([str(args.binary.resolve()), "team", command, "--team", team_name, "--json"],
                                        env=env, capture_output=True, text=True, timeout=8)
                diagnostics[command] = {"exit": result.returncode, "stdout": result.stdout, "stderr": result.stderr}
            failure = {"board": json.loads(board_path.read_text()), "effects": effects, "journals": journals,
                       "sessions": fake.sessions, "diagnostics": diagnostics,
                       "team": (teams / (team_name + ".yaml")).read_text(),
                       "pty": terminal.output.decode(errors="replace"),
                       "http_operations": [{"method": method, "path": path} for method, path, _ in fake.calls]}
            if args.output:
                args.output.mkdir(parents=True, exist_ok=True)
                (args.output / "pool-failure.json").write_text(json.dumps(failure, indent=2))
            print("FAIL evidence:", json.dumps({"effects": len(effects), "journals": len(journals),
                  "diagnostics": diagnostics, "artifact": str(args.output / "pool-failure.json") if args.output else None}))
            raise
        finally:
            terminal.close()
            fake.shutdown()
            fake.server_close()


if __name__ == "__main__":
    main()
