# f6eda20 整体审查：新增发现

审查 HEAD：`f6eda20`（相对 `b6afe1f` 仅增加忽略目录，没有业务代码变化）。本轮扩大检查登录/大厅、房间/专服/对局、通用奖励/任务、背包修改和数据库事务边界。此前已知的两项通知问题见 `CODE_REVIEW_B6AFE1F.md`，本报告不重复计数。

确认新增 **4 项 P1、2 项 P2**，均有独立 overlay 探针。没有操作客户端，没有修改业务代码、现有测试或运行数据。对照源码前已执行 `scripts/check-ref.ps1`，参考树确认为基线 `4859a6e`。

## 1. [P1] 专服模式下，地图轮换保留的专服无法用于下一局

- 位置：`internal/server/host.go:54–60`；关联 `:563–566`、`internal/server/dedicated.go:81–87`。
- `HostConnectingMethod=1` 且地图列表尚未循环回第一张时，`endGame` 有意保留 `r.dedicated`，它的 `room` 也保持非零。但 `startGame` 总是重新调用 `availableDedicated`，后者只返回 `room==0` 的专服，从不复用本房间已保留的实例。
- **复现**：一个房间、唯一一台专服、两张地图列表。第一局结算后切到第二张，专服仍归本房间；点击开始却提示没有可用专服，`r.match` 仍为 nil。
- 有更多空闲专服时也存在分配缺陷：下一局会覆盖 `r.dedicated`，旧实例仍持有房间号，成为无法再次分配的占用。
- 基线 `room.cpp:851–854` 的 `SendStartMatch` 仅在 `m_pServer==NULL` 时重新分配；`:1162` 明确保留尚在轮换中的专服。属于移植偏差。
- 建议：优先使用仍归本房间的 `r.dedicated`，仅在没有保留实例时申请新的；验证连续多局、列表循环以及专服掉线后的释放。

## 2. [P1] 限时背景/复活特效/击杀标记启用后从背包消失

- 位置：`internal/store/effect.go:35`、`:94`；来源 `internal/server/rewards.go:95–96`。
- `prepareGrants` 把 `LobbyBG`、`zbRespawnEffect`、`CombatInfoItem` 都设为 Dormant，限时物品因此以 `status=0,inUse=0,expiryDate=相对天数` 入库。但两个 Toggle 方法只改 `status/inUse`，没有将相对天数转换成绝对分钟。
- **复现**：经 `prepareGrants` 发放 3 天物品，再通过正常 `itemRequest` 启用。三类物品都变成 `status=1,inUse=1,expiryDate=3`，随后 `Inventory` 因认定过期而不再返回它。
- 目标也不在返回的背包更新中，无法靠再次点击关闭或重登正常恢复；外观选择字段却已写入，持久化状态与背包不一致。
- 基线初次 `AddItem` 在 `itemmanager.cpp:737–738` 先将 duration 转为绝对时间；这三类在 `:872` 起单独处理，并不一律走 category-12 的休眠分支。Go 的统一 Dormant 规则未与启用路径配套，属于移植语义不完整。
- 建议：统一发放与启用的时间表示，休眠转激活时在同一事务内转换；同时覆盖第一次发放、换同款副本和关闭后重开。

## 3. [P1] 保护列表用物品 ID 构造，却按背包槽位判断

- 位置：`internal/store/disassemble.go:25`、`internal/store/consume.go:19`。
- 调用方传入的是 `DefaultItems + PseudoDefaultItems`（物品 ID）。`DeleteItems` 却判断 `Contains(protected, slot)`，`ConsumeItem` 判断 `Contains(protected, item.Slot)`。
- **复现 A**：将物品 ID 12 列为 pseudo-default，持有副本位于槽位 2；正常分解请求成功删除该受保护物品。
- **复现 B**：调用 `ConsumeItem(1,12,[]int{12})`，槽位 2 的同一物品也成功被消耗。该方法用于 ZB 插件和 host 场景物品消耗。
- 反过来，普通物品只因槽位数字与某个默认物品 ID 相同，也会被错误跳过。槽位分配和物品目录编号没有这种对应关系。
- 基线 `userinventoryitem.cpp:78–104` 按物品 ID 检查 default/pseudo-default，`RemoveItem` 在 `itemmanager.cpp:1501` 调用该检查。因此分解路径是明确移植偏差；`ConsumeItem` 的问题则是未正确落实 Go 调用方显式传入的保护列表，不推定基线的所有消耗入口都已正确保护。
- 建议：两处统一按 `item.ID` 判断；补“受保护 ID 位于不同槽位”和“普通 ID 恰好位于保护 ID 同号槽位”两类测试。本发现不将锁定规则一并推定为相同要求。

## 4. [P1] ZB 插件设置失败时，之前扣除的道具不会回滚

- 位置：`internal/server/addon.go:30–53`；关联 `internal/store/consume.go`、`internal/store/favorite.go:37`。
- 请求在逐项解析时立即调用 `ConsumeItem`，每次独立提交；整个包的尾部校验和 `SetAddons` 在之后才发生。因此“拒绝请求/插件保存失败”不能回滚已经扣掉的材料。
- 三条探针均复现永久丢道具：
  1. 声明两个插件，载荷只包含第一个合法 ID：返回截断错误，但第一个道具已消耗。
  2. 声明一个插件，末尾附加字节：尾部校验拒绝，但道具已消耗。
  3. 完整合法请求，在 `UserAddon` 插入时注入数据库错误：插件写入回滚，背包扣除没有回滚。
- 基线 `channelmanager.cpp:1876–1894` 同样逐个 `OnItemUse` 后再 `SetAddons`，具有相同事务边界缺陷；应归为继承缺陷/健壮性修复，而非新协议偏离。
- 建议：先完整解析和验证请求；在一个 Store 事务中完成所有材料扣除与插件列表替换。仅提前检查包长仍不能解决数据库失败时的丢失。

## 5. [P2] 同款装饰的不同副本可以同时处于装备状态

- 位置：`internal/store/costume.go:81`、`:143`。
- 替换旧装饰的分支仅在旧 itemID 与目标 ID 不同时执行。切换同一 itemID 的另一副本时，旧副本不会被卸下，新副本却会激活并装备。
- **复现**：发放两份相同的 3 天休眠装饰，依次装备槽位 2、3；普通 HeadCostume 与 ZombieSkinCostume 都出现两行 `inUse=1`。两份副本都开始计时。再卸下其中一份会清空按 itemID 保存的 loadout，另一份仍可能保持装备标志。
- 基线 `OnCostumeEquip` 在 `itemmanager.cpp:2896` 起只要槽位已有装饰就查找旧的 active 副本，即使 itemID 相同；`:2959–2965` 会清除其 inUse。属于移植偏差。
- 建议：按库存副本身份（slot）替换旧副本，同款也需要卸下旧行；在切换与卸下时维护 loadout 与唯一装备副本的一致性。

## 6. [P2] 已激活但卸下的限时装饰，续期把天数当分钟

- 位置：`internal/store/item_extend.go:30–35`。
- 续期仅凭 `InUse==0` 推定 expiry 是相对天数。但装饰装备后再卸下会保留 `Status=1` 和绝对分钟到期值，同时 `InUse` 变为 0。
- **复现**：发放 3 天 HeadCostume，装备后卸下，再消耗 2 个 extender 续期。数据库到期值只增加 **2 分钟**，应增加 **2880 分钟**；材料已扣除。
- 基线 `itemmanager.cpp:1736–1743` 使用相同判断，属于继承的时间单位缺陷，不是 Go 独有的数值偏差。
- 建议：区分尚未激活的相对时长与已激活的绝对时间，不能以是否正装备判断；一并验证相对时长续期、已装备续期、已卸下续期和永久化阈值。

## 验证与范围

- `GOTOOLCHAIN=local` 下现有 `go test ./...`、`go vet ./...` 通过。
- 隔离探针：`.dist/review-overall-f6eda20/overall_test.go`（已 gofmt），overlay 不修改已有测试。7 个测试入口覆盖上述 6 类问题；同款装饰覆盖两种分支，限时外观覆盖三种类型，插件失败覆盖三种情况，均复现失败。

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay .dist/review-overall-f6eda20/overlay.json ./internal/server -run '^TestOverall' -count=1
```

这轮是跨模块静态检查与定向复现，不是全仓无缺陷证明。未完成完整事务调用图、模糊测试、所有结果包字段的金标准或客户端端测。以上发现均依赖具体代码路径与探针结果，不把尚未实现的完整合并等已知功能缺口计为新缺陷。
