"""Dumps the packet sequence of one login, for comparing two servers.

    python blackbox/trace.py --port 30002 --out .dist/trace-30002.txt
    python blackbox/trace.py --port 30003 --out .dist/trace-30003.txt
    fc .dist/trace-30002.txt .dist/trace-30003.txt

Each line is "index id size first-bytes" so a diff points at the first packet
where two builds disagree.
"""

from __future__ import annotations

import argparse
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from cso_client import Client  # noqa: E402


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default=os.environ.get("CSO_HOST", "127.0.0.1"))
    parser.add_argument("--port", type=int, default=int(os.environ.get("CSO_PORT", "30002")))
    parser.add_argument("--user", default=os.environ.get("CSO_USER", "tester1"))
    parser.add_argument("--password", default=os.environ.get("CSO_PASSWORD", "secret1"))
    parser.add_argument("--out", default="")
    args = parser.parse_args()

    with Client(args.host, args.port) as client:
        packets = client.login(args.user, args.password)
        lines = []
        for index, packet in enumerate(packets):
            head = packet.payload[:8].hex()
            lines.append(f"{index:3d} {packet.id:3d} {len(packet.payload):5d} {head}")

    text = "\n".join(lines) + "\n"
    if args.out:
        with open(args.out, "w", encoding="utf-8") as handle:
            handle.write(text)
        print(f"{len(lines)} packets -> {args.out}")
    else:
        sys.stdout.write(text)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
