# b6afe1f 修复复核

审查提交：`b6afe1f32365eedc953a6fec4b143764e62dffc9`。针对 `.tmp/review-response-2.md` 核对实现、复跑上一轮探针，并检查修复边界。没有操作客户端，没有修改业务代码或现有测试。

确认 **1 项 P1、1 项 P2**。上一轮探针全部通过，但到期通知的原问题尚未完整关闭，离线奖励新增一处登录回归。

## 1. [P1] 选择型离线奖励会使账号持续登录断线

- 位置：`internal/server/login_extra.go:76–79`；触发错误位于同文件 `:101–102`。
- `grantCatalogRewardNotice` 对合法的选择型奖励返回 `choice reward needs the selection UI`。这个错误不在 `rewardRejection` 白名单中，因此 `grantRewardNotices` 直接返回错误，而不是保留通知后继续登录。
- `loginPackets` 在 `internal/server/lobby.go:448–450` 继续上抛，`serveClient` 在 `internal/server/server.go:344–346` 因 dispatch 错误退出并关闭连接。通知尚未消费，下一次登录会再次遇到相同错误，无法靠重登恢复。
- **复现**：使用现有登录测试夹具，将有效奖励 3000 设置为 `Select=true`（选项物品 12 在目录中），插入一条 `UserRewardNotice`。连续两次调用登录中的 `grantRewardNotices` 都返回上述错误，通知均仍为 `[3000]`。
- 这不是数据库故障，也不是非法客户端输入；旧实现对该情况记录日志并继续登录，本次错误分类收紧引入了回归。同样，已移除的奖励定义、缺失的奖品目录等构建奖励失败，也会走到这个返回路径。
- 建议：区分“奖励暂不能处理”和数据库故障。选择型奖励应持久化排队并走选择 UI，或至少保留通知、记录原因并继续登录；目录异常也不应让一条奖励永久阻断整个账号。数据库事务失败仍应按既定策略处理。

## 2. [P2] 到期通知仍可能删除未发送项，保留已发送项

- 位置：`internal/store/notice.go:27`；读取顺序在同文件 `:4`，筛选和截断在 `internal/server/login_extra.go:16–28`。
- 读取按 `itemID` 排序，清理却按 `rowid` 排序删除前 N 行。行数相同不代表行身份相同；正常插入顺序与物品 ID 无需一致。
- **复现**：按 260、259、…、1 的顺序插入 260 条通知。第一次实际发送物品 1–255；清理后剩余却是 `[1 2 3 4 5]`，应保留的 256–260 已被永久删除。下次登录会重复通知 1–5，遗漏 256–260。
- 当前回归测试只检查剩余行数为 5，且按物品 ID 升序插入，因此无法发现此错。新增的非法 ID 过滤也使“按数量删前 N 行”更不可靠：过滤掉的行仍参与删除范围。
- 建议：读取通知时带出稳定行身份，发送后精确删除本批选中的 rowid；不要仅凭数量删除。测试应断言剩余行身份，并覆盖乱序插入、重复 itemID、非法 ID 混入。

## 上一轮问题的复核结果

- 自动装备后的奖励回包到期时间：原独立探针通过。
- Bingo 的整次发奖回滚、满包部分成功后重复奖品：两条原探针通过。
- 离线奖励消费与发奖原子性、集字满包重试：原探针通过；离线奖励另有上面的新回归。
- 开箱共用道具归一规则、强化 Ammo 候选、公仓休眠道具：原探针通过。
- 到期通知：原“剩余行数”探针通过，但新增“剩余行身份”探针失败，因此此项不能标记为完整修复。

## 验证与复现文件

均使用 `GOTOOLCHAIN=local`。

- 现有 `go test ./...`、`go vet ./...` 通过。
- `.dist/review-expanded/overlay.json` 的 8 条 `TestAudit*` 全通过；`.dist/review-44b5eed/overlay.json` 的奖励回包探针通过。
- 新增隔离探针 `.dist/review-b6afe1f/boundary_test.go`，通过 overlay 加入测试编译，不修改现有测试。两条均失败，与上述发现一致。
- 新增探针已 gofmt。复现命令：

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay .dist/review-b6afe1f/overlay.json ./internal/server -run '^TestReview(ExpiryDeletesDeliveredIdentities|OfflineChoiceDoesNotAbortLogin)$' -count=1
```

本轮没有完成全仓调用图证明、模糊测试或结果包逐字段金标准验证；既有 TODO 仍成立。未将这些覆盖缺口当作已经确认的代码缺陷。
