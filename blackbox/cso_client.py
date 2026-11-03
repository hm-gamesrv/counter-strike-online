"""A black-box CSNZ client: enough of the handshake to log in and observe packets.

Nothing here talks to the database or imports the Go code; every assertion is
made from the bytes the server sends, which is what makes these tests useful as
a behavior reference for the rewrite.
"""

from __future__ import annotations

import socket
import time

from cso_proto import Codec, GREETING, FrameError, Reader, cstring, u8, u16, u32

# Packet ids used by the tests.
PKT_REPLY = 1
PKT_VERSION = 0
PKT_HANDSHAKE = 3
PKT_MESSAGE = 67
PKT_USER_START = 150
PKT_INVENTORY_DEFAULT = 152
PKT_INVENTORY_UPDATE = 154
PKT_USER_INFO = 157


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

    def login(self, user: str, password: str, capture_handshake: bool = True) -> list[Packet]:
        """Runs version -> (handshake) -> /login and returns every packet seen."""
        packets: list[Packet] = []
        self.send_version()
        packets += self.collect()
        if capture_handshake:
            self.send_handshake()
            packets += self.collect()
        self.chat(f"/login {user} {password}")
        packets += self.collect()
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
