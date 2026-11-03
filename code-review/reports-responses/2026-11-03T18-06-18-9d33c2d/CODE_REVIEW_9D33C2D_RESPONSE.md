# 对 9d33c2d 复核与整体检查的处理

提交：`ccfedd0`（已推送）。`go vet`、`go test ./...` 全绿；两轮新探针（`review-9d33c2d`）均通过。

## 复核报告（1 P2）

| 问题 | 处理 |
|---|---|
| 到期刷新只重发剩余库存，没有清掉客户端的已删除槽位 | `Store.ExpireItemsChanged` 每个受影响账号返回 `Items`：先是被删槽位的墓碑（`ID=0` → `U16(slot)+U8(0)`），再是剩余库存，与分解/消耗路径的 154 补丁同形；服务端改用它下发，不再走"重发 152 + 剩余库存" |

## 整体检查（1 P2）

| 问题 | 处理 |
|---|---|
| 登录中的到期扫描提前发送背包（9d33c2d 引入的顺序回归） | `expireItems` 拆成 `sweepExpiredItems(now, skip)`；`loginPackets` 把正在登录的账号 ID 作为 skip 传入。数据库清理照常，其他在线会话照常收补丁，正在登录的账号交给登录链后面的正常背包初始化 |

## 实现说明

- **为什么不能靠"重发剩余库存"表达删除**：154 是按槽位的增量帧（`U16(slot)+present`），没有"整包替换/清空"标记。基线 `packetmanager.cpp:720` 的 `SendInventoryRemove` 也是显式发 `U16(slot)+U8(0)`。所以到期必须发墓碑。
- **为什么不再发 152**：152 是登录时的默认道具表，运行期本来不该重发；9d33c2d 里重发它是我为了让客户端"看到变化"加的临时手段，现在有墓碑了就不再需要，行为也更贴近基线的 `SendInventoryRemove`。
- **skip 而不是"ready 标志"**：探针 `TestReview9dExpirySendsRemovedSlot` 直接把客户端放进 `s.users` 再调 `s.expireItems`，靠 `ready` 之类的握手标志会把这条推送一起关掉。按"正在登录的账号"排除既满足包序要求，也让运行期扫描的行为保持不变。
- **重连同样受保护**：重连走同一条 `/login` → `loginPackets`，终止旧会话在前、入表在 `loginPackets` 之前，因此 skip 生效。
- **兼容性**：`ExpireItems(now) (int, error)` 与 `ExpireItemsWithUsers` 都保留为薄包装，审查者的 store 探针继续可编译。

## 新增回归

- `internal/server/expiry_sync_test.go::TestExpiryPushClearsDeletedSlot`：到期补丁同时含 `slot=7,present=0` 与存活的 `slot=8`，并整体校验帧尾无多余字节。
- `internal/server/expiry_sync_test.go::TestLoginSweepKeepsInventoryAfterUserStart`：登录包序（150 之前没有 152/154）、过期行确实已删、另一个在线账号仍收到含墓碑的补丁。

## 探针复跑

九批 overlay 全跑。`review-b4cc191` 的 `TestReviewBonusRetryPercent` 在未打开 `ItemBonusEnabled` 时断言 `itemExp==50`，而 R6 起该循环默认关闭（基线 `serverconfig.cpp` 只解析 `BonusPercentage.Items`，结算里没有使用）。该探针写于 R6 之前，属过期期望；同一幂等性由打开开关的 `TestEquipmentBonusRetryIsIdempotent` 覆盖，未改行为迁就探针。

## 仍未做

`FuzzItemRequest`/`FuzzMetadata`、`zsDifficulty` 解析/编码拆分（保存真实值）、等级表口径、完整事务调用图证明、结果包逐字段金标准；以及尚未获准的"库存行消失后级联状态"全量审计。
