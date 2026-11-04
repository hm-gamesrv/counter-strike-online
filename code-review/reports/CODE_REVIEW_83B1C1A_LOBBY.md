# 83B1C1A 审查：大厅/房间出站报文的逐字段保真

日期：2026-11-05（Asia/Hong_Kong）。
审查对象：`83b1c1a`（go-rewrite）。报告落盘时工作树已前进到 `750cf7e`：`2a15e61` 修了登录链、`cf172e5` 只是记录、`750cf7e` 改 /removeitem 文案；这三笔都没有动 `internal/server/rooms.go`（本报告的核心新发现都在那里）。因此下文所有"HEAD"结论都以 **83b1c1a** 为准，并单独标注当前工作树/30005 上的现状；**我没有修改任何产品代码或既有测试**。

基线：`.ref/CSNZ_Server` 位于 `4859a6e`（`pwsh scripts/check-ref.ps1` 输出"已在基线"）。行号均指该提交。

## 一、审查范围、方法与命令

范围：

1. 上一轮遗留的 7 条待办（74/76、103、151、180、153、67、157），逐条对着基线源码核实是不是真偏差、偏差在哪个字节。
2. 同一范围（大厅/房间出站报文）里顺手发现的其它字段级偏离。

方法：

- 逐函数对照：`manager/packetmanager.cpp`、`manager/usermanager.cpp`、`manager/channelmanager.cpp`、`channel/channel.cpp`、`room/room.cpp`、`room/roomsettings.cpp`、`packet/packethelper_fulluserinfo.cpp`、`user/user.cpp`、`definitions.h`。
- overlay 探针 14 条：`code-review/probes/review-83b1c1a-lobby/server_test.go`（`TestReview83*`）。在**纯净的 83b1c1a worktree** 里跑一遍（记录 HEAD 的真实偏差），再在当前工作树跑一遍（记录修掉/未修）。
- 端到端：同一份数据、同一个账号（`tester1/secret1`），对 `30003`（C++ 基线）与 `30005`（新 Go 构建，`.data-test-b`）各做一次登录和一次建房/进房，逐字节比。
- `30002` 上的旧二进制没有用于新结论（只在我开始审查时取过一次基线对照）。

命令（`$env:GOTOOLCHAIN="local"`）：

```powershell
pwsh scripts/check-ref.ps1

# 当前工作树（750cf7e）
go vet -overlay code-review/probes/review-83b1c1a-lobby/overlay.json ./internal/server
go test -overlay code-review/probes/review-83b1c1a-lobby/overlay.json ./internal/server -run '^TestReview83' -count=1 -v

# 83b1c1a 纯净树（证明 HEAD 上的偏差）
git worktree add --detach .dist/review-head 83b1c1a
# .dist/review-head-overlay.json 的内容（Replace 键必须指向 worktree 里的虚拟文件名）：
# {"Replace":{"<repo>/.dist/review-head/internal/server/review_83b1c1a_lobby_test.go":
#             "<repo>/code-review/probes/review-83b1c1a-lobby/server_test.go"}}
go test -overlay ..\..\.dist\review-head-overlay.json ./internal/server -run '^TestReview83' -count=1 -v

# 端到端
python blackbox\trace.py --port 30003 --out .dist\trace-30003.txt
python blackbox\trace.py --port 30005 --out .dist\trace-30005.txt
python code-review\probes\review-83b1c1a-lobby\login_payloads.py --port 30003 > .dist\p-30003.txt
python code-review\probes\review-83b1c1a-lobby\login_payloads.py --port 30005 > .dist\p-30005.txt
python code-review\probes\review-83b1c1a-lobby\room_probe.py --port 30003 --join
python code-review\probes\review-83b1c1a-lobby\room_probe.py --port 30005 --join
```

## 二、结论总览

| # | 项 | HEAD 83b1c1a | 当前 750cf7e / 30005 | 严重度 |
|---|---|---|---|---|
| 1 | 74/76 包序 | 偏离（74 在 76 前） | 已修，与基线逐字节一致 | P2（仅包序） |
| 2 | 103 位置 | 偏离（在 152 之前） | 已修（末尾） | P2（仅包序） |
| 2b | 103 空 URL 时整包不发 | 偏离 | **仍偏离** → N8 | P2 |
| 3 | 151 次数与 count 字段 | 偏离（1 次 vs 2 次）；count 字段本身一致 | 已修（2 次） | P2（重复包） |
| 3b | 151/65-34 每个房间条目的字段 | 偏离（flags 0xF5/0，19 字节 vs 85 字节） | **仍偏离** → N2 | **P1** |
| 4 | 180 登录包位置 | 偏离（在 153 前） | 已修；包内容一直一致 | P2（仅包序） |
| 5 | 153 条目字符串 | 偏离（真实昵称 vs 字面量） | 已修，412 字节逐字节一致 | P1（内容） |
| 6 | 67 欢迎/公告条数与顺序 | 偏离（少 1 条、顺序不同） | 已修，30005/30003 三条 67 全同 | P1（内容） |
| 7 | 157 flags/可选段 | 偏离（0x0c4d03ea/1，300B） | 已修，398B 逐字节一致 | P1（内容） |
| N1 | 65/0 房间设置 flags/字段 | 偏离（60B vs 192B） | **仍偏离** | **P1** |
| N3 | 建房包集合（65/5、2×65/4、65/3） | 偏离 | **仍偏离** | P2 |
| N4 | 进房缺 65/13 SetUserTeam | 偏离 | **仍偏离** | P2 |
| N5 | 被拒设置更新的回滚包为空 | 偏离 | **仍偏离** | P2 |
| N6 | 153 用户列表顺序 | 偏离（map 随机顺序） | **仍偏离** | P2 |
| N7 | 未打 UDP 洞时网络信息全 0 | 偏离 | **仍偏离** | P2 |
| N8 | 103 空 URL 时整包不发 | 偏离 | **仍偏离** | P2 |

计数：**2 项 P1 + 6 项 P2**（N1/N2 是 P1，其余是 P2）；7 条待办中 6 条在 HEAD 上确认为真偏差且已修复，第 7 条（103）的位置已修、同一条里还剩 N8。

---

## 三、7 条待办的逐条结论

### 1. 74/76 的先后（附编号更正）

**先更正编号**：74 不是房间列表，而是 `PacketId::Ban = 74`，76 是 `PacketId::Option = 76`（`definitions.h:42-43`）。房间列表是 151。所以这条的真实内容是**登录链里 Ban 与 Option 的先后**，不是房间条目顺序（条目顺序/字段是 N2）。

基线字节布局与调用点：

- `manager/usermanager.cpp:575-591`（`CUserManager::SendLoginPacket`）：150 → 157 → **若 config 非空则 76** → 若 banList 非空则 74 → **74（BanSettings，无条件）**。
- 76：`packetmanager.cpp:757-769`，`U8(0)+U16(len)+data+U8(1)`。
- 74：`packetmanager.cpp:6915-6926`（BanList：`U8(0)+U16(n)+strings`）、`:6937-6944`（BanSettings：`U8(3)+U8(settings)`）。

HEAD 实现：`internal/server/lobby.go:370` 先 `sendBanLogin`（两/一个 74），`:386` 才发 76。

复现：`TestReview83LoginOptionBeforeBan`（HEAD 失败）：

```
login sequence: [150 157 74 74 76 99 99 99 159 159 103 152 154 77 77 77 72 72 72 180 153 151 69 67 67]
login sends 74 before 76; reference sends 76 then 74
```

结论：**HEAD 偏离（包序，P2）**；`2a15e61` 已修。30005 与 30003 的 76/74 载荷逐字节相同（`login_payloads.py` 的 diff 为空，见 §六）。

### 2. 103 的字段位置 / 是否总发

基线布局：`packetmanager.cpp:7268-7279`，`U8(20)+String(vxl)+String(vmg)`；调用点 `usermanager.cpp:637`，是登录链的**最后一条**（在 69 之后）。

HEAD：`internal/server/lobby.go:424`（`sendVoxelURLs`）排在 159 之后、背包之前；`voxel.go:7-16` 在两个 URL 都为空时直接 return。

复现：

- `TestReview83LoginVoxelURLsPosition`（HEAD 失败）：`103 sent at 8, before 69 at 20`。
- `TestReview83LoginVoxelURLsSentWhenEmpty`（HEAD 与当前树都失败）：`no 103 with empty voxel URLs`。

结论：位置**HEAD 偏离、已修**（P2）；**空 URL 时整包不发仍是偏离 → N8**。URL 非空时包内容 117 字节，30005 与 30003 逐字节相同。

### 3. 151 的次数、count 字段与条目

基线：

- 头：`packetmanager.cpp:1978-1992`，`U8(0 FullRoomList)+U8(4)+U16(0xFFFF)+U16(0)+U16(1)+U16(0)+U16(2)+U16(1)+U8(rooms.size())`。**count 是 U8、来源是 `channel->GetRooms().size()`**。
- 登录时发**两次**：`channel/channel.cpp:68`（`CChannel::UserJoin` 内）与 `manager/channelmanager.cpp:207`（`JoinChannel` 末尾）。
- 客户端主动请求房间列表（`InRoomType::RoomListRequest=22`）时再发一次：`channelmanager.cpp:120-121`。
- 每个条目：`BuildRoomInfo(msg, room, RLFLAG_ALL, RLHFLAG_ALL)`（`:1996`）。见 N2。

HEAD：`internal/server/lobby.go:138` 只调一次 `roomList`；`rooms.go:288-308` 的 count 是 `U8(len(s.rooms))`。

复现：`TestReview83LoginSendsRoomListTwice`（HEAD）：`login sent 1 room lists, reference sends 2`。

结论：**次数 HEAD 偏离、已修（P2）**；count 字段的宽度与来源一致（都是"本频道房间数"，本部署单频道，等价）。头部 15 字节实测两边完全相同（`0004ffff000001000000020001000100` + count）。条目本身的字段问题见 N2。

补充（不是缺陷，记录）：`rooms.go:492` 在 `len(s.rooms)>=255` 时拒绝建房；基线没有这个检查，第 256 个房间会让 U8 count 溢出。Go 的拒绝对客户端更安全，保留即可。

### 4. 180 的两个变体与登录位置

基线有两个 180：

- 登录段位包 `SendLeaguePacket`：`packetmanager.cpp:7022-7040`，`U8(0)+U8(5)+5×{U8(i),U8(1),U16(1),U8(5),U32(0)}`；调用点 `usermanager.cpp:632`，在 JoinChannel（:624）**之后**。
- 开局计量条 `SendLeagueGaugePacket`：`packetmanager.cpp:7042-7054`，`U8(6)+U8(1)+U8(gameModeId)+U8(0)`；调用点 `channelmanager.cpp:1631`，在 HostStart 分支之前、对请求者发。

HEAD：`lobby.go:442` 的 `sendLeague` 在 `joinLobby`（:443）之前；开局计量条 `host.go:42-44` 的 `{6,1,mode,0}` 与基线一致。

复现：`TestReview83LoginLeagueAfterLobby`（HEAD）：`180 sent at 17, before 153 at 18`。

结论：**登录位置 HEAD 偏离、已修（P2）**；两个 180 的载荷一直与基线逐字节相同（47B），属于"已核实一致"。

### 5. 153 的条目字符串（label）

基线：

- `SendLobbyJoin`（`packetmanager.cpp:1746-1761`）：`U8(0 Join)+U16(outsideUsers)`，每个条目 `U32(id)+WriteString("test_lobby")+FullUserInfo`。**这个字符串是字面量，不是昵称**（`:1756`）。
- `SendLobbyUserJoin`（`:1763-1778`）：`U8(1)+U32(id)+WriteString("TIBE YEBAT'?")+FullUserInfo`（`:1770`），同样是字面量。
- 用户集合是 `channel->GetOutsideUsers()`（不在房间里的频道成员）。

HEAD：`lobby.go:113`/`:132` 写 `c.user.Name`/`u.user.Name`。

复现：`TestReview83LobbyEntryLabel`（HEAD）：`153 entry label "loginuser", reference writes "test_lobby"`。

结论：**HEAD 偏离（字段内容，P1）**；`2a15e61` 已修。修后 153 载荷 412B，30005 与 30003 **逐字节相同**；两人时 821B = 412 + (4+11+394)，每个条目宽度也与基线一致。

### 6. 67 的欢迎/公告条数与顺序

基线登录段的 67：

1. `SendUserNotices`（`usermanager.cpp:611, 769-775`）：每个配置公告一条 `SendUMsgNotice`（`packetmanager.cpp:405-421`，`U8(65)+U16(id+10000)+U8(type)+String(url)+U32(start)+U32(end)+String(name)+String(desc)`），在商店之前。
2. `usermanager.cpp:617-619`：**无条件的硬编码英文欢迎框**（`SendUMsgNoticeMsgBoxToUuid`，`U8(10)+String(text)`），168B。
3. `usermanager.cpp:621-622`：配置的 `welcomeMessage`（若非空），在 JoinChannel 之前。

HEAD（83b1c1a）：只有"配置欢迎 + 公告"共 2 条，且都排在 69 之后；硬编码英文欢迎完全缺失。

复现：`TestReview83LoginNoticeSet`（HEAD）：`login sent 2 UMsg packets, reference sends 3`。

结论：**HEAD 偏离（内容缺失，P1）**；`2a15e61` 已修。30005/30003 的 67 序列与大小（150/168/28）完全一致。公告的日期字段两边都写 `StartDate/60`（基线在 `serverconfig.cpp:1181-1182` 加载时除，Go 在 `notice.go:79-80` 发送时除），结果一致，这条已核实。

### 7. 157 的 flags 与可选段

基线：`GetCharacter(UFLAG_LOW_ALL, UFLAG_HIGH_ALL)`（`usermanager.cpp:809`）→ 两个 flag 字都是 `-1`，`CPacketHelper_FullUserInfo::Build`（`packet/packethelper_fulluserinfo.cpp:43-294`）把 `definitions.h:809-843` 的**每一段**都写出来（没数据的补零）。tester1 的 157 载荷 398B。

HEAD：`lobby.go:29-89` 只置位/写出实际有值的段（low=0x0C4D03EA, high=0x00000001，300B），缺 GAMENAME2、UNK4、CASH2、UNK11、RANK、UNK15、UNK17、UNK20、UNK21、UNK23(第二段)、UNK25、UNK28、UNK29、UNK30 等。

复现：`TestReview83UserInfoFlags`（HEAD）：`157 flags low=0x0c4d03ea high=0x00000001, reference writes 0xffffffff/0xffffffff (300 bytes)`。

结论：**HEAD 偏离（内容，P1）**；`2a15e61` 已修。30005 的 157 = 398B，与 30003 逐字节相同（含非零 nameplate 8433）。

备注（不影响判据，但值得记）：当前 `userInfo` 把 `UFLAG_LOW_UNK23` 的第二个 2 字节放在 `UFLAG_LOW_UNK30` 之后，而基线放在 UNK30 之前（`packethelper_fulluserinfo.cpp:272-289`）。两段在基线与当前实现里**都是硬编码 0**，所以最终字节仍然相同（已用 30005/30003 逐字节 diff 证明）；若将来给 UNK30 填非零值，这个顺序会立刻变成错位。建议顺手调正，不作为缺陷计数。

---

## 四、新确认的偏离（`cf172e5` + 30005 上仍然存在）

### N1 [P1] 65/0 的房间设置只带请求里出现过的 flags，基线带全部 flags 与字段

**现象**：同一份数据、同一个建包请求（mode 0/map 0/16 人，flags `0x7C9`）建一个 `Review83Room`：

| | 30003 基线 | 30005 Go |
|---|---|---|
| 建房回包 | `65/5(6B), 65/4(18B), 65/4(18B), 65/0(636B)` | `65/0(504B), 65/3(6B)` |
| 65/0 四个 flag 字 | `ffffffff fffbffff ffffffff ffffffff` | `3077ffc9 04010014 00000000 00000000` |
| 65/0 设置区字节 | 192 | 60 |
| 65/0 房间成员块 | 431 | 431（一致） |

**基线**：`room/roomsettings.cpp:1210-1215` 的 `LoadDefaultSettings` 第一件事就是把四个 flag 字设成 `ROOM_*_ALL`（只清 `ROOM_LOWMID_VOXEL`，`:1320`），然后逐字段填默认值；`CheckSettings` 最后无条件调用它（`:2509`）。`SendRoomCreateAndJoin`（`packetmanager.cpp:2691-2735`，设置区在 `:2704-2705`）与 `SendRoomUpdateSettings`（`:2770-2780`）都走 `WriteSettings`（`:2281-2689`），flag 置位就写对应字段。

**Go**：`internal/server/room_defaults.go:178-269` 的 `roomDefaults` 只对"模式默认值"逐字段 `setNumber`，**从不把四个 flag 字设成 ALL**；`rooms.go:182-193` 的 `write` 按 `s.flags` 原样写。结果：`levelLimit`、`status`、`unk00..`、`gameTime/roundTime/winLimit/killLimit`（这些有值但 flags 没置位的模式除外）、`hostageKillLimit`、`freezeTime`、`bot*`、`kdRule`、`randomMap`、`sd`、`zsDifficulty`、`leagueRule`、`mannerLimit`、`zbLimit`、`isZbCompetitive`、`zbAutoHunting`、`fireBomb`、`mutationRestrict/Limit`、`weaponRestrict`、`familyBattle` 等 30+ 个字段在 65/0 里完全不出现（实测 `65/0 fields: 0:13 3:1 6:1 7:2 8:1 9:1 10:2 11:1 12:1 13:1 14:1 15:1 16:1 17:1 18:1 20:1 21:1 22:1 28:1 29:1 34:5 36:2 48:2 58:1`；基线的字段表是 96 项，其中 2 项是零宽占位）。

**复现**：

```
go test -overlay .../overlay.json ./internal/server -run '^TestReview83RoomCreateSettingsFlags' -v
--- FAIL: 65/0 settings flags [0x3077ffc9 0x4010014 0x0 0x0], reference writes [0xffffffff 0xfffbffff 0xffffffff 0xffffffff] (507 bytes)

python code-review/probes/review-83b1c1a-lobby/room_probe.py --port 30003   # 见 §六
python code-review/probes/review-83b1c1a-lobby/room_probe.py --port 30005
```

**影响**：客户端按 flags 解析；缺段就保留自己的默认值。房间设置面板、模式默认值、状态/受限选项会显示成"客户端默认"而不是服务器实际值；同一份设置在 65/0（建房/进房）与 65/4（更新）里表现不同，客户端重开面板时可能把默认值当成服务器值再提交回来。另外 N5 的回滚包也依赖这组 flags。

**建议**：在 `roomDefaults(creating=true)` 开头把四个 flag 字置 `0xFFFFFFFF`（`lowMid` 清 bit18），并按 `LoadDefaultSettings` 补齐所有字段的**零值**（`unk00..`、`levelLimit`、`hostageKillLimit`、`freezeTime`、`unk21`、`unk25`、`unk28/29`、`status`、`c4Timer/bot*`、`kdRule`、`mapPlaylistIndex`、`enhanceRestrict`、`unk56/57`、`zbLimitFlag`、`unk63/64`、`teamSwitch`、`unk73`、`unk77`、`floatingDamageSkin`、`playerOneTeam`、`unk79_*` 等）。注意 `parseSettings` 侧的宽度表已经是对的，只是写入侧没有这些字段。

### N2 [P1] 151 / 65-34 的房间条目固定只写 6 个字段（flags 0xF5/0），基线写全部字段

**现象**（同一间 `Review83Room`，同一份数据）：

| | 30003 基线 | 30005 Go |
|---|---|---|
| 151 包长 | 117 | 51 |
| 条目 flags | `ffffffff/ffffffff` | `000000f5/00000000` |
| 条目字段字节 | 85 | 19 |
| 151 完整 hex（含头） | `...0e3effffffffffffffff5265...` | `...0e3ef5000000000000005265...` |

**基线**：`SendRoomListFull`（`packetmanager.cpp:1978-2000`）对每个房间调 `BuildRoomInfo(msg, room, RLFLAG_ALL, RLHFLAG_ALL)`（`:1996`）；`BuildRoomInfo`（`:1791-1976`）按 `RLFLAG_*`（`definitions.h:1484-1515`）与 `RLHFLAG_*`（`:1517-1533`）逐字段写出：NAME、UNK、HASPASSWORD、LEVELLIMIT、GAMEMODE、MAPID、PLAYERS、MAXPLAYERS、WEAPONLIMIT、SUPERROOM、UNK2、HOSTNETINFO、CLANBATTLE、UNK3、STATUSSYMBOL（`statusSymbol==3?2:0`）、UNK4、KDRULE、UNK6、UNK7、FRIENDLYFIRE、UNK9、RANDOMMAP、MAPPLAYLIST、UNK12、SD、ZSDIFFICULTY、LEAGUERULE、MANNERLIMIT、ZBLIMIT、UNK18，再是 high 段的 UNK、ISZBCOMPETITIVE、ZBAUTOHUNTING、UNK4、UNK5、FIREBOMB、MUTATIONRESTRICT、MUTATIONLIMIT、UNK9、UNK10、WEAPONRESTRICT、FAMILYBATTLE、FAMILYBATTLECLANIDS(2×U32)、WEAPONBUYCOOLTIME、ZBREBALANCE。`SendVoxelRoomList`（`:3239-3253`）用同一个 `BuildRoomInfo(..., ALL, ALL)`。

**Go**：`internal/server/rooms.go:353-371` 的 `writeRoomListEntry` 只置 `1|4|16|32|64|128`（= NAME、HASPASSWORD、GAMEMODE、MAPID、PLAYERS、MAXPLAYERS，0xF5），只写这 6 个字段；`roomList`（`:288-308`）与 `voxelRoomList`（`:375-395`）都用它。

**复现**：

```
go test -overlay .../overlay.json ./internal/server -run '^TestReview83RoomListEntryFields' -v
--- FAIL: 151 entry flags low=0x000000f5 high=0x00000000 with 19 field bytes;
          reference writes 0xffffffff/0xffffffff and 85 bytes for this room

python code-review/probes/review-83b1c1a-lobby/room_probe.py --port 30003   # 151 len 117 / 85 fields
python code-review/probes/review-83b1c1a-lobby/room_probe.py --port 30005   # 151 len 51 / 19 fields
```

**影响（客户端可见）**：房间列表丢失等级限制、武器限制、超级房、随机图、地图轮换、SD、大灾变难度、联赛/礼仪限制、ZBLimit、竞技/自动狩猎、火雷/变异/武器限制、家族战与 clanID、武器购买冷却、ZB 平衡，以及**房间是否在游戏中的状态标记**（`RLFLAG_STATUSSYMBOL`）。这些正是房间列表页展示/排序用的字段；玩到一半的房间可能在列表里显示为"未开局"，等级限制也不会挡住低等级玩家（服务端侧还有别的校验，但客户端展示已偏离）。

**建议**：`writeRoomListEntry` 改成 `U32(0xFFFFFFFF)+U32(0xFFFFFFFF)`，然后按 `BuildRoomInfo` 的顺序写全部字段；值从 `settings.fields` 取（`number()/text()`），`STATUSSYMBOL` 按 `statusSymbol==3?2:0`，`MAPPLAYLIST` 写 size+每项 (u8,u16)，`ZBLIMIT` 写 flag+4×u32（`size!=4` 时写 4 个 0），`FAMILYBATTLECLANIDS` 写 `clan1/clan2`。`voxelRoomList` 共用即可。

### N3 [P2] 建房包集合：缺 65/5 SetHost 与两条 65/4 主机物品更新，多一条 65/3

**基线**：`CChannelManager::OnNewRoomRequest`（`channelmanager.cpp:1207-1272`）→ `channel->CreateRoom`（`:1243`，`channel.cpp:189-193`）→ `CRoom` 构造（`room.cpp:13-27`）→ `AddUser`（`:94-104`）→ 因为 `m_Users.size()==1` 调 `UpdateHost`（`:969-978`）→

1. `SendRoomSetHost`（`packetmanager.cpp:2808-2818`，6B）给房主；
2. `CheckForHostItems`（`room.cpp:899-930`）：`65/4 superRoom`（`SendRoomUpdateSettings(..., 0, ROOM_LOWMID_SUPERROOM)`，18B）、`65/4 c4Timer`（18B），模式 3/4/5/15/24 再加 `65/4 sd`；
3. `SendJoinNewRoom` → `65/0`（`channelmanager.cpp:1247`，`room.cpp:239-242`）。

实测 30003：`[('05',6), ('04',18), ('04',18), ('00',636)]`。

**Go**：`rooms.go:537-547` 建房后只调 `currentRoom.joined(c)`（`:438-458`）发 65/0 和每个成员的 65/3；`refreshHostRoomItems`（`room_validate.go:312-346`）只在建房前改 `st`，不发 65/5/65/4。实测 30005：`[('00',504), ('03',6)]`。

**复现**：`TestReview83RoomCreatePacketSet`：

```
room create sent 65 subtypes [0 3]; reference sends SetHost, superRoom, c4Timer, CreateAndJoin [5 4 4 0] (65/0 len 507)
```

**影响**：房主进入房间时，客户端收不到"你是房主"的显式通知（65/5）和两条主机物品设置（超级房/C4 音效）；如果客户端依赖 65/5 初始化房主控件，房主 UI 可能靠 65/0 里的 host id 兜底。多出的 65/3 是基线建房时没有的。属于包集合偏离，P2（65/0 里已有 host id，且 superRoom 也在 65/0 里）。

**建议**：建房后按 `UpdateHost` 的顺序发 `65/5`、`65/4 superRoom`、`65/4 c4Timer`（模式 3/4/5/15/24 加 `sd`），把 65/0 放到最后；去掉建房时的 65/3（只在进房/准备切换时发）。

### N4 [P2] 进房时缺 65/13 SetUserTeam 广播

**基线**：`OnJoinRoomRequest`（`channelmanager.cpp:1371-1388`）：`AddUser`（不会 `UpdateHost`，因为 size>1）→ `SetUserToTeam` → `SendJoinNewRoom(user)`（65/0 给进房者）→ 对每个成员：`SendUserReadyStatus(user,u)`（65/3 给进房者）、非本人的 `SendNewUser(u,user)`（65/1）与 `SendTeamChange(u,user,team)`（65/13，`packetmanager.cpp:2782-2793`）。

实测 30003：进房者 `[('00',1066), ('03',6), ('03',6)]`，房主 `[('01',431), ('0d',6)]`。

**Go**：`rooms.go:605-618` 只广播 65/1、给进房者 65/0+65/3；没有 65/13。实测 30005：房主 `[('01',431)]`。

**复现**：`TestReview83RoomJoinTeamPacket`：

```
existing member received 65 subtypes [1]; reference sends PlayerJoin then SetUserTeam [1 13]
```

**影响**：65/1 本身带 team 字段，所以队伍显示大概率仍对；但对已有成员少一条"分队"通知，按基线逐包对照不一致。P2。

**建议**：在广播 65/1 之后，对除进房者之外的成员发 `U8(13)+U32(userID)+U8(team)`（与 `SendRoomSetUserTeam` 一致）。

### N5 [P2] 被拒的设置更新回滚包是空 mask（17B），基线重发完整设置（193B）

**基线**：`OnRoomUpdateSettings` 在 `CheckNewSettings` 失败时调 `currentRoom->SendUpdateRoomSettings(user, roomSettings, NULL, NULL, NULL, NULL)`（`channelmanager.cpp:1712-1715`）。`WriteSettings` 在四个 mask 全 0 时回退到 `newSettings->lowFlag`（`:2288-2301`），也就是 ALL → 把当前设置**完整**重发一遍，让房主对话框回到服务器值。

**Go**：`internal/server/room_clan.go:105-114` 直接写 `U8(4)+4×U32(0)`，没有任何字段。

**复现**：`TestReview83RejectedSettingsRevert`：

```
revert sends an empty change mask (17 bytes); reference re-sends the full settings (192 bytes for this room)
```

**影响**：房主提交一个被服务器拒绝的设置（家族战无公会、非法数值等）后，客户端收不到回滚内容，对话框保持在被拒的值；基线的行为是立刻还原。P2（错误路径，但用户可见）。

**建议**：改成 `w.U8(4)` 后调 `r.settings.write(w)`；这也依赖 N1 修好（否则写出的仍是子集）。

### N6 [P2] 153 大厅用户列表顺序不稳定（map 遍历），基线是入频道顺序

**基线**：`CChannel::m_Users` 是 vector，`UserJoin` 里 `push_back`（`channel/channel.cpp:57`），`SendLobbyJoin` 按 `GetOutsideUsers()` 的顺序写（`packetmanager.cpp:1752-1759`）。

**Go**：`lobby.go:121-139` 用 `for u := range s.clients` 同时算 count 和写条目，Go 的 map 每次 range 顺序随机。

**复现**：`TestReview83LobbyListOrder`（3 个用户、200 次调用）：

```
lobby list order is not stable across 200 calls: 3 distinct orders [231 312 123] (reference keeps join order 123)
```

**影响**：客户端大厅用户列表每次刷新顺序都在变（同一次登录里 count 与条目顺序也可能不同步——count 用一次 range，条目用另一次 range，不过 count 只是数量所以不影响解析）。P2。

**建议**：给 `Server` 维护一个入频道序号（client 上加 `joinSeq`），153 的 count 与条目都按序号排序输出。

### N7 [P2] 未收到 UDP 打洞前，房间包里的网络信息是 0.0.0.0:0，基线是 TCP 对端 IP + 27005/27015

**基线**：`CUser` 构造（`user/user.cpp:22-27`）：external/local IP 都取 socket IP，client/server 端口分别是 27005/27015；`UpdateHolepunch`（`:166-183`）之后才用真实值覆盖（portId 0 = server，1 = client）。

**Go**：`server.go:26-47` 的字段零值；只有 `serveUDP`（`:571-607`）收到 W 帧才填 `localIP/localPorts/externalPorts`；`rooms.go:403-422` 的 `networkInfo` 与 `host.go:142-150` 把零值写进 65/0/65/1/68 的成员块。

**复现**（同一账号 tester2 进房，房主收到的 65/1）：

```
30003: ...02 000000 ac110001 7d69 8769 ac110001 7d69 8769 ...   # 172.17.0.1:27005/27015 ×2
30005: ...02 000000 7f000001 0000 0000 00000000 0000 0000 ...   # 127.0.0.1:0/0, local 0.0.0.0:0
```

（IP 段 172.17.0.1 是容器视角下客户端地址，属环境差异；**端口与 local IP 的 0 是代码差异**。）

**影响**：真实客户端通常在建房/进房前已经打过洞，所以多数情况下看不到；但在"对方还没打洞"的窗口里，房间成员/主机连接信息是 0.0.0.0:0，客户端可能连不上或显示异常。P2。

**建议**：登录时（`loginPackets`/`characters` 建立后）把 `localIP/externalPorts/localPorts` 初始化为 `c.ip()`、27005/27015，之后由打洞覆盖。

### N8 [P2] 两个 Voxel URL 都为空时 103 整包不发，基线照发（两个空串）

**基线**：`usermanager.cpp:637` 无条件调 `SendVoxelURLs`（`packetmanager.cpp:7268-7279`），即使配置为空也写 `U8(20)+"\0"+"\0"`。

**Go**：`voxel.go:8-10` 在两个 URL 都为空时 return。

**复现**：`TestReview83LoginVoxelURLsSentWhenEmpty`：

```
no 103 with empty voxel URLs; reference still sends it: [150 157 74 99 99 99 159 159 152 154 77 77 77 67 72 72 72 67 67 153 151 151 180 69]
```

**影响**：当前 `.data` 的 Voxel 两个 URL 非空，所以线上看不到；空配置（纯基线数据）时客户端少一条 103。P2。

**建议**：去掉提前 return，空串也发。

---

## 五、已核实一致（同轮核对，供收敛清单）

| 项 | 基线 | Go | 证据 |
|---|---|---|---|
| 登录 150/157/153/74/76/180/103 载荷 | usermanager.cpp:575-638、packetmanager.cpp 各 Send* | lobby.go / ban.go | 30003 与 30005 的 `login_payloads.py` 输出 **diff 为空**（74/76/150/153/157/180/103 全部逐字节相同） |
| 157 段落与 flag | packethelper_fulluserinfo.cpp:43-294 | lobby.go:38-133 | 398B，low/high 均 `0xFFFFFFFF`，nameplate 8433 也在 |
| 153 结构/字符串/count | packetmanager.cpp:1746-1778 | lobby.go:147-193 | 412B 相同；两人 821B = 412+409，条目宽度一致 |
| 180 两个变体 | packetmanager.cpp:7022-7054 | lobby.go:764-776、host.go:42-44 | 47B 登录包逐字节相同；计量条源码 `{6,1,mode,0}` 一致 |
| 103 载荷 | packetmanager.cpp:7268-7279 | voxel.go:11-15 | 117B 逐字节相同（URL 非空时） |
| 74 Ban 的 kind 与形状 | packetmanager.cpp:6915-6944；definitions.h:1250-1261 | ban.go:9-38 | `BanList=0/BanSettingsReply=3/BanListMaxSizeReply=4/AddReply=1/RemoveReply=2` 全部对上；实测 30005 `74 2 0302` = 30003 |
| 76 Option 形状 | packetmanager.cpp:757-769 | lobby.go:432-441 | 944B 相同；`U8(0)+U16(len)+data+U8(1)` |
| 151 头部与 count 字段 | packetmanager.cpp:1978-1992 | rooms.go:288-307 | 空列表 15B 相同：`0004ffff00000100000002000100010000`；count 都是 U8 房间数 |
| 65/1 PlayerJoin 成员块 | packetmanager.cpp:2737-2768 | rooms.go:423-429、603-611 | 431B 相同（userInfo 修好后）；只有 N7 的网络字段不同 |
| 65/0 成员块 | packetmanager.cpp:2691-2735 | rooms.go:438-451 | 431B 相同 |
| 65/3 SetPlayerReady | packetmanager.cpp:2795-2806 | rooms.go:452-458 | 6B 相同 |
| 网络端口语义 | user.cpp:166-183（portId 0=server，1=client） | server.go:589-605、rooms.go:413-421 | Go 先写 `[1]`(client) 再写 `[0]`(server)，顺序正确 |
| 公告日期 /60 | serverconfig.cpp:1181-1182 | notice.go:79-80 | 加载时除 vs 发送时除，结果相同 |

---

## 六、实测输出（原文）

### 6.1 83b1c1a 纯净树：14 条探针全部按产品行为失败

```
=== RUN   TestReview83LoginOptionBeforeBan
login sends 74 before 76; reference sends 76 then 74
--- FAIL: TestReview83LoginOptionBeforeBan
=== RUN   TestReview83LoginVoxelURLsPosition
103 sent at 8, before 69 at 20; reference sends 103 last
--- FAIL: TestReview83LoginVoxelURLsPosition
=== RUN   TestReview83LoginVoxelURLsSentWhenEmpty
no 103 with empty voxel URLs; reference still sends it
--- FAIL: TestReview83LoginVoxelURLsSentWhenEmpty
=== RUN   TestReview83LoginSendsRoomListTwice
login sent 1 room lists, reference sends 2
--- FAIL: TestReview83LoginSendsRoomListTwice
=== RUN   TestReview83LoginLeagueAfterLobby
180 sent at 17, before 153 at 18
--- FAIL: TestReview83LoginLeagueAfterLobby
=== RUN   TestReview83LoginNoticeSet
login sent 2 UMsg packets, reference sends 3
--- FAIL: TestReview83LoginNoticeSet
=== RUN   TestReview83LobbyEntryLabel
153 entry label "loginuser", reference writes "test_lobby"
--- FAIL: TestReview83LobbyEntryLabel
=== RUN   TestReview83UserInfoFlags
157 flags low=0x0c4d03ea high=0x00000001, reference writes 0xffffffff/0xffffffff (300 bytes)
--- FAIL: TestReview83UserInfoFlags
=== RUN   TestReview83LobbyListOrder
lobby list order is not stable across 200 calls: 3 distinct orders [312 123 231]
--- FAIL: TestReview83LobbyListOrder
=== RUN   TestReview83RejectedSettingsRevert
revert sends an empty change mask (17 bytes); reference re-sends the full settings (192 bytes)
--- FAIL: TestReview83RejectedSettingsRevert
=== RUN   TestReview83RoomJoinTeamPacket
existing member received 65 subtypes [1]; reference sends PlayerJoin then SetUserTeam [1 13]
--- FAIL: TestReview83RoomJoinTeamPacket
=== RUN   TestReview83RoomCreatePacketSet
room create sent 65 subtypes [0 3]; reference sends SetHost, superRoom, c4Timer, CreateAndJoin [5 4 4 0] (65/0 len 406)
--- FAIL: TestReview83RoomCreatePacketSet
=== RUN   TestReview83RoomCreateSettingsFlags
65/0 settings flags [0x3077ffc9 0x4010014 0x0 0x0], reference writes [0xffffffff 0xfffbffff 0xffffffff 0xffffffff]
--- FAIL: TestReview83RoomCreateSettingsFlags
=== RUN   TestReview83RoomListEntryFields
151 entry flags low=0x000000f5 high=0x00000000 with 19 field bytes; reference writes 0xffffffff/0xffffffff and 85 bytes
--- FAIL: TestReview83RoomListEntryFields
```

### 6.2 当前工作树（750cf7e，登录链已修）：7 通过 / 7 失败

```
--- PASS: TestReview83LoginOptionBeforeBan
--- PASS: TestReview83LoginVoxelURLsPosition
--- FAIL: TestReview83LoginVoxelURLsSentWhenEmpty
--- PASS: TestReview83LoginSendsRoomListTwice
--- PASS: TestReview83LoginLeagueAfterLobby
--- PASS: TestReview83LoginNoticeSet
--- PASS: TestReview83LobbyEntryLabel
--- PASS: TestReview83UserInfoFlags
--- FAIL: TestReview83LobbyListOrder
--- FAIL: TestReview83RejectedSettingsRevert
--- FAIL: TestReview83RoomJoinTeamPacket
--- FAIL: TestReview83RoomCreatePacketSet
--- FAIL: TestReview83RoomCreateSettingsFlags
--- FAIL: TestReview83RoomListEntryFields
```

### 6.3 端到端：30005 与 30003 的登录载荷逐字节比较

```
python code-review\probes\review-83b1c1a-lobby\login_payloads.py --port 30003 > .dist\p-30003.txt
python code-review\probes\review-83b1c1a-lobby\login_payloads.py --port 30005 > .dist\p-30005.txt
Compare-Object (Get-Content .dist\p-30003.txt) (Get-Content .dist\p-30005.txt)
# （无输出 = 无差异）
```

30003 侧原文（30005 与之逐字节相同）：

```
150 len=   28 01000000746573746572310054657374657231000100000000000000
157 len=  398 01000000ffffffffffffffff00f1200000546573746572310054657374657231000000000a00000000d62400...
 76 len=  944 00ac035f42494e30303000ac03f5000100020900020d01021b020220036a2704622b05022c06722d07622e...
 74 len=    2 0302
153 len=  412 00010001000000746573745f6c6f62627900ffffffffffffffff00f120000054657374657231005465737465...
180 len=   47 000500010100050000000001010100050000000002010100050000000003010100050000000004010100...
103 len=  117 14687474703a2f2f643175396461386e796f6f7931382e636c6f756466726f6e742e6e65742f...
```

登录包序（trace.py；干净的一次 30005 是 60 包，153=412B；与 30003 只差 91 的 zip 大小与 72 的标题常量，后者是已批准偏离）：

```
43 152 / 44 154 / 45-47 77×3 / 48 67(公告) / 49-51 72×3 / 52 67(硬编码欢迎 168) /
53 67(配置欢迎 28) / 54 153 / 55 151 / 56 151 / 57 180 / 58 69 / 59 103
```

### 6.4 端到端：房间包（room_probe.py）

```
=== 30003（基线）
create response: [(65, '05', 6), (65, '04', 18), (65, '04', 18), (65, '00', 636)]
65/0 settings flags: low=0xffffffff lowmid=0xfffbffff highmid=0xffffffff high=0xffffffff
65/0 settings bytes 192 members at offset 205
151 len 117 entry flags low=0xffffffff high=0xffffffff fields 85

=== 30005（Go）
create response: [(65, '00', 504), (65, '03', 6)]
65/0 settings flags: low=0x3077ffc9 lowmid=0x04010014 highmid=0x00000000 high=0x00000000
65/0 settings bytes 60 members at offset 73
151 len 51 entry flags low=0x000000f5 high=0x00000000 fields 19

=== 进房（--join）
30003: guest 65 packets [('00',1066),('03',6),('03',6)]; host [('01',431),('0d',6)]
30005: guest 65 packets [('00',934),('03',6),('03',6)];  host [('01',431)]
```

---

## 七、没覆盖/不确定的部分（诚实标注）

1. **没有用真实客户端复验 N1/N2 的渲染差异**。判据是"基线源码 + 30003 基线实例的实际字节"；我能证明字节不同、字段缺失，但不能证明客户端会因此崩溃或具体显示成什么。N2 里"在游戏中的标记/等级限制不显示"是按字段语义推的。
2. N1 的 192B/96 字段目标值只对 **mode 0 + map 0 + ValidateSettings=false** 这一组实测；换成别的模式，字段值会变，但"四个 flag 字 = ALL、字段齐全"这个结论与模式无关。sd/zbLimit/mapPlaylist 等条件段我只按源码核对了写法，没有逐个模式实测。
3. N2 的条目字段我只对 **没有 mapPlaylist、没有 familyBattle、zbLimitFlag=0** 的普通房实测（85B）；带 playlist（每项 3B）、zbLimit（17B）、家族战（+8B）的房间条目会更长，未逐一实测。
4. N4/N7 只在两人房、Python 客户端上验证；真实客户端会在进房前打 UDP 洞，所以 N7 的实际可见窗口可能很小。
5. N3 的"多一条 65/3"我确认了基线建房回包里没有 65/3，但没有验证客户端对这条多余包是否有可见反应（大概率无）。
6. 153 顺序不稳定（N6）只在单人日志里用 3 个用户、200 次调用量化；真实客户端在大厅里的人数更多，现象更明显，但我没上真实客户端。
7. 我没有审 68（对局内 Host*）与 65 的其它子类型（6 结算、7 踢人、13 分队、19 邀请等）的字段；这些超出本轮"大厅/房间出站"的主线，且前几轮已覆盖一部分。
8. 工作树在本报告写作期间一直在前进（2a15e61 → cf172e5 → 750cf7e），我的最后一次全量探针跑在 750cf7e 上（`go build ./...` exit 0）；如果之后 `rooms.go`/`room_defaults.go` 被改动，以重新跑探针的结果为准。

## 八、建议的处理顺序

1. **N1 + N5**（同一根因：设置 flags 不置 ALL）——影响所有 65/0、65/4 与回滚包，改动集中 `room_defaults.go` 与 `room_clan.go`，收益最大。
2. **N2**（151/65-34 条目）——房间列表是玩家最常看的界面，且改动局限 `writeRoomListEntry`。
3. N3/N4（建房/进房的包集合）——补 65/5、2×65/4、65/13，去掉多余 65/3。
4. N7（登录时初始化网络信息）、N6（大厅顺序）、N8（空 URL 也发 103）——小改动。

## 九、一句话总结

本轮核了 7 条待办：**6 条在 83b1c1a 上是真偏差且已被 2a15e61 修到与基线逐字节一致**（74/76、103 位置、151 次数、180 位置、153 字符串、67 欢迎、157 flags 均已实测比对通过），第 7 条（103）的位置也已修、但空 URL 抑制还在；此外在大厅/房间出站里新确认 **2 项 P1**（65/0 设置只带子集 flags/字段、151 与 65-34 房间条目只写 6 个字段）与 **6 项 P2**（建房包集合、进房缺 65/13、回滚包为空、153 顺序、未打洞网络信息、103 空 URL），全部有可复现的探针或 30003/30005 对照。
