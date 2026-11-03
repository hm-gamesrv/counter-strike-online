# 对 154b2fe 复核与整体审查的处理

提交：`33e9ec6`（已推送，工作树干净）。`go vet`、`go test ./...` 全绿；五批 overlay 探针全部通过。

## 复核报告（1 P1，154b2fe 引入的回归）

| 问题 | 处理 |
|---|---|
| 1 件插件材料可用 6 次 | `SetAddonsConsuming` 删除整叠后把快照 `Count` 置 0，并要求 DELETE/UPDATE 恰好影响 1 行；同一请求中重复的 ID 不再匹配已删除的行 |

## 整体审查（2 P2，3 条复现）

| 问题 | 处理 |
|---|---|
| 删除库存行不清理选择引用 | 新增 `clearItemReference`：同一事务内清理 `UserCharacter.nameplateID`、`UserCharacterExtended.zbRespawnEffect/killerMarkEffect`、`UserCostumeLoadout` 七槽、`UserZBCostumeLoadout`；仅当同 ID 已无剩余副本时才清（仍有副本时引用依旧有效） |
| 到期装饰残留引用挡自动装备 | `AutoEquipCostume` 先确认 loadout 指向的物品仍有有效库存，否则清掉该槽继续 |
| 审批越战队人数上限 | `ApproveClanJoin` 在事务内重查容量，满员返回 `-6` 并保留申请；服务端映射 `CSO_CLAN_JOIN_MAX_MEMBER` |

## 实现说明

- `clearItemReference` 在 `DeleteItems`（分解）和 `ExpireItems`（到期扫描）里都在删除行的同一事务调用，避免"行删了、引用还在"。
- `AutoEquipCostume` 的僵尸皮肤分支同样处理：`UserZBCostumeLoadout` 指向的物品没有有效库存时删行后继续。
- 战队审批的容量检查放在"删除申请 + 插入成员"之前，满员时申请保留，供以后腾出位置再批。

## 新增回归

- `internal/store/references_test.go`：`TestDeletingNameplateClearsSelection`、`TestExpiredCostumeDoesNotBlockAutoEquip`、`TestClanApprovalRespectsCapacity`（后者还断言被拒的申请没有丢）。
- `internal/server/review_overall_test.go`：`TestRepeatedAddonIDConsumesOneUnit`。

## 仍未做

`FuzzItemRequest`/`FuzzMetadata`、`zsDifficulty` 解析/编码拆分、等级表口径、完整事务调用图证明、结果包逐字段金标准。
