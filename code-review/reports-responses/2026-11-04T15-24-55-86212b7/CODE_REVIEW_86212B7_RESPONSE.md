# CODE_REVIEW_86212B7 响应

审查报告：`CODE_REVIEW_86212B7.md`（2026-11-04，HEAD 86212b7，探针 `probes/review-86212b7/`）。
这份报告当时归档了但**没有及时响应**，是这一轮的补课：三项发现都在 2026-11-05 复现并修复。

## 状态

| 编号 | 结论 | 现状 |
|---|---|---|
| R1 (P1) 堆叠/延期返回的部分 Item 被当完整 154 下发 | 采纳 | 已修 |
| R2 (P2) 局内通知计数在事件重放时重复累加 | 采纳 | 已修 |
| N1 (P1) 在线目录奖励与 GM 发奖不刷新角色缓存、不发 157 | 采纳 | 已修 |

复现命令（修前三条全失败）：

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay code-review/probes/review-86212b7/overlay.json ./internal/server ./internal/store -run '^TestReview862' -count=1 -v
```

## R1 购买补丁只填了部分列

**问题确认。** 154 是按槽位增量，但每条非墓碑记录带的是**完整**道具字段（`writeItem` 会写出 Status/InUse/Obtain/Expiry/Paint/Lock…）。堆叠分支只填 Slot/ID/Count，延期分支漏了 Status/Paint/Lock，客户端据此把道具当成未使用、无涂装、无锁。

**修法：** 新增 `readInventorySlot(tx, userID, slot)`，在同一事务里把刚写的行整行读回；堆叠、延期、新插入三个分支都改用整行，不再手拼部分字段。没有退回“每次发整个背包”。

**证据：** `TestReview862PurchasePatchMatchesStoredRow/{extend,stack}` 通过（修前 patch 的 Status/Paint/Lock 与 DB 不一致）。

## R2 重复事件推进了局内计数

**问题确认。** `RecordQuestProgressOnce` 命中重复 eventKey 时提交事务并返回成功，但没说“本次未应用”，调用方随即无条件 `questMatchUnits += delta`，于是持久化进度不变、内存进度与里程碑通知却变了。

**修法：** 拆出 `RecordQuestProgressApplied`，多返回一个 `applied bool`；`RecordQuestProgressOnce` 保留原签名作为包装（避免改动既有调用点）。通知只在 `applied` 时推进。

**证据：** `TestReview862RepeatedQuestEventDoesNotRepeatNoticeProgress` 通过（修前 `units=2 notices=1 persisted=1`）。

**未做：** 报告提到的“局内计数未进重连快照”仍成立——重连后通知计数从头开始。这是派生状态而非持久化进度，且基线没有等价概念，暂时记档不修。

## N1 在线发奖后不刷新角色

**问题确认。** `GrantReward` 改了 exp/points/honor/level/passwordBoxes，调用方只发 154 与 67，没有重读 `c.character`、也没有 157；`finishRewardClaim` 与商店成功路径已经这么做了，场景掉落箱与 GM 发奖没有。

**修法：** 新增 `refreshAfterGrant(c)`（重读角色 + 发 157），`grantCatalogReward` 与 `gmGrantReward` 都在库存补丁之后调用。放在登录链上的离线发奖因此只在真正发奖时多一条 157，不影响登录包序（无待领奖励时不发）。

**证据：** `TestReview862CatalogRewardRefreshesOnlineCurrency` 通过（修前 `persisted=50 cache=0 sent157=false`）。

**顺带：** `TestHostDropBoxGrantsReward` 原来断言“154 之后紧跟 67”，现在中间多一条 157（基线 `UpdateExp/UpdatePoints` 也是先发用户信息再发通知），断言改为 157 → 67。

## 上一轮复核部分的回应

报告 §一 对 75cddae 响应的复核结论（八条 `TestReviewHead*` 与九条 `TestStoreProbe*` 通过、S-3 不采纳成立）我照单接受，没有补充异议。R1 指出的“只返回受影响行 ≠ 行内只填受影响列”是那轮修复的口径错误，已按上面改掉。
