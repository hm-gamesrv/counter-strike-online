# 44b5eed 扩大范围审查

本轮检查当前实现中的开箱、强化、公仓、小活动、离线奖励和通知路径；不局限于 5bc14c8 的修复。参考仓库已用 check-ref.ps1 核实为 `4859a6e`。未操作客户端，未修改业务代码、现有测试和运行数据。

确认以下 **6 项 P1、1 项 P2**，都有隔离探针。这里的分级描述影响；“是否偏离基线”另行注明，不能将继承的基线缺陷误报为本次移植错误。Bingo、WeaponRelease 默认均关闭，其问题需启用对应活动才会暴露。

## 1. [P1] Bingo 发奖和领取状态分离，满背包也能触发重复奖品

- 位置：`internal/server/bingo.go:188–196`、`:212`。
- 每个奖品的 GrantReward 各自提交，全部奖品发完才调用 OpenBingo 更新号码和领奖标记。后面的奖品因满背包失败，前面的奖品已经永久入库，但领取状态一个也未更新。
- **不需要数据库故障**：设置一个可达桌面状态，24 格已开，0 号位置尚未开，其他已完成的线均已领取。最后一格会同时完成第 0 行、第 0 列和主对角线，尚有 3 个奖品；背包只剩一格。第一次开格先得到一份奖励，第二份失败；清出一格再开，又得到第一份。探针结果：新增奖品 2，新增领取标记 0。
- 另一个探针在 OpenBingo 更新时注入失败；3 个待领奖项最终能发出 6 份。
- 分类：Go 启用的新业务缺陷；基线的 open-random 入口及主要实现是注释状态，不把 Go 的扩展能力当作已经复刻的有效上游流程。
- 建议：整次开格的全部奖励、号码状态与奖品标记同一事务，或在状态事务内生成持久化待领奖记录。只交换两个调用的顺序会把“重复发”变成“永久丢奖”，不能解决原子性。

## 2. [P1] 离线奖励可能在重登后重复发放

- 位置：`internal/server/login_extra.go:45`、`:70`；`internal/store/notice.go:DeleteRewardNotice`。
- GrantReward 已提交后，才单独删除 UserRewardNotice。两步之间进程退出，或删除失败，通知仍然存在；下次登录会再次发同一奖励。
- 复现：在 UserRewardNotice 的 DELETE 上注入失败，第一次领取已写入物品；移除故障再领取，物品实际发了 2 次。
- 分类：基线 CItemManager::OnUserLogin（`itemmanager.cpp:2795`）同样先 GiveReward 后清通知，具有同类风险。这是继承缺陷/安全改进讨论，不是新的协议偏离。不过当前 Go 的“发奖幂等”不能泛化到这条路径。
- 建议：按通知行的稳定身份生成唯一凭据，并在同一事务内消费来源通知及发奖/排队；不以网络提示是否成功作为数据库完成依据。

## 3. [P1] 集字活动满背包时会永久丢掉已完成奖励

- 位置：`internal/server/minigame.go:219–236`；`internal/store/weapon_release.go:SetWeaponReleaseCharacter`。
- 最后一个字母被消费、opened=1 已提交，之后才尝试 GrantReward。发奖失败仅提示 Inventory is full，未留下待领奖记录。之后即使腾出空间，同一行因 Opened!=0 直接返回。
- 复现：GALIL 已有位图 30，最后补 G（bit 0），背包已满。腾出空间再请求，数据库 opened=1，奖励数仍为 0。
- 分类：基线 `minigamemanager.cpp:457–464` 也是先 SetWeaponReleaseCharacter 再 AddItem，且不处理发奖失败，属于继承的基线缺陷。
- 建议：将最后一个字母和待领奖凭据同时提交；背包不足时可重试领取，不能再扣一次字母。

## 4. [P1] 开箱绕过目录归一规则，同款武器多个副本同时选中

- 位置：`internal/server/inventory.go:143–145`。
- 开箱直接构造 store.Reward 并交给 OpenItemBox，没有像商店/通用奖励一样调用 prepareGrants。默认 Exclusive=false，grantItems 不会处理同款选中副本。
- 复现：固定奖池只含物品 12，一次开两箱，得到两个 inUse=1/status=1 的同款副本。当前配套奖池均为武器，并非仅自定义装饰奖池才会触发。持有旧副本再抽到同款也走同一缺口。
- 分类：偏离共用发放不变量，也未保持基线 AddItem 对重复物品的状态区分（基线 `itemmanager.cpp:714–735` 会将重复副本初始化为未激活状态）。不要求 Go 选择旧副本还是新副本，但不能同时选中多个副本。
- 建议：开箱也统一经过目录归一；按实际物品类型设置 Exclusive/Stackable/Dormant 等标记。修复后应验证批量重复物品及已有副本两类情况。

## 5. [P1] 强化随机候选漏掉 Ammo 属性

- 位置：`internal/server/enhance.go:188`。
- Go rand.IntN(5) 只能产生 0..4，交换 Damage/Accuracy 索引后仍只有 0..4。Ammo 的 index 5 只有前五项全满、随机重试用尽后才可能从兜底扫描获得，正常随机分布中不存在它。
- 基线：`itemmanager.cpp:2206` 附近创建 Randomer(itemEnhanceInfo.size()-2)，size=7，传入 5；`common/utils.h:32` 的 uniform_int_distribution(0,max) 是**含上限**的，即 0..5。
- 复现：六个常规属性上限都为 9、当前均为 0，抽取 1000 次，Ammo 始终为 0 次；实际样本计数 `[201 197 218 190 194 0 0]`。这里不是小概率偶然，Go 取值范围从源码上就排除了 5。
- 分类：明确的基线数值偏离。
- 建议：修正候选范围，并补“每个可升级属性都进入随机候选”的测试；同时独立核对上游取表列和 Damage/Accuracy 的映射，不要用随机次数兜底掩盖表映射问题。

## 6. [P1] 公仓拒收尚未激活的限时道具

- 位置：`internal/store/clan_storage.go:103`；下方 `:111–112` 的相对天数转换分支因此无法命中正常休眠道具。
- 查询只接受 expiryDate=0 或 expiryDate>now，没考虑 inUse=0/status=0 时的相对天数。3 天休眠物品被误认为已经过期，返回 -2。
- 复现：建军团后发放 3 天休眠物品 270，调用 GiveClanItem，返回 -2；该物品在普通 Inventory 查询中仍是有效的。
- 基线：`userdatabase_sqlite.cpp:4604` 的 AddClanStorageItem 通过 GetInventoryItemBySlot 取得道具，然后将相对天数转换为分钟；未使用 Go 这项绝对过期过滤。
- 分类：基线行为偏离、不同存储入口对日期语义理解不一致。
- 建议：复用统一的有效道具读取规则，明确区分休眠相对天数与已经激活的绝对时间；不能仅把所有小 expiryDate 都视作有效。

## 7. [P2] 到期通知截到 255 条后，未发送的余项也被清空

- 位置：`internal/server/login_extra.go:18`、`:32`。
- sendExpiryNotices 只发送前 255 条，但 ClearExpiryNotices 删除该玩家全部通知。260 条通知发送一次后应至少还有 5 条待处理，实际剩余为 0。
- 复现：插入 260 条有效 itemID 的到期通知并调用 sendExpiryNotices，探针报告 remaining=0。
- 分类：Go 为 U8 数量上限做截断时的批处理缺口；基线直接将总量写入 U8，同样不安全，不能据此要求恢复回绕。
- 建议：逐批发送，或只删除实际完成发送的通知行；保留未处理的余项。若使用交付确认，还需明确断线前后是否允许重复提示。

## 验证及边界

隔离探针放在 `.dist/review-expanded/`，通过 Go -overlay 注入测试，不修改 internal 中的真实文件：

```powershell
$env:GOTOOLCHAIN = "local"
go test -overlay .dist/review-expanded/overlay.json ./internal/server ./internal/store -run '^TestAudit' -count=1
```

共 8 条探针覆盖 7 项问题（Bingo 有普通满背包和故障注入两种复现）。断言按应有行为编写，失败用于暴露问题，不是新增业务实现。

当前快照原有完整 `go test ./... -count=1` 和 `go vet ./...` 已通过。没有将已有测试绿灯、没有发现更多问题，等同于全部模块已证明正确。本轮没有新增客户端实测、跨 NAT、SSL、HLDS 或完整间接调用图验证。

前一份 `CODE_REVIEW_44B5EED.md` 中的奖励回包到期时间不同步问题仍单独保留，不计入本轮 7 项。
