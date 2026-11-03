# 9d33c2d 修复复核

核对 `.tmp/review-response-6.md` 与提交 `9d33c2d`。上一轮 GM 缓存同步、自然到期后副本接替两条原探针均通过。另确认 **1 项 P2 修复缺口**：到期同步只重发剩余库存，没有清除客户端的已删除槽位。

## [P2] 到期刷新缺少已删除槽位的清除记录

位置：`internal/server/server.go:635`，关联 `internal/server/lobby.go:511–530`、`internal/store/expiry.go:9` 起的新方法。

`ExpireItemsWithUsers` 只返回删除数量与受影响用户 ID，丢掉了过期行的槽位身份。服务端随后调用 `sendInventory`，发送默认库存 152 和仅含剩余物品的 154。154 使用的仍是同一个按槽位增量更新格式，并没有全量替换或清空现有库存的标记；不在本包中的槽位不能据此被视为已删除。

对照基线前已运行 `scripts/check-ref.ps1`，确认 `4859a6e`。基线 `packetmanager.cpp:720` 的 `SendInventoryRemove` 在相同 Inventory 包中显式发送 `U16(slot)+U8(0)`；Go 的分解、消耗等路径也返回零 ID 墓碑，并由 `writeItem` 编成相同的删除记录。

**复现**：用户持有槽位 2 的限时武器，令其到期并调用实际服务端 `expireItems`，解析排队的 154 包。数据库行已经删除，但没有任何 `slot=2,present=0` 记录；154 只含剩余物品。探针输出：`expiry sent no slot=2,present=0 removal entry`。

因此接替武器的状态会更新，但原到期槽位并未被这次同步显式清掉。客户端可能还有自己的到期计时行为，本轮未端测其显示；这里只确认服务端发送缺失，不将其夸大为已实测的客户端闪退或永久残留。

建议：到期事务返回每个用户的已删槽位及接替更新，提交后给已完成登录的在线会话发送删除墓碑与变更副本。重发剩余库存可以保留，但不能替代明确删除。

## 验证

- `GOTOOLCHAIN=local` 下现有 `go test ./...`、`go vet ./...` 通过。
- `.dist/review-3e6fa14/overlay.json` 的两条 `TestReview3e*` 全通过；原数据库接替与 GM 缓存问题可按其复现场景关闭。
- 新探针 `.dist/review-9d33c2d/server_test.go` 已 gofmt，overlay 加入测试编译，不修改已有测试。

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay .dist/review-9d33c2d/overlay.json ./internal/server -run '^TestReview9dExpirySendsRemovedSlot$' -count=1
```

跨登录流程检查另确认一处包序回归，见 `CODE_REVIEW_OVERALL_9D33C2D.md`。未修改业务代码、运行数据或现有测试，未操作客户端。
