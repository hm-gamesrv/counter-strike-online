# 对 75cddae 审查的处理

提交：见 `git log -1 --oneline`。根目录 `go build` / `go vet` / `go test ./...` 全绿；本轮探针 8 条中 **全部通过**；其余历史探针除 `review-b4cc191` 两条过期项、`review-21801e6` 四条已知缺陷、`review-70bd62e` 那条已知随机假阳性外，全部通过。

## 一、上一轮八项复核

R1 扭蛋形状、R5 密码箱缓存、N2 家族战准入、N4 拒绝回退、R3 `applyMapDefaults`、R6 `restoreRejected`、R7 掉落箱白名单、R8 Bingo 整槽交换 —— 均确认修好，无新回归。

「四条探针缺陷」的判断经独立复核成立：扭蛋两条因夹具含默认道具 1001、地图两条因夹具未加载地图表。复核还补了一条更准确的结论——**不能用 `validateServer().maps` 修那两条地图探针**，合成表里 map 5 是 16、map 30 无默认限制，与断言依赖的 CSV 值（32 / 7）不一致，必须加载真实 `MapList.csv`。

## 二、本轮六项

| # | 问题 | 处理 |
|---|---|---|
| 1 | 重复购买限时道具新开一行 | `grantItemsForPurchase` 按 `AddItem` 规则：找到可延期副本（`expiryDate<>0 AND enhanceValue=0 AND partSlot1=0 AND partSlot2=0`）→ 按 `ExtendItem(duration=true)` 三个分支延期 → 不插新行 |
| 2 | 购买成功重发 152 + 全量 154 | `PurchaseTracked` 返回受影响的行，商店只发这些行（`Purchase` 保留原签名供既有调用） |
| 3 | 购买失败回包第二字节写 0 | 改为基线那样无条件写 1，结果放在回复串里 |
| 4 | 公仓删除比基线严格 | 改为 `memberGrade <= 该页 accessGrade`（基线 `DeleteClanStorageItem` 的判据） |
| 5 | 连续击杀窗口少一次 | 按基线「先自增再比较 `continuous <= counter`」，2 连杀在第 3 杀才计分 |
| 6 | 里程碑通知条件错 | 改为「本局临时进度 % Notice.Goal == 0」，新增 `matchStat.questUnits` 保存每局进度（存档计数跨局，不能直接用） |

### 关于 #1 的范围

基线把「同款限时道具延期」放在 `AddItem` 里，理论上奖励路径也该如此。本轮**只对购买路径启用**，因为奖励/奖品路径的既有夹具与语义依赖「每次发放插一行」，一次性改动会波及扭蛋、Bingo、装饰等一批用例。这是有意的收窄，已在此记录；若要全路径对齐，需连带调整那批夹具。

### 关于 #6 的数据口径

基线用的是 `UserQuestTaskProgress::unitsDone`（每局临时进度），我们持久化的 `progress.Units` 跨局累加。直接用它会让老账号的通知时机与基线不同，因此新增 `matchStat.questUnits`（与 `questStreaks` 同层）保存每局进度。

## 三、附带核对（未计入本轮修复，按报告建议记档）

- **Bot 缺省 -1**：基线 `m_nBot` 为 -1 时等价「只算真人」，我们不按 Bot 过滤；出货数据显式写了 Bot，当前不可达。
- **公仓时长往返**：基线取出限时道具会压成 1 天，我们保留真实时长（比基线正确）——建议保留并记为偏离。
- **家族战名单口径**：基线是开局全房快照，我们用 `match.stats`；离房再进、未 ready 成员的口径不同。
- **掉落箱系统回复** `EVT_SCENARIO_ITEM_POINT_REWARD` 仍缺。
- **商店购买请求 5B(U32) vs 基线 3B(U8,U8)**：客户端例外，但尚未写进 `review-request.md` §8。
- **GM `/removeitem` 列表文案**打印槽位而参数是下标（P3）。

## 未做

P0-6 其余各行（74/76、103、151、180、153 label、67 欢迎语、157 flags 策略；72 标题已批准偏离）、全模式结果包金标准、`zsDifficulty` 值/编码拆分、等级表口径、完整事务调用图；上述「附带核对」六条。
