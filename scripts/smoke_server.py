#!/usr/bin/env python3
"""Exercise detached lifecycle and crash recovery against an isolated fake server."""

import os
import pathlib
import signal
import socket
import subprocess
import tempfile
import time


def main():
    root = pathlib.Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory(prefix=".smoke-server-", dir=root) as directory:
        state = pathlib.Path(directory)
        child = state / "opencode"
        child.write_text(
            "#!/usr/bin/env python3\n"
            "import http.server, os, pathlib, sys\n"
            "pathlib.Path(__file__).with_suffix('.pid').write_text(str(os.getpid()))\n"
            "port = int(sys.argv[sys.argv.index('--port') + 1])\n"
            "class Handler(http.server.BaseHTTPRequestHandler):\n"
            " def do_GET(self):\n"
            "  self.send_response(200); self.end_headers(); self.wfile.write(b'{\"healthy\":true}')\n"
            "http.server.HTTPServer(('127.0.0.1', port), Handler).serve_forever()\n",
            encoding="utf-8",
        )
        child.chmod(0o700)
        with socket.socket() as available:
            available.bind(("127.0.0.1", 0))
            port = available.getsockname()[1]
        binary = str(root / "agent-deck")
        flags = ["--port", str(port), "--state", directory, "--opencode", str(child)]
        run = lambda action: subprocess.run([binary, action, *flags], check=True, timeout=25)
        try:
            run("up")
            run("status")
            old = int(child.with_suffix(".pid").read_text())
            os.kill(old, signal.SIGKILL)
            deadline = time.monotonic() + 8
            while time.monotonic() < deadline:
                if int(child.with_suffix(".pid").read_text()) != old:
                    break
                time.sleep(0.1)
            else:
                raise RuntimeError("crashed child was not restarted")
            run("up")
            run("status")
            print("Server smoke: detached launch, crash restart, idempotent up, status")
        finally:
            run("down")
        if (state / "server.json").exists():
            raise RuntimeError("shutdown retained supervisor identity")
        print("Server smoke: owned shutdown and PID cleanup passed")


if __name__ == "__main__":
    main()
