# Security

## Supported versions

Security fixes target the latest release and the `main` branch. Before the first release,
use the latest `main` revision.

## Reporting a vulnerability

Use [GitHub private vulnerability reporting](https://github.com/qivs/agent-deck/security/advisories/new).
If unavailable, contact **amirmahdiqiasvand@gmail.com** with the subject `agent-deck security`.
Include affected versions, reproduction steps, impact, and any suggested fix.
Do not include credentials, private transcripts, or sensitive repository contents.
Please allow time to investigate before public disclosure.

## Trust boundaries

The normal deployment connects to a local OpenCode HTTP server. Treat access to that server
as access to its sessions and tools. An explicitly configured server has the same authority;
do not expose an unauthenticated server to untrusted networks.

Agent-deck does not store authentication secrets. Team files can contain prompts, local
paths, and session bindings; review full exports before sharing. Portable exports remove
local bindings but retain prompt text. Read-only mode blocks agent-deck mutations; it does
not restrict another client's access to OpenCode. Terminal control sequences are stripped
from remote text before display.
