"""Creates one room and dumps the room packets a server emits.

Room state is memory-only, so this probe writes nothing to the database; the
room disappears when the client disconnects.

    python code-review/probes/review-83b1c1a-lobby/room_probe.py --port 30002
    python code-review/probes/review-83b1c1a-lobby/room_probe.py --port 30003

It prints, for the chosen server:
  * the room-create response packet sequence (65 subtypes and sizes),
  * the settings flag words and the width of every flagged field of 65/0,
  * the room-list (151) entry flags and field bytes.
"""

from __future__ import annotations

import argparse
import os
import struct
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
sys.path.insert(0, os.path.join(ROOT, "blackbox"))

from cso_client import Client  # noqa: E402
from cso_proto import FrameError, Reader, cstring, u8, u16, u32  # noqa: E402

PKT_ROOM = 65
PKT_ROOM_LIST = 151

# ROOM_* flag bit positions (src/definitions.h in the reference build).
ROOM_LOW_ROOMNAME = 0
ROOM_LOW_UNK = 1
ROOM_LOW_CLANBATTLE = 2
ROOM_LOW_PASSWORD = 3
ROOM_LOW_WEAPONLIMIT = 13
ROOM_LOW_UNK33 = 31
ROOM_LOWMID_UNK34 = 0
ROOM_LOWMID_BOT = 2
ROOM_LOWMID_MAPPLAYLIST = 9
ROOM_LOWMID_ZSDIFFICULTY = 13
ROOM_LOWMID_ZBLIMIT = 17
ROOM_LOWMID_VOXEL = 18
ROOM_LOWMID_UNK63 = 20
ROOM_HIGHMID_MUTATIONRESTRICT = 1
ROOM_HIGHMID_UNK79 = 9
ROOM_HIGH_UNK77 = 31


def create_payload(name: str = "Review83Room", mode: int = 0, map_id: int = 0, max_players: int = 16) -> bytes:
    """Packet 65/0 body: 128-bit settings flags followed by the flagged fields.

    The flag set is the one a real client sends for a plain room:
    room name (1<<0), password (1<<3), game mode (1<<6), map (1<<7),
    max players (1<<8), win limit (1<<9), kill limit (1<<10).
    """
    low = (1 << 0) | (1 << 3) | (1 << 6) | (1 << 7) | (1 << 8) | (1 << 9) | (1 << 10)
    body = u32(low) + u32(0) + u32(0) + u32(0)
    body += cstring(name)
    body += cstring("")
    body += u8(mode)
    body += u16(map_id)
    body += u8(max_players)
    body += u8(10)
    body += u16(100)
    return u8(0) + body


def take(reader: Reader, count: int) -> None:
    reader.bytes(count)


def take_settings(reader: Reader, low: int, low_mid: int, high_mid: int, high: int) -> list[tuple[int, int]]:
    """Walks the settings fields exactly as CPacketManager::WriteSettings does.

    Returns (field index, width) for every flagged field, so the caller can
    compare two builds without knowing the values.
    """
    out: list[tuple[int, int]] = []
    for bit in range(32):
        if not low & (1 << bit):
            continue
        start = reader.pos
        if bit == ROOM_LOW_ROOMNAME or bit == ROOM_LOW_PASSWORD:
            reader.cstring()
        elif bit == ROOM_LOW_CLANBATTLE:
            take(reader, 7)
        elif bit == ROOM_LOW_WEAPONLIMIT:
            value = reader.u8()
            if value == 18:
                take(reader, 64)
        elif bit == ROOM_LOW_UNK33:
            take(reader, 1 + 2 * 17)
        elif bit == 7 or bit == 10:
            take(reader, 2)
        else:
            take(reader, 1)
        out.append((bit, reader.pos - start))
    for bit in range(32):
        if not low_mid & (1 << bit):
            continue
        start = reader.pos
        if bit == ROOM_LOWMID_UNK34:
            take(reader, 4)
            reader.cstring()
            take(reader, 3)
        elif bit == ROOM_LOWMID_BOT:
            take(reader, 5)
        elif bit == ROOM_LOWMID_MAPPLAYLIST:
            size = reader.u8()
            take(reader, 3 * size)
        elif bit == ROOM_LOWMID_ZSDIFFICULTY:
            take(reader, 9)
        elif bit == ROOM_LOWMID_ZBLIMIT:
            take(reader, 1 + 16)
        elif bit == ROOM_LOWMID_VOXEL:
            flag = reader.u32()
            take(reader, voxel_width(reader, flag))
        elif bit == ROOM_LOWMID_UNK63:
            size = reader.u8()
            take(reader, 2 * size)
        elif bit == 4 or bit == 16:
            take(reader, 2)
        elif bit == 19 or bit == 31:
            pass
        else:
            take(reader, 1)
        out.append((32 + bit, reader.pos - start))
    # WriteSettings has a payload only for ROOM_HIGHMID bits 0..9; the higher
    # bits travel as flags without data.
    for bit in range(10):
        if not high_mid & (1 << bit):
            continue
        start = reader.pos
        if bit == ROOM_HIGHMID_MUTATIONRESTRICT:
            take(reader, 5)
        elif bit == ROOM_HIGHMID_UNK79:
            take(reader, 4)
            reader.cstring()
            reader.cstring()
            take(reader, 4)
        else:
            take(reader, 1)
        out.append((64 + bit, reader.pos - start))
    if high & (1 << ROOM_HIGH_UNK77):
        start = reader.pos
        take(reader, 1)
        out.append((96 + ROOM_HIGH_UNK77, reader.pos - start))
    return out


def voxel_width(reader: Reader, flag: int) -> int:
    """Width of the Voxel block body, mirroring WriteSettings' per-flag writes."""
    start = reader.pos
    strings = [1, 0, 4, 0, 5, 6, 7, 8, 0, 9, 10, 22]
    for bit in range(25):
        if not flag & (1 << bit):
            continue
        if bit in (0, 1, 3, 6, 7, 8, 9, 10, 22):
            reader.cstring()
        elif bit == 14:
            size = reader.u32()
            for _ in range(size):
                take(reader, 4)
                reader.cstring()
        elif bit in (11, 12, 13, 15, 16, 17):
            take(reader, 4)
        elif bit in (2, 4, 5, 19, 21, 23, 24):
            take(reader, 1)
    return reader.pos - start


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default=os.environ.get("CSO_HOST", "127.0.0.1"))
    parser.add_argument("--port", type=int, default=int(os.environ.get("CSO_PORT", "30002")))
    parser.add_argument("--user", default=os.environ.get("CSO_USER", "tester1"))
    parser.add_argument("--password", default=os.environ.get("CSO_PASSWORD", "secret1"))
    parser.add_argument("--join", action="store_true", help="let a second account join the room and dump both sides' 65 packets")
    parser.add_argument("--other", default=os.environ.get("CSO_OTHER_USER", "tester2"))
    args = parser.parse_args()

    with Client(args.host, args.port) as client:
        client.login(args.user, args.password, idle=0.8)
        client.send(PKT_ROOM, create_payload())
        created = client.collect(0.8)
        print("create response:", [(p.id, p.payload[:1].hex(), len(p.payload)) for p in created])
        for packet in created:
            if packet.id == 67 and packet.payload:
                print("notice:", packet.payload[1:].split(b"\0")[0].decode("utf-8", "replace"))
        joined = [p for p in created if p.id == PKT_ROOM and p.payload[:1] == b"\x00"]
        if not joined:
            print("no 65/0 packet")
            return 1
        payload = joined[0].payload
        reader = Reader(payload)
        reader.u8()
        reader.u8()
        room_id = reader.u32()
        host_id = reader.u32()
        reader.u16()
        reader.u8()
        flags = [reader.u32() for _ in range(4)]
        print("65/0 len", len(payload), "room", room_id, "host", host_id)
        print("65/0 settings flags: low=0x%08x lowmid=0x%08x highmid=0x%08x high=0x%08x" % tuple(flags))
        fields = take_settings(reader, *flags)
        settings_start = 13
        print("65/0 fields:", " ".join(f"{index}:{width}" for index, width in fields))
        print("65/0 settings bytes", reader.pos - settings_start, "members at offset", reader.pos)
        members = reader.u8()
        print("65/0 members", members)
        for _ in range(members):
            reader.u32()
            reader.u32()
            name = reader.cstring()
            take(reader, 20)
            print("  member", name, "at", reader.pos)
        if args.join:
            with Client(args.host, args.port) as guest:
                guest.login(args.other, args.password, idle=0.8)
                join = u8(23) + u8(0) + u16(room_id) + cstring("")
                guest.send(PKT_ROOM, join)
                guest_packets = [p for p in guest.collect(0.8) if p.id == PKT_ROOM]
                host_packets = [p for p in client.collect(0.8) if p.id == PKT_ROOM]
                print("guest 65 packets:", [(p.payload[:1].hex(), len(p.payload)) for p in guest_packets])
                print("host 65 packets:", [(p.payload[:1].hex(), len(p.payload)) for p in host_packets])
                for packet in host_packets:
                    if packet.payload[:1] == b"":
                        print("host 65/1 hex", packet.payload.hex())
        client.send(PKT_ROOM, u8(22))
        listed = client.collect(0.8)
        frames = [p for p in listed if p.id == PKT_ROOM_LIST]
        if not frames:
            print("no 151 packet; ids:", [p.id for p in listed])
            return 1
        list_payload = frames[0].payload
        print("151 len", len(list_payload), "hex", list_payload.hex())
        list_reader = Reader(list_payload)
        take(list_reader, 15)
        take(list_reader, 9)
        low = list_reader.u32()
        high = list_reader.u32()
        print("151 entry flags low=0x%08x high=0x%08x" % (low, high))
        print("151 entry tail", list_payload[list_reader.pos:].hex(), "len", len(list_payload) - list_reader.pos)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
