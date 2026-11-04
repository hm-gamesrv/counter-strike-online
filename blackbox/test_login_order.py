"""Pins the login packet order against the reference build.

The reference sends its login chain in the order CUserManager::SendLoginPacket
(usermanager.cpp:575) performs it, and the lobby join inside CChannel::UserJoin
(channel.cpp:35) appends a second room list. That order is observable - a
scripted client sees the same sequence of ids - so this case records it instead
of leaving it implicit in the Go call graph.

Derived from blackbox/trace.py against the unmodified 30003 build with a fresh
.data snapshot: 60 packets for tester1 with no rooms open.
"""

from __future__ import annotations

import unittest

from cso_harness import PKT_METADATA, BlackBoxTest, ids, packets_of

#: Ids the reference emits before the metadata dump, and after it. The
#: metadata run itself is data driven, so its length comes from the payload.
HEAD = [0, 1, 150, 157, 76, 74]
TAIL = [
    99,
    99,
    99,
    159,
    159,
    152,
    154,
    77,
    77,
    77,
    67,
    72,
    72,
    72,
    67,
    67,
    153,
    151,
    151,
    180,
    69,
    103,
]


class LoginOrderTest(BlackBoxTest):
    def test_login_packet_order(self) -> None:
        _, packets = self.login()
        seen = ids(packets)
        metadata = len(packets_of(packets, PKT_METADATA))
        expected = HEAD + [PKT_METADATA] * metadata + TAIL
        self.assertEqual(
            expected,
            seen,
            "login packet order diverges from the reference build",
        )


if __name__ == "__main__":
    unittest.main()
