# 70bd62e 代码审查

日期：2026-11-03（Asia/Hong_Kong）。审查 HEAD：`70bd62e4c0f5b8a9850c238217deff3d7a19ab89`，业务修复提交为 `e89e18d`。上一轮响应：`../2026-11-03T21-15-03-04608be/CODE_REVIEW_04608BE_RESPONSE.md`。

结论：确认 **1 项 P0、3 项 P1**，均为可复现的基线偏离。上一轮三个探针全部通过，但不足以关闭设置广播和房主迁移的边界问题。没有修改业务代码；新增五条 overlay 探针及本报告。工作区原有 `code-review/INDEX.md` 修改保留。

## 一、对上一轮 RESPONSE 的再审查

复核结果：

| 上轮编号 | 本轮结论 |
|---|---|
| R1：host 激活遗漏已 inUse 的物品 | 新增 `Store.UseInGameItem` 消除了复用 `ActivateItem` 的提前返回；原探针通过。相对期限延长分支已静态对照，本轮没有声称穷尽所有期限组合。 |
| N1：归档进入父模块 `./...` | `code-review/probes/go.mod` 有效隔离归档源码，根目录 build/vet/test 恢复通过。 |
| N2：保存值与广播值不一致 | 原 superRoom 探针通过；清洗删除字段的处理引入 R1 所述编码问题，不能关闭。 |
| N3：房主迁移不刷新道具权益 | 原“旧主有、新主无”探针通过；查询条件与 C4 反向迁移仍不等价，见 R2。 |

### R1 [P0] 广播保留已清洗字段的原始标志，并按原始长度补零，导致变长字段后的解析错位

- **Go 位置**：`internal/server/rooms.go:189–206,688`；清洗位置 `internal/server/room_sanitize.go:299–300`。
- **基线位置**：`manager/channelmanager.cpp:1712,1798`，`room/roomsettings.cpp:1569`，`manager/packetmanager.cpp:WriteSettings`。
- `merged` 清掉某字段后，`writeMasked` 仍使用原始 `st.flags`，再用 `len(mask.fields[index])` 个零填充。变长字段的宽度由其内部标志/计数决定，置零后不能继续使用原宽度。例如 Voxel 的内部 flag 变成 0 后只应编码 4 字节，而不是连原先的字符串空间一起补零。
- **复现**：开启 `Room.ValidateSettings`，普通地图 1 的房主发送更新，包含 `Voxel(flag=1,id="abc")`，随后 `teamSwitch=1`。服务端移除 Voxel 并存下 teamSwitch=1，但广播仍带 Voxel bit，并写 8 字节全零；接收方读取 4 字节 Voxel flag 后，把下一个零当作 teamSwitch，真正的 teamSwitch 留在包尾。
- **实测输出**：`voxel=true teamSwitch=0 stored=1 unread=4`。这是帧内字段错位；本轮没有使用真实客户端，未声称实测闪退。
- **需要纠正上一轮说明的精度**：基线发送的是 **经过 `CheckNewSettings` / `LoadNewSettings` 清洗后的 newSettings flags**，不是原始请求 flags；字段值来自当前房间。基线对非 Studio 地图直接清除 Voxel 的更新 bit，因此也不会把该块补零回显。
- **建议**：保留独立的“有效更新掩码”，清洗请求时同步删去被拒绝的更新位，再从有效房间状态编码。被忽略的请求字段不能通过删除房间已有值来替代。不要按字段是否为空笼统归一化所有 flags，保留位没有载荷仍合法。变长块若确实要重置，应按其有效结构编码。
- **探针**：`TestReview70SanitizedVoxelMaskDoesNotCorruptFollowingField`，失败。

### R2 [P1] 房主迁移的道具检查仍把未启用物品算作权限，且不能从 C4=0 更新为 1

- **Go 位置**：`internal/server/room_validate.go:311–322`，`internal/server/rooms.go:743–753`。
- **基线位置**：`room/room.cpp:899–927,978`，`manager/userdatabase_sqlite.cpp:1554`。
- `refreshMigratedHostItems` 复用了 `refreshHostRoomItems`，后者只检查 `itemID` 与 `count>0`。基线迁移调用 `GetFirstActiveItemByItemID`，SQL 要求 **status=1 AND inUse=1**，不是“持有即可”。新房主只有 `status=0,inUse=0` 的 8357，Go 仍授予 superRoom，并因此可能取得 `kickRequest` 使用的踢人权限。439 同样受影响。
- C4 还有独立边界：只有旧设置存在且非零才重新计算。旧房主没有 C4，房间为 0；新房主持有已启用的 112，迁移后仍为 0。字段原先不存在时还会跳过 C4 广播。基线 `CheckForHostItems` 无条件重算并广播 C4。
- **实测输出**：休眠道具场景 `superRoom=1`；反向迁移场景 `c4Timer=0`，基线应分别为 0、1。
- **建议**：迁移按基线的活跃行查询独立重算三项，不以旧设置为查询前提；适用模式的 sd 条件保留。仓库已有 `Store.HasActiveItem` 实现相同 SQL。建房/设置更新是否需要相同条件应分别对照，不能让现有“持有即有效”的测试代替基线依据。
- **探针**：`TestReview70MigrationRejectsDormantHostItems`、`TestReview70MigrationEnablesNewHostsC4`，均失败。

## 二、新的整体性审查

本轮扩大到房间加入/分队、家族战设置与开局条件、主机击杀事件、活动发奖、专用服和重连相关调用，并抽查了库存删除引用与事务辅助函数。下面仅列新增的确认问题；此前已登记的登录包序、157 flags 等差异没有重复计数。此次是静态对照与隔离测试，没有操作 30002/30003 或真实客户端，也没有完成全调用图/全模式等价证明。

### N1 [P1] 家族战只分配军团槽位，双方加入后被放在同一队

- **Go 位置**：`internal/server/rooms.go:567–571`，`internal/server/room_clan.go:familyBattleJoin`；设置更新的相关分支位于 `rooms.go:650–673`。
- **基线位置**：`manager/channelmanager.cpp:1329–1372`；已有房间启用家族战时的重分队为 `1767–1788`。
- `familyBattleJoin` 只检查/登记 clan1、clan2；随后加入路径无条件执行 `c.team=2`。房主创建房间同样是 team=2。因此对方军团成员成功加入后，与房主处于同一阵营。
- 基线加入时参照已有成员，按军团相同/不同选择同队/对队。Go 还禁止家族战手工换队，`familyBattleCanStart` 只检查两个军团是否有人，不能修正此状态；已有普通房间启用家族战时也未看到基线的队伍重排及队伍变更广播。
- **复现**：用真实 Store 创建两个军团，A 为家族战房主，B 经 `roomRequest` 的 65/23 加入。clan1/clan2 不同且 B 入房成功，但 `host.team=2,guest.team=2`。
- **影响**：家族战阵营与军团关系不一致，开局和按队伍判胜的结算无法按原版工作。
- **建议**：在加入/广播前按军团确定阵营；启用家族战和相关房主迁移路径也核对队伍及客户端/专用服通知。测试需经实际入房入口，而不只验证两个军团 ID。
- **探针**：`TestReview70FamilyBattleOpponentsJoinOppositeTeams`，失败。

### N2 [P1] 主机击杀事件遗漏扭蛋奖励计数和发奖

- **Go 位置**：`internal/server/host.go:297–314`（case 3 仅更新击杀和任务）；数据库列已存在于 `internal/store/schema.sql` 的 `UserCharacterExtended`。
- **基线位置**：`room/gamematch.cpp:318–332` 的 `OnKillEvent`。
- 基线每次处理有效击杀事件都会减少 `killsToGetGachaponItem`，到 0 时重置为 100、随机发放 166 或 5101 中的一件，并发送 `SendItemGachapon` 通知，随后保存计数。此段没有 WeaponRelease 活动开关，也不是上游空桩。
- Go 击杀入口没有这段逻辑，全项目搜索也未发现该计数的读写实现。不能用已经实现的任务、怪物掉落箱或集字 GM 命令替代这项玩家奖励。
- **复现**：将测试账号计数设为 1，发送一条完整、合法且来自当前房主的非自杀击杀帧。预期计数变为 100，并新增一件 166 或 5101；实际 `counter=1,awards=0`。
- **建议**：补接正常击杀奖励分支，按项目现有事务要求同时提交计数与奖品；测试覆盖未达阈值、恰好到阈值、已有同款以及发奖失败重试。本条不要求引入新的自杀规则；基线该分支的自杀行为应另外明确。
- **探针**：`TestReview70KillPaysGachaponAtCounterBoundary`，失败。

### 验证与复现

- 已运行 `scripts/check-ref.ps1`，参照仓库确在 `4859a6e`。
- 所有 Go 检查使用 `GOTOOLCHAIN=local`。
- 根目录 `go build ./...`、`go vet ./...`、`go test ./...` 通过。完整测试首次因环境阻止本机回环连接而失败；允许本机测试连接后重跑通过，该环境失败不计作代码问题。
- 上轮 overlay 的 `TestReview046*` 三条全部通过。
- 新增五条 `TestReview70*` 均编译成功并在预期断言失败，输出如上；探针已 gofmt，overlay vet 未报告问题。
- 新增探针使用独立临时数据库，不修改 `.data` / `.data-test`。未修改既有探针或任何业务代码。

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay code-review/probes/review-04608be/overlay.json ./internal/server ./internal/store -run '^TestReview046' -count=1
go test -overlay code-review/probes/review-70bd62e/overlay.json ./internal/server -run '^TestReview70' -count=1
go vet -overlay code-review/probes/review-70bd62e/overlay.json ./internal/server
```

新增探针及绝对路径 overlay 位于 `code-review/probes/review-70bd62e/`。整体测试通过不消除上述行为偏离；本轮新增探针提供对应的失败证据。
