"""Dumps the login packets whose payload should match the reference byte for
byte, so two builds can be compared directly.

    python code-review/probes/review-83b1c1a-lobby/login_payloads.py --port 30003
    python code-review/probes/review-83b1c1a-lobby/login_payloads.py --port 30005

Read-only: a login writes only the session/last-login rows both builds already
write. Compare the printed lines between the reference port and ours.
"""

from __future__ import annotations

import argparse
import os
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
sys.path.insert(0, os.path.join(ROOT, "blackbox"))

from cso_client import Client  # noqa: E402

WANTED = (74, 76, 150, 153, 157, 180, 103)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default=os.environ.get("CSO_HOST", "127.0.0.1"))
    parser.add_argument("--port", type=int, default=int(os.environ.get("CSO_PORT", "30002")))
    parser.add_argument("--user", default=os.environ.get("CSO_USER", "tester1"))
    parser.add_argument("--password", default=os.environ.get("CSO_PASSWORD", "secret1"))
    args = parser.parse_args()

    with Client(args.host, args.port) as client:
        packets = client.login(args.user, args.password, idle=0.8)
    for packet in packets:
        if packet.id not in WANTED:
            continue
        print(f"{packet.id:3d} len={len(packet.payload):5d} {packet.payload.hex()}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
