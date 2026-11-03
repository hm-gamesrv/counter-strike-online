"""Black-box smoke tests against a running master server.

Run:  python blackbox/test_smoke.py
      CSO_PORT=30003 python blackbox/test_smoke.py

These talk to the server over TCP only; they never touch the database, the Go
packages, or the data directory. Point them at .data-test (the writable copy) or
at a reference server such as the WSL instance on 30003.
"""

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from cso_client import (  # noqa: E402
    PKT_INVENTORY_DEFAULT,
    PKT_INVENTORY_UPDATE,
    PKT_MESSAGE,
    PKT_REPLY,
    PKT_USER_INFO,
    PKT_USER_START,
    PKT_VERSION,
    Client,
    ids,
)
from cso_proto import GREETING  # noqa: E402

HOST = os.environ.get("CSO_HOST", "127.0.0.1")
PORT = int(os.environ.get("CSO_PORT", "30002"))
USER = os.environ.get("CSO_USER", "tester1")
PASSWORD = os.environ.get("CSO_PASSWORD", "secret1")


class SmokeTest(unittest.TestCase):
    def connect(self) -> Client:
        client = Client(HOST, PORT)
        try:
            client.connect()
        except OSError as error:
            client.close()
            self.skipTest(f"no server on {HOST}:{PORT}: {error}")
        self.addCleanup(client.close)
        return client

    def test_greeting(self) -> None:
        client = self.connect()
        self.assertEqual(client.greeting, GREETING)

    def test_version_handshake(self) -> None:
        client = self.connect()
        client.send_version()
        packets = client.collect()
        self.assertTrue(packets, "server sent nothing after the version packet")
        self.assertEqual(packets[0].id, PKT_VERSION)
        self.assertEqual(packets[0].payload, b"\x00")

    def test_login_opens_the_lobby(self) -> None:
        client = self.connect()
        packets = client.login(USER, PASSWORD)
        seen = ids(packets)
        self.assertIn(PKT_USER_START, seen, f"no UserStart in {seen}")
        self.assertIn(PKT_USER_INFO, seen, f"no user info in {seen}")
        self.assertIn(PKT_INVENTORY_DEFAULT, seen, f"no default inventory in {seen}")
        # The login order is pinned: inventory never arrives before UserStart.
        self.assertLess(seen.index(PKT_USER_START), seen.index(PKT_INVENTORY_DEFAULT))
        reply = next(packet for packet in packets if packet.id == PKT_REPLY)
        self.assertEqual(reply.payload[0], 0, "login reply is not success")

    def test_login_consumes_a_session_and_yields_inventory_updates(self) -> None:
        client = self.connect()
        packets = client.login(USER, PASSWORD)
        self.assertIn(PKT_INVENTORY_UPDATE, ids(packets), "no inventory patch after login")

    def test_wrong_password_is_rejected(self) -> None:
        client = self.connect()
        packets = client.login(USER, "definitely-wrong")
        seen = ids(packets)
        self.assertNotIn(PKT_USER_START, seen, "bad password still opened a session")
        self.assertIn(PKT_MESSAGE, seen, "no rejection notice")


if __name__ == "__main__":
    unittest.main(verbosity=2)
