# 库存行消失后的级联状态：全量梳理

目的：把"库存行被删除（或减到 0）"的入口列全，逐个核对四件事——外部引用、剩余副本的选择接替、在线缓存、下发给客户端的包。上一次审查按入口逐个发现问题的收敛速度太慢，这里改成一次列全。

## 会删除库存行的入口

| # | 入口 | 位置 | 外部引用 | 选择接替 | 在线缓存 | 下发 | 结论 |
|---|---|---|---|---|---|---|---|
| 1 | 分解 | `store/DeleteItems` | ✅ | ✅ | ✅ | ✅ 墓碑+剩余 | 已完备 |
| 2 | 自然到期 | `store/ExpireItemsChanged` | ✅ | ✅ | ✅ | ✅ 墓碑+剩余 | 本轮修复 |
| 3 | GM `/removeitem` | 复用 `DeleteItems` | ✅ | ✅ | ✅ | ✅ | 已完备 |
| 4 | 公仓存入 | `store/GiveClanItem` | ✅ 本轮 | ✅ 本轮 | ✅ 本轮 | ✅ 墓碑 | **本轮修复** |
| 5 | 强化材料 | `store/EnhanceItem` | —（材料是武器） | ✅ 本轮 | — | ✅ 墓碑 | **本轮修复** |
| 6 | 消耗 | `store/ConsumeItem` | — | — | — | ✅ 墓碑 | 入口仅掉落箱与喇叭道具（`host.go`、`lobby.go`），目录上不可装备 |
| 7 | 开箱钥匙 | `store/OpenItemBox` | — | — | — | ✅ 墓碑 | 要求 `LotteryKeyItem` 类别 |
| 8 | 续期消耗 | `store/ExtendItem` | — | — | — | ✅ 墓碑 | 扩展器是兑换道具 |
| 9 | 武器喷漆 | `store/ApplyWeaponPaint` | — | — | — | ✅ 墓碑 | 喷漆道具不是外观引用 |
| 10 | 武器部件 | `store/AttachPart` | — | — | — | ✅ 墓碑 | 部件道具不是外观引用 |
| 11 | 改名卡 | `store/RenameCharacter` | — | — | — | ✅ 墓碑 | 要求 `status=0`，不可能被选中 |
| 12 | 奖励来源消耗 | `store/GrantReward`（`SourceSlot`） | — | — | — | ✅ 墓碑 | 要求 `inUse=1 && status=0`，是兑换券 |
| 13 | 插件材料 | `store/SetAddonsConsuming` | — | — | — | ✅ 墓碑 | 材料 ID 来自 `AddonMaterial` 配置，客户端只允许插件材料 |
| 14 | 公仓取出/删除 | `TakeClanItem`/`DeleteClanItem` | 不涉及玩家库存行 | — | — | — | 不适用 |

## 本轮改动

- **公仓存入**（`store/clan_storage.go`）：存入路径不检查 `status`/`inUse`，任何在背包里的道具都能存进公仓，包括正在装备的背景、特效、装饰和选中的武器。删除行之后补 `clearItemReference` 与 `promoteItemSelection`，服务端 `clanStorageGive` 在发完 154 补丁后调用 `refreshCharacterCache`（`nameplateID` 变化时广播 157）。
- **强化材料**（`store/enhance.go`）：材料是普通库存行，可以是某把武器的"选中副本"。删除整叠之后调用 `promoteItemSelection`，把选择交给同 ID 的剩余副本；材料是武器，不会成为背景/装饰引用，所以没有加 `clearItemReference`。

## 已核对但判定不改的

- 第 6～13 项都是"消耗一个特定用途道具"，客户端把可用道具限定在对应类别（钥匙、扩展器、喷漆、部件、改名卡、兑换券、插件材料），这些类别在目录里不会被选作背景/特效/装饰，也不会成为武器选择。加 `clearItemReference` 只是多几条 UPDATE，但会掩盖真正的类别判断缺失，因此留原样并在此记录判断依据。
- 公仓取出、购买合并堆叠、启用/停用装饰只是写入或改 `status`，不会让行消失。

## 顺带发现（未修）

- `UserInventory` 在 `(userID, slot)` 上没有唯一约束：测试里插两条同槽位的行会让 `EnhanceItem` 的 `RowsAffected() != 1` 判定失败。正常流程的两个写入点（`grantItems`、`TakeClanItem`）都按槽位查重，所以线上不会出现；这里只记录该前提，不改表结构（改约束会影响基线数据库导入）。
