# 3e6fa14 修复复核

核对 `.tmp/review-response-5.md` 与提交 `3e6fa14`。上一轮三条独立探针全部通过，原有复现场景已修复；本轮未确认新引入的回归。不过缓存同步仍有一个低优先级入口遗漏。

## [P3] GM 删除背景没有同步在线角色缓存

位置：`internal/server/gm.go:189–196`。

`/removeitem` 同样调用 `Store.DeleteItems`，数据库会清掉背景引用，但这个入口没有像分解路径一样调用 `refreshCharacterCache`，随后只下发库存更新和聊天提示。

**复现**：启用背景 7001，执行 GM 命令 `/removeitem 7001 0`。数据库重读的 nameplateID 已为 0，在线 `c.character.NameplateID` 仍为 7001；后续 `broadcastUserInfo` 仍可能重发旧背景，直到重登或其他缓存刷新。

探针经 `gmCommand` 执行实际删除逻辑；该命令在产品入口受 GM 权限限制，因此按 P3 报告，不将其描述为普通玩家的权限绕过。这是上轮缓存问题的另一入口未覆盖，不是本次新增回归。

建议：GM 删除成功后也调用共用缓存同步逻辑，或将“删除库存后的服务端同步”封装为共用流程，避免各入口漏接。

## 原问题状态

| 原复现 | 本轮结果 |
|---|---|
| 普通分解删除背景后缓存仍旧 | 原探针通过；分解路径已刷新缓存并广播 |
| 休眠同款副本阻止清除到期装备引用 | 原探针通过；休眠副本不再保留占位 |
| 分解删除选中武器后没有接替副本 | 原探针通过；整批删除后恢复选择 |

到期扫描现在也调用缓存刷新；本轮另发现自然到期路径没有做武器副本接替，见 `CODE_REVIEW_OVERALL_3E6FA14.md`，与缓存字段刷新不同。

## 验证

- `GOTOOLCHAIN=local` 下，现有 `go test ./...`、`go vet ./...` 通过。
- `.dist/review-33e9ec6/overlay.json` 的三条 `TestReview33*` 全部通过。
- 新增 `.dist/review-3e6fa14/server_test.go` 已 gofmt，`TestReview3eGMRemovalRefreshesCache` 复现失败。overlay 不修改现有测试。

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay .dist/review-3e6fa14/overlay.json ./internal/server -run '^TestReview3eGMRemovalRefreshesCache$' -count=1
```

未修改业务代码、现有测试或运行数据，未操作客户端。
