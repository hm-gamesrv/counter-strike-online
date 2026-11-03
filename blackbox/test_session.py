"""Sessions: credential errors, takeover by a second login, relogin.

Every case logs in as one of the two test accounts and only observes packet 67
(notices) and 150 (the session start); the sockets are closed by the base class.
"""

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from cso_client import MSG_NOTICE, PKT_LOBBY, PKT_USER_START, Client, ids  # noqa: E402
from cso_harness import PASSWORD, USER, BlackBoxTest, notices, user_start, wait_for_lobby_text  # noqa: E402

CREDENTIALS_REJECTED = "Wrong password or username."


class CredentialsTest(BlackBoxTest):
    """A rejected login must say why and must not open a session."""

    def anonymous(self) -> Client:
        client = self.connect()
        client.send_version()
        client.collect(0.4)
        return client

    def assert_rejected(self, command: str) -> list:
        client = self.anonymous()
        client.chat(command)
        packets = client.collect(0.7)
        reasons = notices(packets)
        self.assertTrue(reasons, f"no reply to {command!r}")
        kind, text = reasons[0]
        self.assertEqual(kind, MSG_NOTICE, "the rejection is not a message box")
        self.assertEqual(text, CREDENTIALS_REJECTED)
        self.assertNotIn(PKT_USER_START, ids(packets), "a session was opened")
        self.assertNotIn(PKT_LOBBY, ids(packets), "the lobby was entered")
        return packets

    def test_wrong_password_is_rejected(self) -> None:
        self.assert_rejected(f"/login {USER} definitely-wrong")

    def test_unknown_account_gets_the_same_rejection(self) -> None:
        client = self.anonymous()
        client.chat("/login nosuchuser9 " + PASSWORD)
        reasons = notices(client.collect(0.7))
        self.assertTrue(reasons, "no reply for an unknown account")
        kind, text = reasons[0]
        self.assertEqual(kind, MSG_NOTICE)
        self.assertEqual(text, CREDENTIALS_REJECTED, "an unknown account must not reveal that it is unknown")
        client.chat(f"/login {USER} {PASSWORD}")
        self.assertIn(PKT_USER_START, ids(client.collect(0.9)), "the socket is unusable after a failed login")


class SessionTakeoverTest(BlackBoxTest):
    """One account, one live session."""

    def test_second_login_kicks_the_first_socket(self) -> None:
        first, first_packets = self.login()
        self.assertIn(PKT_USER_START, ids(first_packets))
        second, second_packets = self.login()
        self.assertIn(PKT_USER_START, ids(second_packets), "the second login did not open a session")
        self.assertTrue(first.expect_closed(4.0), "the previous socket stayed open")
        second.chat("takeover complete")
        self.assertTrue(wait_for_lobby_text(second, "takeover complete"), "the new session is not usable")

    def test_relogin_after_a_disconnect_succeeds(self) -> None:
        first, first_packets = self.login()
        uid = user_start(first_packets).uid
        first.close()
        second, second_packets = self.login()
        self.assertIn(PKT_USER_START, ids(second_packets), "the account could not log back in")
        self.assertEqual(user_start(second_packets).uid, uid, "the returning session is another account")


if __name__ == "__main__":
    unittest.main(verbosity=2)
