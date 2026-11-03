# 对 75cddae 存储层与健壮性审查的处理

提交：见 `git log -1 --oneline`。全量检查绿；`review-75cddae-store` 的 9 条探针全部通过（含原先失败的两条）。

## 一、采纳并修复（2 项）

| 编号 | 问题 | 处理 |
|---|---|---|
| S-1 | `SetTitle` 读-改-写未包事务，并发丢更新 | 两条语句放进同一事务。实测探针 `TestStoreProbeSetTitleLostUpdate` 从「第 7 轮就丢、2000 轮丢 20 次」变成 2000 轮无丢；新增常驻回归 `TestSetTitleKeepsConcurrentSlots`（两协程 200 轮写不同槽） |
| S-2 | `activateExpiry` 不走 `liveItemSQL`，过期行可被激活成名牌 | `SELECT` 加上 `count>0 AND liveItemSQL`，过期行返回 `ErrItemUnavailable`；新增常驻回归 `TestToggleNameplateRejectsExpiredRow` |

## 二、不采纳（1 项，附证据）

**S-3 「未建模 flag 位被存成 0 长度字段并重播」不是与基线的偏离。**

`CPacketManager::WriteSettings`（`packetmanager.cpp:2281` 起）先把四个 flag DWORD **原样写出**，再按**固定字段表**逐项写值：未建模的位既不读也不写，于是同样产生「flag 置位、无 payload」的帧。也就是说基线与本实现在这类位上的出站形状一致，而 CSO2 客户端同样按固定表读，不会因此错位。

据此本轮**保留**现有解析行为，并给 `parseSettings` 补了三条注释：未建模位原样保留并重播（与基线同形）、该解析器不是自身逆运算（难度字段 1 字节进 9 字节出）、截断通过 `truncated` 报告而不是返回错误。探针 `TestRoomSettingsZeroWidthFieldEcho` 的期望与本结论相反，保留其失败状态作为记录。

附带修正了它指出的 harness 隐患：`FuzzItemRequest` 的队列从 64 提到 8192，避免“队列溢出 → `client.send` 默认分支对 nil 连接 `Close()`”被误记为产品缺陷。

## 三、逐项核对结论（通过，无动作）

- **单连接自锁扫描**：54 个 `s.DB.Begin()` 方法 T1–T8 全绿；唯一在 `Begin` 前用 `s.DB` 的是 `Store.init`（PRAGMA/user_version/空库判定），属正确顺序。扫描器随探针交付，可复现。
- **部分失败回滚**：6 条注入用例（开箱 / 延期 / 强化材料 / 过期清扫 / 结算第二参与者 / 到期通知身份）全部回滚干净；结算失败不烧 `matchID`。
- **幂等重放**：`GoRewardClaim`、`GoMatchSettlement`、`UserExpiryNotice`、`UserRewardNotice` 均只生效一次。
- **长跑 fuzz**：`FuzzItemRequest` 180s / 463,716 次、`FuzzMetadataFrames` 180s / 286,819 次、新 `FuzzRoomUpdateSettings` 180s / 447,358 次，均无 panic/超时、无内存异常增长。
- **`liveItemSQL` 使用点**：7 处正确使用；6 处「未使用但语义正确」逐条核对通过；唯一例外 `activateExpiry` 已按 S-2 修复。

## 四、其它建议的处理

- **`internal/server/testdata/fuzz` 未入 `.gitignore`**：保持现状。Go 约定是把最小化后的崩溃语料**提交**作为回归种子；本轮那份语料对应的 S-3 判定为非缺陷，已移到 `code-review/probes/review-75cddae-store/corpus/` 归档，不入库。
- **`FuzzItemRequest` 断言强度**：认可「只查不 panic」不足以覆盖一致性，但补断言需要把库存不变量写进 fuzz body（`count>0`、槽位唯一、`slot<=65535`）。列入待办，不阻塞本轮。
- **`SettleMatch` 里 `CREATE TABLE IF NOT EXISTS`**：同意生命周期不一致，可移到启动期建表。列入待办。
