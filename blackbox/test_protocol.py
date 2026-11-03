"""Protocol-level robustness: malformed frames, size limits, reassembly.

Run:  python blackbox/test_protocol.py
      CSO_PORT=30003 python blackbox/test_protocol.py

The shared cases pin behavior both builds show: a bad signature or an
unexpected sequence number drops the connection, a frame at MAX_PAYLOAD is
accepted, frames may be split across TCP writes, and sequence numbers wrap.
Frame-length validation is Go-specific: the reference build never checks the
declared length (it waits for the bytes it was promised, and a zero length
becomes an empty-payload packet), and because its poll loop reads the whole
frame inline one half-sent frame blocks every other client until that socket
closes. Those cases skip on the reference build; the comparison is recorded in
blackbox/FINDINGS.md.
"""

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from cso_client import PKT_MESSAGE, PKT_UNSUPPORTED, PKT_USER_START, PKT_VERSION, Client, ids  # noqa: E402
from cso_harness import BlackBoxTest, wait_for_lobby_text  # noqa: E402
from cso_proto import MAX_PAYLOAD, cstring, frame, u8, u16, u32  # noqa: E402


class FrameValidationTest(BlackBoxTest):
    """Frames the server must reject outright, and frames it must accept."""

    def test_bad_signature_drops_the_connection(self) -> None:
        client = self.connect()
        client.send_raw(b"X" + u8(1) + u16(1) + u8(PKT_VERSION))
        self.assertTrue(client.expect_closed(), "the server kept the connection after a bad signature")

    def test_unexpected_sequence_drops_the_connection(self) -> None:
        client = self.connect()
        client.send_raw(frame(PKT_VERSION, u8(2) + u16(0) + u32(0) + u32(0), sequence=9))
        self.assertTrue(client.expect_closed(), "the server accepted sequence 9 as the first frame")

    def test_malformed_frames_do_not_wedge_the_server(self) -> None:
        # Only frames both builds reject synchronously: the reference build has
        # no declared-length check, so a lying length is covered by FrameLengthTest.
        malformed = [
            ("bad signature", b"X" + u8(1) + u16(1) + u8(PKT_VERSION)),
            ("wrong sequence", frame(PKT_VERSION, u8(2) + u16(0) + u32(0) + u32(0), sequence=7)),
            ("all zero header", b"\0\0\0\0"),
            ("truncated header", b"U" + u8(1)),
        ]
        for label, chunk in malformed:
            with self.subTest(frame=label):
                client = self.connect()
                try:
                    client.send_raw(chunk)
                except OSError:
                    pass  # the peer may reset before the whole frame is written
                client.expect_closed(1.0)
        client, packets = self.login()
        self.assertIn(PKT_USER_START, ids(packets), "the server stopped accepting logins")

    def test_frame_at_max_payload_is_accepted(self) -> None:
        client, packets = self.login()
        self.assertIn(PKT_USER_START, ids(packets))
        client.send_frame(PKT_UNSUPPORTED, b"\0" * MAX_PAYLOAD)
        self.assertEqual(client.collect(0.5), [], "an unsupported packet id was answered")
        client.chat("max payload accepted")
        self.assertTrue(wait_for_lobby_text(client, "max payload accepted"), "session died after a maximum-size frame")

    def test_frame_split_across_writes_is_reassembled(self) -> None:
        client, _ = self.login()
        text = "split over several socket writes"
        client.send_split(PKT_MESSAGE, u8(1) + cstring(text), chunk=4)
        self.assertTrue(wait_for_lobby_text(client, text), "the server did not reassemble a fragmented frame")

    def test_sequence_numbers_wrap_after_255(self) -> None:
        client, _ = self.login()
        for _ in range(300):
            client.send(PKT_UNSUPPORTED)
        client.chat("after the sequence wrap")
        self.assertTrue(wait_for_lobby_text(client, "after the sequence wrap"), "the server lost the stream across the 255 -> 0 wrap")


class FrameLengthTest(BlackBoxTest):
    """Declared lengths outside 1..65532 are refused before the body is read.

    Go-specific: the reference build has no length check, so it either waits for
    the declared bytes or treats a zero length as an empty-payload packet. See
    blackbox/FINDINGS.md for the comparison.
    """

    def setUp(self) -> None:
        self.go_specific("declared frame lengths are validated only by the Go build")

    def assault(self, data: bytes) -> Client:
        """Sends *data* and returns the client, tolerating a reset mid-write."""
        client = self.connect()
        try:
            client.send_raw(data)
        except OSError:
            pass
        return client

    def test_zero_length_is_refused(self) -> None:
        client = self.assault(b"U" + u8(1) + u16(0))
        self.assertTrue(client.expect_closed(), "a zero-length frame was accepted")

    def test_length_past_the_frame_limit_is_refused(self) -> None:
        for length in (65533, 65535):
            with self.subTest(length=length):
                client = self.assault(b"U" + u8(1) + u16(length))
                self.assertTrue(client.expect_closed(), f"declared length {length} was accepted")

    def test_payload_one_byte_over_max_payload_is_refused(self) -> None:
        client = self.assault(frame(PKT_UNSUPPORTED, b"\0" * (MAX_PAYLOAD + 1)))
        self.assertTrue(client.expect_closed(), "a frame one byte over MAX_PAYLOAD was accepted")

    def test_half_open_frame_does_not_block_other_clients(self) -> None:
        # The declared body never arrives. The session owning it may wait, but
        # every other client has to keep being served: the reference build's
        # single poll loop blocks here until the socket closes (FINDINGS.md).
        slow = self.connect()
        slow.send_raw(b"U" + u8(1) + u16(64) + u8(PKT_UNSUPPORTED) + b"\0" * 10)
        client, packets = self.login()
        self.assertIn(PKT_USER_START, ids(packets), "a half-sent frame blocked a new login")


if __name__ == "__main__":
    unittest.main(verbosity=2)
