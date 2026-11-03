# 33e9ec6 整体检查补充

在核对修复之外，继续检查了道具移除后的状态迁移及其对 host 背包的影响。新增确认 **1 项 P2**；修复未覆盖的两项边界另见 `CODE_REVIEW_33E9EC6.md`，不重复计数。

## [P2] 删除当前选中的武器副本后，没有选中仍有效的同款副本

位置：`internal/store/disassemble.go:30–48`；影响路径：`internal/server/host.go:186–189`。

共用发放逻辑允许同款武器存在多个已激活副本，其中一个 status=1，其余 status=0。删除选中副本时，`DeleteItems` 只移除行，没有为仍有效的同款副本恢复 status。新的 `clearItemReference` 也不处理库存副本的选择状态。

**复现**：依次发放两份 Exclusive 武器 12，第二份成为选中副本；通过正常分解请求删除第二份。库存仍有第一份永久武器，`inUse=1,status=0`，但没有任何选中副本。`hostInventory` 明确过滤 status=0 的武器，所以随后进入对局时不会下发该武器，直到玩家手动选择剩余副本。

这与“删除背景后残留 nameplate 引用”是不同状态：此处要维护的是剩余库存行的 status，而非清理外部字段。分解与 GM 的共用删除入口均受影响。

对照前执行了 `scripts/check-ref.ps1`，基线确认是 `4859a6e`。基线 `itemmanager.cpp:1507–1530` 的 `RemoveItem` 在删除 active 副本时查找其他同 ID、inUse 非零副本，把它的 status 置 1 并下发库存更新。Go 未保留这段替代选择，属于移植偏差，非 `33e9ec6` 新引入。

建议：在删除事务内，结合整批将删除的槽位，为仍有效且已激活的同款副本恢复选择；整批删完才选择，避免选中本批稍后也会被删除的副本。返回更新中包含替代副本。覆盖仅删选中副本、删未选中副本、批量全删以及只有休眠/过期副本等情况。

## 验证与范围

- 独立探针：`.dist/review-33e9ec6/server_test.go` 中 `TestReview33DeletingSelectedWeaponPromotesRemainingCopy`，已 gofmt；经服务端 `itemRequest` 复现失败。
- 现有完整测试与 vet 在 `GOTOOLCHAIN=local` 下通过；未修改业务代码或现有测试，未操作客户端。

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay .dist/review-33e9ec6/overlay.json ./internal/server -run '^TestReview33DeletingSelectedWeaponPromotesRemainingCopy$' -count=1
```

本轮整体检查围绕删除、到期和同款副本的后续状态展开，覆盖范围小于全仓审计；未完成的调用图证明、模糊测试和结果包金标准不因本轮检查而视为完成。
