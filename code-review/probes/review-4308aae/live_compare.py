"""Capture normal login and temporary room creation on the two review servers."""
import json
import os
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / "blackbox"))
from cso_client import Client
from cso_proto import Reader, cstring, u8, u16, u32

result = {}
for port in (30002, 30003):
    with Client(port=port) as client:
        login = client.login(os.environ.get("CSO_USER", "tester1"), os.environ.get("CSO_PASSWORD", "secret1"))
        info = {"login_ids": [p.id for p in login]}
        for packet in login:
            if packet.id == 72 and packet.payload[0] == 15:
                reader = Reader(packet.payload)
                reader.u8()
                reader.u32()
                info["recommended_titles"] = [reader.cstring(), reader.cstring()]
            if packet.id == 153 and packet.payload[0] == 0:
                reader = Reader(packet.payload)
                reader.u8()
                count = reader.u16()
                if count:
                    reader.u32()
                    info["first_lobby_label"] = reader.cstring()
                info["lobby_count"] = count
        flags = sum(1 << bit for bit in (0, 3, 6, 7, 8, 9, 10))
        request = u8(0) + u32(flags) + u32(0) * 3
        request += cstring("Review4308") + cstring("") + u8(0) + u16(1) + u8(8) + u8(5) + u16(0)
        client.send(65, request)
        room = client.collect()
        info["room_packets"] = [{"id": p.id, "length": len(p.payload), "hex": p.payload.hex()} for p in room]
        info["login_packets"] = [{"id": p.id, "length": len(p.payload), "hex": p.payload.hex()} for p in login if p.id != 91]
        result[str(port)] = info
        print(port, "titles=", info.get("recommended_titles"), "lobby_label=", info.get("first_lobby_label"), "room=", [(p.id, len(p.payload)) for p in room])
Path(__file__).with_name("live-comparison.json").write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding="utf-8")
