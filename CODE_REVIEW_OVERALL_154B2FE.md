# 154b2fe 整体审查

审查 HEAD：`154b2fe`。本报告记录不局限于上一轮修复的新增问题；修复回归另见 `CODE_REVIEW_154B2FE.md`，不重复计数。

本轮跨读了登录/重连、房间/对局、奖励构建与领取、插件/强化、背包删除与到期、公仓/战队成员管理等路径。确认下面 **2 类 P2**，共有 3 条独立复现。没有修改业务代码或现有测试，没有操作客户端。参考源码已通过 `scripts/check-ref.ps1` 校验为 `4859a6e`。

## 1. [P2] 移除库存行后没有维护外观与装备选择引用

位置：`internal/store/disassemble.go:30`、`internal/store/expiry.go:36–39`；受影响的自动装备判断：`internal/store/costume.go:222–223`。

删除和到期路径只移除 `UserInventory` 行，未清理仍指向该物品的外观/装备选择记录。两条探针分别确认：

1. **已删除背景继续被角色引用。** 发放并启用 LobbyBG，调用分解共用的 `DeleteItems` 删除该行，再从数据库重读 Character，`NameplateID` 仍为已不存在的物品 7001。重新登录仍会从持久化角色字段向客户端下发这个背景引用，不是仅内存缓存延迟。
2. **到期装饰阻断后续自动装备。** 发放并装备 HeadCostume，令其到期并执行 `ExpireItems`；再发另一件 HeadCostume 并调用 `AutoEquipCostume`，返回 false。已无任何旧装饰库存行，但 `UserCostumeLoadout.head` 残留旧 ID，被当作槽位仍被占用。该问题影响商店和奖励完成后的自动装备流程。

前者是可核对的基线偏差：`itemmanager.cpp:1611` 起的 `RemoveItem` 对 LobbyBG 清理 nameplate，随后也清理两类特效选择。后者在本轮仅按 Go 内部状态一致性问题报告，未证明基线所有到期路径也能正确清理 costume loadout，不将其笼统归为 Go 新引入。

建议：库存移除与对应选择引用清理应在同一事务内完成；对仍存在的同款副本，需要按其实际装备/选择状态决定保留或切换，不能无条件按 itemID 清掉所有副本的引用。服务器侧相应缓存与角色信息也应同步刷新。自动装备可额外检查已有引用是否真的对应有效库存，避免历史残留永久阻断。

探针：`TestReview154DeletingNameplateClearsSelection`、`TestReview154ExpiredCostumeDoesNotBlockAutoEquip`。

## 2. [P2] 审批已排队的入队申请可突破战队人数上限

位置：`internal/store/clan.go:427–451`，`ApproveClanJoin`。

`JoinClan` 在提交申请时检查 `memberCount < maxMemberCount`，但申请不会预占名额；后续审批时不再检查人数，直接删除申请并调用 `joinClanTx` 加入成员。多个申请在尚有空位时排队，依次批准就能超过配置上限；不需要并发。

**复现**：创建最大 2 人的战队，队长占 1 人；两名玩家先后提交申请，两条均合法排队；队长依次审批，两次都成功。最后数据库 `memberCount=3,maxMemberCount=2`。

这也适用于默认上限附近，只是探针使用 2 人以缩小夹具。基线 `userdatabase_sqlite.cpp:5312–5352` 的 `ClanApprove` 同样没有容量检查，属于继承缺陷/健壮性修复，而不是新协议偏离。

建议：在审批事务内、删除申请和插入成员前重新检查当前容量；满员时保留申请供以后审批。覆盖正常申请、审批、邀请加入等所有最终插入成员的路径，而不是只检查申请入口。

探针：`TestReview154ClanApprovalsRespectCapacity`，输出 `members=3 capacity=2`。

## 验证与边界

- `GOTOOLCHAIN=local` 下现有完整测试与 vet 通过。
- 新增 `.dist/review-154b2fe/overall_test.go`（已 gofmt），通过 overlay 测试编译加入；上述三条探针均复现失败。

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay .dist/review-154b2fe/overlay.json ./internal/store -run '^TestReview154' -count=1
```

本轮未完成全仓事务调用图证明、模糊测试、结果包逐字段金标准或客户端端测。现有测试通过与整体无缺陷不是同一个结论；未将未完成的功能清单直接计为新 bug。
