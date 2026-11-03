# 对 4308aae 审查的处理

提交：见 `git log -1 --oneline`（已推送）。`go vet`、`go test ./...` 全绿；新探针（`review-4308aae`）三条全部通过；十批历史探针全跑（`review-b4cc191` 两条已知过期探针除外）；两服黑盒 5 例通过，元数据 32/32 内容一致。

## 一、上轮修复复核

R1–R5 与元数据顺序复核均通过，无新回归。历史两条过期探针（内存职业库存、装备加成默认开）的前提已被后续决定取代，不算回归。

## 二、本轮发现

| 编号 | 问题 | 处理 |
|---|---|---|
| N1 | host 场景消耗入口能删除已选中的武器 | `hostRequest` kind 100 改为复刻 `CItemManager::UseItem`：先 `CanUseItem`（`count<>0 && status==0`，选中武器直接拒），再按 ClassName/Category 分派；这条消息不带 additionalArg，所以 `Requirement`/`RewardItem`/`NicknameChange` 与 `LotteryKeyItem` 都不消耗，676 缺目标槽同理，只有 `Category<9 || ==11` 走激活（`ChangeItem(ActivateItem)`）。回复包 `68/100 + userID + itemID + 255` 在道具存在时始终发送，与基线 `OnUseInGameItem` 一致 |
| N2 | 大厅人数上限没算战队等可选字段 | `maxUserInfoSize` 改为**最坏角色**：最长角色名 + nameplate(5B) + clan(8B + 20B 名 + 3B) + tournament(1B) + 固定 titles。`maxLobbyPlayers` 因此从 202+ 降到 **181**（`maxUserInfoSize=341`），配置的 `MaxPlayers=100` 仍远在安全区；`validateRoomFrame` 复用同一预算，也随之收紧 |
| N3 | 公仓存入后接替副本只落库不下发 | `GiveClanItem` 在促销后重读库存，返回"墓碑 + 剩余库存"，与分解路径同形；服务端照此下发 154，客户端能看到接替副本的新选中状态 |

### 实现要点

- **N1 为什么不能只加 `clearItemReference`**：基线的拒绝发生在 `CanUseItem`，删除本身就不该发生。补齐引用清理只会让"非法删除"变得一致。`TestHostScenarioItemUse` 原来钉的是旧行为（status=1 也扣 1 件），已按基线改写：refuse 分支（库存不动、仍回包）、activation 分支（category<9 的道具被置为使用中并下发补丁）、未知道具分支（无包）。
- **N2 上限降到 181 的影响**：`.data/ServerConfig.json` 的 `MaxPlayers` 是 100，无需改动；若有人把上限配到 181 以上，启动时会被 `validateWireLimits` 拒绝并给出允许区间。`TestRoomSettingsReserveSpaceForEveryMember` 的夹具同时改成最坏角色，否则它测的边界和校验用的预算不是同一个。
- **N3 的补丁形状**：与 `DeleteItems`、`ExpireItemsChanged` 保持一致——先墓碑、再剩余库存，客户端按槽位增量应用。`clan_storage_test.go` 的断言改为"首条是墓碑，且补丁里不再有该槽位的有效条目"。

## 三、旧差异表的方向更正

4308aae 轮的实测纠正了 `review-request.md` §5 P0-6 里若干写反的方向，已按实测更新：

- `103`：我们索引 43（背包前），基线索引 59（`69` 之后）——旧表写反。
- `151`：我们 1 条，基线 2 条——旧表写反。
- `180`：我们在 `153`/`151` 之前，基线在之后——旧表写反。
- `153`：我们 314B(label=`tester1`)，基线 412B(label=`test_lobby`)——长度差来自 label 常量，不是人数。
- `72`：我们发 `Recommended`/空，基线发 `Test`/`Test2`——是标题常量差异（`shop.go:203` vs 基线 `packetmanager.cpp:2142`），不是同一份 JSON 读出不同结果。
- `67`：我们 2 条，基线 3 条（多一条 168B 固定欢迎语）。

这些仍是待处理项，本轮未改动；`157` 的 flag 策略需要明确"批准为例外"还是"补齐所有可选段"。

## 本轮未做

包序差异（74/76、103、151、180）、153 label、72 标题常量、67 欢迎语、157 flags 策略；`FuzzItemRequest`/`FuzzMetadata`；全模式结果包金标准；`zsDifficulty` 值/编码拆分；等级表口径；完整事务调用图。
