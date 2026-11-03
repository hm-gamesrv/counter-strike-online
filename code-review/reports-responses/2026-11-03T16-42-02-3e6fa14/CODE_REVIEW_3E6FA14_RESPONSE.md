# 对 3e6fa14 复核与整体检查的处理

提交：`9d33c2d`（已推送，工作树干净）。`go vet`、`go test ./...` 全绿；七批 overlay 探针全部通过。

## 复核报告（1 P3）

| 问题 | 处理 |
|---|---|
| GM `/removeitem` 删除背景没同步在线缓存 | `DeleteItems` 之后调用 `refreshCharacterCache`，与分解路径一致；`nameplateID` 变化时 `broadcastUserInfo` |

## 整体检查（1 P2）

| 问题 | 处理 |
|---|---|
| 选中的武器自然到期后没有接替副本 | 把接替逻辑抽成 `promoteItemSelection`（`references.go`），`DeleteItems` 与 `ExpireItems` 共用；到期扫描按用户清理引用并接替选择 |
| 在线客户端看不到到期引起的背包变化（同一发现的隐含部分） | 新增 `ExpireItemsWithUsers` 返回受影响用户；服务端对在线用户重发 154 背包与 157 角色信息。`ExpireItems` 保留两返回值签名，避免破坏既有调用与审查探针 |

## 实现说明

- 接替统一在"整批删除完成后"执行，避免选中同批里马上被删的副本。
- `ExpireItemsWithUsers` 是新增方法而不是改签名，原因是审查者的 store 探针直接调用 `ExpireItems(now)`，改签名会让探针编译失败。
- GM 路径复用同一个 `refreshCharacterCache`，后续若还有第三个删除入口，只需接这一个调用。

## 新增回归

- `internal/store/references_promote_test.go::TestExpiryPromotesRemainingWeapon`
- `internal/server/review_overall_test.go::TestGMRemoveItemRefreshesCache`

## 仍未做

`FuzzItemRequest`/`FuzzMetadata`、`zsDifficulty` 解析/编码拆分、等级表口径、完整事务调用图证明、结果包逐字段金标准。
