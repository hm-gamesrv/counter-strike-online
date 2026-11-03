# 75cddae 代码审查

日期：2026-11-04（Asia/Hong_Kong）。审查 HEAD：`75cddaee55789a0cdb81cf16859fbcc2f5710796`（分支 `go-rewrite`）。基线：`JusicP/CSNZ_Server@4859a6e73451ccbfce167b1d28deeb0457468ac3`，动手前已跑 `pwsh scripts/check-ref.ps1`（输出「参照仓库已在基线 4859a6e」）。

上一轮响应：`../2026-11-04T00-05-40-21801e6/CODE_REVIEW_21801E6_RESPONSE.md`（对应修复提交 `306233c`、`e9f3973`）。

结论：上一轮八项**全部按声明修好**，未发现由这些修复引入的新回归；响应里「四条探针缺陷」的判断**成立**（我按四条探针的原意重写后全部转绿）。本轮新确认 **6 项 P2**，其中 4 项落在未充分覆盖的商店购买链路与公仓权限边界，2 项落在活动任务的事件语义上。没有修改任何业务代码；新增 8 条 overlay 探针（`code-review/probes/review-75cddae/`），6 条在预期断言处失败、2 条作为对上一轮「探针缺陷」的独立复核而通过。

## 一、对上一轮修复回复的再审查

| 上轮编号 | 声明 | 本轮结论 |
|---|---|---|
| 1（P1）扭蛋奖品绕过目录规则 | `Store.ItemRule` + 按 `AddItem` 规则落库、只回受影响行 | 已修。新探针 `TestReviewHeadGachapon5101Shape` 证明：新 5101 为 `status=0,inUse=1`，重复中奖合并成 `count=2` 的单行 |
| 5（P2）166 不刷缓存不发 157 | `reloadCharacter` + `broadcastUserInfo` | 已修，原探针通过 |
| 2（P1）家族战对局中允许非参战者 | 以 `match.stats` 为参战名单 | 已修，原探针通过；名单口径与基线不同，见下（不计级） |
| 4（P2）家族战拒绝不回退 | `familyBattleSettings` + `revertRoomSettings`（17B 空掩码） | 已修，原探针通过；回退包形状与基线逐字节一致 |
| 3（P1）换地图不加载派生设置 | `applyMapDefaults` + `writeUpdate` 的 `added` 掩码 | 已修。`TestReviewHeadMapDefaultsWithMapTable` 通过（含广播掩码断言） |
| 6（P2）被拒绝的设置值删掉房间原值 | `settings.restoreRejected` | 已修，原探针通过 |
| 7（P2）掉落箱缺模式白名单 | `grantDropBox` 先判 15/17/26/28 | 已修，原探针通过；基线随后发的系统回复仍缺，见下（不计级） |
| 8（P2）Bingo 洗牌拆散号码与 opened | 整槽交换 | 已修，原探针通过 |

`review-21801e6` 十条探针本轮复跑：6 PASS / 4 FAIL，与响应声明一致，失败的四条就是它声称的探针缺陷。

### 1. 扭蛋 5101（#1）已修

- `internal/store/gachapon.go:90–105` 的 `gachaponReward` 在 `ItemRule` 已安装时按 `CountableWithButton`/`Equippable` 取形状；`internal/server/server.go:171–175` 在 `loadItems` 之后安装钩子，顺序正确（先目录后任何击杀）。
- `gachapon.go:63–82` 现在只把 `grantItemsTracked` 返回的受影响行交给服务端，`host.go:342–346` 按行发 `154`，与基线 `itemmanager.cpp:771–773` 的「只发新增/加数的行」一致。
- 无钩子时的兜底是「按可数处理」，而 166 在 `grantItemsTracked` 里提前分流，两个奖品都落在可数类，所以兜底不会写错形状——响应这句站得住。
- 遗留（上轮已记录、非新问题）：满包时 Go 保留计数待下次重试、且不发 `78/10`；基线先重置为 100 再丢奖（`room/gamematch.cpp:318–330`）。已写在 `docs/MIGRATION.md:663`。

### 2. 扭蛋 166（#5）已修

`host.go:336–346`：密码箱分支先 `reloadCharacter` 再 `broadcastUserInfo`，对应基线 `user/user.cpp:320–334` 的「更新缓存 + `UpdateClientUserInfo`」。原探针通过（该探针 64 次尝试内命中 166，否则 `t.Skip`——不是产品问题，只是探针有跳过分支）。

### 3. 家族战对局中准入（#2）已修，但名单口径与基线不同

- `internal/server/rooms.go:579–592` 在 `to.match != nil` 时用 `familyBattleParticipant`（`room_clan.go:120–126`，读 `match.stats`）判定；基线的名单来源不同：`room/room.cpp:1082–1088` 在 `HostStartGame` 把**当时房间里的全部成员**快照进 `m_FamilyBattleUsers`，只在下一局开始时清空。
- 后果（不计级，属实现口径差异）：中途离开房间再 `65/23` 回来的参战者，Go 拒绝（`leaveRoom` 删了 `match.stats`）、基线放行；反之开局时在房内但没 ready 的成员，Go 拒绝、基线放行。
- 建议：若要保持「只有实际参战者」，把这条写进 `docs/MIGRATION.md` 作为有意偏离即可；要完全对齐则需在 `startGame` 快照成员名单并在 `endGame` 清空。

### 4. 家族战启用被拒的回退（#4）已修

`room_clan.go:107–114` 发的 `65/4` 是「`U8(4)` + 4×`U32(0)`」= 17 字节，与基线 `channelmanager.cpp:1718–1759` 的 `SendUpdateRoomSettings(user, roomSettings, NULL,NULL,NULL,NULL)` 形状、长度一致；三个拒绝文案与判定顺序（无军团 / >2 军团 / 人数超上限）也对得上。

### 5. 换地图派生设置（#3）已修

`room_defaults.go:134–162` 复刻 `room/roomsettings.cpp:2169–2192`（容量、地图默认武器限制、`ball_default`、`isZbCompetitive`）与 `:2313–2320`（`mapId2`、`MAPPLAYLIST` 位），并且不受 `Room.ValidateSettings` 门控（出货配置 `ServerConfig.json:589` 为 `false`，基线这段也在门控之外）。

本轮用**带地图表**的夹具复核（`TestReviewHeadMapDefaultsWithMapTable`）：

```text
map 5  -> mapId2=5  maxPlayers=32          （MapList.csv max_player=32）
map 30 -> mapId2=30 maxPlayers=20 restrict=7（MapList.csv max_player=20, weapon_restrict_deault=7）
广播掩码含 8/38/48/59（writeUpdate 的 added 掩码）
--- PASS
```

一个边角（P3，不作为独立条目）：基线这段在 `lowFlag & ROOM_LOW_MAPID` 时**总是**置位 `ROOM_LOWMID_MAPPLAYLIST`（`roomsettings.cpp:2316`），`WriteSettings` 会写 `U8(playlistSize)`（空表只写一个 0 字节，`manager/packetmanager.cpp:1873–1880`）。Go 的 `applyMapDefaults` 只在房间已经带字段 41 时补位（`room_defaults.go:156–158`）。若某个客户端的建房包不带空的 MAPPLAYLIST，换图广播就会比基线少一个字段；带则完全一致。值得抓一次真实建房包确认，或直接按基线无条件补一个 `U8(0)`。

### 6. 被拒绝字段恢复（#6）已修

`room_sanitize.go:18–32` 的 `restoreRejected` 把「请求带过、清洗后消失、房间原本有值」的字段恢复，符合基线「`CheckNewSettings` 只清更新位、房间值不动」的语义；`sd` 改成写 0 也与 `roomsettings.cpp:1644–1645`（`lowMidFlag |= ROOM_LOWMID_C4TIMER; c4Timer = 0`）一致。原探针（buyTime）通过。

### 7. 掉落箱白名单（#7）已修

`host.go:778–786` 先判 `dropBoxModes`（`host.go:770–775`）再发奖，与 `room/gamematch.cpp:395–410` 的 `#ifndef PUBLIC_RELEASE` 分支一致；原探针（模式 0 无奖励）通过。基线在发奖成功后还会发一条红色系统回复 `EVT_SCENARIO_ITEM_POINT_REWARD`（`gamematch.cpp:408–410`），Go 目前只走通用奖励通知——上轮已作为建议提出，仍未做，见「附带核对」。

### 8. Bingo 洗牌（#8）已修

`internal/server/bingo.go` 改为整槽交换，原探针（洗牌前后 opened 号码集合不变）通过。

### 9. 对「四条探针缺陷」判断的独立复核：**成立**

我把四条探针按它们**本来想断言的东西**重写（不改产品代码），在当前 HEAD 上全部转绿，说明失败确实来自断言写法而不是产品行为：

- `TestReview218Gachapon5101IsUsable`：断言 `len(items)==1`，但 `newLoginExtraServer` 建角时写入了默认道具 1001，实测输出里的两行就是 `{1001 …} {5101 status=0 inUse=1 …}`——5101 的字段本身已经正确。我的 `TestReviewHeadGachapon5101Shape` 按 itemID 过滤后 PASS。
- `TestReview218Gachapon5101StacksExistingCopy`：实测 `{1001} {5101 count=2 status=0 inUse=1}`，合并已经发生，失败只因 `len(items)!=1`。同一条探针里按 ID 过滤后 PASS。
- `TestReview218MapUpdateReloadsMapDerivedSettings` / `…AppliesWeaponRestriction`：`review218Room` 用 `newLoginExtraServer`，`s.maps` 为 nil，`applyMapDefaults` 走 `known=false`，所以只能改写 `mapId2`（实测 `mapId2=5` 已经更新）而容量/限制不动。补上地图表后 PASS（见上）。
- 补充一条响应里没说的细节：**不能**用现成的 `validateServer().maps` 修这两条探针——那张合成表里 map 5 的 `MaxPlayer` 是 16、map 30 没有 `weapon_restrict_deault`，而探针断言的是 `MapList.csv` 的 32 与 7，仍会失败。要么像响应说的加载真实 `MapList.csv`，要么把合成表的值改成与断言一致。

### 10. 回归检查

- 八项修复涉及的调用点（建房/改设置广播、家族战入房、扭蛋发奖、166 分支、Bingo）都重新读过：没有发现新引入的分支错误；`go build`/`go vet`/`go test ./...` 全绿（见末节）。
- 响应里提到的两处夹具调整（掉落箱用例改模式 15、`sd` 不再适用时字段存在且为 0）与基线一致，不是放宽断言。
- 上一轮报告的其余四条（R1 的加位方向、R2、N1、N2 计数）仍按上轮结论成立。

## 二、新的整体性审查

本轮范围：`quests.go`、`shop.go` 与购买链路、`clan.go`/`store/clan*.go`、`reconnect.go`、`dedicated.go`、`gm.go`，以及上一轮新代码的边界。逐段对照基线源码后，确认以下 6 项。

### 1 [P2] 重复购买同一件限时道具不延期，而是新开一行

- **Go 位置**：`internal/server/shop.go:251–269`（`prepareGrants` 后直接 `Store.Purchase`），`internal/store/purchase.go:137–171`（除 `Stackable` 外一律 `INSERT` 新行；`Exclusive` 只把同 ID 旧行的 `status` 置 0）。`shop_test.go:41–55` 把当前形状（两次购买 = 两行 212，第一行 `status=0`）写成了断言。
- **基线位置**：`manager/itemmanager.cpp:716–735`——存在同 ID 副本且本次带 `duration` 时，先找 `GetFirstExtendableItemByItemID`（`userdatabase_sqlite.cpp:1576–1592`：同 ID、`expiryDate!=0`），命中就 `ExtendItem(..., duration, true)` 并把该行 `SendInventoryAdd` 出去后 `return ITEM_ADD_SUCCESS`，**不插入新行**；`itemmanager.cpp:1726–1754` 的 `ExtendItem` 按天把 `expiryDate` 往后推。
- **复现**：给账号放一件 3 天的 212（`expiryDate=now+3*1440`），再购买同一件 3 天的商品。
- **实测输出**：`second purchase of a timed item opened 2 rows; the reference extends the existing copy (itemmanager.cpp:721-735)`（两行，各 3 天；基线应是一行 6 天）。
- **影响**：常用商品每次续购都多占一个背包槽位，客户端列表出现两条同款；选中态也随 `Exclusive` 规则从旧行搬到新行，而基线是把旧行延长、选中态不动。名义总时长相同，但持久化与客户端可见状态都不同。
- **建议**：在 `grantItemsTracked` 里补「存在同 ID 且 `expiryDate!=0` 的副本 + 本次 `Duration>0` → 把该行 `expiryDate += Duration*1440` 并只回这一行」的分支，放在 `Stackable` 判定之后、插入之前；同时把 `shop_test.go` 的两次购买断言改成单行 6 天。注意这一条与 `docs/MIGRATION.md:85` 的旧记录（「完整堆叠、延期…仍待迁移」）是同一处，若决定保持现状应把该记录升级为明确的有意偏离。
- **探针**：`TestReviewHeadTimedPurchaseExtendsExistingCopy`

### 2 [P2] 购买成功后的背包下发是整包重发

- **Go 位置**：`internal/server/shop.go:267`（`s.sendInventory(c)`）→ `internal/server/lobby.go:518–537`：先发 `152`（默认道具包），再按 `inventoryPackets` 发**全量** `154`。
- **基线位置**：`manager/itemmanager.cpp:771–773` 只 `SendInventoryAdd` 本次新增/加数/延长的行（`packetmanager.cpp:617` 起）；余额变化由 `user/user.cpp:207–217` 的 `UpdatePoints`/`UpdateCash` → `UpdateClientUserInfo`（`157`）推送。整条购买链路没有 `152`。
- **复现**：买一件在售商品（`Shop.json` 任一 `SubProducts`），收集回包 id。
- **实测输出**：`purchase resent the whole inventory (152 x1, 154 x1); the reference sends one incremental 154`
- **影响**：每次购买多发一个 `152` 与一整包 `154`；背包越大差距越大（3000 槽时 `154` 会切成多帧），与「154 是增量语义」的既定契约相反（`code-review/review-request.md` §8.5）。客户端不会坏（全量帧本身合法），但顺序/数量与基线不一致。
- **建议**：购买成功后改为 `sendInventoryItems(c, changed)`——`Store.Purchase` 已经能返回受影响行（`grantItemsTracked` 的 `touched`，目前被 `grantItems` 丢掉），把返回值透出来即可，无需再查库。
- **探针**：`TestReviewHeadShopPurchaseInventoryPackets`

### 3 [P2] 购买失败回包的第二字节写成 0

- **Go 位置**：`internal/server/shop.go:277–286`：`w.U8(1)` 之后按结果写 `1`/`0`。
- **基线位置**：`manager/packetmanager.cpp:2108–2140`（`SendShopBuyProductReply`）在 `WriteUInt8(ShopPacketType::RequestBuyProduct)` 之后**无条件** `WriteUInt8(1)`，成功与否只体现在后面的字符串里。
- **复现**：发一个不存在的子商品 ID（`[1, 200, 0, 0, 0]`）。
- **实测输出**：`failed purchase reply writes 0 as its second byte; the reference always writes 1`（Go 的帧：`01 00 "BUY_FAIL_NOITEM" 00 00`；基线：`01 01 "BUY_FAIL_NOITEM" 00 00`）。
- **影响**：字节级不一致。这个字节在基线里含义未明（源码里是常量 1），但客户端若把它当成功标志，会把「购买失败」显示成成功或反之；至少值得确认一次实机。
- **建议**：按基线改成常量 1，并把 `shop_test.go:59` 的 `success != 0` 断言改成 `1`。
- **探针**：`TestReviewHeadShopFailureReplyByte`

### 4 [P2] 公仓删除比基线严格：普通成员删不掉自己有权页里的道具

- **Go 位置**：`internal/store/clan_storage.go:228–244`（`DeleteClanItem` 的 `WHERE … memberGrade<=1`，即只有军团长/副军团长）。服务端 `internal/server/clan.go:823–839` 不再做任何等级判断，失败统一回 `CSO_CLAN_DB_SYSTEM_ERROR`。
- **基线位置**：`manager/userdatabase_sqlite.cpp:4684–4722`（`DeleteClanStorageItem`）只校验「`memberGrade <= ClanStoragePage.accessGrade`」，与存入同一口径；`manager/clanmanager.cpp:901–915` 也只把 `<=0` 当失败。
- **复现**：军团长建团、成员用邀请码加入（grade>1）→ 团长往 0 号页（默认 `accessGrade=3`）存入一件 270 → 该成员删除 0 号槽。
- **实测输出**：`member delete = -1, <nil>`；基线应返回 1 并清空该槽。
- **影响**：权限边界不一致。客户端 UI 大概只给团长显示删除按钮，所以正常操作路径不易察觉；但改包/插件用户会看到两边行为不同（Go 还会把「无权限」报成数据库错误）。
- **建议**：把 `memberGrade<=1` 换成与存入相同的页访问判定；若坚持只让管理层删除，应在 `MIGRATION.md` 记一条有意偏离，并把失败文案从 `CSO_CLAN_DB_SYSTEM_ERROR` 换成更贴近原因的一档。
- **探针**：`TestReviewHeadClanStorageMemberDelete`

### 5 [P2] 连续击杀（Continuous）的窗口比基线少一次击杀

- **Go 位置**：`internal/server/quests.go:280–289`：每来一次匹配的击杀 `questStreaks[key]++`，达到 `cond.Continuous` 就记分并清零。
- **基线位置**：`quest/questevent.h:241–251`：先判 `m_nContinuous <= continuousKillsCounter` 才计分，否则只 `continuousKillsCounter++`。于是 `Continuous=2` 时第 1、2 次击杀只把计数器推到 2，**第 3 次**才计第一分；之后每 3 次一分。
- **复现**：构造一个 `Goal=2`、唯一条件 `EventType=2, GoalPoints=1, Continuous=2` 的任务，连发 4 次合格击杀。
- **实测输出**：`continuous=2 paid out after 4 kills (units=2); the reference needs 3 kills for the first point and 3 more for the second`
- **影响**：连续击杀型任务比基线早一档完成（出货数据 `EventQuests.json` 里没有 `Continuous`，属配置驱动的行为）。另外基线的「连续」计数是**按条件各自维护**且只在击杀类条件上使用，Go 的 `stat.questStreaks` 也满足这一点，差异只在窗口边界。
- **建议**：把判定改成「`streak >= Continuous` 时先计分再清零」之前多等一次，即复刻基线的 `counter++/then pay` 顺序；或明确记录「按语义而非按基线 bug 实现」。
- **探针**：`TestReviewHeadContinuousKillWindow`

### 6 [P2] 里程碑通知换成「每跨过一个 Notice.Goal」而不是「恰好是其整数倍」

- **Go 位置**：`internal/server/quests.go:310`：`progress.Units/task.Notice.Goal > old.Units/task.Notice.Goal` 时发聊天通知，用的是**落库后**的进度。
- **基线位置**：`quest/questevent.h:137–156`（`IncrementTempCount`）：用本局临时进度，`if (tempProgress.unitsDone % m_pTask->GetNoticeGoal() == 0)` 才发，且发的是临时值。
- **复现**：`Goal=100, Notice.Goal=10, GoalPoints=15`，连发 2 次击杀（累计 30 点）。
- **实测输出**：`two 15-point kills produced 2 chat notices; the reference notifies only on exact multiples of Notice.Goal (30, 60, ...)`（Go 在 15 和 30 各发一条，基线只在 30 发一条）。
- **影响**：出货的 `EventQuests.json` 三个条件分别是 1/5/15 点、`Notice.Goal=10`，所以 15 点那条会**每次击杀都刷一条**（floor 从 1 涨到 2、3……）；基线整条任务可能一条都不发。属于可观测的聊天刷屏，且通知里的进度数字也不同。
- **建议**：判定改成 `progress.Units % task.Notice.Goal == 0 && old.Units % task.Notice.Goal != 0`（或直接按基线的临时进度模型比较），文本里的数字用同一来源。
- **探针**：`TestReviewHeadMilestoneNoticeCondition`

### 附带核对（未单独计级）

- **任务条件的 `Bot` 默认值**：基线 `questevent.h:235` 的 `if (m_nBot >= 0 && m_nBot ? victimUserID != 0 : victimUserID == 0)` 在 `Bot` 缺省（-1）时等价于「只算真人」，Go（`quests.go:227`）在 `Bot==-1` 时不过滤。出货数据三个条件都显式写了 `Bot`，所以当前不可达；但换一份 `EventQuests.json` 就会出现「机器人击杀也算」。
- **公仓时长的往返**：`store/clan_storage.go:110–113` 把绝对分钟原样存、取出时原样写回；基线 `AddClanStorageItem` 把 `expiryDate*1440` 写进 `itemDuration`，而 `GetClanStorageItem`（`userdatabase_sqlite.cpp:4724–4750`）只读 `itemID,itemCount` 并把 `expiryDate` 硬置 1 —— 也就是基线存进去的限时道具**取出时一律变成 1 天**。Go 保留了真实时长，属于「比基线正确」的偏离，建议在 `MIGRATION.md` 记一条，别在后续对照里被当成回归。
- **家族战参战名单口径**：见 §一.3。
- **掉落箱的系统回复**：基线发奖后按 `RewardNotice.status` 发 `SendUMsgSystemReply(…"EVT_SCENARIO_ITEM_POINT_REWARD", {points})`（`gamematch.cpp:408–410`），Go 只走通用奖励通知；上轮建议项，仍未做（P3）。
- **商店购买请求的形状**：`shop.go:238–241` 要求 5 字节（`U8(1)+U32(subID)`），基线 `manager/shopmanager.cpp:269–275` 读的是 `U8(type)+U8(productType)+U8(subID)` 三个字节。`shop_test.go` 的函数名 `TestCapturedCurrentClientShopPurchase` 说明这是按实机抓包做的**客户端例外**，但 `review-request.md` §8 与 `MIGRATION.md` 都没有记录。建议补进 §8（同时说明「按基线 3 字节发来的客户端会被 `invalid shop purchase length` 断开」），否则下一位审查者还会把它当新 bug。
- **GM /`removeitem` 的槽位语义**：`gm.go:173–184` 把参数当「同 ID 副本的下标」，但列表里打印的是真实槽位号（`%d) slot %d`），基线打印的是下标（`[%d], count: %d`，`channelmanager.cpp:727–741`）。文案不一致但行为等价，P3。

## 验证与复现

- `pwsh scripts/check-ref.ps1`：参照仓库在基线 `4859a6e`。
- 所有 Go 命令均设 `GOTOOLCHAIN=local`。
- 标准检查（HEAD `75cddae`，未注入探针）：`go build ./...`、`go vet ./...`、`go test ./... -count=1 -timeout 300s` 全绿（`internal/server` 49.6s、`internal/store` 27.3s、`internal/protocol` 4.3s、`internal/config` 5.5s）。
- 说明：审查期间另一路并行审查改动了 `internal/store/effect.go`、`internal/store/title.go` 并新增了 `code-review/probes/review-75cddae-store/`。这两处改动不涉及本轮任何结论所依赖的代码（购买、公仓、任务、商店），我在新工作树上重新跑过 `go build`/`go vet`/`go test ./...`（全绿）与本轮探针（结果与上表逐条相同）；`review-75cddae-store/` 不是本报告的产物。
- 探针 gofmt 干净、`go vet -overlay` 无告警，且都在预期断言处先失败再记录：

```text
--- FAIL: TestReviewHeadContinuousKillWindow
    continuous=2 paid out after 4 kills (units=2); the reference needs 3 kills for the first point and 3 more for the second
--- FAIL: TestReviewHeadMilestoneNoticeCondition
    two 15-point kills produced 2 chat notices; the reference notifies only on exact multiples of Notice.Goal (30, 60, ...)
--- FAIL: TestReviewHeadTimedPurchaseExtendsExistingCopy
    second purchase of a timed item opened 2 rows; the reference extends the existing copy (itemmanager.cpp:721-735)
--- FAIL: TestReviewHeadShopPurchaseInventoryPackets
    purchase resent the whole inventory (152 x1, 154 x1); the reference sends one incremental 154
--- FAIL: TestReviewHeadShopFailureReplyByte
    failed purchase reply writes 0 as its second byte; the reference always writes 1
--- FAIL: TestReviewHeadClanStorageMemberDelete
    member delete = -1, <nil>; the reference only checks the page access grade
--- PASS: TestReviewHeadMapDefaultsWithMapTable
--- PASS: TestReviewHeadGachapon5101Shape
```

后两条是复核项：前者证明上一轮 #3 的修复在有地图表的夹具下成立（含 `writeUpdate` 的 added 掩码），后者证明上一轮「扭蛋探针缺陷」的判断成立。

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay code-review/probes/review-75cddae/overlay.json ./internal/server ./internal/store -run '^TestReviewHead' -count=1 -v
go vet -overlay code-review/probes/review-75cddae/overlay.json ./internal/server ./internal/store
```

- 新增探针与绝对路径 overlay 位于 `code-review/probes/review-75cddae/`（`server_test.go`、`store_test.go`、`overlay.json`），复用 `newLoginExtraServer`、`testStore`、`validRoomSettings` 等既有夹具，全部跑在独立临时库上，未触碰 `.data` / `.data-test`，未操作 30002/30003/30004。
- 本报告未修改任何业务代码（`internal/`、`cmd/` 不在本轮的改动里），未改动他人探针目录，未执行 `git commit`。