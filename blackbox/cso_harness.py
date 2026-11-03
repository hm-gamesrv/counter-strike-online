"""Shared scaffolding for the black-box suites.

Every test file reaches the server through this module: the environment, the
login helper, the packet decoders and the "wait for one event" helpers live
here, so a test states what the client observes instead of byte offsets.
Nothing here imports the Go code or opens the database; the only input is what
a server writes to a socket, which is what lets the same suite run against the
reference build.
"""

from __future__ import annotations

import io
import os
import sys
import unittest
import zipfile
from typing import NamedTuple

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from cso_client import (  # noqa: E402
    LOBBY_JOIN,
    LOBBY_LEAVE,
    MSG_LOBBY,
    MSG_ROOM,
    MSG_TEAM,
    MSG_WHISPER,
    PKT_LOBBY,
    PKT_MESSAGE,
    PKT_METADATA,
    PKT_USER_INFO,
    PKT_USER_START,
    Client,
    Packet,
    ids,
)
from cso_proto import Reader  # noqa: E402

HOST = os.environ.get("CSO_HOST", "127.0.0.1")
PORT = int(os.environ.get("CSO_PORT", "30002"))
BASELINE_PORT = int(os.environ.get("CSO_BASELINE_PORT", "30003"))
USER = os.environ.get("CSO_USER", "tester1")
OTHER_USER = os.environ.get("CSO_OTHER_USER", "tester2")
PASSWORD = os.environ.get("CSO_PASSWORD", "secret1")
LOGIN_IDLE = float(os.environ.get("CSO_LOGIN_IDLE", "0.7"))

#: 30003 is the unmodified C++ build used as the behavioral reference. Cases
#: that pin behavior only the Go build has skip when the suite targets it; the
#: differences themselves are recorded in blackbox/FINDINGS.md.
REFERENCE = os.environ.get("CSO_REFERENCE", "1" if PORT == BASELINE_PORT else "0") == "1"


class BlackBoxTest(unittest.TestCase):
    """Base class: helpers that always close the sockets they open."""

    def connect(self, port: int | None = None) -> Client:
        target = PORT if port is None else port
        client = Client(HOST, target)
        try:
            client.connect()
        except OSError as error:
            client.close()
            self.skipTest(f"no server on {HOST}:{target}: {error}")
        self.addCleanup(client.close)
        return client

    def login(
        self,
        user: str = USER,
        password: str = PASSWORD,
        port: int | None = None,
    ) -> tuple[Client, list[Packet]]:
        """Connects and logs in; returns the live client and every packet seen."""
        client = self.connect(port)
        return client, client.login(user, password, idle=LOGIN_IDLE)

    def go_specific(self, behaviour: str) -> None:
        """Skips a case that pins behavior only the Go build has."""
        if REFERENCE:
            self.skipTest(f"{behaviour}; the reference build differs (see blackbox/FINDINGS.md)")


def packets_of(packets: list[Packet], packet_id: int) -> list[Packet]:
    return [packet for packet in packets if packet.id == packet_id]


def first_packet(packets: list[Packet], packet_id: int) -> Packet:
    """Returns the first packet with *packet_id*; fails the test when absent."""
    for packet in packets:
        if packet.id == packet_id:
            return packet
    raise AssertionError(f"no packet {packet_id} in {ids(packets)}")


class UserStart(NamedTuple):
    uid: int
    user: str
    character: str
    tail: bytes


def user_start(packets: list[Packet]) -> UserStart:
    """Decodes packet 150: user id, account name, character name, then flags."""
    reader = Reader(first_packet(packets, PKT_USER_START).payload)
    uid = reader.u32()
    user = reader.cstring()
    character = reader.cstring()
    return UserStart(uid, user, character, reader.bytes(reader.remaining()))


def user_info_block(packets: list[Packet]) -> tuple[int, bytes]:
    """Splits packet 157 into its user id and the user info block.

    The same block is embedded in every 153 entry and its width depends on the
    flags a build sets, so the reader has to learn the width from this packet.
    """
    payload = first_packet(packets, PKT_USER_INFO).payload
    if len(payload) < 5:
        raise AssertionError(f"packet 157 is only {len(payload)} bytes")
    return int.from_bytes(payload[:4], "little"), payload[4:]


class LobbyEntry(NamedTuple):
    uid: int
    name: str
    info: bytes


def lobby_list(payload: bytes, infos: dict[int, bytes]) -> tuple[int, int, list[LobbyEntry], int]:
    """Decodes a 153 lobby list.

    Returns (kind, declared count, parsed entries, bytes consumed). *infos* maps
    a user id to that user's block from packet 157: it is the only
    build-independent length oracle for the variable-width user info block, so
    parsing stops at the first user whose block is unknown. A caller that knows
    every online user gets count == len(entries) and a fully consumed payload.
    """
    reader = Reader(payload)
    kind = reader.u8()
    if kind != LOBBY_JOIN:
        return kind, 0, [], reader.pos
    count = reader.u16()
    entries: list[LobbyEntry] = []
    while len(entries) < count:
        uid = reader.u32()
        name = reader.cstring()
        info = infos.get(uid)
        if info is None or reader.remaining() < len(info):
            break
        entries.append(LobbyEntry(uid, name, reader.bytes(len(info))))
    return kind, count, entries, reader.pos


class LobbyEvent(NamedTuple):
    kind: int
    uid: int
    name: str
    info: bytes


def lobby_event(payload: bytes) -> LobbyEvent:
    """Decodes a 153 add/leave event."""
    reader = Reader(payload)
    kind = reader.u8()
    if kind == LOBBY_LEAVE:
        return LobbyEvent(kind, reader.u32(), "", b"")
    return LobbyEvent(kind, reader.u32(), reader.cstring(), payload[reader.pos :])


def collect_until(
    client: Client,
    match,
    idle: float = 0.6,
    attempts: int = 4,
) -> tuple[list[Packet], Packet | None]:
    """Collects until match(packet) succeeds, or the attempts run out."""
    seen: list[Packet] = []
    for _ in range(attempts):
        chunk = client.collect(idle)
        seen.extend(chunk)
        for packet in chunk:
            if match(packet):
                return seen, packet
    return seen, None


def wait_for_lobby_event(
    client: Client,
    kind: int,
    uid: int | None = None,
    idle: float = 0.6,
    attempts: int = 4,
) -> LobbyEvent | None:
    """Waits for one 153 add/leave event, optionally for a specific user."""

    def matches(packet: Packet) -> bool:
        if packet.id != PKT_LOBBY or not packet.payload:
            return False
        event = lobby_event(packet.payload)
        return event.kind == kind and (uid is None or event.uid == uid)

    _, found = collect_until(client, matches, idle=idle, attempts=attempts)
    return None if found is None else lobby_event(found.payload)


class LobbyMessage(NamedTuple):
    kind: int
    name: str
    text: str


def lobby_messages(packets: list[Packet]) -> list[LobbyMessage]:
    """Decodes 67 channel/room chat frames (kinds 1, 2 and 4)."""
    messages: list[LobbyMessage] = []
    for packet in packets:
        if packet.id != PKT_MESSAGE or not packet.payload:
            continue
        kind = packet.payload[0]
        if kind not in (MSG_LOBBY, MSG_ROOM, MSG_TEAM):
            continue
        reader = Reader(packet.payload)
        reader.u8()
        messages.append(LobbyMessage(kind, reader.cstring(), reader.cstring()))
    return messages


class Whisper(NamedTuple):
    direction: int
    name: str
    text: str


def whispers(packets: list[Packet]) -> list[Whisper]:
    """Decodes 67 kind 0 frames: direction 0 is the sender's copy, 1 the target's."""
    found: list[Whisper] = []
    for packet in packets:
        if packet.id != PKT_MESSAGE or len(packet.payload) < 3:
            continue
        if packet.payload[0] != MSG_WHISPER:
            continue
        reader = Reader(packet.payload)
        reader.u8()
        found.append(Whisper(reader.u8(), reader.cstring(), reader.cstring()))
    return found


def notices(packets: list[Packet]) -> list[tuple[int, str]]:
    """Decodes every 67 frame as (kind, first string after the kind byte)."""
    found: list[tuple[int, str]] = []
    for packet in packets:
        if packet.id != PKT_MESSAGE or not packet.payload:
            continue
        payload = packet.payload
        end = payload.find(b"\0", 1)
        text = payload[1:end].decode("utf-8", "replace") if end > 0 else ""
        found.append((payload[0], text))
    return found


def wait_for_lobby_text(client: Client, text: str, idle: float = 0.6, attempts: int = 4) -> bool:
    """True when a channel chat frame carrying exactly *text* arrives."""
    _, found = collect_until(
        client,
        lambda packet: any(message.text == text for message in lobby_messages([packet])),
        idle=idle,
        attempts=attempts,
    )
    return found is not None


def metadata_frames(packets: list[Packet]) -> list[bytes]:
    return [packet.payload for packet in packets if packet.id == PKT_METADATA]


def metadata_zip(payload: bytes) -> tuple[str, bytes] | None:
    """Unzips a metadata frame, returning (entry name, content) or None.

    Not every frame is a zip: the block-oriented metadata built in code (item
    boxes, weapon paints, weapon lists) is raw, so its payload starts with its
    own count field instead of the "PK" archive signature.
    """
    data = payload[3:]
    if data[:2] != b"PK":
        return None
    with zipfile.ZipFile(io.BytesIO(data)) as archive:
        names = archive.namelist()
        return "+".join(names), b"".join(archive.read(name) for name in names)


class Item(NamedTuple):
    slot: int
    item: int
    count: int


def read_item(reader: Reader) -> Item:
    """Decodes one inventory entry; the width varies with paints and parts."""
    slot = reader.u16()
    if reader.u8() == 0:
        return Item(slot, 0, 0)
    item = reader.u16()
    count = reader.u16()
    reader.u8()
    reader.u8()
    reader.u32()
    reader.u32()
    reader.u16()
    paints = reader.u16()
    reader.bytes(paints * 2)
    reader.u16()
    reader.u32()
    reader.u32()
    reader.u8()
    reader.u32()
    for _ in range(reader.u8()):
        reader.u8()
        reader.u16()
    reader.u8()
    reader.u8()
    reader.u8()
    reader.u32()
    reader.u8()
    return Item(slot, item, count)


class Inventory(NamedTuple):
    count: int
    items: list[Item]


def default_inventory(payload: bytes) -> Inventory:
    """Decodes packet 152: a count and the items every account starts with."""
    reader = Reader(payload)
    count = reader.u16()
    items = [read_item(reader) for _ in range(count)]
    if reader.remaining():
        raise AssertionError(f"packet 152 has {reader.remaining()} trailing bytes")
    return Inventory(count, items)


def inventory_patch(payload: bytes) -> Inventory:
    """Decodes one 154 frame: u32 header, count, then the changed items."""
    reader = Reader(payload)
    reader.u32()
    count = reader.u16()
    items = [read_item(reader) for _ in range(count)]
    if reader.remaining():
        raise AssertionError(f"packet 154 has {reader.remaining()} trailing bytes")
    return Inventory(count, items)


class RoomList(NamedTuple):
    header: bytes
    count: int
    tail: bytes


def room_list(payload: bytes) -> RoomList:
    """Decodes the fixed 14-byte 151 header and the room count that follows."""
    reader = Reader(payload)
    header = reader.bytes(14)
    return RoomList(header, reader.u8(), payload[reader.pos :])


def login_packets(port: int, user: str = USER, password: str = PASSWORD) -> list[Packet]:
    """One-shot login for the cross-server comparison; the socket is closed."""
    with Client(HOST, port) as client:
        return client.login(user, password, idle=LOGIN_IDLE)


def open_client(port: int) -> Client:
    """Connects without a cleanup hook; the caller closes (cross-server use)."""
    client = Client(HOST, port)
    client.connect()
    return client


__all__ = [
    "BASELINE_PORT",
    "BlackBoxTest",
    "HOST",
    "Inventory",
    "Item",
    "LOGIN_IDLE",
    "LobbyEntry",
    "LobbyEvent",
    "LobbyMessage",
    "OTHER_USER",
    "PASSWORD",
    "PORT",
    "REFERENCE",
    "RoomList",
    "USER",
    "UserStart",
    "Whisper",
    "collect_until",
    "default_inventory",
    "first_packet",
    "inventory_patch",
    "ids",
    "lobby_event",
    "lobby_list",
    "lobby_messages",
    "login_packets",
    "metadata_frames",
    "metadata_zip",
    "notices",
    "open_client",
    "packets_of",
    "room_list",
    "user_info_block",
    "user_start",
    "wait_for_lobby_event",
    "wait_for_lobby_text",
    "whispers",
]
