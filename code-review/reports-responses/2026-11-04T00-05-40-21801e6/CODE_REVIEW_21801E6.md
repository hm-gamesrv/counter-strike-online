# 21801e6 代码审查

日期：2026-11-04（Asia/Hong_Kong）。审查 HEAD：`21801e65b8ce677c5c2061c3624b831ef85949e7`（分支 `go-rewrite`）。上一轮响应：`../2026-11-03T22-24-51-70bd62e/CODE_REVIEW_70BD62E_RESPONSE.md`，对应的修复提交为 `27deb21`。

说明：审查过程中工作区被并行推进到 `1d30eba`（新增 `internal/server/fuzz_test.go` 与文档），`21801e6..1d30eba` 对 `internal/server`、`internal/store` 的业务文件**零改动**，本报告的结论针对 `21801e6` 的业务代码，探针在推进后的工作树上复跑结果相同。

结论：确认 **3 项 P1、5 项 P2**，均为可复现的基线偏离；其中第 1、5 项直接来自上一轮新增的扭蛋发奖代码（`27deb21`），第 2、4 项位于上一轮 N1 同一片家族战代码。没有修改业务代码；新增十条 overlay 探针（`code-review/probes/review-21801e6/`）。上一轮五条探针中四条稳定通过，扭蛋边界探针约有一半概率失败——原因是探针自身只统计库存行，见 N2 小节。

## 一、对上一轮修复回复的再审查

| 上轮编号 | 本轮结论 |
|---|---|
| R1：广播保留已清洗标志并按原长度补零 | 报告的场景已修（`TestReview70SanitizedVoxelMaskDoesNotCorruptFollowingField` 通过），但修法只覆盖「删除」方向，见下。 |
| R2：迁移的道具判定与 C4 方向 | 已修且与基线一致，两条原探针通过。 |
| N1：家族战双方被分到同一队 | 加入分队与重排已修，原探针通过；同一片代码上的对局中入房与拒绝回退仍缺，见新发现 2、4。 |
| N2：击杀事件缺扭蛋计数与发奖 | 计数/阈值/事务已修；边界探针本身不稳定（见下）；奖品内容与 166 分支仍错，见新发现 1、5。 |

### R1 已修，但只覆盖了「删除」方向

- 现有实现 `internal/server/rooms.go:185–209`（`settings.writeUpdate`）把掩码裁成「请求 flags ∩ 房间仍携带的字段」，值取 `merged`，与基线「清洗后的 newSettings flags + 房间当前值」在**被清位**的字段上一致。
- 残留：基线送出的 flags 是 `LoadNewSettings`/`CheckNewSettings` 处理**之后**的集合，它不只删位，还会**加位**：`room/roomsettings.cpp:2173–2187`（换地图时 `MAXPLAYERS`/`WEAPONRESTRICT`/`BALLNUMBER`/`ISZBCOMPETITIVE`）、`:2307–2318`（`MAPPLAYLISTINDEX`、`MAPPLAYLIST`、`MAPID2`）、`:2606`（关掉联赛/换边时 `lowFlag \|= ROOM_LOW_WINLIMIT` 并重置默认值）。Go 的掩码完全由请求派生，结构上不可能加位，于是这些「服务器补发」的字段既进不了房间状态也进不了广播——具体后果见新发现 3、6。
- 建议：把「基线会主动加位的字段」建模成显式的 added-bits 集合（换地图/换模式/关联赛），广播掩码取「请求位 ∪ 服务器补位」，值仍从 `merged` 编码。

### R2 已修

- `internal/server/rooms.go:750–791` 三项均用 `Store.HasActiveItem`（`status=1 AND inUse=1`）无条件重算并逐字段广播，与 `room/room.cpp:899–930` 一致；`refreshHostRoomItems`（建房/更新）也统一成活跃行判定。两条原探针通过，本轮未发现新的边界问题（sd 只在 3/4/5/15/24 广播、C4 无条件广播都与基线同形）。

### N1 已修，同区域仍有两处

- `familyBattleTeam`（`internal/server/room_clan.go:69–83`）对应 `manager/channelmanager.cpp:1329–1372`；`regroupFamilyBattle`（`:88–118`）对应 `:1767–1790`。`TestReview70FamilyBattleOpponentsJoinOppositeTeams` 通过。
- 响应里声明的有意偏离（重排按**新** `clan1` 而非基线的旧设置指针）已记录在 `docs/MIGRATION.md`，本轮不再计为新问题；但同一片代码仍缺：对局中非参战者可以加入（新发现 2）、启用被拒时不给房主回退（新发现 4）、`>2` 个军团与「无军团成员」的拒绝文案（新发现 4 一并覆盖）。

### N2 计数与事务已修，奖品内容仍错

- `Store.TickGachapon`（`internal/store/gachapon.go:12–51`）递减、到 0 重置 100、同事务发奖，随机与通知字节（`78/10` 对应 166、`78/12` 对应 5101）与 `room/gamematch.cpp:318–332`、`manager/packetmanager.cpp:1737–1744` 一致；命中 5101 分支时边界探针通过（166 分支见下条探针缺陷）。
- 但奖品是以「裸 `store.Reward`」直接落库的，绕过了仓库里统一的可数道具判定，导致 5101 写成已选中、不叠放（新发现 1）；166 只加了角色列，没有刷新会话缓存、没有 157（新发现 5）。
- 两处次要偏离（响应已声明其一，建议补记档）：`TickGachapon` 用 `readInventory` 返回**整个背包**，`host.go:335` 按 154 重发所有行；基线 `AddItem` 只回新增/加数的行。满包时 Go 保留计数留待下次重试（`gachapon.go:31–35`），基线先把计数重置为 100 再丢奖（`room/gamematch.cpp:321` + `itemmanager.cpp:674–675`）。
- **上一轮探针本身有缺陷**：`TestReview70KillPaysGachaponAtCounterBoundary` 用 `SELECT sum(count) FROM UserInventory WHERE itemID IN (166,5101)` 统计发奖，但 166 按基线是角色列、不落库存行；随机到 166 时该断言必然失败，与产品行为无关。实测连跑 6 次：2 次 ok、4 次报 `counter=100 awards=0`。建议改成同时读 `passwordBoxes`，并按随机分支分别断言（本报告的新发现 5 用循环命中 166 的写法覆盖了这一支）。

## 二、新的整体性审查

本轮把范围放在离开/踢人/房主迁移、家族战入房与设置、扭蛋、设置广播与地图派生、掉落箱、Bingo 洗牌；事务边界另做了静态扫描（`internal/store` 内没有在事务中再开事务或改用 `s.DB` 的函数，`SetMaxOpenConns(1)` 不会自锁）。下面只列新增确认项。

### 1 [P1] 扭蛋 5101 奖品写成「已选中、不可叠放」的库存行

- **Go 位置**：`internal/server/host.go:316–337`（只传 `{ID:award, Count:1}`），`internal/store/gachapon.go:27–30`，`internal/store/purchase.go:130–137`（未置 `Unselected` ⇒ 新行 `status=1,inUse=1`）、`internal/store/purchase.go:107–121`（`Stackable` 才合并同 ID 行）。
- **基线位置**：`manager/itemmanager.cpp:744–776`（可数带按钮分支：新行 `itemStatus=0,itemInUse=1`；已存在同 ID 行则 `count+=` 并只发该行），`:710–736`（存在同 ID 时的默认 status/inUse）。5101 在 `Data/Item.csv` 为 `Category=8, UseType=0`，正落在该分支；166 另有特例（`itemmanager.cpp:686–708`）。
- **复现**：仓库其它发奖路径都先过 `Server.prepareGrants`（`internal/server/rewards.go:79–101`，其中 `Unselected`/`Stackable` 就是这两个判定：`category==9 && useType∈{0,1,2}` 或 `category==8 && useType∈{0,2}`），只有新增的扭蛋路径绕过它。把 `killsToGetGachaponItem` 置 1 后调用 `Store.TickGachapon(1, 5101, …)`；再预放一件未选中的 5101 重复一次。
- **实测输出**：
  - 新行：`items=[{… 1001 …} {2 5101 1 1 1 …}]` ⇒ 5101 的 `Status=1, InUse=1`，基线应为 `0/1`。
  - 叠加：`got [{… 1001 …} {2 5101 1 0 1 …} {3 5101 1 1 1 …}]` ⇒ 两条 5101 行，基线应合并成 `Count=2` 的一行。
- **影响**：扭蛋 50% 的奖项（5101 = FabEnduMax1，可数、带使用按钮）落成「已选中」状态，客户端把它当已使用/已装备，使用按钮不出现；重复中奖会持续吃槽位而不是加数量。这是上一轮 N2 修复的直接回归。
- **建议**：`host.go` 在调用 `TickGachapon` 前先 `s.prepareGrants([]store.Reward{{ID: award, Count: 1}})`（或把目录判定下沉到 store），并让 store 只返回受影响的行（新行或加数槽位），按基线 `SendInventoryAdd` 的量发 154，而不是整包重发。
- **探针**：`TestReview218Gachapon5101IsUsable`、`TestReview218Gachapon5101StacksExistingCopy`

### 2 [P1] 家族战进行中的对局允许非参战者加入

- **Go 位置**：`internal/server/rooms.go:528–575`（65/23 只判房间存在、人数、密码、封禁、踢出、家族槽位，没有 `match != nil` 或参战者名单判断），`internal/server/room_clan.go:39–64`（没有 `m_FamilyBattleUsers` 等价物，全项目也没有参战者名单）。
- **基线位置**：`manager/channelmanager.cpp:1354–1358`（`room->GetStatus()==STATUS_INGAME && !room->IsUserInFamilyBattleUsers(user->GetID())` ⇒ 通知 `You are not participating in this Family Battle.` 并拒绝），`room/room.cpp:1234–1237`。
- **复现**：`familyBattleSetup` 取两名异族玩家，`familyBattleInit`/`familyBattleJoin` 后 `startGame`；第三名玩家（`inLobby=true`）发送 65/23 加入房间 7。
- **实测输出**：`new nonparticipant joined ongoing family battle: room=7 members=3`（基线应保持 `room=0 members=2`）。
- **影响**：家族战对局中可被任意玩家（含无军团者、第三军团者若 clan2 已空）挤进房间，队伍/比分/结算按 `r.members` 与 `m.stats` 展开，与基线的「只有参战者」不一致；也绕开了 `ROOM_JOIN_FAILED_*` 之外唯一的对局准入检查。
- **建议**：在 `startGame` 时记录参战名单（`r.match.stats` 的 userID 集合可以作为起点，但要在 `joinGame`/重连时维护），65/23 命中「`match != nil` 且不在名单」时按基线拒绝；`endGame`/`leaveRoom` 清理。
- **探针**：`TestReview218FamilyBattleRejectsNewMidMatchMember`

### 3 [P1] 仅换地图时不重新加载地图派生设置

- **Go 位置**：`internal/server/rooms.go:640–650`（只有 `st.fields[6]` 变化才 `roomDefaults`），`:688–696`（保存 `merged` 并只广播请求字段），`internal/server/room_defaults.go:231–233`（`mapId2` 只在建房/换模式时写）。
- **基线位置**：`room/roomsettings.cpp:2169–2192`（`lowFlag & ROOM_LOW_MAPID` 时**无条件**执行：非 254 地图 `maxPlayers=map_max_player`；`weaponRestrict=GetMapDefaultWeaponRestrict` 且 `highMidFlag |= WEAPONRESTRICT`；`ballNumber=ball_default`；`isZbCompetitive`），`:2313–2320`（`MAPPLAYLIST`/`MAPID2` 置位并 `mapId2=mapId`）。该段在 `validateSettings` 之外，出货配置 `ValidateSettings=false` 也照样执行。
- **复现**：房间初始为模式 0、地图 1、容量 8（`validRoomSettings`，并补 `mapId2=1` 以模拟建房），发送 65/5 只改地图（bit7 + `U16` 新图）。
- **实测输出**：
  - 地图 1 → 5：`map-derived settings not reloaded: mapId2=1 maxPlayers=8, reference stores 5/32`（`MapList.csv` 中 map 5 `max_player=32`）。
  - 地图 1 → 30：`map default restriction/capacity not applied: weaponRestrict=0 maxPlayers=8, reference stores 7/20`（map 30 `weapon_restrict_deault=7`；341 张图里 13 张有默认限制）。
- **影响**：换到有默认武器限制的地图后，房间与开局发给专用服的完整设置里没有该限制（`r.joined`、`room.joined` 都取 `r.settings`），玩家可以买/带被限制的武器；`mapId2` 留旧图，新加入者/重连者拿到的完整设置与当前地图不一致；容量也不按新地图收敛。
- **建议**：在 case 5 保存 `merged` 前，对「请求含地图位且值变化」执行与基线同构的 map 块，并把新增字段的位并入广播掩码（与 R1 的 added-bits 一起做）。注意这段不依赖 `ValidateSettings`，不能放在 `validateRoomSettings` 里（现在 `sanitizeRoomSettings` 只在地图 254 之外清 Voxel，且整体受开关门控）。
- **探针**：`TestReview218MapUpdateReloadsMapDerivedSettings`、`TestReview218MapUpdateAppliesWeaponRestriction`

### 4 [P2] 家族战启用被拒时不给房主回退设置包

- **Go 位置**：`internal/server/rooms.go:662–680`（候选房间建立失败时 `return nil`；通知发给被判定的成员 `room_clan.go:39–64`，房主什么也收不到）。
- **基线位置**：`manager/channelmanager.cpp:1718–1763`（逐条判 `ROOM_SETTING_FAILED_CLAN_NOT_MEMBER`/`_NO_CLAN_USER`/`_TOO_MANY_CLANS`/`_TOO_MANY_CLAN_MEMBERS`，失败时 `currentRoom->SendUpdateRoomSettings(user, roomSettings, NULL,NULL,NULL,NULL)`），`:1712–1715`（`CheckNewSettings` 失败同样回退）。
- **复现**：房主有军团、房内另有一名无军团成员，房主发送 65/5 把 70 置 1（家族战）。
- **实测输出**：`rejected family battle update sent no settings revert to the host`（房主队列为空；基线应收到一条 65/4、17 字节、首字节 4）。
- **影响**：房主界面停在「家族战已开启」，服务端仍是非家族战；下一次全量设置（新加入者/重连）才暴露差异，期间队伍与开局判定按另一套状态走。
- **建议**：失败分支按基线回退当前设置（全零掩码的 65/4）并发对应 `ROOM_SETTING_FAILED_*` 文案；补上基线的「>2 军团」「房间人数 > maxPlayers」两条拒绝。
- **探针**：`TestReview218FamilyBattleRejectionRevertsHostSettings`

### 5 [P2] 扭蛋 166 密码箱不刷新角色缓存、不发 157

- **Go 位置**：`internal/server/host.go:321–338`（只发 78 通知与 154 背包），`internal/store/purchase.go:94–106`（166 只 `UPDATE UserCharacter SET passwordBoxes=…`）。
- **基线位置**：`room/gamematch.cpp:314–332` → `manager/itemmanager.cpp:686–708`（166 不是库存行）→ `CUser::UpdatePasswordBoxes`（`user/user.cpp:320–334`）：更新会话角色缓存并 `UpdateClientUserInfo`（157）。
- **复现**：计数置 1，发送合法的击杀帧，循环到随机命中 166。
- **实测输出**：`password box award: database=1 cache=0 sent157=false`（基线：缓存 = 1 且发出 157）。
- **影响**：客户端密码箱计数不变，直到下一次 157/重登；服务端 `c.character.PasswordBoxes` 也是旧的，任何基于会话缓存的广播都会发出 0。
- **建议**：166 分支提交后重读角色（`reloadCharacter`/`Store.Character`）回填 `c.character` 并发 157（`userInfo` 已编码 `U16(passwordBoxes)`，`internal/server/lobby.go:76`）。
- **探针**：`TestReview218PasswordBoxKillRefreshesCharacter`

### 6 [P2] 被拒绝的设置值会删掉房间原值

- **Go 位置**：`internal/server/rooms.go:647`（`merged.merge(st)` 先落请求值）、`:681`（`validateRoomSettings(&merged, …)` 直接改 `merged`）、`:688`（`currentRoom.settings = merged`）、`internal/server/room_sanitize.go:227–229`（`buyTime` 非法时 `clearField(16)`，连房间旧值一起删）。
- **基线位置**：`room/roomsettings.cpp:1799–1806`（只 `lowFlag &= ~ROOM_LOW_BUYTIME`），`room/room.cpp:244–260`（`UpdateSettings` 按位 OR，只覆盖请求携带的字段），`manager/channelmanager.cpp:1793–1799`。
- **复现**：开启 `Room.ValidateSettings`，房间已有 `buyTime=20`，房主发送模式 0 下非法的 17。
- **实测输出**：`ignored buyTime update erased old value: present=false value=0`（基线：字段仍在，值 20）。
- **影响**：广播只回请求字段（R1 的修法正确），在场客户端继续显示 20，但服务端已丢字段；之后加入者/专用服拿到缺失 `buyTime` 的完整设置。`sanitizeRoomSettings` 里每个 `clearField`（36/18/20/34/38/46/49/54/55/56/60/61/64/67/68/71/72/75/76 等）都有同样的问题。
- **建议**：清洗只作用于「请求副本」，再按位合并进房间；或 `clearField` 时若该字段不在本次请求中则恢复房间旧值。用 buyTime 与另外两三个字段补测试。
- **探针**：`TestReview218RejectedUpdatePreservesOldBuyTime`

### 7 [P2] 场景掉落箱（game event 34）缺模式白名单

- **Go 位置**：`internal/server/host.go:408–410`（只判 `3000..3023`），`:760–764`（直接发奖）。
- **基线位置**：`room/gamematch.cpp:395–410`：`m_nGameMode` 不在 15/17/26/28 时按 `DROPBOXABUSE` 拒绝并 return（`#ifndef PUBLIC_RELEASE`），随后 `GiveReward` 并回 `EVT_SCENARIO_ITEM_POINT_REWARD` 系统回复。
- **复现**：模式 0 的房间、合法主机帧 `68/11`（event 34、reward 3000）。
- **实测输出**：`mode 0 accepted a scenario drop box: [{Slot:1 ID:1001 …} {Slot:2 ID:12 …}]`（基线：无奖励）。
- **影响**：任意模式都能吃场景掉落箱奖励；奖励目录（`ItemRewards.json`）里的 3000–3023 本是场景专用。
- **建议**：`grantDropBox` 先判模式白名单再发奖，并补系统回复包；该判定在 `#ifndef PUBLIC_RELEASE` 内，注释里写明参照构建是私有构建。
- **探针**：`TestReview218DropBoxRejectsNonScenarioModes`

### 8 [P2] Bingo 洗牌把号码与 opened 状态拆开

- **Go 位置**：`internal/server/bingo.go:254–261`（只重排 `slot.Number`，`Opened` 留在原槽位），限额判定在 `:239–253`。
- **基线位置**：`manager/minigamemanager.cpp:317–353`（该处理整段被注释，但写的是 `random_shuffle(slots.begin(), slots.end())`，整槽移动，号码与 opened 保持配对）。
- **复现**：初始化 Bingo 后把前 11 个槽位标 opened，反复洗牌并比对「opened 号码集合」。
- **实测输出**：`shuffle changed the opened-number set: number=19 opened=1 previouslyOpened=false`。
- **影响**：玩家已开的号码会变成未开、未开的变成已开；判行按槽位下标（`:173–187`），号码与显示错位后，客户端进度与服务端判定不一致，反复洗牌还能让同一批槽位交替「已开」。
- **建议**：洗牌时整体交换 `BingoSlot`（Number 与 Opened 一起），或在洗牌后按号码重建 opened；加一条「洗牌前后 opened 号码集合不变」的用例。
- **探针**：`TestReview218BingoShufflePreservesOpenedNumbers`

### 附带核对（未单独计级）

- 离开/踢人/房主迁移路径（`leaveRoom` 的调用方 `endSession`、`stashReconnect`、登录接管、专服回收、`kickRoomUser`、`kickClanMembers`）本轮逐一读过：成员移除、`65/2` 广播、房主迁移、`refreshMigratedHostItems`、匹配统计与家族槽位重算的顺序与基线一致；未发现新的可达缺口。唯一相关的既有偏离是「房间清空时直接删房间、不发任何结束包」，基线同样只走 `RemoveRoom`，不计。
- `internal/store` 事务边界做过一次静态扫描（是否有事务内再开事务/改用 `s.DB`），没有命中；`SetMaxOpenConns(1)` 下不会自锁。
- `internal/server/host.go` 其余 kind 与 `manager/hostmanager.cpp` 逐个对过：`0/1/2/3/5/7/8/9/10/11/13/14/16/17/20/21/24/100/101/102` 的字段宽度一致（`13` 是 `U32+4×U16`，与 `p.Bytes(8)` 等价；`9` 的 `ter/ct` 顺序按基线保留）。

## 验证与复现

- 已运行 `pwsh scripts/check-ref.ps1`，参照仓库确在 `4859a6e`。
- 所有 Go 命令使用 `GOTOOLCHAIN=local`。审查起始 HEAD `21801e6`：`go build ./...`、`go vet ./...`、`go test ./... -count=1 -timeout 300s` 全绿；工作区推进到 `1d30eba` 后复跑仍全绿（该提交只加 `internal/server/fuzz_test.go`）。
- 上一轮探针复跑：`go test -overlay code-review/probes/review-70bd62e/overlay.json ./internal/server ./internal/store -run '^TestReview70' -count=1`。`MigrationRejectsDormantHostItems`、`MigrationEnablesNewHostsC4`、`SanitizedVoxelMaskDoesNotCorruptFollowingField`、`FamilyBattleOpponentsJoinOppositeTeams` 四条稳定 PASS；`KillPaysGachaponAtCounterBoundary` 单测连跑 6 次为 2 PASS / 4 FAIL（`counter=100 awards=0`）：探针只统计 `UserInventory`，而 166 按基线进 `passwordBoxes`，随机到 166 时断言必然失败，属探针缺陷，已在 N2 小节说明。
- 本轮新增十条探针全部编译通过、gofmt 干净、`go vet -overlay` 无告警，并全部在预期断言处失败（即先复现再记录）：

```text
--- FAIL: TestReview218RejectedUpdatePreservesOldBuyTime
    ignored buyTime update erased old value: present=false value=0
--- FAIL: TestReview218MapUpdateReloadsMapDerivedSettings
    map-derived settings not reloaded: mapId2=1 maxPlayers=8, reference stores 5/32
--- FAIL: TestReview218MapUpdateAppliesWeaponRestriction
    map default restriction/capacity not applied: weaponRestrict=0 maxPlayers=8, reference stores 7/20
--- FAIL: TestReview218Gachapon5101IsUsable
    items=[{1 1001 1 0 1 …} {2 5101 1 1 1 …}] error=<nil>
--- FAIL: TestReview218Gachapon5101StacksExistingCopy
    5101 should stack into the existing row, got [{1 1001 1 0 1 …} {2 5101 1 0 1 …} {3 5101 1 1 1 …}]
--- FAIL: TestReview218PasswordBoxKillRefreshesCharacter
    password box award: database=1 cache=0 sent157=false
--- FAIL: TestReview218DropBoxRejectsNonScenarioModes
    mode 0 accepted a scenario drop box: [{Slot:1 ID:1001 …} {Slot:2 ID:12 …}]
--- FAIL: TestReview218FamilyBattleRejectionRevertsHostSettings
    rejected family battle update sent no settings revert to the host
--- FAIL: TestReview218FamilyBattleRejectsNewMidMatchMember
    new nonparticipant joined ongoing family battle: room=7 members=3
--- FAIL: TestReview218BingoShufflePreservesOpenedNumbers
    shuffle changed the opened-number set: number=19 opened=1 previouslyOpened=false
```

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay code-review/probes/review-21801e6/overlay.json ./internal/server -run '^TestReview218' -count=1 -v
go vet -overlay code-review/probes/review-21801e6/overlay.json ./internal/server
```

- 新增探针与绝对路径 overlay 位于 `code-review/probes/review-21801e6/`；全部使用 `newLoginExtraServer(t)`、`store.Open(t.TempDir())`、`familyBattleSetup(t)`、`bingoServer(t)`、`validRoomSettings()`、`pipeClient(t,…)` 等既有夹具，独立临时库，不触碰 `.data` / `.data-test`，也不操作 30002。
- 未修改任何业务代码，未改动别人的探针目录，未执行 git commit。
