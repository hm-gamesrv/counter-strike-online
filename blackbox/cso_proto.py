"""Minimal CSNZ wire protocol for black-box tests.

Frame layout (little endian), as implemented by internal/protocol/packet.go:

    'U' | sequence u8 | length u16 | packet id u8 | payload (length - 1 bytes)

The length counts the packet id. Sequences start at 1 and wrap after 255. The
development server runs with Crypt=false, so frames are plaintext; the RC4
handshake path is intentionally not implemented here.
"""

from __future__ import annotations

import struct

GREETING = b"~SERVERCONNECTED\n"
MAX_FRAME = 65536
MAX_PAYLOAD = MAX_FRAME - 5


class FrameError(RuntimeError):
    """The peer sent a frame this codec cannot parse."""


class Reader:
    """Bounds-checked reader; every getter raises FrameError on a short buffer."""

    def __init__(self, data: bytes) -> None:
        self.data = data
        self.pos = 0

    def bytes(self, count: int) -> bytes:
        if count < 0 or self.pos + count > len(self.data):
            raise FrameError(f"short payload: need {count} bytes at {self.pos}")
        chunk = self.data[self.pos : self.pos + count]
        self.pos += count
        return chunk

    def u8(self) -> int:
        return self.bytes(1)[0]

    def u16(self) -> int:
        return struct.unpack("<H", self.bytes(2))[0]

    def u32(self) -> int:
        return struct.unpack("<I", self.bytes(4))[0]

    def cstring(self) -> str:
        end = self.data.find(b"\0", self.pos)
        if end < 0:
            raise FrameError("unterminated string")
        text = self.data[self.pos : end]
        self.pos = end + 1
        return text.decode("utf-8", "replace")


def u8(value: int) -> bytes:
    return struct.pack("<B", value)


def u16(value: int) -> bytes:
    return struct.pack("<H", value)


def u32(value: int) -> bytes:
    return struct.pack("<I", value)


def cstring(text: str) -> bytes:
    return text.encode("utf-8") + b"\0"


class Codec:
    """Reads and writes frames on one socket, tracking both sequence counters."""

    def __init__(self, sock) -> None:
        self.sock = sock
        self.send_sequence = 1
        self.receive_sequence = 1

    def send(self, packet_id: int, payload: bytes = b"") -> None:
        if len(payload) > MAX_PAYLOAD:
            raise FrameError("frame too large")
        frame = bytearray()
        frame += b"U"
        frame.append(self.send_sequence)
        frame += u16(len(payload) + 1)
        frame.append(packet_id)
        frame += payload
        self.sock.sendall(frame)
        self.send_sequence = (self.send_sequence + 1) & 0xFF

    def receive(self) -> tuple[int, bytes]:
        header = self._read_exact(4)
        if header[0:1] != b"U":
            raise FrameError(f"bad signature {header[0:1]!r}")
        if header[1] != self.receive_sequence:
            raise FrameError(f"sequence {header[1]} != {self.receive_sequence}")
        length = struct.unpack("<H", header[2:4])[0]
        if length < 1 or length + 4 > MAX_FRAME:
            raise FrameError(f"bad length {length}")
        body = self._read_exact(length)
        self.receive_sequence = (self.receive_sequence + 1) & 0xFF
        return body[0], body[1:]

    def _read_exact(self, count: int) -> bytes:
        chunk = b""
        while len(chunk) < count:
            piece = self.sock.recv(count - len(chunk))
            if not piece:
                raise FrameError("peer closed the connection")
            chunk += piece
        return chunk
