# 83b1c1a 代码审查：对局生命周期、持久化与并发

- 日期：2026-11-05（Asia/Hong_Kong）。分支 `go-rewrite`，HEAD `83b1c1afddfe130dfbc0cc572c84b224209b386d`。
- 基线：`JusicP/CSNZ_Server@4859a6e73451ccbfce167b1d28deeb0457468ac3`。动手前运行 `pwsh scripts/check-ref.ps1`，输出「参照仓库已在基线 4859a6e」。
- 本轮只新增 `code-review/probes/review-83b1c1a-lifecycle/` 与本文件；未修改业务代码、既有测试、`.data`/`.data-test`，未重启或操作 30002 实例。

## 一、范围与方法

审查范围是存储/会话侧的生命周期主题：对局结算（`internal/server/host.go`、`internal/store/match.go`）、家族（clan）存储（`internal/store/clan_storage.go`、`internal/server/clan.go`）、家族战（`internal/server/room_clan.go`、`rooms.go`、`host.go`）、限时道具边界（`internal/store/effect.go`、`purchase.go`、`use_item.go`、`item_extend.go`）、连接与会话生命周期（`internal/server/server.go`、`lobby.go`、`reconnect.go`、`internal/store/session.go`）、事务覆盖。

方法：

1. 先在基线源码里定位对应行为，逐行确认基线实际语义（包括它自己的怪癖），再对照 Go 实现。
2. 每个结论都写 overlay 探针，先在当前代码上跑出失败，再检查失败点是否由产品行为而非夹具造成；夹具尽量用出货数据（`assets/defaults` 与 `.data` 一致）或与现有测试相同的建库路径。
3. 静态检查：用上一轮交付的 AST 扫描器重跑事务不变量；用真实监听器 + 16 条连接验证连接级 goroutine 生命周期。
4. 全程 `$env:GOTOOLCHAIN='local'`。

跑过的命令（真实输出见第六节）：

```powershell
pwsh scripts/check-ref.ps1
$env:GOTOOLCHAIN = 'local'
go test ./...
go test -overlay code-review/probes/review-83b1c1a-lifecycle/overlay.json ./internal/server ./internal/store -run '^(TestReview83|TestStoreProbe)' -count=1 -v
go vet -overlay code-review/probes/review-83b1c1a-lifecycle/overlay.json ./internal/server ./internal/store
cd code-review/probes; go run ./review-75cddae-store/scan_tx.go ../../internal/store
```

## 二、结论速览

| 编号 | 位置 | 结论 | 严重度 |
|---|---|---|---|
| L1 | `internal/server/room_clan.go:120-126`、`rooms.go:592-600`、`rooms.go:854-856` | 家族战参赛名单用 `match.stats` 代替基线的开赛快照；中途离队再进同一局会被拒 | P2 |
| L2 | `internal/server/rooms.go:503-540`、`room_clan.go:206-224`、`host.go:55` | 家族战开房时的 clan 槽初始化与开赛门槛都与基线不同（两个方向：该拒的开、该开的拒） | P2 |
| L3 | `internal/server/room_clan.go:150-180` | 开启家族战时的分队用「新 clan1」，基线用更新前的 clan 槽；同族玩家分到哪边不一致 | P2 |
| L4 | `internal/store/match.go:31`、`internal/server/host.go:588-631` | 结算把服务端自己按主机计数算出的奖励按 10,000,000 校验；一个极端计数就让整场结算永久失败，且房主连接被断开 | P2 |
| L5 | `internal/server/rewards.go:87-90`、`internal/store/purchase.go:205-216` | 出货数据里的 `Duration: -1`（永久）被落成休眠道具（status=0/inUse=0），基线是永久且首份选中（status=1/inUse=1） | P2 |
| L6 | `internal/store/session.go:3-16`、`internal/server/lobby.go:247-264` | 重复登录接管时旧会话不写 `UserSessionHistory`；基线在销毁旧 CUser 时写 | P2 |

全部为当前代码真实可复现的行为偏差；探针中 6 条预期失败、2 条对照通过（见第五节）。

## 三、详细结论

### L1 [P2] 家族战参赛名单用 `match.stats`，离队者被永久移出名单

**现象。** 家族战对局进行中，某玩家主动退房（或掉线超过重连窗口后被清掉）后再尝试加入同一房间，Go 回 `"You are not participating in this Family Battle."` 并拒绝；基线会接受，因为 `m_FamilyBattleUsers` 是该局开始时拍下的快照，之后不再删除任何条目。

**复现。** `TestReview83FamilyBattleRejoinAfterLeave`（失败）：

```
review_83b1c1a_test.go:159: rejoin refused (notices ["You are not participating in this Family Battle."]);
the reference still has user 2 in m_FamilyBattleUsers and accepts the join
```

探针用真实军团建库（与 `room_clan_test.go` 同一夹具），构造一局 `mode=0`、两名参赛者的家族战，让 2 号玩家 `leaveRoom` 后重新发 65/23 进房请求。当前代码在 `rooms.go:594` 就拒绝，`b.room` 保持 0。

**根因。**

- 基线：`room.cpp:1082-1088` 在 `HostStartGame` 时 `m_FamilyBattleUsers.clear()` 后把**当时房间里的所有用户** ID 抄进去；整局只读取（`room.cpp:1234-1236`），下一次开赛才重建。`OnUserRemoved`（`room.cpp:932-937`）只调用 `CGameMatch::Disconnect` 删统计，不动该名单。进房检查只在 `STATUS_INGAME` 时查这份名单（`channelmanager.cpp:1354-1358`）。
- Go：`room_clan.go:120-126` 的 `familyBattleParticipant` 直接查 `r.match.stats`，而 `rooms.go:854-856` 的 `leaveRoom` 会删掉该玩家的 stat。`rooms.go:594` 的注释声称“statistics 就是 m_FamilyBattleUsers”，但 `leaveRoom` 的删除使这一等价关系在“离队后重进”这条路径上不成立。

**影响。** 家族战进行中，任何中途退房的玩家（包括客户端崩溃后超过 5 分钟重连窗口的玩家）都无法回到自己仍在进行的那一局；房主也无法用「重新邀请」把他放回来（该路径同样走 `rooms.go:594`）。基线允许这种回填。

**建议。** 给 `room` 增加开赛时快照的参赛 ID 集合（例如 `startGame` 时从 `r.members` 生成，`endGame` 清空），`familyBattleParticipant` 查快照而不是 `match.stats`；`leaveRoom`/重连只改统计，不改快照。另注意 Go 现在连“开赛时在房间但还没被 host 请求过背包（没有 stat）”的玩家也一并排除，这与基线快照口径也不一致。

**探针。** `code-review/probes/review-83b1c1a-lifecycle/server_test.go` 的 `TestReview83FamilyBattleRejoinAfterLeave`。

### L2 [P2] 家族战开房的 clan 槽初始化与开赛门槛与基线不一致

**现象（两个方向）。**

1. **该开的被拒。** 房主与同族好友建一个家族战房间（只有一族），好友准备后房主点开始：Go 回 `ROOM_START_FAILED_CLAN_NOT_READY`、不起局；基线会起局（它只要求 `familyBattleClanID2 != 0` 且至少一名非房主玩家准备，而房间创建时 clan1=0，第一个进房的人无论哪族都会填进 clan2）。
2. **该拒的开。** 房间里有对立两族但没有任何非房主玩家准备：Go 直接起局；基线用 `GetNumOfReadyRealPlayers()==0` 拒绝，并回同一条 `ROOM_START_FAILED_CLAN_NOT_READY`。

**复现。** `TestReview83FamilyBattleStartGate` 两个子用例均失败：

```
--- FAIL: TestReview83FamilyBattleStartGate/same-clan-only-room
    family battle start refused for a same-clan room (notices ["ROOM_START_FAILED_CLAN_NOT_READY"]);
    the reference starts it because clan2 is filled by any first joiner
--- FAIL: TestReview83FamilyBattleStartGate/no-ready-player
    match started with zero ready players; the reference refuses with ROOM_START_FAILED_CLAN_NOT_READY (channelmanager.cpp:1618)
```

**根因。**

- 基线创建房间时 `CRoomSettings::Reset` 把 `familyBattleClanID1/2` 置 0（`roomsettings.cpp:495-497`），客户端上行的设置帧只带 `familyBattle` 一个字节（`roomsettings.cpp:362-364`），建房路径没人补 clan1；只有设置更新（`channelmanager.cpp:1761-1763`）和房主迁移（`room.cpp:993-1001`）才会写。进房时 `if (character.clanID != familyBattleClanID1)` 对 clan1=0 恒真，于是第一个进房者填 clan2（`channelmanager.cpp:1340-1352`）。开赛只检查 `clan2 != 0 && GetNumOfReadyRealPlayers() > 0`（`channelmanager.cpp:1616-1622`）。
- Go 在建房时就把房主的 clan 写进 `currentRoom.clan1`（`rooms.go:503-515,537-540`）；`familyBattleJoin` 遇到同族不填 clan2（`room_clan.go:54-56`）；`familyBattleCanStart` 要求“当前成员里两族都有人”（`room_clan.go:206-224`），`startGame` 用它替代了基线的准备人数检查（`host.go:55-58`），完全没有 `GetNumOfReadyRealPlayers` 对应物（`mode 32` 的 maxPlayers-1 检查也缺席，`channelmanager.cpp:1624-1628`）。

**影响。** 家族战房间的可开赛条件与基线不同：同族娱乐局在 Go 上永远开不了；而没有对手准备的房间在 Go 上会起一局只剩房主/少数人实际参战的“家族战”。这是房主与所有房间成员都能看到的开始/拒绝差异。

**建议。** 二选一并与基线对齐：要么建房时保持 clan1=0、按基线在进房时填 clan2，并在 `startGame` 加上“至少一名非房主 ready”与 mode 32 的人数门槛；要么明确把这条当作已批准偏离写进文档。无论哪种，`familyBattleParticipant`/开赛检查应使用与基线同源的名单（见 L1），不要再临时遍历当前成员。

**探针。** `TestReview83FamilyBattleStartGate`（两个子用例）。

### L3 [P2] 开启家族战时的分队用新 clan1，基线用更新前的 clan 槽

**现象。** 房间未开家族战时把开关打开：同族好友被留在房主所在队（Go），基线会把**除房主外的每一名有族玩家**都换到房主队的对面（因为比较的是更新前的 clan1，此时为 0）。客户端收到的 65/13 队伍变更因此不同。

**复现。** `TestReview83FamilyBattleRegroupSeparatesClanmates`（失败）：

```
review_83b1c1a_test.go:111: host clanmate kept team 2 (host team 2);
the reference compares against the old clan slot 0 and moves every other member to the opposite team
```

**根因。**

- 基线 `OnRoomUpdateSettings` 里先把 `newSettings.familyBattleClanID1/2` 填好（`channelmanager.cpp:1761-1763`），紧接着的分队循环却拿 **`roomSettings`（旧设置）** 的 `familyBattleClanID1` 做比较（`channelmanager.cpp:1765-1777`）；`currentRoom->UpdateSettings(newSettings)` 在循环之后（`:1793`）。首次开启时旧值恒为 0，所以所有非房主玩家都被放到对面。
- Go `rooms.go:698-708` 先调 `familyBattleSettings`，它已经把 **新** 的 clan 写进 `r.clan1/clan2`（`room_clan.go:72-103`），随后 `rooms.go:724` 调 `regroupFamilyBattle`，后者按 `r.clan1`（新值）判断同族（`room_clan.go:158-179`）。

**影响。** 家族战开关打开后的第一局，同族玩家在 Go 上与房主同队、在基线上与房主敌对；这是可被双方客户端直接观察到的分队差异，也会影响该局平衡与结果包里的队伍字段。后续再次改设置时旧值与新值相同，两者才重新一致。

**建议。** 在 `regroupFamilyBattle` 里显式接收“更新前的 clan1”，由 `roomRequest` case 5 在赋值 `currentRoom.settings` 之前把旧值传进去（或把 regroup 移到赋值之前、用旧 `currentRoom.clan1`）。不要为了“看起来更合理”保留现状而不登记偏离。

**探针。** `TestReview83FamilyBattleRegroupSeparatesClanmates`。

### L4 [P2] 结算把主机提供的极端计数按 10,000,000 硬校验，整场结算永久失败

**现象。** 玩家击杀计数（主机 68/7 消息，上限被 Go 放到 1,000,000）足够大时，`endGame` 算出的 `exp+bonusExp` / `points+bonusPoints` 超过 10,000,000，`SettleMatch` 直接返回 `invalid match reward`；该局所有玩家都拿不到经验/金币，也没有结果包。`endGame` 的错误会从 `hostRequest` 冒到 `serveClient`，于是**房主连接被关闭**，而 `r.match` 仍非 nil、`playing` 仍为 true，重试还是同一组统计、同样失败。

**复现。** `TestReview83SettlementRejectsExtremeHostCounter`（失败）：

```
review_83b1c1a_test.go:227: settlement aborted: invalid match reward;
the reference clamps the same counters (exp 30069814) and pays points
```

夹具：mode 20（系数 320/290）、激活的职业道具 55（20% 加成）、`kills=1000000`。Go 算出 `exp=9999+64,000,000`、`points=9999+58,000,000`；期望基线值 `exp=30069814`、`points=58009999`。

**根因。**

- 基线 `CalculateGameResult` 用未封顶的乘法算加成，最后只把 base 截到 9999（`gamematch.cpp:537-545,604-612`）；`ApplyGameResult` 把 `totalExp/totalPoints` 交给 `UpdateExp/UpdatePoints`（`:615-630`），前者把角色经验封在 30069814（`user.cpp:295-317`），后者直接相加（`user.cpp:207-220`）。没有“奖励值上限”这一层。
- Go 在 `store/match.go:31` 校验 `Exp/Points <= 10000000`，而这个值是 `host.go:604-627` 用主机上报的 `kills` 现算的。`kills` 本身已被 `host.go:370` 限制到 1,000,000，但 1,000,000×320 再乘职业加成足以超过 10M。

**影响。** 正常对局（击杀数在几百以内）到不了这个值；但主机上报的计数不受客户端信任边界保护（P2P 主机就是玩家），一个恶意/异常主机可以把自己或所有人的 `kills` 抬到 1,000,000，使该局结算永远失败：所有参赛者无收益，房主每次点结束都会被踢下线，房间卡在 INGAME（只有手动退房能解开）。这是“结算失败但不骗客户端成功”的安全一侧，但失败本身是 Go 新增的、基线没有的。

**建议。** 不要在结算入口拒绝服务端自己产生的值：改为像基线一样封顶（经验按 30069814 截断、points 用 int64 范围校验），或把 10M 校验缩到“单次原始计数”而不是“加成后的总额”。同时 `endGame` 失败时不要让 `hostRequest` 直接断连，至少回一条错误通知并把房间恢复到可结束状态。

**探针。** `TestReview83SettlementRejectsExtremeHostCounter`。

### L5 [P2] 出货数据的 `Duration: -1`（永久）被落成休眠道具

**现象。** 购买/领取 `Duration: -1` 的永久道具后，数据库里是 `status=0,inUse=0,expiryDate=0`（休眠）；基线是 `status=1,inUse=1,expiryDate=0`（永久且首份选中）。客户端因此看到“已拥有但未装备”，该武器/职业也不会进入 `hostInventory` 的可用列表，直到玩家手动装备。

**复现。** `TestReview83MinusOneDurationIsNotDormant`（失败）：

```
review_83b1c1a_test.go:317: permanent -1 shop item stored status=0 inUse=0 expiry=0;
the reference stores status=1 inUse=1 expiry=0
```

探针用 `assets.Init` 解出的出货数据（与 `.data` 逐字节相同）：`Shop.json` 里商品 255 的 `Duration` 为 -1，走真实 `shopRequest`（`reply=BUY_OK`），再读 `UserInventory` 断言状态。

**根因。**

- 基线 `CItemManager::AddItem` 在入口把负数 duration 归 0（`itemmanager.cpp:683-684`），首份道具以 `itemStatus=1,itemInUse=1` 入库（`:669-672`、`:917-927`）；只有已存在同 ID 副本时才降为 0/0（`:716-719`）。`CShopManager::BuyProduct` 原样把 `item.duration` 传给 `AddItem`（`shopmanager.cpp:328-330`）。
- Go `prepareGrants` 把 -1 改写成 `Duration=0, Dormant=true`（`rewards.go:87-90`），`purchase.go:205-208` 对 `Dormant` 一律 `status=0,inUse=0`；`Exclusive` 的“替换旧选中”也因为 `status==0` 被跳过（`purchase.go:209-213`）。

**影响范围。** 出货数据里共 11 个 `Shop.json` 商品和 22 条 `ItemRewards.json` 条目使用 -1，全部是武器/职业/PeriodicItem（无装饰），例如商品 255/279/280/281/327/363/389/417/425/426/427。买了永久武器后它不进游戏内可用列表、也不会像基线那样自动换下旧选中，属于稳定可见的状态偏差；玩家手动装备后功能正常，所以不是 P1。商店列表本身两边都写 0xFFFF（基线 `WriteInt16(-1)`、Go `U16(-1)`），客户端看到的商品信息一致，问题只在发放后的行状态。

**建议。** 把 -1 的语义固定在“永久”而不是“休眠”：`prepareGrants` 只做 `Duration=0`，不要置 `Dormant`（装饰类仍由 ClassName/category 12 决定休眠）；若要保留“永久但未选中”，需要先确认它与基线首份选中的差异是否被批准。

**探针。** `TestReview83MinusOneDurationIsNotDormant`。

### L6 [P2] 重复登录接管时旧会话的历史记录丢失

**现象。** 同一账号第二次登录时，`lobby.go` 先 `endSession` 再 `StartSession`；`StartSession` 直接 `DELETE FROM UserSession` 后插入新行（`session.go:3-16`），旧会话没有写入 `UserSessionHistory`。基线在 `Login` 发现已有会话时销毁旧 CUser，其析构函数调用 `DropSession`（`user.cpp:32-34`），先写历史再删行（`userdatabase_sqlite.cpp:452-465`），然后才插入新会话。

**复现。** `TestStoreProbeTakeoverDropsSessionHistory`（失败）：

```
review_83b1c1a_test.go:96: takeover left 0 history rows for the superseded session;
the reference writes one (DropSession on the old CUser)
```

探针完全按 `lobby.go:247-264` 的顺序调用两次 `StartSession`（1000 → 1010 分钟）；当前代码历史行 0 条。

**根因。** Go 的接管路径：`lobby.go:247-251` 的 `endSession` 只关连接、退房、删内存会话，不调 `Store.EndSession`（因为旧连接的 `serveClient` defer 会检查 `s.users[id]==c`，此时已不是它，所以也不会补写）。随后 `StartSession` 的 DELETE 把唯一的会话行覆盖掉。基线是同步删除 CUser（`tcpserver.cpp:442-465` → `usermanager.cpp:1132-1140`），历史在 INSERT 之前落库。

**影响。** 只影响 `UserSessionHistory`（会话审计/历史表）；对局数据、协议都不受影响，所以定 P2。但如果沿用基线的管理工具（例如按 IP/HWID 关联账号的 `GetUsersAssociatedWithIP/HWID`，`userdatabase_sqlite.cpp:4024-4078`），被接管会话的 IP/HWID 关联会少一段。另外 Go 的 `EndSession` 用 `sessionDate=结束时间` 而基线用 `lastLogonTime`（登录时间），同一张表的口径也不同，建议一并明确。

**建议。** 在 `endSession`（或 `StartSession` 内）先按旧行的 `sessionTime` 写一条历史再删除；`StartSession` 可以改成“先 EndSession 再插入”的组合。若要保留 Go 的“接管不产生历史”口径，请在文档中登记，并确认不再有工具依赖该表。

**探针。** `TestStoreProbeTakeoverDropsSessionHistory`。

## 四、已核对、未发现新问题的项

- **家族仓库并发取件**：`TakeClanItem`/`GiveClanItem` 都包在 `s.DB.Begin()` 里，且 `Store.Open` 设 `SetMaxOpenConns(1)`（`store.go:80`）。实测两协程抢同一槽位：一条成功、一条 -1，库存只多出一行（`TestStoreProbeClanStorageConcurrentTake` 通过）。公仓页权限（5 页×20 槽、master 才能改 accessGrade、`memberGrade<=accessGrade` 才能取/删）与基线一致；时长往返的“Go 比基线保真”是上一轮已登记偏离，不再重复。
- **事务覆盖**：用上一轮的 AST 扫描器重跑 `internal/store`，159 个函数、55 个 `Begin` 所有者，T1/T2/T4/T5/T6/T7 全清，T3 仍只有 `Store.init` 在 Begin 前用 `s.DB`（PRAGMA/user_version/空库判定，顺序正确）。`SetTitle` 已改为事务；本轮没有再发现“读整列-改-写回”的单连接读改写。
- **连接/会话 goroutine**：真实监听器 + 16 条空闲连接，关闭后 `s.clients` 归零、进程 goroutine 数回到基线（`TestReview83ConnectionGoroutinesExit` 通过，连跑 3 次稳定）。每条连接是 reader + writer 两个 goroutine（`server.go:303,335`），`serveClient` defer 关闭队列并等 writer 退出（`:324-328`），未发现泄漏。
- **结算原子性/幂等**：`SettleMatch` 单事务、`(matchID,userID)` 主键、失败回滚（既有 `TestSettlementAtomicAndIdempotent`、`TestCurrencyOverflowRollsBack` 覆盖），结算在发送结果包之前提交，因此不存在“客户端收到成功但未入账”。结算、退房、房主迁移、房间销毁全部在 `s.mu` 下串行，房间清空时丢弃未结算对局与基线 `~CRoom` 删除 `CGameMatch` 的行为一致。
- **DDL 生命周期**：`SettleMatch` 每次结算仍执行 `CREATE TABLE IF NOT EXISTS GoMatchSettlement`（`match.go:27`）。表已存在时无副作用，不构成正确性问题；上一轮已把它列为“启动期建表”的待办，本轮不重复计为缺陷。
- **限时道具边界**：`relativeExpiryMax=36500` 与绝对分钟时间戳在当前纪元不歧义；`UseInGameItem` 的“续期/激活”分支、`purchase.go` 的 `GetFirstExtendableItemByItemID` 等价查询与 999 天永久化阈值逐条对照基线一致（`itemmanager.cpp:1726-1756`），未发现新的偏移。
- **重复登录踢旧连接本身**：`endSession` 会先 stash 重连槽位再退房、关连接，旧 `serveClient` defer 通过 `s.users[id]==c` 判断不会误删新会话；数据库层 `StartSession` 覆盖行不会留下双会话。L6 只是历史表缺失。

## 五、复现方式与实测输出

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay code-review/probes/review-83b1c1a-lifecycle/overlay.json ./internal/server ./internal/store `
    -run '^(TestReview83|TestStoreProbe)' -count=1 -v
```

当前代码的实际结果（预期：6 条失败 + 2 条通过）：

```
--- FAIL: TestReview83FamilyBattleStartGate
    --- FAIL: TestReview83FamilyBattleStartGate/same-clan-only-room
    --- FAIL: TestReview83FamilyBattleStartGate/no-ready-player
--- FAIL: TestReview83FamilyBattleRegroupSeparatesClanmates
--- FAIL: TestReview83FamilyBattleRejoinAfterLeave
--- FAIL: TestReview83SettlementRejectsExtremeHostCounter
--- FAIL: TestReview83MinusOneDurationIsNotDormant
--- PASS: TestReview83ConnectionGoroutinesExit
--- PASS: TestStoreProbeClanStorageConcurrentTake
--- FAIL: TestStoreProbeTakeoverDropsSessionHistory
FAIL	github.com/hm-gamesrv/cso-server-go/internal/server
FAIL	github.com/hm-gamesrv/cso-server-go/internal/store
```

（耗时逐次浮动，省略；失败行后的断言文本见第三节各条。）

基线检查、格式化与全量回归：

```
pwsh scripts/check-ref.ps1                              -> 参照仓库已在基线 4859a6e
gofmt -l code-review/probes/review-83b1c1a-lifecycle    -> （空）
go vet -overlay ... ./internal/server ./internal/store  -> exit 0
go test ./...                                           -> ok internal/server, ok internal/store（全绿）
```

## 六、不确定与未覆盖

- 本轮没有启动客户端或专用服务器，家族战 L1-L3 的结论来自基线与 Go 的源码逐行对照加进程内探针，不是实机抓包。L2 的“同族局基线可开”依赖基线 `familyBattleClanID1` 在建房后确实为 0；我核对了 `Reset`（roomsettings.cpp:495-497）、上行设置只带一个字节（:362-364）和建房路径（channelmanager.cpp:1232-1241）三处，但没有在实机里跑 C++ 基线复验。
- L3 的结论依赖“基线比较的是更新前设置”这一读法；`UpdateSettings` 在循环之后调用（channelmanager.cpp:1793）是明摆着的，但如果上游还有别处提前更新了 `roomSettings` 的 clan 槽，结论会翻转。我按 `GetSettings()` 返回当前指针、循环内未调用 `UpdateSettings` 判断。
- L4 的 10M 上限是否本来就对应某个客户端/配置口径（例如“单场收益上限”）没有文档依据；我只按基线行为判断它不该让整场结算失败。
- L6 的实际影响取决于是否还有工具读 `UserSessionHistory`；Go 自身没有读取点，定 P2 而不是更高。
- 未覆盖：`bingo`/`gachapon` 的奖励分布、`UserSession` 字段与基线 GUI 的兼容性、房间设置里未建模 flag 的既有结论、跨进程/双实例共享同一 SQLite 的场景。`.data`/`.data-test` 与 30002 实例全程未动。