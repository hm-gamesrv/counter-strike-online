"""The login chain: 150/157/153/151/152/154/99/91/180/77 and lobby presence.

Run:  python blackbox/test_lobby.py
      CSO_PORT=30003 python blackbox/test_lobby.py

Assertions decode the framing the client parses (counts, entry widths, order)
rather than exact bytes, so they hold for both builds. The metadata type list is
the dispatch order this repository configures; the wire framing of every frame
is checked separately and the compressed bytes are compared by
blackbox/metadata.py instead.
"""

from __future__ import annotations

import os
import struct
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from cso_client import (  # noqa: E402
    LOBBY_ADD,
    LOBBY_JOIN,
    LOBBY_LEAVE,
    PKT_FAVORITE,
    PKT_INVENTORY_DEFAULT,
    PKT_INVENTORY_UPDATE,
    PKT_LEAGUE,
    PKT_LOBBY,
    PKT_MATCH_TABS,
    PKT_REPLY,
    PKT_ROOM_LIST,
    ids,
)
from cso_harness import (  # noqa: E402
    OTHER_USER,
    USER,
    BlackBoxTest,
    default_inventory,
    first_packet,
    inventory_patch,
    lobby_list,
    metadata_frames,
    metadata_zip,
    packets_of,
    room_list,
    user_info_block,
    user_start,
    wait_for_lobby_event,
)
from cso_proto import Reader  # noqa: E402

#: metadataOrder filtered by the metadata this repository's ServerConfig enables.
EXPECTED_METADATA_TYPES = [
    0,
    1,
    3,
    4,
    6,
    8,
    9,
    15,
    17,
    20,
    24,
    27,
    28,
    29,
    30,
    31,
    37,
    38,
    39,
    40,
    48,
    43,
    49,
    45,
    35,
    42,
    16,
    50,
    52,
    53,
    54,
    55,
]

#: The fixed 14-byte 151 header: server, channel, mode and marker fields.
ROOM_LIST_HEADER = bytes.fromhex("0004ffff00000100000002000100")


class LoginChainTest(BlackBoxTest):
    """What one successful login sends, decoded stage by stage."""

    def test_login_reply_is_success(self) -> None:
        _client, packets = self.login()
        reply = first_packet(packets, PKT_REPLY)
        self.assertEqual(reply.payload[0], 0, f"login reply is not success: {reply.payload!r}")
        self.assertIn(b"S_REPLY_OK", reply.payload)

    def test_user_start_identifies_the_account(self) -> None:
        _client, packets = self.login()
        start = user_start(packets)
        self.assertEqual(start.user, USER)
        self.assertTrue(start.character, "packet 150 carries no character name")
        self.assertEqual(start.tail, b"\x01\x00\x00\x00\x00\x00\x00\x00", "the 150 trailer changed")

    def test_user_info_matches_user_start(self) -> None:
        _client, packets = self.login()
        start = user_start(packets)
        uid, block = user_info_block(packets)
        self.assertEqual(uid, start.uid, "packet 157 is for another user")
        flags = struct.unpack_from("<I", block, 0)[0]
        self.assertNotEqual(flags, 0, "packet 157 advertises no optional field")
        self.assertIn(start.character.encode() + b"\0", block, "the character name is missing from the user info")

    def test_inventory_defaults_and_patch(self) -> None:
        _client, packets = self.login()
        defaults = packets_of(packets, PKT_INVENTORY_DEFAULT)
        self.assertEqual(len(defaults), 1, "the default inventory is sent exactly once")
        default = default_inventory(defaults[0].payload)
        self.assertGreater(default.count, 0, "no default items")
        self.assertEqual(
            [item.slot for item in default.items],
            list(range(default.count)),
            "default inventory slots are not contiguous",
        )
        seen = ids(packets)
        patches = [inventory_patch(packet.payload) for packet in packets_of(packets, PKT_INVENTORY_UPDATE)]
        self.assertTrue(patches, "no inventory patch after login")
        self.assertLess(seen.index(PKT_INVENTORY_DEFAULT), seen.index(PKT_INVENTORY_UPDATE))
        for index, patch in enumerate(patches):
            self.assertEqual(patch.count, len(patch.items), f"patch {index} count mismatch")
            slots = [item.slot for item in patch.items]
            if not slots:
                continue
            self.assertEqual(slots, sorted(slots), f"patch {index} slots are not ordered")
            self.assertGreater(min(slots), default.count, "the patch rewrites a default inventory slot")

    def test_match_tabs(self) -> None:
        _client, packets = self.login()
        tabs = packets_of(packets, PKT_MATCH_TABS)
        self.assertEqual(len(tabs), 3, f"expected three 99 frames, got {[len(p.payload) for p in tabs]}")
        reader = Reader(tabs[0].payload)
        self.assertEqual(reader.u8(), 40)
        self.assertEqual(reader.u16(), 45)
        self.assertEqual(reader.u8(), 2)
        self.assertEqual(reader.u8(), 1)
        self.assertEqual(reader.cstring(), "")
        count = reader.u8()
        self.assertEqual(count, 4, "the lobby exposes exactly four match tabs")
        entries = [(reader.u8(), reader.u8(), reader.u16(), reader.cstring()) for _ in range(count)]
        self.assertEqual(reader.remaining(), 0, "trailing bytes after the match tabs")
        self.assertEqual([entry[0] for entry in entries], [0, 1, 2, 3])
        self.assertEqual([entry[3] for entry in entries], ["Zombie3z", "quickstart", "roomlist", "studio"])
        self.assertEqual(len(tabs[1].payload), 8)
        self.assertEqual(tabs[1].payload[0], 0x65)
        self.assertEqual(len(tabs[2].payload), 9)
        self.assertEqual(tabs[2].payload[0], 9)

    def test_metadata_frames_are_well_formed(self) -> None:
        _client, packets = self.login()
        frames = metadata_frames(packets)
        self.assertGreaterEqual(len(frames), 20, f"only {len(frames)} metadata frames")
        types = [frame[0] for frame in frames]
        self.assertEqual(len(set(types)), len(types), f"duplicate metadata type in {types}")
        archives = 0
        for frame in frames:
            content = metadata_zip(frame)
            if content is None:
                continue
            archives += 1
            name, data = content
            self.assertTrue(name, f"metadata type {frame[0]} has an empty archive")
            self.assertTrue(data, f"metadata type {frame[0]} decompresses to nothing")
        self.assertGreaterEqual(archives, 10, "most metadata frames should be zip archives")
        self.check_block_metadata({frame[0]: frame for frame in frames})

    def check_block_metadata(self, by_type: dict[int, bytes]) -> None:
        """The four count-prefixed metadata frames are built by the server."""
        boxes = by_type.get(4)
        if boxes is not None:
            count = struct.unpack_from("<I", boxes, 1)[0]
            self.assertGreater(count, 0, "no item box entries")
            self.assertEqual(len(boxes), 5 + 26 * count, "item box entries are not 26 bytes apart")
        paints = by_type.get(6)
        if paints is not None:
            reader = Reader(paints)
            reader.u8()
            for _ in range(reader.u16()):
                reader.u16()
                for _ in range(reader.u16()):
                    reader.u16()
            self.assertEqual(reader.remaining(), 0, "weapon paint entries do not fill the frame")
        weapons = by_type.get(15)
        if weapons is not None:
            reader = Reader(weapons)
            reader.u8()
            reader.bytes(reader.u16() * 4)
            self.assertEqual(reader.remaining(), 0, "the zombie war weapon list is not a u32 array")
        random_weapons = by_type.get(16)
        if random_weapons is not None:
            reader = Reader(random_weapons)
            reader.u8()
            for _ in range(reader.u32()):
                reader.u32()
                for _ in range(reader.u32()):
                    reader.u8()
                    reader.u32()
                    reader.u32()
            self.assertEqual(reader.remaining(), 0, "the random weapon list does not fill the frame")

    def test_metadata_type_order(self) -> None:
        _client, packets = self.login()
        types = [frame[0] for frame in metadata_frames(packets)]
        self.assertEqual(types, EXPECTED_METADATA_TYPES, "the metadata dispatch order changed")

    def test_room_list_header_and_empty_lobby(self) -> None:
        _client, packets = self.login()
        listings = packets_of(packets, PKT_ROOM_LIST)
        self.assertTrue(listings, "no room list after login")
        listing = room_list(listings[0].payload)
        self.assertEqual(listing.header, ROOM_LIST_HEADER, "the room list header changed")
        self.assertEqual(listing.count, 0, "the test instance should not have rooms open")
        self.assertEqual(listing.tail, b"", "trailing bytes after the room count")

    def test_league_packet_has_five_divisions(self) -> None:
        _client, packets = self.login()
        reader = Reader(first_packet(packets, PKT_LEAGUE).payload)
        self.assertEqual(reader.u8(), 0)
        count = reader.u8()
        self.assertEqual(count, 5, "the league ladder has five divisions")
        for index in range(count):
            self.assertEqual(reader.u8(), index)
            self.assertEqual(reader.u8(), 1)
            self.assertEqual(reader.u16(), 1)
            self.assertEqual(reader.u8(), 5)
            self.assertEqual(reader.u32(), 0)
        self.assertEqual(reader.remaining(), 0, "trailing bytes after the league divisions")

    def test_favorites_packets(self) -> None:
        _client, packets = self.login()
        favorites = packets_of(packets, PKT_FAVORITE)
        self.assertEqual([packet.payload[0] for packet in favorites], [2, 0, 6], "the favorite subtypes changed")
        loadouts = Reader(favorites[0].payload)
        loadouts.u8()
        self.assertGreater(loadouts.u16(), 0, "no selected character")
        current = loadouts.u8()
        self.assertEqual(loadouts.u8(), 12, "twelve loadouts are sent")
        self.assertEqual(loadouts.u8(), 4, "a loadout holds four slots")
        for _ in range(12 * 4):
            loadouts.u16()
        self.assertEqual(loadouts.remaining(), 0, "trailing bytes after the loadouts")
        self.assertLess(current, 12, "the current loadout is out of range")
        self.assertEqual((len(favorites[1].payload) - 1) % 25, 0, "buy menu rows are not 25 bytes apart")
        self.assertEqual(favorites[2].payload[1], 8, "the bookmark list holds eight entries")

    def test_lobby_list_contains_self_with_the_same_user_info(self) -> None:
        _client, packets = self.login()
        uid, block = user_info_block(packets)
        listing = first_packet(packets, PKT_LOBBY)
        kind, count, entries, consumed = lobby_list(listing.payload, {uid: block})
        self.assertEqual(kind, LOBBY_JOIN, "the first 153 is not the lobby list")
        self.assertGreaterEqual(count, 1)
        found = {entry.uid: entry for entry in entries}
        self.assertIn(uid, found, "the lobby list does not contain this session")
        self.assertEqual(found[uid].info, block, "the lobby entry and packet 157 disagree")
        if count == 1:
            self.assertEqual(consumed, len(listing.payload), "trailing bytes after the lobby entry")


class LobbyPresenceTest(BlackBoxTest):
    """Two live sessions: the list, the join event and the leave event."""

    def test_second_user_appears_in_the_lobby_list(self) -> None:
        _first, first_packets = self.login()
        _second, second_packets = self.login(OTHER_USER)
        infos = {}
        for packets in (first_packets, second_packets):
            uid, block = user_info_block(packets)
            infos[uid] = block
        uid_first = user_start(first_packets).uid
        uid_second = user_start(second_packets).uid
        self.assertNotEqual(uid_first, uid_second, "both test accounts report the same user id")
        listing = first_packet(second_packets, PKT_LOBBY)
        kind, count, entries, consumed = lobby_list(listing.payload, infos)
        self.assertEqual(kind, LOBBY_JOIN)
        self.assertGreaterEqual(count, 2, "the second session does not see both users")
        found = {entry.uid for entry in entries}
        self.assertTrue({uid_first, uid_second} <= found, f"lobby members are {found}")
        if count == len(entries):
            self.assertEqual(consumed, len(listing.payload), "trailing bytes after the lobby list")

    def test_join_and_leave_events_are_broadcast(self) -> None:
        first, _first_packets = self.login()
        second, second_packets = self.login(OTHER_USER)
        uid_second, block = user_info_block(second_packets)
        joined = wait_for_lobby_event(first, LOBBY_ADD, uid_second)
        self.assertIsNotNone(joined, "the existing session did not see the new user join")
        assert joined is not None
        self.assertEqual(joined.info, block, "the join event and packet 157 disagree")
        second.close()
        left = wait_for_lobby_event(first, LOBBY_LEAVE, uid_second)
        self.assertIsNotNone(left, "the existing session did not see the user leave")


if __name__ == "__main__":
    unittest.main(verbosity=2)
