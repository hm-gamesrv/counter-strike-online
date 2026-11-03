# 存储层与健壮性审查（store / 长跑 fuzz）

- 仓库：`C:\Users\11717\Desktop\cso-server`
- 分支：`go-rewrite`，HEAD `75cddae`
- 基线：`.ref/CSNZ_Server` = `4859a6e`（`pwsh scripts/check-ref.ps1` → 参照仓库已在基线 4859a6e）
- 审查范围：仅 A（事务与单连接不变量）、B（long-running fuzz）、C（数据一致性回归）。任务/商店/公会/重连/专服/GM 的业务语义不在本文范围内。
- **未修改任何业务代码**（`internal/`、`cmd/` 全部未改）。新增内容只有 `code-review/probes/review-75cddae-store/` 与本报告。

## 0. 结论速览

| 编号 | 位置 | 结论 | 严重度 |
|---|---|---|---|
| S-1 | `internal/store/title.go:51` `SetTitle` | 读-改-写未包事务，并发下丢失更新（实测 20/2000 轮） | 中低（需同账号并发） |
| S-2 | `internal/store/effect.go:18` `activateExpiry` | 唯一不走 `liveItemSQL` 的「取行并改写」路径：已过期行仍可被激活成名牌 | 中低 |
| S-3 | `internal/server/rooms.go:24` `parseSettings` / `:170` `settings.write` | 未建模的 flag 位被存成 0 长度字段并原样重播；参照实现只重播它建模过的字段 | 低（fuzz 发现） |
| — | 单连接自锁 | 54 个 `s.DB.Begin()` 方法无自锁、无 `s.DB` 逃逸、无缺失 `defer tx.Rollback()` | 通过 |
| — | 多写事务部分失败 | 6 条新注入用例全部回滚干净（原仓库未覆盖） | 通过 |
| — | 幂等重放 | `GoRewardClaim` / `GoMatchSettlement` / `UserExpiryNotice` / `UserRewardNotice` 重放均只生效一次 | 通过 |

---

## A. 事务与单连接不变量

### A.1 静态扫描（可复现）

`store.Open` 用 `db.SetMaxOpenConns(1)`（`store.go:80`）。因此任何在持有 `*sql.Tx` 期间再去 `s.DB.*` 的调用都会永久阻塞，任何忘记 `Rollback` 的早退也会让该 `*Store` 之后所有查询永久阻塞。这两类问题纯文本 grep 不可靠，所以写了一个 `go/ast` 扫描器并随探针交付：

```powershell
cd code-review/probes
$env:GOTOOLCHAIN='local'
go run ./review-75cddae-store/scan_tx.go ../../internal/store
```

扫描器判定 8 项：T1/T2（Begin 所有者可达另一个 Begin 所有者，直接/传递，含自由函数如 `grantItems`、`grantRewardTx`、`joinClanTx`）、T3（Begin 体内仍用 `s.DB`）、T4（`tx *sql.Tx` 辅助函数逃逸到 `s.DB`）、T5（tx 辅助函数调用 Begin 所有者）、T6（包内 `go` 语句）、T7/T8（有 Begin 但无 `defer tx.Rollback()`）。扫描结果（157 个函数 / 54 个 Begin 所有者）：

```text
== [T1/T2] Begin-owning func reaching another Begin-owning func (self-deadlock) ==
  (none)
== [T3] Begin-owning func that still touches s.DB (must use tx only) ==
  Store.init    store.go:94  s.DB@[95 99 104]  begin@110
== [T4] tx *sql.Tx helper referencing s.DB (escapes the tx) ==
  (none)
== [T5] tx *sql.Tx helper calling a Begin-owning Store method ==
  (none)
== [T6] funcs launching goroutines ==
  (none)
== [T7] Begin-owning func WITHOUT defer tx.Rollback() ==
  (none)
```

判定：

- **T1/T2 清零**：不存在事务内再调自身 Begin 的 Store 方法，直接与传递闭包都为空。公会那组共用的 `s.clanMembership(tx, ...)` / `s.joinClanTx(tx, ...)` 都正确接收 `tx` 而非自己开事务；`Purchase` → `chargePurchase(tx)` / `grantItems(tx)`、`GrantReward` → `grantRewardTx(tx)`、`TickGachapon` → `gachaponReward(tx)` 亦然。
- **T3 仅 `Store.init`**：95/99/104 行的 `s.DB` 全部在 110 行 `Begin()` **之前**（PRAGMA、`user_version`、空库判定），是正确顺序而不是逃逸。
- **T4/T5/T7 清零**：没有 `tx` 辅助函数偷偷用 `s.DB`；54 个 Begin 所有者全部有 `defer tx.Rollback()` 且在 `Commit` 之前注册（T8）。漏掉一个就会把整个 `*Store` 挂死，这一项特意单独判定。
- **T6 清零**：包内没有 `go` 语句，事务不会跨协程泄漏。

### A.2 事务方法与判定清单（54 个）

| 文件 | 方法@行 |
|---|---|
| ban.go | AddBan@8 |
| bingo.go | InitBingo@65, SetBingoSlots@104, OpenBingoAwards@124, OpenBingo@154, ResetBingo@171 |
| clan_storage.go | GiveClanItem@75, TakeClanItem@159 |
| clan.go | CreateClan@241, JoinClan@341, ApproveClanJoin@428, LeaveClan@505, InviteToClan@535, SetClanGrade@634, KickClanMember@669, SetClanMark@764, DissolveClan@804, DelegateClanMaster@849 |
| consume.go | ConsumeItem@8 |
| costume.go | ToggleCostume@33, AutoEquipCostume@198 |
| disassemble.go | DeleteItems@8 |
| effect.go | ToggleNameplate@40, ToggleEffectItem@103 |
| enhance.go | EnhanceItem@9 |
| expiry.go | ExpireItemsChanged@38 |
| favorite.go | SetAddonsConsuming@42, SetAddons@114 |
| gachapon.go | TickGachapon@42 |
| gm.go | setItemColumn@125, AddPoints@179 |
| inventory.go | ChangeItem@29 |
| item_extend.go | ExtendItem@9 |
| itembox.go | OpenItemBox@15 |
| match.go | SettleMatch@22 |
| notice.go | ClearExpiryNotices@48 |
| paint.go | ApplyWeaponPaint@28, SwitchWeaponPaint@77 |
| part.go | ApplyWeaponPart@22, RemoveWeaponPart@73 |
| profile.go | RenameCharacter@42 |
| purchase.go | Purchase@25 |
| quests.go | RecordQuestProgressOnce@31 |
| reward_claims.go | PrepareRewardClaim@41 |
| rewards.go | GrantReward@42, GrantRewardNotice@63 |
| session.go | StartSession@4, EndSession@19 |
| store.go | init@110, Register@161, CreateCharacter@246 |
| survey.go | SaveSurvey@17 |
| use_item.go | UseInGameItem@13 |
| weapon_release.go | SetWeaponReleaseCharacterReward@94 |

54 个方法逐个判定为通过（T1–T8 全绿），没有发现需要业务方立刻修的自锁点。

### A.3 多写事务的部分失败回滚（新增注入用例）

仓库原有的失败注入只覆盖 `UserInventory`（`grant_test.go`、`rewards_test.go`、`inventory_test.go`、`store_test.go`）与 `GoRewardClaim`（`quests_test.go`）。下面几条路径原本没有任何「第二步失败、第一步必须回滚」的证据，本轮用 `CREATE TRIGGER ... RAISE(ABORT)` 注入补齐：

| 测试 | 注入点 | 断言 | 结果 |
|---|---|---|---|
| `TestStoreProbeOpenItemBoxRollback` | grant 侧 `INSERT UserInventory` | 钥匙堆叠仍是 5，未被扣 | PASS |
| `TestStoreProbeExtendItemRollback` | 目标行 `UPDATE UserInventory NEW.slot=3` | 延长道具仍是 3，目标 expiry 未变 | PASS |
| `TestStoreProbeEnhanceMaterialRollback` | 目标行 `UPDATE NEW.slot=3` | 材料行仍在（DELETE 已执行但被回滚） | PASS |
| `TestStoreProbeExpirySweepRollback` | `INSERT UserExpiryNotice` | 过期行未被删（删除与通知同事务） | PASS |
| `TestStoreProbeSettlementSecondParticipantRollback` | 第二参与者 `UPDATE UserCharacter NEW.userID=2` | 第一参与者未结算；同一 matchID 事后可重试成功（失败没烧掉幂等键） | PASS |
| `TestStoreProbeClearExpiryNoticesIdentityAndReplay` | 真实重放 | 只删投递过的 rowid，未投递的保留；重复调用幂等 | PASS |

补充观察（非缺陷）：`SettleMatch` 在每次结算的事务里执行 `CREATE TABLE IF NOT EXISTS GoMatchSettlement`（`match.go:27`）。表已存在时是无副作用的空操作，但它意味着这张表是「首次结算时懒创建」的；像 `initRewardClaims` 那样在启动期建表会更一致。不影响正确性，只是 schema 生命周期的可预期性差一点。

### A.4 幂等性重放

| 路径 | 机制 | 独立验证 | 结果 |
|---|---|---|---|
| `GoRewardClaim` | `UPDATE ... SET claimed=1 WHERE claimed=0` 加 `RowsAffected()!=1` → `ErrRewardClaimed`（`rewards.go:91`） | 仓库已有覆盖（含 4 协程并发、重启、过期源退役） | 通过，未重复造轮子 |
| `GoMatchSettlement` | `(matchID,userID)` 主键加 `INSERT OR IGNORE`，`RowsAffected()==0` 则 `continue`（`match.go:34`） | 仓库已有重放覆盖；**本轮新增**：失败结算不烧 matchID（A.3 第 5 行） | 通过 |
| `UserExpiryNotice` | `ExpiryNoticeRows` 返回 `rowid`；`ClearExpiryNotices` 按 `(userID,rowid)` 删 | 本轮新增身份作用域加重放用例 | 通过 |
| `UserRewardNotice` | `DELETE ... LIMIT 1` 与发放同事务，`RowsAffected()!=1` → `ErrRewardClaimed` | 本轮新增重放用例 | 通过（重复领取被拒，物品只发一次） |

---

## B. 长时间 fuzz

### B.1 三个目标的执行数据

全部在 `$env:GOTOOLCHAIN='local'`、`GOMAXPROCS=8`、`-parallel 8` 下运行。

| 目标 | 时长 | 实际墙钟 | 执行次数 | 结果 |
|---|---|---|---|---|
| `FuzzItemRequest` | 180s | 182.9s | **463,716** | PASS，无 panic/超时；语料 126 到 145（新增 19） |
| `FuzzMetadataFrames` | 180s | 180.9s | **286,819** | PASS，无 panic/超时；语料 4 到 6 |
| `FuzzRoomUpdateSettings`（新，见 B.3） | 180s | 181.9s | **447,358** | PASS，无 panic/超时；语料 109 到 135 |

对应命令：

```powershell
$env:GOTOOLCHAIN='local'; $env:GOMAXPROCS='8'
go test ./internal/server -run '^$' -fuzz '^FuzzItemRequest$' -fuzztime 180s
go test ./internal/server -run '^$' -fuzz '^FuzzMetadataFrames$' -fuzztime 180s
go test ./internal/server -overlay code-review/probes/review-75cddae-store/overlay.json -run '^$' -fuzz '^FuzzRoomUpdateSettings$' -fuzztime 180s
```

内存：三个进程都没有出现 RSS 异常增长，也没有临时文件堆积——两个旧目标的 `os.MkdirTemp` 都只在 setup 阶段执行一次，不在 fuzz body 里。`FuzzItemRequest` 的速率在 ~0.3k/s 到 ~10k/s 之间波动（种子覆盖阶段与队列排空造成），属正常现象而非泄漏；180 秒结束时仍在持续产出 new interesting。

### B.2 `internal/server/testdata/fuzz` 与 `.gitignore`

**未被忽略。** 实测 `git check-ignore -v internal/server/testdata/fuzz/FuzzItemRequest/xxx` 返回 exit 1（无匹配）。当前 `.gitignore` 只覆盖 `/.ref/`、`/.tmp/`、`/.data*/`、`/.dist/`、`*.exe`、`*.test`、`*.out`、`__pycache__/`、`/data/`。**建议（仅建议，未修改）**追加：

```gitignore
# go test -fuzz 的崩溃语料（本地复现用，不入库）
**/testdata/fuzz/
```

本轮 fuzz 确实落了一个失败语料到 Git 可见的位置（见 B.3），`git status` 会看到它。

### B.3 新探针：`FuzzRoomUpdateSettings`（overlay 注入）

**为什么选它。** 现有两个目标覆盖 item 分发与元数据分帧；设置增量（`roomRequest` kind 5）是 lobby 里唯一一处「把对端给的 flag 表解析一半、合并进房间状态、再序列化成给所有人广播的帧」的路径，也是全仓库唯一一处「服务端用自己的解析器反过来读自己写的帧」的地方。flag 与 field 一旦不同步，后面每个字段都会错位。这与存储层是一致性问题，不是业务语义问题。

**实现**（`code-review/probes/review-75cddae-store/server_test.go`）：单进程内建一个 `store.Store` 加 `Server` 加宿主 `client` 加一个房间，每个迭代先把房间设置重置回同一起点，所以任一失败输入都能由语料单独重放（不是「多次 delta 累积」）。种子 8 条：空、整帧回显、玩家上限/模式/地图/家族战单字段 delta、全 flag 置位、单 flag 置位。断言两条：

1. 房间当前设置序列化出来的帧，必须能被同一个 `parseSettings` 整段读回（宽度逐字段一致、没有被截断的字段）。这条直接抓 flag/field 失配。
2. 接受后的容量 `number(8)` 不得小于房间现有成员数。

**它抓到了东西（S-3）。** 第一次长跑在 **36.4 秒 / 66,439 次执行** 时触发断言，`go` 把语料保存到：

```text
internal/server/testdata/fuzz/FuzzRoomUpdateSettings/7742cbf9344e819e
```

内容为 25 字节（首字节 `0x31`，其后是 `0x30` 与零字节的混合）。**按要求保留，未删除，也未提交。**

定位与判定：

- 该输入让 `flags[2]` 的 bit 12/13/20/21/28/29 置位。`parseSettings` 的 group 2 分支只对 `bit<=8` 读 1 字节（`rooms.go:96`），其余位一字节都不消费，但仍然在 `rooms.go:120` 记下一个长度 0、非 nil 的字段。
- `settings.write`（`rooms.go:170`）对该 flag 位原样 `w.Bytes(nil)`，于是带 flag 却没有 payload。房间随后 `writeUpdate` 把这一帧广播给全房间。
- 参照实现是反过来的：`roomsettings.cpp` 按固定顺序「flag 置位才读」，而且 `CheckNewSettings` 会把它没建模的 `ROOM_*_UNK` 位统一清掉（`roomsettings.cpp:1481-1542`），出站帧由建模过的字段重建，永远不会重播一个没建模的位。

严重度判为**低**：CSO2 客户端的读取器同样是「按 flag 查表、未建模位不读」的固定序列，所以大概率不会真的错位；但它意味着服务端发出的帧形状与它接受的帧形状不再一致，且这个不一致由房主单方面控制并广播给其他玩家。保守做法是在 `settings.write` 前把长度 0 的字段连同 flag 位一起清掉，与参照 `CheckNewSettings` 的清理对齐。

**关于 field 45 的补位（改这段代码的人最容易踩的坑）。** `parseSettings` 对 group 1 bit 13（难度）只读 1 字节，但把存下来的值补到 9 字节（`rooms.go:117-119`），注释说明客户端写 1 读 9。因此 `parseSettings` 不是 `settings.write` 的逆运算：服务端把自己写的帧读回来时，那 8 个补位字节会被当成后续字段的字节吃掉（宽度不变、取值错位）。探针已按此把「应有消费字节数」减去 8 字节。建议在 `parseSettings` 注释里直接写明「本解析器不可用于自校验」。

固定回归用例：`TestRoomSettingsZeroWidthFieldEcho`（同文件，用上面那份语料的 25 字节），在当前代码上失败，把 S-3 钉成一条可判定的断言。

### B.4 顺带核对：`FuzzItemRequest` 的断言强度

结论：作为 crash fuzzer 强度是合适的；作为一致性断言不够，且现有写法会掩盖一类问题。

- 它只做 `_ = s.itemRequest(...)`，既不看返回的 `[]Item`，也不看 `c.queue` 里排出去的帧。所以「没 panic 但库存和回包对不上」这类问题它结构上抓不到。文件注释里也明说了是 crash fuzzer。
- 值得补的是回包与库存的一致性：每次迭代后 `s.Store.Inventory(c.user.ID)` 应与最近一次成功请求返回的 `[]Item` 一致；且任何被接受的操作都不能让 `UserInventory` 出现 `count<=0`、重复 slot、`slot>65535` 这类结构性不变量破坏。这些同样能在 3 分钟内跑完，是性价比最高的加强方向。
- 一个现有写法上的隐患：它每次迭代前把 `c.queue` 排空，但 `c.conn` 是 nil；一旦某次调用发出超过 64 个帧，`client.send` 的 `default:` 分支会执行 `c.conn.Close()` 并对 nil 接口 panic，那会被 fuzz 记成「目标崩了」，其实是 harness 假阳性。建议把队列做大（本轮新探针用 8192）或给 harness 装一个丢弃连接。
- 种子只覆盖 8 个 kind，而 `itemRequest` 的 kind 空间更宽；靠首字节随机变异能覆盖到未列出的 kind，长期没问题，只是前期覆盖率偏低。

---

## C. 数据一致性回归

### C.1 「读一次、写一次」但中间可被并发插入

对 `internal/store` 做 AST 扫描（同一函数内既 `s.DB.Query*` 又 `s.DB.Exec`、且没有 `s.DB.Begin()`），命中 3 处：

| 位置 | 读 | 写 | 判定 |
|---|---|---|---|
| `store.go:193` `Login` | 196 取 userID/密码哈希，207 查封禁 | 214 更新登录时间/IP/HWID | **无害**。读的是鉴权输入，写的是与判定无关的审计列；重复登录语义由 `StartSession` 事务决定。 |
| `profile.go:15` `SetChatColor` | 18 查是否拥有该颜色道具 | 25 写 `chatColorID` | **无害**。写的是单列绝对值，不依赖读到的内容；校验失败就返回，不是 read-modify-write。 |
| `title.go:51` `SetTitle` | 56 读整列 `titles` | 66 写回整列 `titles` | **缺陷 S-1**，见下。 |

**S-1：`SetTitle` 丢失更新。** `titles` 是 5 槽位的序列化字符串，`SetTitle` 的做法是「读整列、改一个槽、写回整列」，两条语句各自 autocommit。`SetMaxOpenConns(1)` 只保证单条语句串行，不保证这两条之间不被别的协程插入；插入后后写者会用自己读到的陈旧副本覆盖前者。

实测（`TestStoreProbeSetTitleLostUpdate`）：两个协程分别写 slot 0 与 slot 1，2000 轮里**第 7 轮就丢**，累计 20 次（探针到 20 次即停）。这不是理论窗口，是百分之几量级的稳定复现。

可达性：同一账号的网络协程是单线程的，所以正常路径不会自撞；但服务端允许重连接管（`lobby.go:247` 调用 `s.endSession`），而 `endSession` 只 `conn.Close()`，**不会中断已经进入 `itemRequest` 的旧协程**；新旧会话在切换窗口内可同时落到同一 `userID` 的 `SetTitle`。另外定时器协程（`sweepExpiredItems`）与 GM 路径也与客户端协程并行。

修法很小：把两条语句包进 `s.DB.Begin()`（仓里其余 54 处都是这么做的），或在事务里先读后写。

### C.2 `liveItemSQL` 的一致性

定义在 `store.go:317`，注释要求「Every path that takes an item out of the bag must use it」。

**使用点（7 处，全部正确）：**

| 位置 | 用途 |
|---|---|
| `store.go:320` `readInventory` | 所有「从背包里取一件」的基础读取器 |
| `clan_storage.go:103` `GiveClanItem` | 取寄存的道具行 |
| `clan_storage.go:194` `TakeClanItem` | 同名去重判定 |
| `costume.go:226` `AutoEquipCostume`（普通时装） | 槽位是否被活体副本占用 |
| `costume.go:245` `AutoEquipCostume`（僵尸皮肤） | 同上 |
| `references.go:13` `clearItemReference` | 该道具是否仍是选中态 |
| `references.go:47` `promoteItemSelection` | 找接替的活体副本 |

**未使用但正确（按语义就该不同）：**

- `purchase.go:118`（叠加合并）用 `inUse=1 AND (expiryDate=0 OR expiryDate>?)`：这是「找一个可叠加的已选副本」，故意排除 dormant（`inUse=0,status=0`）副本，否则会把未激活的道具合进去。
- `costume.go:206`（自动装备）用 `inUse=0 AND status=0 AND (...)`：这是 `liveItemSQL` 的补集，找的正是「未激活可激活」的副本。
- `expiry.go:43` 用 `expiryDate>36500 AND expiryDate<=?`：过期清理，谓词方向相反。
- `active.go:13` `HasActiveItem` 用 `status=1 AND inUse=1`、不带 expiry：注释明确说明为对齐参照的 `GetFirstActiveItemByItemID`，属于有意的参照一致性。
- `reward_claims.go:20`（`retireStaleRewardClaims`）用 `count>0 AND (expiryDate=0 OR expiryDate>?)`：判断「挂起领取的源还在不在」。比 `liveItemSQL` 更严（不含 dormant 分支），但真正领取时 `grantRewardTx` 还会用 `readInventory` 加 `InUse==1 && Status==0` 复验一遍，不会导致错发。只是两处谓词写法不同，读代码时容易误判为漏洞。
- `clan_storage.go:187/202`：前者查 `isClanItem=1` 的存在性（任意状态）做重复寄存判定，后者 `count(*),max(slot)` 算槽位预算。都不是「取用」。

**未使用且判定为缺陷：**

| 位置 | 问题 |
|---|---|
| `effect.go:18` `activateExpiry`（被 `ToggleNameplate` / `ToggleEffectItem` 调用） | 按 `slot` 直取一行，**没有任何 liveness 谓词**，然后把该行改成 `status=1,inUse=?` |

**S-2：名牌/特效切换可以激活一行已经死掉的道具。** `activateExpiry` 只做

```sql
SELECT coalesce(status,0),coalesce(expiryDate,0) FROM UserInventory WHERE userID=? AND slot=?
```

`ToggleNameplate` 随后执行 `UPDATE ... SET status=1,inUse=?,expiryDate=?`，并以 `rowsAffected==0` 作为唯一的失败条件——那只能挡住「行不存在」，挡不住「行已过期」。后果：`UserCharacter.nameplateID` 被写成该 `itemID` 并随 157 包广播，而 `readInventory` 依然把它过滤掉，账号就「戴着」一件背包里看不见的道具。

实测（`TestStoreProbeToggleNameplateAcceptsExpiredRow`）：种一行 `expiryDate=now-10` 的名牌，`Inventory()` 看不到它，`ToggleNameplate(1,7001,6)` 返回 `nameplate=7001`、`UserCharacter.nameplateID=7001`，且 `err == nil`。

可达性：正常客户端只会提交自己背包里出现过的 slot，所以需要「列出背包之后、提交之前这一分钟里刚好过期」的竞态（`nowMinutes()` 的粒度是分钟），或者改过的客户端直接指定 slot。属于窄窗口缺陷，但它是全包唯一一处「拿到一行就改写」却不套 `liveItemSQL` 的路径，和文件自身的不变量注释冲突。

修法：`activateExpiry` 的 WHERE 加上 `count>0` 与 liveness 谓词（或直接复用 `readInventory` 找到 source 再校验），顺带补上「`slot` 上的行必须真的就是这个 `itemID`」这一条当前也没做的检查。

---

## D. 复现方式

```powershell
$env:GOTOOLCHAIN='local'
$env:GOMAXPROCS='8'
$P = 'code-review/probes/review-75cddae-store'

# A 与 C：事务回滚、幂等重放、S-1、S-2
go test ./internal/store -overlay $P/overlay.json -run '^TestStoreProbe' -count=1 -v

# B：新的 fuzz 目标（3 分钟）
go test ./internal/server -overlay $P/overlay.json -run '^$' -fuzz '^FuzzRoomUpdateSettings$' -fuzztime 180s

# B：S-3 的确定性回归用例（当前失败）
go test ./internal/server -overlay $P/overlay.json -run '^TestRoomSettingsZeroWidthFieldEcho$' -count=1 -v

# A.1：静态事务扫描
cd code-review/probes
go run ./review-75cddae-store/scan_tx.go ../../internal/store
```

预期：

- `TestStoreProbe*` 共 9 条，7 条通过；`TestStoreProbeSetTitleLostUpdate`、`TestStoreProbeToggleNameplateAcceptsExpiredRow` **失败**（S-1 / S-2，未修）。
- `FuzzRoomUpdateSettings` 3 分钟不崩（含已保留的 7742cbf9 语料）。
- `TestRoomSettingsZeroWidthFieldEcho` **失败**（S-3，未修）。
- `scan_tx` 全项清零，只剩 T3 的 `Store.init`，属正确顺序。

## E. 未覆盖 / 交回主审查者

- 事务扫描是静态可达性判定，不是运行时证明。若要更强证据，可加 `SetMaxOpenConns(1)` 下的「人为占住连接加超时」探针；但 54 个方法已在 A.1-A.3 有静态加注入两层证据，本轮没有再投入。
- `ChangeItem`（`inventory.go:29`）的选中/取消选中语义、`bingo` / `gachapon` 的奖励分布、公会/房间业务规则按分工未审。
- 线上实例 `127.0.0.1:30004` 全程未重启、未停止；`127.0.0.1:30003` 只做了代码对照，未发送任何帧；`.data` 未改动。
