# 对 33e9ec6 复核与整体检查的处理

提交：`3e6fa14`（已推送，工作树干净）。`go vet`、`go test ./...` 全绿；六批 overlay 探针全部通过。

## 复核报告（2 P2）

| 问题 | 处理 |
|---|---|
| 数据库清了背景引用，在线缓存仍下发旧背景 | 新增 `refreshCharacterCache`：重读 `Store.Character`，`nameplateID` 变化时 `broadcastUserInfo`。分解入口在 `DeleteItems` 后调用；到期扫描在 `removed>0` 时对全部在线会话刷新 |
| 同款休眠副本让已到期的装备引用残留 | `clearItemReference` 与 `AutoEquipCostume` 的占用判定改为"存在 `inUse<>0` 且仍有效的副本"；休眠/过期副本不再占位，loadout 会被清掉 |

## 整体检查（1 P2）

| 问题 | 处理 |
|---|---|
| 删除选中武器副本后没有接替选择 | `DeleteItems` 在整批删除完成后，对每个被删的 `status=1` 副本，找同 ID、`inUse<>0`、有效的副本置 `status=1`（对应基线 `RemoveItem`），随返回值下发；`hostInventory` 不再漏发 |

## 实现说明

- 缓存刷新只比较 `nameplateID`（当前缓存里唯一相关的字段）；特效列不进入 `client` 缓存，客户端下次请求角色信息时会拿到新值。
- 到期扫描的刷新放在 `removed>0` 分支，正常无到期时不会多读数据库。
- 武器接替在"整批删完"之后执行，避免选中同一批里马上要删的副本。

## 新增回归

- `internal/store/references_promote_test.go`：`TestDeletingSelectedWeaponPromotesCopy`、`TestDormantCopyDoesNotKeepLoadout`。
- `internal/server/review_overall_test.go`：`TestRemovalRefreshesCharacterCache`。

## 仍未做

`FuzzItemRequest`/`FuzzMetadata`、`zsDifficulty` 解析/编码拆分、等级表口径、完整事务调用图证明、结果包逐字段金标准。
