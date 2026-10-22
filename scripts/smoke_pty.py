#!/usr/bin/env python3
"""Launch a bounded read-only picker in a PTY and verify a clean quit."""

import fcntl
import os
import pathlib
import pty
import select
import struct
import subprocess
import tempfile
import time


def main():
    root = pathlib.Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory(prefix=".pty-", dir=root) as config:
        master, slave = pty.openpty()
        fcntl.ioctl(slave, 0x80087467, struct.pack("HHHH", 40, 120, 0, 0))
        env = dict(os.environ, TERM="xterm-256color", XDG_CONFIG_HOME=config)
        process = subprocess.Popen(
            [str(root / "agent-deck"), "--read-only", "--no-git", "--notify=false", "--theme=dark"],
            stdin=slave, stdout=slave, stderr=slave, env=env, start_new_session=True,
        )
        os.close(slave)
        output = bytearray()
        started = time.monotonic()
        sent = False
        try:
            while process.poll() is None and time.monotonic() - started < 12:
                if select.select([master], [], [], 0.1)[0]:
                    data = os.read(master, 65536)
                    output.extend(data)
                    if b"\x1b]11;?" in data:
                        os.write(master, b"\x1b]11;rgb:1414/1616/1919\x1b\\")
                    if b"\x1b[6n" in data:
                        os.write(master, b"\x1b[1;1R")
                if not sent and time.monotonic() - started > 3:
                    os.write(master, b"q")
                    sent = True
            if process.poll() is None:
                process.kill()
                raise RuntimeError("PTY launch did not quit within 12 seconds")
            if process.returncode != 0 or b"Teams" not in output:
                raise RuntimeError("PTY did not render picker and quit cleanly")
            print("PTY: rendered team picker and quit cleanly (read-only)")
        finally:
            if process.poll() is None:
                process.kill()
            process.wait()
            os.close(master)


if __name__ == "__main__":
    main()
