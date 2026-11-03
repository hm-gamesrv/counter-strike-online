"""Channel chat and private messages.

Chat arrives as packet 67: kind 1 is channel chat, kind 0 is a private message.
The shared cases assert delivery, addressing and text round-trips for both
builds. The Go build's 1024-byte cap (an over-long message drops the socket)
is Go-specific and skips on the reference build; see blackbox/FINDINGS.md.
"""

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from cso_client import MSG_LOBBY  # noqa: E402
from cso_harness import (  # noqa: E402
    OTHER_USER,
    BlackBoxTest,
    lobby_messages,
    notices,
    user_start,
    wait_for_lobby_text,
    whispers,
)


class ChannelChatTest(BlackBoxTest):
    """Kind 1: one lobby, one channel, every member sees the message."""

    def test_channel_chat_reaches_every_lobby_user(self) -> None:
        first, first_packets = self.login()
        second, _second_packets = self.login(OTHER_USER)
        name = user_start(first_packets).character
        first.chat("hello channel")
        for label, client in (("sender", first), ("listener", second)):
            messages = [message for message in lobby_messages(client.collect(0.7)) if message.text == "hello channel"]
            self.assertTrue(messages, f"the {label} did not receive the message")
            self.assertEqual(messages[0].kind, MSG_LOBBY)
            self.assertEqual(messages[0].name, name, f"the {label} sees another author")

    def test_utf8_text_round_trips(self) -> None:
        client, _ = self.login()
        text = "\u54c8\u55bd w\u00f6rld \u00f1"
        client.chat(text)
        self.assertTrue(wait_for_lobby_text(client, text), "the message text was mangled on the way back")

    def test_message_at_1024_bytes_is_accepted(self) -> None:
        client, _ = self.login()
        text = "x" * 1024
        client.chat(text)
        self.assertTrue(wait_for_lobby_text(client, text), "a 1024-byte message was not delivered")


class WhisperTest(BlackBoxTest):
    """Kind 0: private messages addressed by character name."""

    def test_whisper_to_an_online_user_is_delivered(self) -> None:
        first, first_packets = self.login()
        second, second_packets = self.login(OTHER_USER)
        sender = user_start(first_packets).character
        target = user_start(second_packets).character
        first.whisper(target, "just between us")
        received = [whisper for whisper in whispers(second.collect(0.7)) if whisper.text == "just between us"]
        self.assertTrue(received, "the target did not receive the private message")
        self.assertEqual(received[0].direction, 1, "the target copy is not marked as incoming")
        self.assertEqual(received[0].name, sender)
        copies = [whisper for whisper in whispers(first.collect(0.6)) if whisper.text == "just between us"]
        self.assertTrue(copies, "the sender did not receive its own copy")
        self.assertEqual(copies[0].direction, 0, "the sender copy is not marked as outgoing")
        self.assertEqual(copies[0].name, target)

    def test_whisper_to_an_unknown_user_reports_not_found(self) -> None:
        client, _ = self.login()
        client.whisper("NoSuchPlayer", "anyone there")
        reasons = [text for _kind, text in notices(client.collect(0.7))]
        self.assertTrue(
            any("MSG_TELL_USER_NOT_FOUND" in reason for reason in reasons),
            f"no user-not-found notice in {reasons}",
        )
        client.chat("still connected")
        self.assertTrue(wait_for_lobby_text(client, "still connected"), "the session died on an unknown target")


class OverlongMessageTest(BlackBoxTest):
    """The Go build caps channel chat at 1024 bytes and drops the socket."""

    def setUp(self) -> None:
        self.go_specific("the channel chat length cap is Go-only")

    def test_message_above_1024_bytes_drops_the_connection(self) -> None:
        client, _ = self.login()
        client.chat("y" * 1025)
        self.assertTrue(client.expect_closed(), "an over-long message was accepted")


if __name__ == "__main__":
    unittest.main(verbosity=2)
