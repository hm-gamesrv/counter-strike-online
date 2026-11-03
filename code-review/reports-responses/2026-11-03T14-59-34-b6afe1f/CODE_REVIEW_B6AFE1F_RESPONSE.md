# 对 b6afe1f 复核与 f6eda20 整体审查的处理

提交：`154b2fe`（已推送，工作树干净）。`go vet`、`go test ./...` 全绿；四批 overlay 探针全部通过。

## b6afe1f 复核（1 P1 + 1 P2）

| 问题 | 处理 |
|---|---|
| 选择型离线奖励锁死登录 | `grantCatalogRewardNotice` 把"需要选择 UI / 奖励定义缺失 / 目录缺项"包装为 `errRewardDeferred`，`rewardRejection` 认它 → 保留通知、继续登录；数据库错误仍上抛 |
| 到期通知删错行 | 新增 `ExpiryNoticeRows`（rowid+itemID）与 `ClearExpiryNotices(id, rowIDs)`，只删真正下发的行；非法 itemID 行直接清掉。测试改用乱序插入并断言剩余行身份，不再只数行数 |

## f6eda20 整体审查（4 P1 + 2 P2）

| 问题 | 处理 |
|---|---|
| 专服轮换无法开下一局 | `startGame` 先复用仍归本房间的 `r.dedicated`（仍在池中且 `room==r.id`），没有再向池申请 |
| 限时外观启用后消失 | 新增 `activateExpiry`：`LobbyBG`/`zbRespawnEffect`/`CombatInfoItem` 启用时把相对天数换成绝对分钟并置 `status=1` |
| 保护列表按槽位判断 | `DeleteItems`/`ConsumeItem` 改为比较 `item.ID`；补了"受保护 ID 在别的槽位"的两条用例 |
| ZB 插件失败不回滚 | 先完整解析 + 尾部校验，再用 `SetAddonsConsuming` 单事务扣材料 + 替换 `UserAddon`；截断/尾部/写库失败都不扣道具 |
| 同款装饰两个副本同时装备 | 替换分支去掉 `oldID != target.ID` 条件，按槽位排除目标后清旧副本（普通 + 僵尸皮肤） |
| 已卸下装饰续期按天算 | `ExtendItem` 只有"未激活"（status=0/inUse=0/相对天数）按天加，其余按 `days*1440` 分钟 |

## 实现说明

- 到期通知的删除不再"按数量取前 N 行"，而是发送时收集 rowid 再精确删除；`ExpiryNotices(id) ([]int, error)` 保留为兼容包装（探针用到）。
- 插件扣材料从"逐条 ConsumeItem 独立提交"改成"整包一个事务"，因此 `SetAddons` 的旧入口保留给内部调用，但 `setZBAddons` 不再使用它。
- 未改动协议字节：这批全是持久化语义与事务边界。

## 仍未做

`FuzzItemRequest`/`FuzzMetadata`、`zsDifficulty` 解析/编码拆分、等级表口径、完整事务调用图证明、结果包逐字段金标准。
