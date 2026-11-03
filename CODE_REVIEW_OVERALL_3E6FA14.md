# 3e6fa14 整体检查补充

本轮围绕库存生命周期继续检查分解、GM 删除、自然到期和对局背包的联动，并复核上一轮修复。独立确认 **1 项 P2**。缓存同步的 GM 入口遗漏另见 `CODE_REVIEW_3E6FA14.md`，不重复计数。本轮范围不是全仓逐函数审计。

## [P2] 选中的武器自然到期后，仍有效的同款副本没有接替选择

位置：`internal/store/expiry.go:35–44`；关联 `internal/store/disassemble.go:46–61`、`internal/server/host.go:186–189`。

`DeleteItems` 现在会为被删除的选中副本寻找仍有效的同款副本并置 status=1。但到期扫描直接删除库存行，只清外部引用，没有执行这段库存选择状态迁移。`refreshCharacterCache` 只刷新角色数据，也不会改变剩余库存行的 status。

**复现**：依次发放同款 7 天和 1 天武器，两份都已激活，后发的 1 天副本被选中。让选中副本到期并执行真实 `ExpireItems` 后，7 天副本仍在有效期内，但保持 `inUse=1,status=0`，没有任何同款副本被选中。

`hostInventory` 会过滤 status=0 的武器，所以下次进入对局不会下发该武器，直到玩家手动重新选择。这里并非背包物品被永久删除，也不是数据库丢失奖励。

这是已有到期路径的状态维护缺口，不是 `3e6fa14` 引入的新回归。上一轮的分解修复只覆盖手动删除入口；不能把其测试结论推广为所有库存移除入口均能正确接替。

建议：抽取事务内共用的“移除后选择接替副本”逻辑，供分解与到期扫描使用；到期扫描应先完成整批删除，再从剩余有效、已激活副本中选择，避免短暂选中本轮也要删除的行。对在线客户端同步相应库存变化，确保服务端状态与客户端显示一致。

## 验证

- 隔离探针：`.dist/review-3e6fa14/store_test.go` 的 `TestReview3eExpiryPromotesRemainingWeapon`，已 gofmt，复现失败。
- 使用两份有效发奖结果，只把短期副本时间提前至刚过期以缩短等待；没有手工构造不可能的选择组合。
- 现有完整测试和 vet 在 `GOTOOLCHAIN=local` 下通过，未修改业务代码或已有测试，未操作客户端。

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay .dist/review-3e6fa14/overlay.json ./internal/store -run '^TestReview3eExpiryPromotesRemainingWeapon$' -count=1
```

未完成的全仓事务调用图证明、模糊测试与结果包金标准仍未完成；本轮不据此宣称整个实现无缺陷。
