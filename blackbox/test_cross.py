"""Cross-server invariants: one script, both builds, stable properties only.

The comparison stays away from bytes that are known to differ between the Go
build and the reference (metadata zip output, 157 flags, 151 repetition, 67
welcome notices, 153 lobby label, 72 title, 103 position). What is compared is
what a client must see either way: the packet families a login sends and how
often, the metadata type sequence, the order of the login stages, inventory
framing, the room list header, and the bytes of frames whose content is data or
configuration driven and therefore identical (0, 1, 69, 74, 76, 77, 99, 103,
150, 152, 159, 180).
"""

from __future__ import annotations

import collections
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from cso_client import (  # noqa: E402
    LOBBY_ADD,
    PKT_INVENTORY_DEFAULT,
    PKT_INVENTORY_UPDATE,
    PKT_MESSAGE,
    PKT_ROOM_LIST,
    ids,
)
import cso_harness as harness  # noqa: E402

#: Packet families both builds send once per login, with identical bytes.
STABLE_PAYLOAD_IDS = [0, 1, 69, 74, 76, 77, 99, 103, 150, 152, 159, 180]

#: Families whose multiplicity is a known difference; compared for presence only.
KNOWN_COUNT_DIFFERENCES = [PKT_MESSAGE, PKT_ROOM_LIST]

#: Login stages whose relative order must not depend on the build.
STAGE_ORDER = [
    (0, 1),
    (1, 150),
    (150, 157),
    (157, 91),
    (91, 99),
    (99, 152),
    (150, 152),
    (152, 154),
    (154, 77),
    (152, 153),
    (153, 151),
]


def observe(port: int) -> dict:
    """Logs in once and returns the parts of the chain the comparison needs."""
    try:
        packets = harness.login_packets(port)
    except OSError as error:
        raise unittest.SkipTest(f"no server on {harness.HOST}:{port}: {error}") from error
    payloads: dict[int, list[bytes]] = {}
    for packet in packets:
        payloads.setdefault(packet.id, []).append(packet.payload)
    return {
        "ids": ids(packets),
        "payloads": payloads,
        "default": harness.default_inventory(harness.first_packet(packets, PKT_INVENTORY_DEFAULT).payload),
        "patches": [
            harness.inventory_patch(packet.payload)
            for packet in harness.packets_of(packets, PKT_INVENTORY_UPDATE)
        ],
        "metadata_types": [frame[0] for frame in harness.metadata_frames(packets)],
        "room_list": harness.room_list(harness.first_packet(packets, PKT_ROOM_LIST).payload),
    }


def chat_frames(port: int) -> tuple[list, list]:
    """Two live sessions: returns the channel and private frames the listener sees."""
    first = harness.open_client(port)
    try:
        first.login(harness.USER, harness.PASSWORD, idle=harness.LOGIN_IDLE)
        second = harness.open_client(port)
        try:
            packets = second.login(harness.OTHER_USER, harness.PASSWORD, idle=harness.LOGIN_IDLE)
            target = harness.user_start(packets).character
            first.chat("cross server chat")
            first.whisper(target, "cross server whisper")
            seen = second.collect(1.0)
            chats = [
                (message.kind, message.name, message.text)
                for message in harness.lobby_messages(seen)
                if message.text == "cross server chat"
            ]
            private = [
                (whisper.direction, whisper.name, whisper.text)
                for whisper in harness.whispers(seen)
                if whisper.text == "cross server whisper"
            ]
            return chats, private
        finally:
            second.close()
    finally:
        first.close()


class CrossServerTest(unittest.TestCase):
    """Compares one login on the target server with one login on the baseline."""

    @classmethod
    def setUpClass(cls) -> None:
        if harness.PORT == harness.BASELINE_PORT:
            raise unittest.SkipTest("the target is the reference build; the comparison needs both")
        cls.ours = observe(harness.PORT)
        cls.reference = observe(harness.BASELINE_PORT)

    def test_packet_family_multiplicity_matches(self) -> None:
        ours = collections.Counter(self.ours["ids"])
        reference = collections.Counter(self.reference["ids"])
        for family in KNOWN_COUNT_DIFFERENCES:
            ours.pop(family, None)
            reference.pop(family, None)
        self.assertEqual(ours, reference, "the login sends a different set of packet families")
        for family in KNOWN_COUNT_DIFFERENCES:
            self.assertIn(family, self.ours["ids"])
            self.assertIn(family, self.reference["ids"])

    def test_stable_payloads_are_identical(self) -> None:
        for packet_id in STABLE_PAYLOAD_IDS:
            with self.subTest(packet=packet_id):
                self.assertEqual(
                    self.ours["payloads"].get(packet_id),
                    self.reference["payloads"].get(packet_id),
                    f"packet {packet_id} differs between the builds",
                )

    def test_login_stage_order_agrees(self) -> None:
        for earlier, later in STAGE_ORDER:
            with self.subTest(stage=(earlier, later)):
                self.assertLess(
                    self.ours["ids"].index(earlier),
                    self.ours["ids"].index(later),
                    f"the Go build sends {later} before {earlier}",
                )
                self.assertLess(
                    self.reference["ids"].index(earlier),
                    self.reference["ids"].index(later),
                    f"the reference sends {later} before {earlier}",
                )

    def test_metadata_type_sequence_matches(self) -> None:
        self.assertEqual(
            self.ours["metadata_types"],
            self.reference["metadata_types"],
            "the metadata dispatch order differs",
        )

    def test_inventory_framing_matches(self) -> None:
        ours = self.ours["default"]
        reference = self.reference["default"]
        self.assertEqual(ours.count, reference.count, "the default inventory size differs")
        self.assertEqual(
            len(self.ours["payloads"][PKT_INVENTORY_DEFAULT][0]),
            len(self.reference["payloads"][PKT_INVENTORY_DEFAULT][0]),
            "packet 152 has a different width",
        )
        self.assertEqual(len(self.ours["patches"]), len(self.reference["patches"]), "a different number of 154 frames")
        self.assertEqual(
            [patch.count for patch in self.ours["patches"]],
            [patch.count for patch in self.reference["patches"]],
            "the inventory patches carry a different number of items",
        )

    def test_room_list_header_matches(self) -> None:
        self.assertEqual(self.ours["room_list"].header, self.reference["room_list"].header)
        self.assertEqual(self.ours["room_list"].count, self.reference["room_list"].count)

    def test_lobby_join_event_is_delivered_by_both(self) -> None:
        for port, label in ((harness.PORT, "Go"), (harness.BASELINE_PORT, "reference")):
            with self.subTest(server=label):
                first = harness.open_client(port)
                try:
                    first.login(harness.USER, harness.PASSWORD, idle=harness.LOGIN_IDLE)
                    second = harness.open_client(port)
                    try:
                        packets = second.login(harness.OTHER_USER, harness.PASSWORD, idle=harness.LOGIN_IDLE)
                        uid, block = harness.user_info_block(packets)
                        joined = harness.wait_for_lobby_event(first, LOBBY_ADD, uid)
                        self.assertIsNotNone(joined, f"{label}: no join event for the second user")
                        assert joined is not None
                        self.assertEqual(joined.info, block, f"{label}: the join event and packet 157 disagree")
                    finally:
                        second.close()
                finally:
                    first.close()

    def test_chat_frames_are_identical(self) -> None:
        ours_chat, ours_whisper = chat_frames(harness.PORT)
        reference_chat, reference_whisper = chat_frames(harness.BASELINE_PORT)
        self.assertTrue(ours_chat, "the Go build delivered no channel chat")
        self.assertTrue(ours_whisper, "the Go build delivered no private message")
        self.assertEqual(ours_chat, reference_chat, "channel chat frames differ")
        self.assertEqual(ours_whisper, reference_whisper, "private message frames differ")


if __name__ == "__main__":
    unittest.main(verbosity=2)
