# b4cc191 代码审查

日期：2026-11-03。工程 HEAD `b4cc191`，基线 `4859a6e73451ccbfce167b1d28deeb0457468ac3`；已运行 `scripts/check-ref.ps1` 并确认参考仓库工作树干净。未修改业务代码、已有测试或运行数据，未操作客户端。

本次以请求中的道具路由、装饰、房间设置、对局/结算、重连为主要检查路径，辅查元数据、会话清理及事务调用。没有对 clan/rank/bingo/强化等全部模块逐函数证明等价；这是一轮风险审查，不是“100% 复刻已验收”。没有确认新的 P0 字节错位，确认以下 7 项 P1。其中 6 项有隔离测试复现，第 6 项为直接源码差异。

## 发现

### R1 [P1，偏离基线] 手动装备休眠装饰没有激活 status

- Go：`internal/store/costume.go:92`、`:145`，普通装饰与僵尸皮肤两条路径。
- 基线：`src/manager/itemmanager.cpp:2834` 的 `OnCostumeEquip`，尤其 `2966–2974`。
- 期望：装备休眠副本时置 `inUse=1,status=1`；返回的道具状态同样应为已激活。基线还对 status=0 的道具调用 `ConvertDurationToExpiryDate`。
- 实际：Go 仅 UPDATE inUse，返回 Item 也只改 InUse。一个 3 天休眠道具变为 `inUse=1,status=0,expiryDate=3`；`readInventory` 不再把它视作休眠相对时长，立即从可见背包中过滤掉，loadout 却已经指向它。
- 复现：`TestReviewDormantCostume`。发放 `Reward{ID:471,Count:1,Duration:3,Dormant:true}`，调用 `ToggleCostume(1,2,"HeadCostume",0)`；得到 `status=0 expiry=3 visibleItems=1`（剩余可见项只有新角色礼包）。永久休眠道具也会保持错误的 status=0，只是不触发日期过滤。
- 建议：激活与 loadout 更新同一事务，处理 status 和时长转换；普通装饰与僵尸皮肤共用这段规则。
- **基线自身的额外问题**：OnCostumeEquip 修改了内存 expiryDate，但最终 UpdateInventoryItems 的 flag 没带 `UITEM_FLAG_EXPIRYDATE`；所以“应把转换后 expiryDate 持久化”也涉及修正基线缺陷，不能宣称基线已正确持久化。Go 缺 status 激活及出站时长转换则是明确差异。

### R2 [P1，偏离基线] 限时新装饰无法自动装备

- Go：`internal/store/costume.go:170`，关联 `internal/server/rewards.go:autoEquipCostumes`、`internal/store/purchase.go:grantItems`。
- 基线：`src/manager/itemmanager.cpp:AddItem`，`735–858` 附近的时长处理和空装饰槽自动装备分支。
- 期望：首件限时装饰在对应 loadout 为空时自动装备，并具有有效的绝对到期时间。
- 实际：prepareGrants/grantItems 将新装饰记为 Dormant，expiryDate 为相对天数；AutoEquipCostume 查询却只接受 `expiryDate=0 OR expiryDate>now`，将 3/7/30 天等新道具排除。永久装饰测试通过不能覆盖此情况。仅放宽 SELECT 仍不够，下面 UPDATE 也没有转换日期。
- 复现：`TestReviewTimedAutoCostume`；发放上述 3 天装饰后调用 AutoEquipCostume，返回 false，而对应装饰槽仍为空。

### R3 [P1，偏离基线] 合法的 ZombieSkin=0 被拒绝

- Go：`internal/store/costume.go:26`、`:161`。
- 基线：`src/manager/itemmanager.cpp:AddItem`、`OnCostumeEquip`；`src/definitions.h:1540` 的 `ZB_COSTUME_SLOT_COUNT_MAX=18`。类别以 ClassName 区分，不靠 skin 是否非零区分。
- 期望：僵尸皮肤类型 0 是有效槽位，合法区间对应 0..17。
- 实际：`zombieSkin<=0` 同时阻断手动和自动装备；反而放行了 18..255 这些超出基线槽数的值。
- 数据证据：当前嵌入 Item.csv 中 8009、8021 等共 5 件 ZombieSkinCostume 的 ZombieSkin 为 0。
- 复现：`TestReviewZombieSkinZero`；发放永久 8009，调用 ToggleCostume(...,"ZombieSkinCostume",0)，返回 `Item unavailable.`。

### R4 [P1，偏离基线] 基础奖励在计算加成前被封顶，少发高击杀奖励

- Go：`internal/server/host.go:494–509`、`:604` 的 matchBaseReward。
- 基线：`src/room/gamematch.cpp:531` 的 CalculateGameResult，先计算加成，最后 `604–610` 才将基础 Exp/Points 截为 9999。
- 期望：加成使用未截断的 `coefficient * max(kills,1)` 计算，再截断基础字段。整数除法仍逐项取整。
- 实际：Go 先把基础值截到 9999，后续加成据此计算，数据库实际发放也随之减少。
- 复现：`TestReviewBonusBeforeCap`；系数 100、200 击杀、持有激活角色且角色加成 50%。基线 `9999+20000*50/100=19999`；实际 endGame 入库 Exp=14998。Points 有同样差异。
- 建议：将“用于计算加成的原始基础值”和“结果包显示的封顶基础值”分开，保留防溢出验证；不要恢复不安全的整数乘法。

### R5 [P1，Go 重试路径缺陷] 结算失败后重试，装备百分比重复累计

- Go：`internal/server/host.go:496–510`、`:683–684`。
- 对照：基线在 CGameMatchUserStat 构造时初始化这些百分比；Go 额外支持结算事务失败后保留同一 match 并重试。
- 期望：同一 match 的重试重新计算出相同结果包，不依赖上次计算是否写库成功。
- 实际：endGame 重置 bonusExp/bonusPoints/classExp/classPoints，却不重置 itemExp/itemPoints。第一次结算数据库失败后再次 endGame，数据库总奖励重算为正确的一次值，但结果包里的装备百分比翻倍；多次失败可最终造成 U16 截断。
- 复现：`TestReviewBonusRetryPercent`；装备加成为 50%，在 UserCharacter 上注入 BEFORE UPDATE 失败触发器，第一次 endGame 失败；删触发器后重试，stat.itemExp=100，期望 50。
- 建议：每次计算入口清零所有派生字段，或用局部不可变结果对象计算后赋值。不能用 GoMatchSettlement 的数据库去重代替这项内存幂等检查。

### R6 [P1，未记录为客户端例外的基线偏离] 默认启用了基线不生效的装备加成

- Go：`internal/server/host.go:510`、`:665`。
- 基线：`src/room/gamematch.cpp:564–581`，物品加成循环在注释内；实际生效的是角色加成。配置存在 BonusPercentage.Items 不等于计算生效。
- 期望：按当前“100% 复刻”目标，默认不得因持有某物品多发经验/积分。
- 实际：Go 无条件调用 applyEquipmentBonus，配置匹配的激活道具会实际增加奖励。这不是已记录的客户端格式例外。
- 复现：角色基础经验 100，持有 InUse=1/Status=1 的 73，配置 Items[73].Exp=50。Go 增加 50，基线增加 0。已有 TestApplyEquipmentBonus 正好钉住了这条扩展行为。
- 建议：基线模式关闭它；如需求方批准扩展，再以明确配置启用并记录差异。稳定排序解决不了“本来不应生效”的问题。

### R7 [P1，Go 重连扩展缺陷] 重连后把离线时间补为对局任务分钟

- Go：`internal/server/reconnect.go:122` 保留 started；`internal/server/quests.go:370` 通过 now-started 追赶分钟数。
- 基线：`src/room/gamematch.cpp:206–230` 的 Connect/Disconnect；Disconnect 删除该用户的 match stat，重新加入不继承旧统计。当前 Go 的五分钟统计恢复是额外行为。
- 期望：即使保留重连能力，任务只能累计真实在线参战时间；不能将离线空档算作参战。
- 实际：started 和 questMinutes 原样恢复，下一次 tickQuestTime 自动补齐整个离线区间，足以触发任务完成与奖励快照。
- 复现：`TestReviewReconnectOfflineMinutes`；断线时已结算 1 分钟、started 距重连时 4 分钟，重连后立即 tick，额外得到 3 分钟任务进度，期望 0。
- 建议：保存断线时已参战时长，恢复时重建计时锚点或扣除离线间隔。是否继续恢复 kills/score/streak 则应单独决定并记入基线偏离清单；当前 b4cc191 的“退出放弃奖励”并不自动约束这条恢复路径。

## 已核对但不作为新 bug 的项目

- 普通装饰七槽映射 head/back/arm/pelvis/face/tattoo/pet 与基线一致。**卸下旧装饰不是清 status=0**：基线保留/设置 status=1，仅清 inUse，不能机械套用武器选中副本语义。
- kind 0 的 inventoryType=8 / Tattoo 路由确实存在于基线；游戏槽位在此路径传入 toggleCostume 后再减默认数量，未发现这条路径二次扣默认偏移。计数不能一概归为“基线总按 1”：它传给 UseItem 的 additionalArg，具体消费取决于道具类。
- ItemBox 两个 U16 的实参求值顺序以及 HostRestart 空存档按请求中的已知例外对待；没有再把它们作为猜测性 bug。
- 主机击杀事件的 24 字节位置数据和结果包各模式附加字节数，抽查未发现新增宽度错误。zsDifficulty 的输入 1/输出 9 属已记录例外，不要求增加无实机需求的客户端兼容分支。
- 元数据 65528 上限和四块截断问题在当前 HEAD 已修正，不重复报告上一轮问题。Hash 是基线的空 payload；客户端是否接受仍需单独实测，不宜默认为全开安全。
- CheckSettings、LoadNewSettings 中都有 validateSettings 条件。不能仅因 Go 提前返回就断言所有 sanitize 都应在开关关闭时执行；尤其 LoadNewSettings 的大段清理本身也受基线开关控制。
- PlayerCoop 需要纠正请求前提：基线虽有计算代码，但全 src 搜索只有成员声明和计算引用，没有加载/填充 bonusPlayerCoop 的代码；当前配置下它并非一个已经生效、仅 Go 漏掉的多人奖励。不把它列为确认的少发奖励。
- 基线 OnZbsResult 是只记录日志的空壳；不能看到 OnZBSWin 函数存在就推断 kind 19 必须发奖励。
- SettleMatch 在事务里插入唯一 (matchID,userID)，重复时跳过角色 UPDATE；现有回滚/并发测试通过。没有确认重复入库发奖问题，但 R5 说明包与内存派生状态还需独立覆盖。
- 事务入口及其直接调用抽查未发现“持有唯一 tx 又调用另一个自行 Begin 的 Store 方法”的阳性路径。clan 等所有间接 helper 调用没有做完整调用图证明，不把此结果当作死锁不可能发生的保证。
- serveClient 的 queue 关闭在 s.mu 下，常规 dispatch/计时/房间清理发送也在同一锁下；旧会话清理已有 `s.users[id]==c` 判断，未发现原请求猜测的直接删除新会话映射问题。

## 六个开放问题

| 问题 | 建议 |
| --- | --- |
| 等级表与客户端经验条不同 | 保持基线服务端等级表。视觉差异单独记录；没有先确定结果页字段含义与客户端资源前，不反推并替换服务端数值。 |
| 难度 padding | 固定当前客户端输入 1、输出 9；将输入解析和输出编码的职责分开，最好保存实际 difficulty 值而非复用一段双向 blob。现阶段无需旧版/新版自动猜包分支。 |
| normalize 清 flag、截断及尾部 | 不应把“C++ 越界读取”视为可复刻行为或容错正确性的证据。已知的单字节难度可精确处理；完整标量或计数数组截断应在应用状态前拒绝并回 notice，不能应用前半包。无载荷的保留 bit 则按明确 schema 处理，不能笼统按 len=0 判断。仅尾部多字节先保留日志和样本，不盲目扩展格式。 |
| send 队列满 | 不建议在全局 s.mu 内阻塞写队列，会造成全服停顿。保留有界失败策略，补 user/id/queueLen/room 等断连原因；只对明确可合并的状态更新做合并，不随意丢弃关键包。基线阻塞 socket 也可能阻塞事件循环，属于健壮性讨论。 |
| 混合 MapList/GameModeList | 在完成可逆语义 A/B 前，保留为明确命名的客户端测试配置，不声称是基线发布数据。发布默认资源需注明来源、哈希和与基线表的差异；菜单非空不是跨表引用完整性的证据。 |
| 新增幂等表 | 建议保留，但列为经需求方确认的持久化扩展：定义表版本、失败恢复、重复请求和来源约束，避免把数据安全改进藏在“逐字复刻”声明里。测试必须同时覆盖 DB、内存和出站包。 |

## 验证与后续

- `GOTOOLCHAIN=local` 下，现有 `go test ./...`、`go vet ./...`、`go build ./...` 完成且退出码为 0。build 输出过一次模块 stat cache 无写权限警告，不影响本次退出码。
- 使用 Go `-overlay` 加入诊断测试，未写入 internal 的实际测试文件。6 个复现断言按预期失败；这表示新断言发现缺陷，不表示已有测试原来就红。
- 诊断文件及 overlay 位于 `.dist/review-b4cc191/`，复现：

```powershell
$env:GOTOOLCHAIN = "local"
go test -overlay .dist/review-b4cc191/overlay.json ./internal/store ./internal/server -run '^TestReview' -count=1
```

- 建议补 FuzzItemRequest、FuzzMetadata；前者需构造合法认证/背包上下文并断言截断不产生部分写入，后者应包含 zip 边界、计数/枚举溢出、raw blob 类型校验与全开下发顺序。
- 修复优先级：R1/R2/R3（玩家实际装备）→ R4/R6（发奖数值）→ R5/R7（失败/重连路径）。本轮没有做跨 NAT、SSL、HLDS 或新的客户端实测。
