"""A black-box CSNZ client: enough of the handshake to log in and observe packets.

Nothing here talks to the database or imports the Go code; every assertion is
made from the bytes the server sends, which is what makes these tests useful as
a behavior reference for the rewrite.
"""

from __future__ import annotations

import socket
import time

from cso_proto import Codec, GREETING, FrameError, Reader, cstring, frame, u8, u16, u32

# Packet ids used by the tests.
PKT_REPLY = 1
PKT_VERSION = 0
PKT_HANDSHAKE = 3
PKT_CHARACTER = 2
PKT_SERVER_LIST = 5
PKT_CHANNEL = 7
PKT_PING = 16
PKT_ROOM = 65
PKT_MESSAGE = 67
PKT_HOST = 68
PKT_UPDATE_INFO = 69
PKT_SHOP = 72
PKT_BAN = 74
PKT_PLAYER_CONFIG = 76
PKT_FAVORITE = 77
PKT_METADATA = 91
PKT_MATCH_TABS = 99
PKT_VOXEL = 103
PKT_USER_START = 150
PKT_ROOM_LIST = 151
PKT_INVENTORY_DEFAULT = 152
PKT_LOBBY = 153
PKT_INVENTORY_UPDATE = 154
PKT_USER_INFO = 157
PKT_MY_INFO = 159
PKT_LEAGUE = 180

#: A packet id neither build implements; dispatching it must be a no-op.
PKT_UNSUPPORTED = 200

# Packet 153 subtypes (first payload byte).
LOBBY_JOIN = 0
LOBBY_ADD = 1
LOBBY_LEAVE = 2

# Packet 67 subtypes (first payload byte) the tests care about.
MSG_WHISPER = 0
MSG_LOBBY = 1
MSG_ROOM = 2
MSG_TEAM = 4
MSG_NOTICE = 10
MSG_CHAT_NOTICE = 11


class Packet:
    def __init__(self, packet_id: int, payload: bytes) -> None:
        self.id = packet_id
        self.payload = payload

    def reader(self) -> Reader:
        return Reader(self.payload)

    def __repr__(self) -> str:
        return f"Packet(id={self.id}, size={len(self.payload)})"


class Client:
    """One TCP session. Use as a context manager so the socket always closes."""

    def __init__(self, host: str = "127.0.0.1", port: int = 30002, timeout: float = 5.0) -> None:
        self.host = host
        self.port = port
        self.timeout = timeout
        self.sock: socket.socket | None = None
        self.codec: Codec | None = None
        self.greeting = b""

    def __enter__(self) -> "Client":
        self.connect()
        return self

    def __exit__(self, *_exc) -> None:
        self.close()

    def connect(self) -> bytes:
        self.sock = socket.create_connection((self.host, self.port), timeout=self.timeout)
        self.sock.settimeout(self.timeout)
        self.codec = Codec(self.sock)
        self.greeting = self._read_exact(len(GREETING))
        return self.greeting

    def close(self) -> None:
        if self.sock is not None:
            self.sock.close()
            self.sock = None

    def send(self, packet_id: int, payload: bytes = b"") -> None:
        assert self.codec is not None, "connect first"
        self.codec.send(packet_id, payload)

    def send_raw(self, data: bytes) -> None:
        """Writes bytes verbatim, bypassing the codec.

        The sequence counter is not advanced; this is only for deliberately
        malformed frames the codec refuses to build.
        """
        assert self.sock is not None, "connect first"
        self.sock.sendall(data)

    def send_frame(self, packet_id: int, payload: bytes = b"") -> None:
        """Sends one frame without the MAX_PAYLOAD guard (boundary tests)."""
        assert self.sock is not None and self.codec is not None, "connect first"
        self.sock.sendall(frame(packet_id, payload, self.codec.send_sequence))
        self.codec.send_sequence = (self.codec.send_sequence + 1) & 0xFF

    def send_split(self, packet_id: int, payload: bytes = b"", chunk: int = 4, delay: float = 0.03) -> None:
        """Sends one frame as several small writes, to exercise reassembly."""
        assert self.sock is not None and self.codec is not None, "connect first"
        data = frame(packet_id, payload, self.codec.send_sequence)
        self.codec.send_sequence = (self.codec.send_sequence + 1) & 0xFF
        for start in range(0, len(data), chunk):
            self.sock.sendall(data[start : start + chunk])
            time.sleep(delay)

    def expect_closed(self, timeout: float = 3.0) -> bool:
        """Waits for the peer to close the connection, discarding any data.

        Returns True when the socket reports end of stream or a reset, and False
        when the peer was still connected after *timeout* seconds. Received
        bytes are discarded, so the codec cannot be used afterwards.
        """
        assert self.sock is not None, "connect first"
        deadline = time.monotonic() + timeout
        try:
            while True:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    return False
                self.sock.settimeout(remaining)
                try:
                    if not self.sock.recv(4096):
                        return True
                except (socket.timeout, TimeoutError):
                    return False
                except (ConnectionResetError, ConnectionAbortedError):
                    return True
        finally:
            self.sock.settimeout(self.timeout)

    def receive(self) -> Packet:
        assert self.codec is not None, "connect first"
        packet_id, payload = self.codec.receive()
        return Packet(packet_id, payload)

    def collect(self, idle: float = 0.8, limit: int = 400) -> list[Packet]:
        """Reads until the peer is quiet for *idle* seconds or *limit* packets."""
        assert self.sock is not None, "connect first"
        packets: list[Packet] = []
        deadline = time.monotonic() + idle
        while len(packets) < limit:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                break
            self.sock.settimeout(remaining)
            try:
                packets.append(self.receive())
            except (socket.timeout, TimeoutError):
                break
            except (ConnectionResetError, ConnectionAbortedError):
                break
            except FrameError:
                break
            deadline = time.monotonic() + idle
        self.sock.settimeout(self.timeout)
        return packets

    def send_version(self, launcher: int = 2, build: int = 0) -> None:
        """Packet 0: launcher id, unknown u16, client build, unknown u32."""
        self.send(PKT_VERSION, u8(launcher) + u16(0) + u32(build) + u32(0))

    def send_handshake(self, hwid: bytes = b"blackboxhwid0001") -> None:
        """Packet 3: opaque launcher string, u16 blob, 16-byte hwid, two u32s."""
        blob = b""
        self.send(
            PKT_HANDSHAKE,
            cstring("blackbox")
            + u16(len(blob))
            + blob
            + hwid[:16].ljust(16, b"\0")
            + u32(0)
            + u32(0)
            + cstring(""),
        )

    def chat(self, text: str) -> None:
        """Packet 67 kind 1: channel chat; the server parses leading '/' commands."""
        self.send(PKT_MESSAGE, u8(1) + cstring(text))

    def whisper(self, target: str, text: str) -> None:
        """Packet 67 kind 0: private message, addressed by character name."""
        self.send(PKT_MESSAGE, u8(0) + cstring(target) + cstring(text))

    def login(self, user: str, password: str, capture_handshake: bool = True, idle: float = 0.8) -> list[Packet]:
        """Runs version -> (handshake) -> /login and returns every packet seen."""
        packets: list[Packet] = []
        self.send_version()
        packets += self.collect(idle)
        if capture_handshake:
            self.send_handshake()
            packets += self.collect(idle)
        self.chat(f"/login {user} {password}")
        packets += self.collect(idle)
        return packets

    def _read_exact(self, count: int) -> bytes:
        assert self.sock is not None, "connect first"
        chunk = b""
        while len(chunk) < count:
            piece = self.sock.recv(count - len(chunk))
            if not piece:
                raise FrameError("peer closed the connection")
            chunk += piece
        return chunk


def ids(packets: list[Packet]) -> list[int]:
    return [packet.id for packet in packets]
