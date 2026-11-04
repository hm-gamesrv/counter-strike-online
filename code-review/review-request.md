# CSO 主服务端 Go 重写：审查请求

> 写给审查者（人或其他模型）。
> 仓库：`hm-gamesrv/counter-strike-online`，分支 `go-rewrite`，当前 HEAD **`4cf6859`**（2026-11-03）。
> 注意：`code-review/` 已纳入版本库，审查报告与逐条响应都在里面，按轮次归档在 `code-review/reports-responses/<时间戳>-<短哈希>/`。
> 作者自述：这套代码是按 C++ 参考实现逐函数翻译 + 实机抓包校对写出来的，
> 因此**最可能的错误形态是“照抄时表抄错了一位”和“异常路径没照抄”**，请优先从这里下手。

---

## 1. 目标：100% 复刻基线行为

本项目的目标不是“做一个能玩的 CSO 服务端”，而是**把 CSNZ_Server 的 master server 用 Go 等价重写**。

判据（按优先级）：

1. **协议字节级一致**：同一客户端操作，服务器发出的包 id、子类型、字段顺序、字段宽度、字节数应与 C++ 基线一致（允许的差异见 §7、§8）。
2. **状态机一致**：连接/登录/大厅/房间/对局/结算的状态迁移与 C++ 相同，包括异常路径（断线、重连、踢人、房主迁移、房间满、非法包）。
3. **持久化一致**：同样的操作对 SQLite 的写入（列、值、幂等语义）一致。
4. **数值一致**：经验、点数、等级、价格、概率、上限、取整方式一致。
5. **可观测行为一致**：什么情况下服务器回 notice、回错误码、静默丢弃、断开连接，应与 C++ 相同。

**基线固定为**：`JusicP/CSNZ_Server@4859a6e73451ccbfce167b1d28deeb0457468ac3`（2025-03-20，PR #45 合并点；其分支头 `e5721ed` 树完全相同，`git diff` 为空）。

- 该提交相对 `12644f2` 只改了 `bin/Data/*` 与 `bin/*.json`，**没有源码变更**，所以协议形状与 `12644f2` 相同。
- 仓库里 `.ref/CSNZ_Server` 是对照克隆（已 gitignore）。**读源码前先跑 `pwsh scripts/check-ref.ps1`**，它会断言 HEAD 就在基线；`.ref` 若停在更新的构建（如 `3aac2d2`），元数据帧、ItemBox、房间设置等 payload 形状与基线不同，照它实现会发出客户端不接受的包（已经因此踩过崩溃，见 §8）。
- `.ref` 只用于对照，不在本仓库的版本控制内；审查时请以 `4859a6e` 的源码为准，不要用 `origin/main`。

---

## 2. 审查前准备

```powershell
git clone https://github.com/hm-gamesrv/counter-strike-online.git
cd counter-strike-online
git checkout go-rewrite

# 对照源码（放到 .ref，已被 gitignore）
mkdir .ref
git clone https://github.com/JusicP/CSNZ_Server .ref/CSNZ_Server
git -C .ref/CSNZ_Server checkout 4859a6e73451ccbfce167b1d28deeb0457468ac3
pwsh scripts/check-ref.ps1        # 必须输出“参照仓库已在基线”

# 构建与测试
$env:GOTOOLCHAIN = "local"        # 本机 Go 1.27.2；auto 会联网解析工具链
go build ./...
go vet ./...
go test ./...                     # 199 个 Test/Fuzz/Benchmark，约 25s
```

规模：`cmd/` + `internal/` 约 25.6k 行 Go，其中测试 10.0k 行。

跑服务端：

```powershell
go build -o .dist\cso-server.exe ./cmd/cso-server
& ./.dist/cso-server.exe -data ./.data -debug
```

- `.data/` 是运行数据目录（gitignore），结构对应基线的 `bin/`：`ServerConfig.json`、`Data/*.csv|json|bin`、`UserDatabase.db3`。
- 现在本机有**两套**数据：`.data`（基线数据 + 客户端自带的 `MapList.csv`/`GameModeList.csv`，因为该客户端会读服务器下发的这两张表，缺了菜单会空）与 `.data-upstream`（纯基线数据，322 图/51 模式，跑在 30003 端口）。审查协议/逻辑时以代码为准，不要被数据目录的现状误导。
- 客户端联调用 `S:\.temp\CSNZ\Bin\CSOLauncher.exe -ip <ip> -port 30002 -login <user> -password <pass> -window -novid -debug -dumpmetadata`；`-dumpmetadata` 会把收到的 91 包落到 `Bin\MetadataDump\`。

### 2.0 基线实例（30003）——最有力的对照物

本机 WSL 里跑着一个**原版 C++ 基线**，可以从 Windows 直接连：

- 仓库 `~/repos/hm-gamesrv/counter-strike-online`（分支 `main`），容器名 `cso-baseline`，镜像内是 `/app/csnz-server`。
- 启动脚本 `scripts/start-baseline.sh`：先 `rsync -a --delete .data/ .data-test/`，再把 `.data-test` 挂成容器的 `/data`，端口映射到 `30003`。
- 已确认 **`.data` 与容器 `/data` 的 45 个文件逐个 sha256 相同**（含 `ServerConfig.json`），所以两边差异都是代码差异，不是数据差异。
- 这条对照路径此前被低估：`blackbox/` 就是围绕它建的。

```powershell
# 两边各登录一次，把包序列 dump 出来对比
python blackbox\trace.py --port 30002 --out .dist\trace-30002.txt
python blackbox\trace.py --port 30003 --out .dist\trace-30003.txt
python blackbox\metadata.py --a 30002 --b 30003     # 91 帧解压后比内容
```

- 客户端联调用 `S:\.temp\CSNZ\Bin\CSOLauncher.exe -ip <ip> -port 30002 -login <user> -password <pass> -window -novid -debug -dumpmetadata`；`-dumpmetadata` 会把收到的 91 包落到 `Bin\MetadataDump\`。

### 2.1 审查探针的工作流（前八轮一直这么用，建议继续）

产出物的位置固定：

- 审查结果写成**一个文件** `code-review/CODE_REVIEW_<提交短哈希>.md`（不再拆 OVERALL / COVERAGE / EXPANDED 多份）。
- 我对同一轮写 `code-review/CODE_REVIEW_<哈希>_RESPONSE.md`。
- 探针放进 `code-review/probes/review-<哈希>/`，`overlay.json` 里用绝对路径（下文）。

你（审查者）把复现放进一个 overlay 目录，我原样跑：

```
code-review/probes/review-<短哈希>/server_test.go      # package server 的探针测试
code-review/probes/review-<短哈希>/store_test.go       # package store 的探针测试
code-review/probes/review-<短哈希>/overlay.json        # {"Replace": {"<绝对路径>/internal/server/review_x_test.go": "<绝对路径>/code-review/probes/review-<哈希>/server_test.go"}}
```

我来运行：

```powershell
go test -overlay code-review/probes/review-<短哈希>/overlay.json ./internal/server ./internal/store -run '^(TestReview|TestAudit|TestOverall)' -count=1
```

要点：

- overlay 现在可以直接**增补**仓库里不存在的测试路径（`a0bb9a6` 那轮就用了 `internal/server/review_a0_test.go` 这个不存在的目标），不必先放占位文件；只要 `Replace` 的键是绝对路径即可。
- 探针里请只调用**已存在**的导出/包内符号。上一轮 `TestReview3e*ExpiryPromotesRemainingWeapon` 直接调 `ExpireItems(now)`，所以我保留了它原来的两返回值签名；如果某个签名确实该改，请在报告里写清楚，我改探针而不是让探针编译不过。
- 探针请自带 `go vet`/`gofmt` 干净的代码；历史上出现过未 gofmt 导致 overlay 编译失败的往返。

---

## 3. 审查范围

- 全部：`cmd/` + `internal/`。
- 最近一段 `915782c..4cf6859`：**43 个提交，178 文件 +10959/−1108**，是最需要看的部分（其中 `code-review/` 归档占了不少文件数，业务改动集中在 `internal/`）。

```
b908064 docs: move GAPS.md into docs and record the black-box round
8fa33d5 fix(server): keep the metadata JSON order instead of sorting it
7691112 test(blackbox): add a Python black-box suite for the wire behavior
5b6c6f4 fix(server,store): apply the a0bb9a6 review findings
a0bb9a6 fix(store,server): close the inventory removal cascade gaps
ccfedd0 fix(server,store): apply the 9d33c2d review findings
9d33c2d fix(server,store): apply the 3e6fa14 review findings
3e6fa14 fix(server,store): apply the 33e9ec6 review findings
33e9ec6 fix(store,server): apply the 154b2fe review findings
154b2fe fix(server,store): apply the b6afe1f and f6eda20 review findings
f6eda20 chore: ignore the second instance data directory
b6afe1f fix(store,server): apply the 44b5eed review findings
44b5eed refactor(server): stop rewriting room settings flags on write
5bc14c8 fix(store,server): apply the b4cc191 review findings (R1-R7)
b4cc191 revert(server): leaving mid-match forfeits the reward, as in the reference
25c45b2 fix(server): settle the match when its host leaves the room      ← 已被 b4cc191 回退
5070786 fix(server): keep reward bonus percentages stable and log the breakdown
1a9e424 fix(server): pad the scenario difficulty to the nine bytes clients parse
790cc2f fix(server): read and write the scenario difficulty as one byte
54200de fix(server): stop forwarding host save data and tolerate short room settings
06ac9dd chore(server): reject Studio rooms with the upstream failure text
bd0d5dd feat(server): add /tournament, /addgroom, /reqsnapshot and Hash metadata
41a33fd fix(server): equip costumes from the character tab use request
```

- 前九轮的每条处理与判断都写在 `.tmp/review-response-*.md`（工作区未跟踪），`MIGRATION.md` 里有同一批改动的中文小结。**如果你要复核我的修复，请以这两个文件为索引**，别从 diff 里猜意图。
- 看单点改动 `git show <commit>`；看全量 `git diff 915782c..HEAD`。

---

## 4. 怎么审（四种手段）

### 4.1 逐函数对照（主力手段）

映射表（Go ↔ C++ 基线）：

| Go | C++ 基线 |
|---|---|
| `internal/protocol/packet.go` | `src/net/*.cpp`、`src/common/buffer.cpp`、`src/obfuscate.h` |
| `internal/server/server.go` | `src/net/tcpserver.cpp`、`udpserver.cpp`、`manager/usermanager.cpp`（分发） |
| `internal/server/lobby.go` | `channel/channel.cpp`、`manager/channelmanager.cpp`、`manager/packetmanager.cpp`（大厅/角色/角色列表） |
| `internal/server/rooms.go` | `room/room.cpp`、`room/roomsettings.cpp`、`channelmanager.cpp` 的 `On*Room*` |
| `internal/server/room_defaults.go` / `room_validate.go` / `room_sanitize.go` | `roomsettings.cpp` 的 `LoadDefaultSettings` / `CheckSettings` / `LoadNewSettings` / `Is*Valid` |
| `internal/server/host.go` | `room/gamematch.cpp`、`manager/hostmanager.cpp`、`packetmanager.cpp` 的 `SendHost*` |
| `internal/server/inventory.go` | `manager/itemmanager.cpp`（`OnItemPacket` 及各 kind） |
| `internal/server/metadata.go` | `manager/packetmanager.cpp` 的 `SendMetadata*` + `packet/packet_metadata_data.h` |
| `internal/server/clan.go` | `manager/clanmanager.cpp` |
| `internal/server/quests.go` | `manager/questmanager.cpp`、`quest/quest.cpp` |
| `internal/server/shop.go` / `bingo.go` / `minigame.go` / `rewards.go` / `reward_claims.go` | `manager/shopmanager.cpp`、`minigamemanager.cpp`、`luckyitemmanager.cpp`、`itemrewardmanager.cpp` |
| `internal/server/gm.go` | `src/servercommands.h` |
| `internal/store/*.go` | `manager/userdatabase_sqlite.cpp`、`user/user.cpp`、`user/userinventoryitem.cpp` |
| `internal/store/references.go` / `expiry.go` | 基线没有对应文件：`RemoveItem` 散落在 `userinventoryitem.cpp` 与各 manager 的删除分支里（见 §5 P0-4） |
| `internal/config/config.go` | `src/serverconfig.cpp` |

对照时特别看三件事：**字段顺序**、**字段宽度**（`U8/U16/U32/U64`）、**分支条件**。这三样只要错一处，包长就对不上，客户端要么错位解析要么崩。

### 4.2 抓包对照

服务端 `-debug` 会打印 `packet received id/size`、`send id/len`、`metadata type/len`、`room settings flags/layout`、`match reward`。可以直接对着 C++ 的 `Send*` 函数核对长度：

```
msg="packet received" id=65 size=49 user=1
msg=send id=65 len=421
msg="room settings" kind=0 len=49 flags="[1993 8448 0 0]" layout="0:22 3:1 6:1 7:2 8:1 9:1 10:2 40:1 45:1"
```

`layout` 按位列出「字段号:字节数」，用来和客户端真实发包逐字段比对（见 §8 的难度字段）。

### 4.3 测试

- 191 个 `Test/Fuzz/Benchmark`。单元测试覆盖包编解码、房间设置、背包、装饰、结算、任务、公会、快捷购买、到期与通知；集成测试用 `net.Pipe` 起真实 `serveClient`（`internal/server/server_test.go` 的 `TestLoginRoomUDPAndShutdown`），覆盖登录→大厅→建房→开局→结算→房主迁移→关服整条链。
- fuzz：`FuzzFrame`、`FuzzRoomSettings`、`FuzzItemRequest`（`internal/server/fuzz_test.go`，对道具分发器喂任意载荷，跑在真实 Store 上）、`FuzzMetadataFrames`（校验每个 91 帧的长度字段自洽、不超 `MaxPayload`、分块拼起来等于原档案）。日常 `go test` 会跑种子；要真正变异需要 `go test -run '^$' -fuzz '^FuzzItemRequest$' -fuzztime 25s`。
- 测试里有“钉行为”的用例，改逻辑时会红，例如 `TestHostLeaveForfeitsReward`（中途退不给奖励）、`TestRoomSettingsScenarioCreatePayload`（大灾变建房包逐字节回放）、`TestExpiryPushClearsDeletedSlot`（到期必须发墓碑）。

### 4.4 黑盒对比（`blackbox/`，Python）

只通过 TCP 与服务器对话，不 import Go 包、不读数据库，因此同一套脚本可以打 30002（我们的 Go）和 30003（基线）。

```powershell
python blackbox\test_smoke.py                       # 5 个冒烟用例
$env:CSO_PORT="30003"; python blackbox\test_smoke.py
python blackbox\metadata.py --a 30002 --b 30003     # 32 个 91 类型，解压后比内容
```

这一层能抓住白盒测试抓不到的东西——**包序、包长、登录时序**。已知它能测出"元数据条目顺序"这种逐字段测试覆盖不到的偏差（见 §5 P0-6）。

### 4.5 实机（只作佐证，不作判据）

已在本机真实客户端（不是原生 CSO，见 §7）验证过：登录、大厅、房间列表、建房/进房/换模式/准备、0/2/3/4 模式对局、结算、房主中途退出（迁移）、装饰装备/替换/卸下/持久化并进对局可见、38 个 metadata 包、背包、GM 命令、30003 纯基线数据实例。

**未验证**：跨 NAT 双机联机、SSL（`-usessl`）、HLDS 专用服、开箱、荣誉/里程/活动商店、Bingo、强化/合成、公会、排行、问卷、`Item(32)` metadata 开关、大灾变“正常打完”的结算路径、运行期自然到期（只在测试里验证过）。

---

## 5. 我希望重点审查的地方（按风险排序）

### P0-1 `internal/server/inventory.go:412 itemRequest` —— 道具请求路由

全仓库最容易出错的地方：78 种 kind，每种读字段都不同。

- kind 路由（0/4/7/9/14/15/16/17/21/23/24/26…）是否漏了基线的分支？基线 `CItemManager::OnItemPacket` 的 switch 每一项对应的读字段我是否都对上了？
- **`slot` 有两套语义**：客户端发“游戏槽位”，DB 存“扣除默认道具后的槽位”。`gameSlot := slot` 之后 `slot -= len(DefaultUser.DefaultItems)`，请检查所有用到 slot 的地方是否都用了正确的那个。
- 兜底拒绝条件 `!ok || (!weapon && Category != 7) || kind == 0 && inventoryType == 1 && !weapon` 与基线四段 if/else 链是否等价。
- kind 0 的分支 `inventoryType == 8 || ClassName == "Tattoo"` → `toggleCostume` 是实机调试出来的，请确认它没有抢走本该走“切换状态”的道具。
- 数量 `count`：基线在 kind 0 里读 `U16 count` 但只当 1 用；我是否在某处真的消耗了 count？

### P0-2 `internal/server/rooms.go:24 parseSettings` / `:166 settings.write` —— 房间设置编解码

128 个 bit 的表是手抄的，最可能错位。请逐位对照 `CRoomSettings::CRoomSettings(Buffer&)` 与 `CPacketManager::WriteSettings`：

- 每个 bit 的宽度是否一致（尤其 8/32/128/512 这类“带子结构”的字段）。
- 截断策略：解析不足时现在**拒绝**该请求并回 `Unable to create a room due to incorrect settings`（早期是“保留前半包继续”）；尾部多余字节只记日志。这个取舍是否合适？
- `write` 现在不再改写 `flags`（`44b5eed` 去掉了有副作用的 `normalize()`）。请确认“置位但无载荷”的 flag 在回包里的处理与基线一致。
- `zsDifficulty`（`room_validate.go:283`）按“输入 1 字节 / 输出补 9 字节”处理，见 §8 第 4 条。

### P0-3 `internal/server/host.go` —— 对局生命周期与结算

- `startGame:33` 包序：`180{6,1,mode,0}`（联赛计量条）→ `68/0 GameStart` → `65/4 房间状态` → 每个 ready 成员 `68/1 HostJoin`。基线 `SendLeagueGauge → SendStartMatch → OnGameStart(状态 + HostJoin)` 的顺序与对象（发给谁）是否一致？`180` 只发给发起者对不对？
- `hostRequest:230` 各 kind 的读字段：`p.Bytes(24)`（kill 事件）、`U8/U16` 顺序、`case 100 ConsumeItem` 的保护列表（默认道具+伪默认道具）、`case 1` 存档上限。
- `endGame:485`：`matchBaseReward` 的 9999 上限与整数除法、职业加成、装备加成（见 P1-3）、`SettleMatch` 参数、结果包 `matchResult` 里 18 个 `U16` 的顺序（基线有一处 `classBonusPoints` 疑似笔误，我按“exp 放 exp 位”写，请确认该不该照抄这个 bug）。
- 职业加成现在按基线 `GetFirstActiveItemByItemID`（`status=1 AND inUse=1`）在**结算时查库**（`Store.HasActiveItem`，故意不带过期/数量过滤）。请确认这个查询与基线的窗口完全一致，并检查"进入对局后被摘下的职业物品"是否也该失效。
- `migrateHost:632`：只对 `members` 里 `playing` 的人发；新房主的选择（基线优先 `m_UserStats[0]`，我取第一个 playing 成员）；`HostRestart` 的存档**必须为空**（见 §8）。

### P0-4 库存行消失后的级联（本轮新增，前五轮的 P2 全在这里）

前五轮审查反复命中的都是同一类：**库存行被删掉之后，别处的引用/状态没跟着走**。本轮把入口列全做了一次梳理，结论在 `.tmp/inventory-removal-audit.md`，请重点复核这份判断表，尤其“判定不需要级联”的那 8 条。

四件必须同时成立的事：

1. 外部引用：`UserCharacter.nameplateID`、`UserCharacterExtended.zbRespawnEffect`/`killerMarkEffect`、`UserCostumeLoadout` 七列、`UserZBCostumeLoadout`。
2. 选择接替：同 ID 还有 `inUse<>0` 的有效副本时，把 `status=1` 交给它（`store/references.go:41 promoteItemSelection`）。
3. 在线缓存：`server/update_info.go:39 refreshCharacterCache`。
4. 下发：154 是按槽位的增量帧，删除必须显式发 `U16(slot)+U8(0)`，不能靠“没提到”。

已接上级联（引用清理 + 选择接替）的入口：分解 `store/disassemble.go:7`、GM `/removeitem`、自然到期 `store/expiry.go:37`、公仓存入 `store/clan_storage.go:71`、强化材料 `store/enhance.go:5`。服务端在分解、到期、公仓存入、强化四条路径上都调用 `refreshCharacterCache`。

判定“不需要级联”的入口（上一轮已按“材料类别”推定的部分，请继续挑刺）：消耗 `consume.go:7`（掉落箱/喇叭道具）、开箱钥匙 `itembox.go`、续期 `item_extend.go`、喷漆 `paint.go`、部件 `part.go`、改名卡 `profile.go`、奖励来源 `rewards.go:38` 的 `SourceSlot`、插件材料 `favorite.go` 的 `SetAddonsConsuming`。注意：强化材料那条上一轮就是被这条推定误判的，所以请**优先从“服务端有没有真正的类别校验”入手**，而不是客户端 UI 能否选中。

具体问题：

- `clearItemReference` 的判定是“同 ID 已无 `count>0 && inUse<>0` 的有效副本才清”。这个条件与基线 `RemoveItem` 一致吗？休眠副本（相对天数）算不算“有效”？
- `promoteItemSelection` 只处理 `status<>0` 的被删副本，且只找 `inUse<>0` 的副本。基线的“接替”是否也要求 `inUse<>0`？
- 公仓存入是否需要额外规则（基线的 `clanmanager` 是否直接允许存入已装备道具）？

### P0-5 到期扫描 / 登录初始化顺序（`server.go:623 sweepExpiredItems`、`lobby.go:347 loginPackets`）

- 上一轮引入的回归是“登录过程中到期扫描先发了 152/154，UserStart 150 反而在后面”。现在 `loginPackets` 传 `skip=c.user.ID`：库清理照做，其他在线会话照收补丁，正在登录的账号交给登录链后面的正常背包初始化。请确认这个“按账号跳过”没有漏掉重连（同一条 `/login` 路径）与新建角色的路径（`server.go` case 2）。
- 到期补丁的形状：墓碑在前、剩余库存在后。基线 `SendInventoryRemove` 只发删除记录；我们额外把剩余库存也发一遍（与分解路径一致）。这个“多余的字段”会不会让客户端做多余的重建？
- skip 账号现在走 `reloadCharacter`（重读不发包）。请确认这个“清库 + 只重读”的组合在所有登录支路（`/login`、新建角色 case 2、重连）上都执行到了，且没有第二次广播。
- 后台扫描（`server.go:241` 的定时器）与登录扫描可能同时对同一账号执行，请检查事务与在线推送的竞态（`s.mu` 覆盖范围）。
- 到期通知（`UserExpiryNotice` → 67/62）与背包补丁的顺序：现在通知在登录链后段才发，运行期到期没有 67 通知。基线是这样吗？

### P0-6 黑盒对比暴露的剩余差异（`blackbox/`）——已关闭（2026-11-05）

两边数据目录逐文件相同，下列差异全部是代码差异。2026-11-05 对着重新拉起的 30003 逐包对比后已全部处理：登录链现在是 **60 包逐包相同**，只剩 `72`（已批准偏离）与元数据帧压缩方式（已记录例外）。

| # | 包 | 原差异 | 处理 |
|---|---|---|---|
| 1 | `74`/`76` | 我们 74 → 76，基线 76 → 74 | 已改：`loginPackets` 把存档配置 76 放到封禁 74 之前（`usermanager.cpp:583` 先 Option 后 BanList/BanSettings） |
| 2 | `103` | 我们在背包之前，基线最后 | 已改：`sendVoxelURLs` 挪到 `69` 之后（`usermanager.cpp:637`） |
| 3 | `151` | 我们 1 条，基线 2 条 | 已改：首次进频道发两次（`channel.cpp:68` + `channelmanager.cpp:207`） |
| 4 | `180` | 我们在 `153`/`151` 之前 | 已改：`sendLeague` 挪到 `joinLobby` 之后（`usermanager.cpp:632`） |
| 5 | `153` | 314B，label=`tester1` | 已改：label 用基线字面量 `test_lobby`（`packetmanager.cpp:1756`）；另外 95B 来自 `157` 的字段策略（第 8 条）。现在 412B，与基线相同 |
| 6 | `72` | 34B 真实标题 vs 32B `Test` | **已批准偏离**，保留（`packetmanager.cpp:2142` 的占位串） |
| 7 | `67` | 我们 2 条，基线 3 条 | 已改：补上基线硬编码的 168B 英文欢迎框（`usermanager.cpp:618`），公告与商店的相对顺序也按基线调整 |
| 8 | `157` | 303B，flags `0c4d03eb` | 已改：flag 恒为 `0xFFFFFFFF`，按 `packethelper_fulluserinfo.cpp:43` 写出全部分段。现在 398B，**载荷与基线逐字节相同** |

回归护栏：`blackbox/test_login_order.py` 固化 60 包 id 序列（在 30003 上同样通过），`internal/server/userinfo_test.go` 提供按全 flag 固定布局解析 user info 的测试辅助函数。

### P1-1 `internal/server/metadata.go:44 zipMetadataFrames`

- 帧预算：`MaxFrame=65536`，帧头 5 字节，所以 `payload ≤ 65531`；客户端帧格式是 `U8(id)+U16(size)+data`，即 data 只有 **65528**。请核对 `protocol.MaxPayload-3` 的推导与所有调用点。
- `zipType` 分块路径（64000×4）只在配置打开时走，默认关闭（这个客户端不接受 `U8(5)`，见 §8）。
- `Hash`（255）下发零长度 payload，客户端是否接受没有实机验证。
- `loadMetadata` 的 `metadataOrder` 是否与基线 `SendMetadata` 顺序逐项一致；缺文件时跳过会不会让客户端缺表。

### P1-2 `internal/server/server.go:50 send` / `:285 serveClient` / `:550 serveUDP`

- `send`：队列容量 512（`server.go:267`），满了直接 `conn.Close()`。哪些场景会瞬时超过 512（元数据 38 包、房间列表广播、批量背包）？被误杀时该记什么日志？
- `serveClient` 的 defer：`stashReconnect → removeDedicated → leaveRoom → 清 users → close(queue)`，顺序是否正确；`close(c.queue)` 与写 goroutine 的竞态。
- `serveUDP`：`W` 帧格式、`portID 0/1` 语义、本地 IP 取反（`^b`）、只接受与 TCP 同 IP 的包。**双机没复测过**。
- 全局锁 `s.mu` 粒度：所有 `dispatch` 都在锁内（含 DB 查询）。这是有意的简化，是否可接受？

### P1-3 结算数值（`host.go:485 endGame`、`:683 applyEquipmentBonus`）

- 装备加成：基线里那段是**注释掉**的，因此`默认关闭`，只有 `GameMatch.CalcResults.ItemBonusEnabled=true` 才生效（R6 决定）。请判断：保留这段“开关 + 默认关”是否比彻底删掉更合适？
- 缺 `PlayerCoop` 加成（基线 `m_UserStats.size() >= 2` 时按模式给百分比）。`ServerConfig.json` 没有这段配置，是“配置缺失”还是“功能缺失”？
- 取整：base（`min(raw,9999)`）与 bonus（未封顶的 raw 计算后累加）的顺序，请对照 `CGameMatch::CalculateGameResult` 核一遍。日志里每局打 `match reward base/class/item/bonus/total`。

### P1-4 `internal/server/room_validate.go:300 refreshHostRoomItems` / `:331 validateRoomSettings`

- `Room.ValidateSettings` 默认 `false` 时整个校验（含 `sanitizeRoomSettings`）被跳过。基线 `CheckSettings` 是不是也无条件调用、内部按配置短路？如果是无条件跑，这个开关的位置就错了。
- `sanitizeRoomSettings` 的每个 `Is*Valid` 分支是否正确对应基线的“清字段/夹紧”策略（尤其 zbLimit、mutationRestrict、mapPlaylist 的重复/首项校验）。
- `refreshHostRoomItems` 用宿主背包决定 superRoom(8357)、sd(439)、c4Timer(112)；模式 3/4/5/15/24 的 sd 处理是否与基线一致。

### P1-5 `internal/server/reconnect.go`

- `stashReconnect` 在持有 `s.mu` 时调用；`restoreSession` 重建状态并向房间广播。重连窗口内的过期道具、踢出标记、`r.match.stats` 归属（旧 client 指针）是否正确？
- 与 `endSession`（同账号新登录踢旧连接）的交互：旧连接的 defer 会不会把新会话的 users 映射删掉。

### P1-6 `internal/store/store.go` —— 单连接与事务

`SetMaxOpenConns(1)` + 事务。请检查是否存在“事务内调用另一个自己开事务的 `Store.*`”的路径——那会死锁（`database/sql` 不会自动提升）。列出所有 `tx` 内再调用 `s.Xxx()` 的地方。

### P1-7 解析健壮性

`parseSettings`（`n*3`、`n*2`）、`parseVoxel`（U32 计数上限 4096）、`hostRequest` 的 `Bytes(n)`、`itemRequest` 的 `Bytes(n)`：检查每处上界是否在**分配/循环之前**。`FuzzItemRequest`/`FuzzMetadataFrames` 已补，但种子覆盖有限——新增分支时请同时加种子。

### P2-1 文档与代码一致性

`GAPS.md`、`MIGRATION.md`、`README.md`、`AGENTS.md` 里最近改了较多数字与能力声明，请抽查：基线哈希、支持的能力、数据文件行数、GM 命令列表是否与代码一致。

### P2-2 代码约定

`AGENTS.md` 要求结构体字段一行一个、复合字面量一行一个字段。新代码基本照做，旧代码有历史遗留，抽查即可（不阻塞）。

---

## 6. 我会怎么判断你的反馈

请按这个分级给，便于排期：

- **P0 / 会致客户端崩溃或错位**：包长、字段顺序、字段宽度、缺失/多余的包。
- **P1 / 行为与基线不一致**：分支条件、状态迁移、数值、持久化语义。
- **P2 / 健壮性、可维护性、文档**。

每条请给：`文件:行`、基线对应的 `文件:行`（或函数名）、期望行为、复现方式（能抓包/能跑测试最好）。
并明确区分三类：**「偏离基线」**（要改）、**「与基线一致但我认为基线这里也会出问题」**（讨论）、**「风格/偏好」**（不急）。

---

## 7. 明确不做的（请不要当 bug 报）

1. **Qt 管理 GUI**（基线 `src/gui/`）：明确不做，Go 版只有命令行/日志。
2. **Studio/Voxel 建房**（`mapId==254`）：需要 `CVoxelManager::GetSlotDetails` 的 HTTP 服务；基线在没有该服务时同样拒绝，我们直接回基线那句 `Unable to create a room due to incorrect settings`。
3. **`73 Rank` 的 `RankUserInfo` 单条回包**：基线只有空壳/注释，没有可复刻的实现。
4. **面部/面具装饰槽**：客户端“应用的装饰”面板只列 ~5 行，没有面部槽；服务端有列、模型上生效，但客户端不展示。属于客户端限制。
5. **客户端兼容性优先**：当“客户端实际行为”与“基线源码”冲突时，按客户端实现并记录（见 §8）。这类偏离是**故意的**，请只审查“有没有记录清楚、有没有更小的偏离方式”，不要直接判为 bug。
6. **商店推荐页标题**（`shop.go:203`）：基线在 `SendShopRecommendedProducts` 里硬编码占位串 `Test`/`Test2`，客户端会把它显示成推荐页标题。**已批准保留 `Recommended`/`""`**，除该字符串外整块字节与基线相同（`72` 帧 34B vs 32B 的差就来自这里）。不要再报为缺陷。

---

## 8. 已经踩过的坑（判据的例外）

都有实机证据，都是“照基线写却崩客户端”的例子：

1. **metadata 帧格式**：客户端只接受 `U8(id)+U16(size)+data`，data 上限 65528。基线更新版本（`3b56abf` 之后）在 data 前加 `U8(ZIPMETADATA_FULL_ZIP=5)` 并做 64000×4 分块，**这个客户端不接受**；照新版本发，客户端一登录就崩（`0xE0005916`）。
2. **ItemBox 头**：基线是 `U8(4)+U32(count)+26×count`（5 字节头）；写成 `U8(4)+U8(0)+U32(count)`（6 字节）时空表能过、非空表必崩。
3. **HostRestart 的存档**：基线 `SendHostRestart` 里 `vector<unsigned char> saveData = {};`（真正取存档那行是注释掉的），必须发空。塞 12KB 存档会让房主中途退出时新主机客户端直接崩。
4. **大灾变难度字段**：客户端**写** 1 字节、**读** 9 字节（`UInt8 + UInt32 + UInt32`）。回包必须补足 9 字节，否则整体错位 8 字节：成员列表空、开局 `Runtime Error`。
5. **154 背包帧是增量语义**：不是“整包替换”。分解/消耗/到期都必须显式发 `U16(slot)+U8(0)` 墓碑，重发剩余库存不能表达删除（`ccfedd0` 修的就是这条）。
6. **商店购买请求是 5 字节**：30.12.24 客户端发 `U8(1)+U32(子商品ID)`。基线 `CShopManager::OnShopPacket`（`shopmanager.cpp:273`）读的是 `U8+U8+U8`——只取 id 的低字节，剩下两个字节当空气。我们按客户端的真实形状读 U32，并要求长度**正好 5 字节**（基线与客户端之外的形状在基线上也只是被静默误读）。这条形状差异是照着客户端定的，别再按基线改成 3 字节。

---

## 9. 需要你给结论的开放问题

1. **等级表口径**：服务端用基线 `CUser::CheckForLvlUp` 的表（10→11 = 11491）。客户端结果窗显示“10 级 / 9000”，两条口径不一致，导致“经验条满格但不升级”。要不要为显示一致去反推客户端的表（在 `.nar` 里，二进制无明文）？还是保持与基线一致？
2. **`zsDifficulty` 的 padding**：存 9 字节（1 实际 + 8 零）。若将来遇到“写 9 字节”的客户端会读错位。是否有更稳的写法（按客户端版本分支）？现在解析侧已按 1 字节读、写入侧补 9 字节，存储里保留的是客户端真实值吗？
3. **`send` 队列满就断连**：是否应改成阻塞/丢弃非关键包/记日志？基线是阻塞写 socket。
4. **`.data` 的最终形态**：现在 `.data` 用客户端版 MapList/GameModeList、`.data-upstream` 用纯基线。这个双轨是否要固化成默认发布数据？
5. **新增的三张表** `GoRewardClaim` / `GoMatchSettlement` / `UserExpiryNotice`（后者基线有）：前两张是为幂等加的，基线没有。是否接受“在基线 DB 上新增表”这种偏离？（已决定接受，请只审实现正确性）
6. **装备加成的开关化**：基线里那段是注释掉的。保留“配置开关 + 默认关”还是彻底删除？
7. **到期补丁“多发剩余库存”**：基线 `SendInventoryRemove` 只发删除记录。我们跟着发一份剩余库存（与分解路径一致）。这个额外内容是否应去掉，只发墓碑？
8. ~~**`157` 的 flag 策略**~~（已结案 2026-11-05）：已改为与基线一致——flag 恒 `0xFFFFFFFF`、写出全部分段，rank 段按无排行角色的取值写 `0xFF+4×0+0`。tester1 的 157 载荷现在与基线逐字节相同（398B）。
9. **元数据 zip 的字节差异**：`91` 各段的压缩后大小与基线不同（Go flate vs 基线 zlib），解压内容一致。这算可接受的实现差异吗？

---

## 10. 最后

如果你只有时间看三处，请按这个顺序（新增了 P0-6 的黑盒差异，可与旧的三处并列）：

1. `internal/server/inventory.go:412 itemRequest`（路由 + slot 语义）
2. `internal/server/rooms.go:24 parseSettings` / `:166 settings.write`（128 bit 表与宽度）
3. `internal/server/host.go` 的对局生命周期（`startGame:33` / `hostRequest:230` / `endGame:485` / `migrateHost:632`）

这三处一旦错，客户端不是错位就是崩，而实机上只覆盖了少数模式。之后按 §5 的 P0-4（库存级联判断表）、P0-5（到期/登录顺序）往下即可。

谢谢。
