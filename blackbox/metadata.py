"""Compares metadata frames (packet 91) between two servers by content.

Each 91 payload is "U8(type) + U16(size) + data", where data is usually a zip
with one entry. Two servers can send different frame sizes only because of the
compressor, so this tool unzips every frame and hashes the decompressed bytes:
equal hashes mean equal content even when the zipped sizes differ.

    python blackbox/metadata.py --a 30002 --b 30003
"""

from __future__ import annotations

import argparse
import hashlib
import io
import os
import sys
import zipfile

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from cso_client import PKT_INVENTORY_DEFAULT, Client  # noqa: E402
from cso_proto import Reader  # noqa: E402

PKT_METADATA = 91


def decode(payload: bytes) -> tuple[int, int, bytes]:
    reader = Reader(payload)
    kind = reader.u8()
    size = reader.u16()
    data = payload[reader.pos:]
    return kind, size, data


def content_digest(data: bytes) -> tuple[str, int, str]:
    """Returns (digest, length, kind) of the decompressed metadata payload."""
    if len(data) >= 4 and data[:2] == b"PK":
        try:
            with zipfile.ZipFile(io.BytesIO(data)) as archive:
                names = archive.namelist()
                blobs = [archive.read(name) for name in names]
                joined = b"".join(blobs)
                return hashlib.sha256(joined).hexdigest(), len(joined), "+".join(names)
        except zipfile.BadZipFile:
            pass
    return hashlib.sha256(data).hexdigest(), len(data), "raw"


def collect(host: str, port: int, user: str, password: str) -> dict[int, tuple[int, str, int, str]]:
    with Client(host, port) as client:
        packets = client.login(user, password)
    result: dict[int, tuple[int, str, int, str]] = {}
    for packet in packets:
        if packet.id != PKT_METADATA:
            continue
        kind, size, data = decode(packet.payload)
        digest, length, entry = content_digest(data)
        result[kind] = (size, digest, length, entry)
    return result


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--a", type=int, default=30002, help="first port")
    parser.add_argument("--b", type=int, default=30003, help="second port")
    parser.add_argument("--user", default=os.environ.get("CSO_USER", "tester1"))
    parser.add_argument("--password", default=os.environ.get("CSO_PASSWORD", "secret1"))
    args = parser.parse_args()

    left = collect(args.host, args.a, args.user, args.password)
    right = collect(args.host, args.b, args.user, args.password)

    kinds = sorted(set(left) | set(right))
    same = 0
    differ = []
    for kind in kinds:
        a = left.get(kind)
        b = right.get(kind)
        if a is None or b is None:
            differ.append((kind, "missing", a, b))
            continue
        if a[1] == b[1]:
            same += 1
        else:
            differ.append((kind, "content", a, b))
    print(f"metadata types: {len(kinds)}, identical content: {same}, differing: {len(differ)}")
    for kind, reason, a, b in differ:
        print(f"  type {kind:3d} {reason}")
        print(f"    {args.a}: zip={a[0]:6d} content={a[2]:6d} {a[3]} {a[1][:16]}")
        print(f"    {args.b}: zip={b[0]:6d} content={b[2]:6d} {b[3]} {b[1][:16]}")
    return 0 if not differ else 1


if __name__ == "__main__":
    raise SystemExit(main())
