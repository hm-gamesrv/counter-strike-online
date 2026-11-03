# 154b2fe 修复复核

审查提交：`154b2fe`；处理回复：`.tmp/review-response-3.md`。未修改业务代码、现有测试或运行数据，未操作客户端。

## [P1] 新插件事务重复使用已删除的材料快照

位置：`internal/store/favorite.go:64–75`，`SetAddonsConsuming`。

材料剩余数量大于 1 时会更新内存里的 Count；但剩余数量为 1 时，只删除数据库行，没有将 `items[target].Count` 清零或从候选集中移除。后续相同 itemID 再次找到这份快照，重复 DELETE 虽然影响 0 行仍返回成功，随后照常追加插件记录。

**复现**：仅持有 1 件 zbsaddonitem，发送合法长度的插件设置请求，包含 6 个相同 ID。通过 `Server.setZBAddons` 执行后，`UserAddon` 实际保存 6 条相同 ID，而只消耗了 1 件材料。探针输出：

```text
one owned unit paid for 6 addon entries: [7001 7001 7001 7001 7001 7001]
```

这是 `154b2fe` 引入的新回归。原有逐条 `ConsumeItem` 会重读数据库，已删除的行不会再次成为材料。无需并发、数据库异常或截断请求。

建议：每次成功扣材料后统一减少快照 Count（包括减到 0），跳过已耗尽的副本；或在事务内用数据库条件扣除并检查影响行数。多份同款副本应按实际库存继续匹配。是否允许重复插件 ID 可以另按协议决定，但任何已接受条目都必须有相应材料，不能复用不存在的行。

建议回归矩阵：数量 1/2 的单堆叠、多份同款副本、重复请求 ID 数大于库存、耗尽后再请求，以及失败回滚。

## 上轮问题复核

| 上轮问题 | 本轮结果 |
|---|---|
| 选择型离线奖励阻断登录 | 原探针通过；延后处理错误允许登录继续，通知保留 |
| 到期通知按错误身份删除 | 原探针通过；现在按具体 rowid 删除 |
| 专服轮换保留实例无法开下一局 | 原探针通过；启动优先复用本房间实例 |
| 三类限时外观启用后消失 | 原三分支探针通过；激活时转换绝对时间 |
| 保护列表混用槽位和物品 ID | 原删除/消耗两条探针通过 |
| 插件请求失败前已扣道具 | 截断、尾部、数据库失败三条原探针通过；但有上述新回归 |
| 同款装饰副本同时装备 | 普通/僵尸皮肤原探针通过 |
| 卸下装饰续期只加分钟 | 原探针通过 |

原问题的定向复现均已修复；插件事务存在新的库存计数缺口，因此不能将整条插件设置路径标记为无问题。

## 验证

- `GOTOOLCHAIN=local` 下，现有 `go test ./...`、`go vet ./...` 通过。
- `.dist/review-overall-f6eda20/overlay.json` 的 `TestOverall*`、`.dist/review-b6afe1f/overlay.json` 的 `TestReview*` 均通过。
- 新探针位于 `.dist/review-154b2fe/fix_test.go`，已 gofmt；overlay 仅在测试编译时加入，不修改现有测试。

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay .dist/review-154b2fe/overlay.json ./internal/server -run '^TestReview154RepeatedAddonCannotReuseDeletedStack$' -count=1
```

整体审查的独立新增问题见 `CODE_REVIEW_OVERALL_154B2FE.md`。
