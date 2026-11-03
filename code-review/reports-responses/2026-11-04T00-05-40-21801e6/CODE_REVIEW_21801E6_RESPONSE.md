# 对 21801e6 审查的处理

提交：见 `git log -1 --oneline`（已推送）。根目录 `go build` / `go vet` / `go test ./...` 全绿；`review-21801e6` 十条探针中 **6 条通过**，其余 4 条的失败原因见下文「探针缺陷」；其余十二批历史探针除 `review-b4cc191` 两条已知过期探针外全部通过。

## 一、上轮修复复核

R2 已修；R1 的「删位」方向已修，本轮补上了「加位」方向（新发现 3）；N1 分队/重排已修，补上对局中准入与拒绝回退（新发现 2、4）；N2 计数/事务已修，奖品内容与 166 分支改为本轮修正（新发现 1、5）。

## 二、本轮八项

| # | 级别 | 处理 |
|---|---|---|
| 1 | P1 | 扭蛋奖品绕过目录规则 → `Store` 增加 `ItemRule` 目录钩子（服务端在加载 Item.csv 后安装），`TickGachapon` 按 `AddItem` 的规则落库：可数带按钮的道具 `status=0,inUse=1` 并叠进已有行；只回受影响的行 |
| 2 | P1 | 家族战对局中允许非参战者加入 → 加入路径新增 `familyBattleParticipant`（以 `match.stats` 为参战名单，重连保留），不在名单者按基线回 `You are not participating in this Family Battle.` |
| 3 | P1 | 换地图不加载地图派生设置 → 新增 `applyMapDefaults`，复刻 `roomsettings.cpp:2169–2192/2313–2320`：容量随 `max_player`、地图默认武器限制、`ball_default`、`isZbCompetitive`（仅 282）、`mapId2`，并把 `MAPPLAYLIST` 位一并送出；**该段不受 `ValidateSettings` 门控**。`writeUpdate` 增加 `added` 掩码，服务器补位的字段也进广播 |
| 4 | P2 | 家族战启用被拒不给房主回退 → 新增 `familyBattleSettings`（缺军团成员 / >2 军团 / 人数超上限）与 `revertRoomSettings`（65/4 + 全零掩码 = 17 字节），并按基线发 `ROOM_SETTING_FAILED_*` |
| 5 | P2 | 扭蛋 166 不刷缓存不发 157 → 166 走 `reloadCharacter` + `broadcastUserInfo`（`UpdatePasswordBoxes` → `UpdateClientUserInfo` 的等价物） |
| 6 | P2 | 被拒绝的设置值删掉房间原值 → 新增 `settings.restoreRejected`：请求里带了、清洗后消失、且房间原本有值的字段恢复旧值（基线只清更新位）。顺带把 `sd` 的「模式不使用」从删字段改为写 0（与基线一致） |
| 7 | P2 | 掉落箱缺场景模式白名单 → `grantDropBox` 先判 15/17/26/28，其余模式记日志并忽略 |
| 8 | P2 | Bingo 洗牌拆散号码与已开状态 → 改为整槽交换 |

### 关于 #1 的实现位置

基线 `AddItem` 的「可数带按钮」判定依赖物品目录（Category/UseType），而 `store` 原本没有目录。新增 `Store.ItemRule func(id int) (category, useType int)`，服务端在 `loadItems` 之后安装；`grantItemsTracked` 之外只有扭蛋用到它。没有钩子时按「可数」处理——两个扭蛋奖品都是这一类（166 更早就分流为密码箱）。

规则本身抽成 `store.CountableWithButton` / `store.Equippable` 两个函数，避免与 `prepareGrants` 各写一份。

### 关于 #3 的验证

探针在 `newLoginExtraServer` 夹具上跑，而该夹具**没有加载地图表**（`s.maps` 为空），所以两条地图探针在当前夹具下不可能通过。我用同一个夹具补上真实 `MapList.csv` 单独验证过：

```
map 5 : map=5  mapId2=5  maxPlayers=32
map 30: map=30 mapId2=30 maxPlayers=20 restriction=7
```

与报告期望值完全一致。

## 三、探针缺陷（四条）

四条残留失败的探针不是产品行为问题：

- `TestReview218Gachapon5101IsUsable` / `…StacksExistingCopy`：断言 `len(items) == 1`，但 `newLoginExtraServer` 创建角色时会写入默认道具 1001，库存本来就是两行；失败信息里的两行分别是 1001 与 5101。实测行为已经正确：新 5101 为 `status=0,inUse=1`，重复中奖合并为 `count=2` 的单行。
  建议改成「按 itemID 过滤后的行数」。
- `TestReview218MapUpdateReloadsMapDerivedSettings` / `…AppliesWeaponRestriction`：断言来自 `MapList.csv` 的 `max_player` / `weapon_restrict_deault`，但夹具没有加载地图表，`s.maps` 为 nil。
  建议在 `review218Room` 里补 `s.maps, _ = loadMapRows(<数据目录>)`（或复用 `validateServer()` 的地图表）。

上一轮的 `TestReview70KillPaysGachaponAtCounterBoundary` 也是同类问题（只统计 `UserInventory`，命中 166 时必红），报告已指出，本轮一并记录。

## 四、夹具调整

- `TestHostDropBoxGrantsReward`：房间模式改为 15（场景），否则按新白名单不发奖，测试会等在队列上。
- `TestSuperRoomFlagComesFromHostItem`：模式不使用 sd 时期望「字段存在且值为 0」，不再期望字段消失。

## 未做

P0-6 其余各行（74/76 顺序、103 位置、151 条数、180 位置、153 label、72 标题常量、67 欢迎语、157 flags 策略）、全模式结果包金标准、`zsDifficulty` 值/编码拆分、等级表口径、完整事务调用图；`regroupFamilyBattle` 仍按新 `clan1` 而非基线的旧设置指针（已记档的有意偏离）。地图 282 的 `LoadZbCompetitiveSettings` 子块未实现。
