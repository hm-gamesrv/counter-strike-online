# a0bb9a6 审查结果

依据 `.tmp/review-request.md` 审查当前 HEAD `a0bb9a6`。参考树已通过 `scripts/check-ref.ps1` 确认为 `4859a6e`。没有修改业务代码、已有测试或运行数据，没有操作客户端。

确认 **4 项 P1 基线/状态偏差、1 项 P2 解析边界问题**。未确认新的 P0 客户端崩溃；下面涉及包或状态的结论不等于已经做过客户端端测。审查覆盖与开放问题答复另见 `CODE_REVIEW_A0BB9A6_COVERAGE.md`。

## R1 [P1] 登录扫描跳过推送，也跳过到期清理后的角色缓存重读

- 位置：`internal/server/lobby.go:351`、`internal/server/server.go:641–643`。
- 实际登录先从数据库读取 Character，再进入 loginPackets。扫描若清掉背景，skip 分支会在 refreshCharacterCache 之前 continue；后续 157 仍使用扫描前的 c.character。
- **复现**：准备已启用且刚到期的背景，按登录流程调用 loginPackets。数据库 nameplateID=0，发送所依据的缓存却仍为 7001。新包序正确了，但账号会继续被初始化为已失效背景，直到重登或另一次刷新。
- 基线 `itemmanager.cpp:1611` 起移除背景时同步角色选择；这里也违反当前 Go 的“数据库清理后缓存一致”要求。这是 ccfedd0 为修包序新增 skip 时遗漏的非网络状态同步。
- 建议：跳过的是推送，不能跳过缓存更新；在 UserStart/首个角色信息之前无广播地重读正在登录账号的角色字段。
- 探针：`TestReviewA0LoginRefreshesExpiredBackground`，`cache=7001 database=0`。

## R2 [P1] 强化材料并未限为武器，删除外观材料后引用和缓存残留

- 位置：`internal/server/enhance.go:300–337`、`internal/store/enhance.go:48`。
- `.tmp/inventory-removal-audit.md` 第 5 项假设“材料是武器”，但服务器只验证库存归属、重复槽位及 Grade，没有材料 Category/ClassName 限制；材料还在 grade 检查之前加入将被消耗的列表。
- **复现**：在已有强化夹具中把材料定义为已启用 LobbyBG，数量为 1，经真正 itemRequest/强化入口提交。材料被删除，角色数据库仍引用该背景 9001；客户端 UI 是否允许选择它并不能替代服务端约束。该探针使用类型替身，不声称默认客户端会主动生成这一请求。
- 基线 `itemmanager.cpp:1993 InsertExp` 同样不只接收武器，`:2036` 经 OnItemUse → RemoveItem 处理材料，RemoveItem 会清背景引用。Go 只做选择接替而没做外部引用清理，属于级联语义缺失。
- 建议：按基线允许的材料范围执行完整移除级联并刷新在线状态；如果决定收紧材料类别，需要明确记录这是约束变更，不能仅依赖客户端 UI。
- 探针：`TestReviewA0EnhanceCosmeticRemovalCascade`。

## R3 [P1] 职业物品已到期删除，结算仍按旧快照发放职业加成

- 位置：`internal/server/host.go:513–520`；快照来源 `:226`，重连也会保留该快照。
- 当前结算遍历进入对局时保存的 stat.items；自然到期的库存清理并不更新这份快照。
- 基线 `room/gamematch.cpp:554` 在 CalculateGameResult 中调用 GetFirstActiveItemByItemID，查询结算时的数据库。已被到期扫描移除的职业物品不会命中。
- **复现**：角色装备提供 50% 加成的职业物品进入对局，之后物品到期并完成真实 Store 到期扫描，再结算。Go 仍给出 classPercent=50、bonusExp=10000；基线查询应不再获得该职业加成。基础奖励仍为 9999，不是封顶顺序问题。
- 建议：职业加成按结算时有效、激活、选中的库存重读判断；重试仍需重新清零派生字段。装备加成是显式扩展，其时间语义也应单独说明，不能混同于基线职业加成。
- 探针：`TestReviewA0ExpiredClassDoesNotEarnBonus`。

## R4 [P1] 同款强化材料的翻倍对象从累计经验变为单件经验

- 位置：`internal/server/enhance.go:334–337`。
- 基线 `itemmanager.cpp:2031–2034` 先把本件经验加到 totalExp，再在材料与目标同 ID 时将整个 totalExp 翻倍。Go 先仅把本件 exp 翻倍，再累加。
- 确定性算例：先提交值 500 的普通材料，再提交值 10 的同款材料。基线为 `(500+10)*2=1020`，Go 为 `500+10*2=520`。阈值 1000 时，成功率分别为 100% 与 52%；材料顺序的基线语义也被改变。
- 该处基线的累计翻倍可能不合理，但目标明确要求复刻数值，且未列为已授权偏离，不能无记录地改成单件翻倍。
- 建议：按基线顺序计算，或明确批准并记录数值规则变更。与随机抽取分离后为纯经验计算补确定性金标准。
- 此项通过逐表达式源码核对与算例确认，**没有随机探针**；不以多次随机抽取作为公式相同的证明。

## R5 [P2] Voxel 保留 bit 18/20 被错误当作 U32 载荷

- 位置：`internal/server/rooms.go:146–147`，parseVoxel 的 default 分支。
- 基线 `definitions.h:1673–1695` 没有定义 Voxel bit18/20；`roomsettings.cpp` 构造函数和 `packetmanager.cpp::WriteSettings` 也不为它们读写任何字节。Go 却将未命中的 bit 一律按 U32 消费。
- **复现**：外层设置包含 Voxel 与后续 FireBomb，Voxel 只置 bit18 或 bit20，不附载荷，随后 FireBomb=77。基线应跳过保留位并读到 77；Go 吞后续字节，标记 truncated，FireBomb 未解析。当前房间入口会拒绝，未证明这会令客户端崩溃。
- 建议：仅对实际定义的数字 bit11/12/13/15/16/17 消费 U32，未定义位保留 flag、不消费载荷；bit14 的数组保留其专用边界检查。
- 探针：`TestReviewA0VoxelReservedBitsHaveNoPayload`（两个子用例）。未证明当前客户端实际发送过这些保留位，因此按解析健壮性 P2 分级。

## 验证和复现

- `GOTOOLCHAIN=local` 下现有 build、vet、完整测试通过。上轮 `.dist/review-9d33c2d/overlay.json` 两条探针通过。
- 新 overlay：`.dist/review-a0bb9a6/overlay.json`。目标文件为 **`internal/server/review_a0_test.go`**、**`internal/server/review_a0_settings_test.go`**；实际探针放在同目录的 server_test.go/settings_test.go，均已 gofmt。当前 Go 工具链可以通过 overlay 增补虚拟测试路径，无需向源码树放占位文件。
- R1/R2/R3/R5 均复现失败。128 个外层 bit 的独立宽度矩阵全部通过（空字符串/空数组，difficulty 按已知 1→9 例外）；这不覆盖所有非空子结构和值语义。

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay .dist/review-a0bb9a6/overlay.json ./internal/server -run '^TestReviewA0' -count=1
```

R4 不包含在上述失败探针里。未将旧报告中的已修问题、明确排除的功能或缺少测试本身重复计为缺陷。
