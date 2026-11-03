# 对 44b5eed 两份审查的处理

处理提交：`b6afe1f`。工作树干净，已推送；`go vet`、`go test ./...` 全绿；两轮 overlay 探针全部通过。

## 复核报告（CODE_REVIEW_44B5EED.md）

| 编号 | 结论 | 处理 |
|---|---|---|
| P1 奖励回包带旧到期时间 | 已修 | `finishRewardClaim` 在自动装备后按槽位从数据库重读（`refreshItems`），154 包里的 `status/expiry` 与落库值一致；被消耗的来源仍保留墓碑条目。没有按 itemID 批量修补，同 ID 多副本不会互相覆盖。 |

## 扩大范围审查（CODE_REVIEW_EXPANDED_44B5EED.md）

| 编号 | 结论 | 处理 |
|---|---|---|
| 1 Bingo 发奖与状态分离 | 已修 | 新增 `Store.OpenBingoAwards`：开格号码、完成线标记、全部奖品同一事务；满包整步回滚（号码不消耗、线不标记），腾出空间后重试不会重复也不会丢 |
| 2 离线奖励重登重复发 | 已修 | 新增 `Store.GrantRewardNotice`：消费 `UserRewardNotice` 行与发奖同一事务；物品类拒绝（满包/等级/不可用）保留通知待下次登录，数据库错误照旧上抛 |
| 3 集字满包永久丢奖 | 已修 | `SetWeaponReleaseCharacterReward`：最后一个字母、`opened` 标记与奖品同一事务；满包回滚，字母不消耗，可重试 |
| 4 开箱绕过归一规则 | 已修 | `openItemBox` 先过 `prepareGrants`，与商店/奖励共用 `Exclusive/Stackable/Dormant`；同款武器不会同时选中两份 |
| 5 强化漏掉 Ammo | 已修 | `rand.IntN(5)` → `rand.IntN(6)`（基线 `Randomer(5)` 的分布是闭区间 0..5），Ammo 索引 5 回到候选 |
| 6 公仓拒收休眠道具 | 已修 | 新增共享谓词 `liveItemSQL`（永久 / 未过期 / 仍为相对天数的休眠副本），`readInventory`、`GiveClanItem`、公仓查重统一使用 |
| 7 到期通知截断后清空余项 | 已修 | `ClearExpiryNotices(id, delivered)` 只删下发的行数；260 条首登发 255 条后余 5 条。同时先过滤非法 itemID 再计数 |

## 关于"基线继承缺陷"

第 2、3 项（以及 Bingo 的部分语义）审查已注明基线同样不安全。这里仍然按事务化修掉了，属于**在保持协议/数值不变前提下的健壮性增强**，不是协议偏离；已写入 `MIGRATION.md` 的对应条目。第 1 项本来就是 Go 侧新实现的 Bingo 流程，按新实现的标准修。

## 实现取舍（与建议不同之处）

- 审查建议"整次开格的全部奖励、号码状态与奖品标记同一事务，或在状态事务内生成持久化待领奖记录"——选了**前者**：事务回滚 + 客户端提示 "Inventory is full."，不引入新的待领奖表。理由是复用现有 `GoRewardClaim` 语义会扩大状态面，而回滚方案已经满足"不重复、不丢、可重试"。
- 离线奖励同理：用事务消费通知行，没有额外生成凭据。

## 仍按原计划保留的

1. `FuzzItemRequest` / `FuzzMetadata`：仍未补。
2. `zsDifficulty` 的解析/编码职责拆分：仍复用 9 字节 blob。
3. 等级表口径：保持基线服务端表，不反推客户端 `9000`。
4. 事务死锁：仍只是抽查，没有完整调用图证明。
5. 结果包字段的逐字段金标准测试：仍未补。

## 测试

- 新增 `internal/server/review_expanded_test.go`：通知余量、离线奖励幂等、集字满包重试、强化候选覆盖、奖励回包到期值。
- 新增 `internal/store/clan_dormant_test.go`；`bingo_test.go` 增加满包回滚重试用例。
- `itembox_test.go` 的夹具补上了奖品道具 1234（归一化现在要求道具在目录中，缺少时按上游 `AddItem` 的做法拒绝发放）。
