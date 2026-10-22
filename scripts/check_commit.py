#!/usr/bin/env python3
"""Validate the project's single-line commit convention without dependencies."""

import pathlib
import re
import sys


def main():
    message = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8").rstrip("\n")
    match = re.fullmatch(r"(?:feat|fix|ref|chore)(?:\([a-z0-9-]+\))?: ([^\n]+)", message)
    if not match or message.endswith("."):
        print("commit must be one line: <feat|fix|ref|chore>[(scope)]: <imperative subject>")
        return 1
    subject = match.group(1)
    first = subject.split()[0]
    if first != first.lower() or subject != subject.strip():
        print("start the subject with a lowercase imperative verb")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
