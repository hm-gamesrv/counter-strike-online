# 9d33c2d 跨模块检查补充

本轮沿到期扫描的调用方检查登录与在线同步的交叉行为，确认 **1 项 P2 新回归**。本报告不是全仓逐函数审计；到期删除记录缺失另见 `CODE_REVIEW_9D33C2D.md`。

## [P2] 登录中的到期扫描提前发送背包，破坏初始化顺序

位置：`internal/server/server.go:629–635`；调用方 `internal/server/lobby.go:348`。

正常登录会先把用户放入 `s.users`，再调用 `loginPackets`。后者第一步调用 `expireItems`，随后才发 UserStart 150、角色信息、元数据，以及原来的背包初始化。

本次到期扫描新增对 `s.users` 中受影响会话直接执行 `sendInventory`，没有区分会话是否已完成登录初始化。因此只要登录账号有需要扫描删除的过期物品，就会在 150 之前发送 152/154 背包包；稍后正常登录链还会再发一遍。

**复现**：用有效登录夹具，把账号放入在线用户表（与实际登录路径一致），准备一件过期物品，直接执行 `loginPackets` 并检查队列。探针确定首个 152 在 150 之前，输出：`inventory packet 152 was queued before UserStart 150`。

这是 `9d33c2d` 的新背包推送引入的顺序回归。基线 `usermanager.cpp:579–609` 和 Go 原来的登录链均先发 UserStart，再发元数据，之后才初始化库存。当前客户端对提前到达的数据会忽略、错误处理还是出现更严重故障，本轮没有端测，不作确定断言；已确认的是协议初始化顺序被打乱。

建议：把到期数据库清理与在线通知拆开；登录中的账号直接使用后续正常背包初始化结果，暂不接收运行时到期推送。扫描仍可通知其他已完成初始化的在线会话。测试覆盖有/无过期物品的首次登录、重连，以及其他用户登录触发扫描时对已在线玩家的同步。

## 验证

- `.dist/review-9d33c2d/server_test.go` 的 `TestReview9dLoginInventoryFollowsUserStart` 已 gofmt，复现失败。
- 现有完整测试、vet 通过；没有修改业务代码、现有测试或运行数据，没有操作客户端。

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay .dist/review-9d33c2d/overlay.json ./internal/server -run '^TestReview9dLoginInventoryFollowsUserStart$' -count=1
```

既有模糊测试、完整事务调用图和结果包金标准等未完成项仍未完成。
