"""Chat command parsing: usage errors, registration validation, unauthenticated use.

Commands arrive as packet 67 kind 1 with a leading '/'; whatever the parser does
not recognize falls through to channel chat. The shared cases assert what both
builds do (the grammar matches, the wording does not), and the replies only the
Go build defines skip on the reference build. Wording and reply-shape
differences are recorded in blackbox/FINDINGS.md.
"""

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from cso_client import (  # noqa: E402
    MSG_CHAT_NOTICE,
    MSG_NOTICE,
    PKT_LOBBY,
    PKT_REPLY,
    PKT_ROOM,
    PKT_ROOM_LIST,
    PKT_USER_START,
    Client,
    ids,
)
from cso_harness import (  # noqa: E402
    OTHER_USER,
    PASSWORD,
    USER,
    BlackBoxTest,
    first_packet,
    lobby_messages,
    notices,
    wait_for_lobby_text,
)


class CommandUsageTest(BlackBoxTest):
    """Argument and validation errors; every case is read-only."""

    def anonymous(self) -> Client:
        """A socket that completed the version handshake but never logged in."""
        client = self.connect()
        client.send_version()
        client.collect(0.4)
        return client

    def test_login_without_arguments_asks_for_them(self) -> None:
        for command in ("/login", f"/login {USER}", f"/login {USER} {PASSWORD} extra"):
            with self.subTest(command=command):
                client = self.anonymous()
                client.chat(command)
                found = notices(client.collect(0.6))
                self.assertTrue(found, f"no reply to {command!r}")
                kind, text = found[0]
                self.assertEqual(kind, MSG_CHAT_NOTICE, f"usage is not reported in the chat log: {found}")
                self.assertIn("<username>", text)
                self.assertIn("<password>", text)

    def test_register_without_arguments_asks_for_them(self) -> None:
        client = self.anonymous()
        client.chat("/register")
        found = notices(client.collect(0.6))
        self.assertTrue(found, "no reply to /register")
        kind, text = found[0]
        self.assertEqual(kind, MSG_CHAT_NOTICE)
        self.assertIn("<username>", text)
        self.assertIn("<password>", text)

    def test_register_rejects_a_short_username(self) -> None:
        client = self.anonymous()
        client.chat(f"/register ab {PASSWORD}")
        found = notices(client.collect(0.6))
        self.assertTrue(found, "no reply to an invalid registration")
        kind, text = found[0]
        self.assertEqual(kind, MSG_NOTICE, "a validation failure is not a message box")
        self.assertTrue(text.startswith("Username"), text)
        self.assertIn("5", text)
        self.assertIn("15", text)
        client.chat(f"/login ab {PASSWORD}")
        self.assertNotIn(PKT_USER_START, ids(client.collect(0.7)), "the rejected account exists")

    def test_register_rejects_a_username_with_symbols(self) -> None:
        client = self.anonymous()
        client.chat(f"/register bad-name {PASSWORD}")
        found = notices(client.collect(0.6))
        self.assertTrue(found, "no reply to an invalid registration")
        kind, text = found[0]
        self.assertEqual(kind, MSG_NOTICE)
        self.assertTrue(text.startswith("Username"), text)

    def test_register_rejects_a_numeric_only_password(self) -> None:
        client = self.anonymous()
        client.chat("/register ghostreg1 12345")
        found = notices(client.collect(0.6))
        self.assertTrue(found, "no reply to an invalid registration")
        kind, text = found[0]
        self.assertEqual(kind, MSG_NOTICE)
        self.assertTrue(text.startswith("Password"), text)
        self.assertIn("5", text)
        self.assertIn("15", text)
        client.chat("/login ghostreg1 12345")
        self.assertNotIn(PKT_USER_START, ids(client.collect(0.7)), "the rejected account exists")

    def test_register_rejects_an_existing_username(self) -> None:
        client = self.anonymous()
        client.chat(f"/register {USER} {PASSWORD}")
        found = notices(client.collect(0.6))
        self.assertTrue(found, "no reply to a duplicate registration")
        kind, text = found[0]
        self.assertEqual(kind, MSG_NOTICE)
        self.assertEqual(text, "User with this username already exists.")


class UnauthenticatedTest(BlackBoxTest):
    """Chat and room requests from a socket that has not logged in."""

    def anonymous(self) -> Client:
        client = self.connect()
        client.send_version()
        client.collect(0.4)
        return client

    def test_chat_before_login_is_not_broadcast(self) -> None:
        lobby, _ = self.login()
        anonymous = self.anonymous()
        anonymous.chat("broadcast attempt from an anonymous socket")
        anonymous.collect(0.5)
        self.assertEqual(
            lobby_messages(lobby.collect(0.5)),
            [],
            "an unauthenticated socket reached channel chat",
        )
        anonymous.chat(f"/login {OTHER_USER} {PASSWORD}")
        self.assertIn(
            PKT_USER_START,
            ids(anonymous.collect(0.8)),
            "the socket is unusable after a rejected chat",
        )

    def test_room_request_before_login_is_refused(self) -> None:
        anonymous = self.anonymous()
        anonymous.send_frame(PKT_ROOM, b"\0" * 16)
        packets = anonymous.collect(0.6)
        self.assertNotIn(PKT_ROOM_LIST, ids(packets), "an unauthenticated room request was answered")
        self.assertNotIn(PKT_LOBBY, ids(packets), "an unauthenticated socket entered the lobby")
        client, login_packets = self.login()
        self.assertIn(PKT_USER_START, ids(login_packets), "the server stopped accepting logins")


class LoggedInCommandTest(BlackBoxTest):
    """Commands sent by a client that is already in the lobby."""

    def test_unknown_command_is_broadcast_as_channel_chat(self) -> None:
        client, _ = self.login()
        client.chat("/definitely-not-a-command")
        packets = client.collect(0.6)
        messages = lobby_messages(packets)
        self.assertTrue(messages, f"no channel chat in {ids(packets)}")
        self.assertEqual(messages[0].text, "/definitely-not-a-command")

    def test_registration_while_logged_in_creates_nothing(self) -> None:
        client, _ = self.login()
        client.chat(f"/register newuser42 {PASSWORD}")
        packets = client.collect(0.6)
        self.assertNotIn(PKT_USER_START, ids(packets), "a second session was opened")
        self.assertTrue(notices(packets), "the server answered nothing")
        other = self.connect()
        other.send_version()
        other.collect(0.3)
        other.chat(f"/login newuser42 {PASSWORD}")
        self.assertNotIn(PKT_USER_START, ids(other.collect(0.7)), "the account was created while logged in")

    def test_second_login_on_the_same_connection_is_refused(self) -> None:
        client, _ = self.login()
        client.chat(f"/login {USER} {PASSWORD}")
        packets = client.collect(0.6)
        self.assertNotIn(PKT_USER_START, ids(packets), "a second session was opened on the same socket")
        self.assertNotIn(PKT_LOBBY, ids(packets), "the lobby was entered twice")
        client.chat("still the same session")
        self.assertTrue(wait_for_lobby_text(client, "still the same session"), "the session is no longer usable")


class GoCommandReplyTest(BlackBoxTest):
    """Replies the Go build defines for commands the reference treats as chat."""

    def setUp(self) -> None:
        self.go_specific("these command replies are Go-specific")

    def test_second_login_replies_with_code_4(self) -> None:
        client, _ = self.login()
        client.chat(f"/login {USER} {PASSWORD}")
        reply = first_packet(client.collect(0.6), PKT_REPLY)
        self.assertEqual(reply.payload[0], 4, "expected the already-authenticated reply code")

    def test_second_login_does_not_leak_the_password_to_the_lobby(self) -> None:
        # The reference build treats the line as channel chat, so every other
        # user in the lobby receives "/login <user> <password>" verbatim.
        first, _ = self.login()
        listener, _ = self.login(OTHER_USER)
        first.chat(f"/login {USER} {PASSWORD}")
        first.collect(0.6)
        received = [message.text for message in lobby_messages(listener.collect(0.6))]
        self.assertEqual(received, [], f"the repeated login was broadcast to the lobby: {received}")

    def test_registration_while_logged_in_reports_already_logged_in(self) -> None:
        client, _ = self.login()
        client.chat(f"/register someone1 {PASSWORD}")
        texts = [text for _kind, text in notices(client.collect(0.6))]
        self.assertIn("Already logged in.", texts)

    def test_room_request_before_login_drops_the_connection(self) -> None:
        client = self.connect()
        client.send_version()
        client.collect(0.4)
        client.send_frame(PKT_ROOM, b"\0" * 16)
        self.assertTrue(client.expect_closed(), "an unauthenticated room request kept the socket open")


if __name__ == "__main__":
    unittest.main(verbosity=2)
