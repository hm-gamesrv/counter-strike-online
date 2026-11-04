# 86212b7 代码审查

日期：2026-11-04（Asia/Hong_Kong）。HEAD：`86212b7ee2fc8780091d905d59d8f86ca8a5cf4e`。复核对象为 `2026-11-04T01-24-42-75cddae/CODE_REVIEW_75CDDAE_RESPONSE.md` 及 `2026-11-04T01-18-26-75cddae-store/CODE_REVIEW_75CDDAE_STORE_RESPONSE.md`。

确认 **2 项 P1、1 项 P2**。本轮仅新增探针和这一份报告，没有修改业务代码。基线检查脚本确认 `.ref/CSNZ_Server` 位于 `4859a6e`。本轮没有操作客户端、线上实例或数据快照。

## 一、对上一轮 RESPONSE 的再审查

### 复核结果

`review-75cddae` 的八条 `TestReviewHead*` 全部通过；`review-75cddae-store` 的九条 `TestStoreProbe*` 全部通过。原场景已修，但增量购买补丁和任务通知的重试仍有以下遗漏。

| 响应事项 | 本轮结论 |
|---|---|
| 同款限时购买延期 | 原探针通过，数据库延期生效；客户端补丁不完整，见 R1。奖励路径仍插新行是响应明确记录的未对齐范围，本轮不重复算新问题。 |
| 购买只下发受影响行 | 已去掉全量 152/154；新返回值不能当完整道具编码，见 R1。 |
| 购买失败第二字节 | 常量 1，原探针通过。 |
| 公仓删除页权限 | 原探针通过。 |
| 连杀窗口 | 原基线窗口探针通过。本轮没有重新把已决定复刻的边界行为算作缺陷。 |
| 任务通知局内口径 | 原通知探针通过；事件重复投递会重复推进内存计数，见 R2。 |
| SetTitle 事务 | 原并发探针通过，事务包含读取和写回。 |
| 名牌/特效活性检查 | 原过期行探针通过，`activateExpiry` 使用 liveItemSQL。 |
| S-3 未建模位无载荷 | 同意响应不采纳的结论。`packetmanager.cpp:WriteSettings` 原样写 flags，再走固定字段表；不能仅凭置位但零载荷判为错位。原 `TestRoomSettingsZeroWidthFieldEcho` 仍失败属于探针预期与基线不符，不是未修产品缺陷。 |

此前 `review-21801e6` 的四条夹具问题和 `review-70bd62e` 的 166 随机误报已由后续报告解释，本轮不再拿这些断言要求产品修改。

### R1 [P1] 堆叠/延期返回的部分 Item 被作为完整 154 道具下发，清零客户端使用状态等字段

- **位置**：`internal/store/purchase.go:147–152,180–187`；调用为 `internal/server/shop.go:PurchaseTracked → sendInventoryItems`。编码器 `internal/server/lobby.go:473–516`。
- **基线**：`manager/itemmanager.cpp:AddItem` 的可数物品分支（约 750–773）保留完整 `itemWithSameID`，只修改 count 后发 `SendInventoryAdd`；`ExtendItem`（约 1726–1754）同样保留原道具字段，只修改期限后发送。
- **问题**：堆叠分支只填 Slot/ID/Count，其他字段变成 Go 零值；延期分支只填 Slot/ID/Count/Obtain/Expiry/InUse，遗漏 Status、Paint、Lock 等。154 是按槽位增量，但每条非墓碑记录包含完整道具字段，`writeItem` 会把这些零值全部写到线上，不能把“只返回受影响行”理解为“行内只填受影响列”。
- **复现**：对一件 `status=1,inUse=1` 的限时物品续购；DB 仍为 status=1，返回补丁却为 status=0。对一件可数道具再购一次；DB 的 inUse=1，返回补丁却为 inUse=0。为同一行保留 paintID=744、lockStatus=1 时，两条分支都会把补丁中的它们清为 0。
- **实测**：延期补丁 `Status=0,Paint=0,Lock=0`，DB 为 `Status=1,Paint=744,Lock=1`；堆叠补丁 `InUse=0,Obtain=0,Expiry=0`，DB 为 `InUse=1` 且取得时间、期限均未清零。
- **影响范围**：堆叠分支也是 `grantItemsTracked` 的共享实现，重复扭蛋 5101 的返回补丁同样受影响。旧扭蛋探针验证了数据库合并和首次奖品形状，没有验证第二次返回补丁的 InUse，因此未发现此问题。
- **建议**：在同一事务内读取完整的最终受影响行再返回，或保留完整原行并只改动实际列。发送前还应考虑自动装备等后续写库对补丁的影响。不要改回每次发送整个背包。
- **探针**：`TestReview862PurchasePatchMatchesStoredRow/extend`、`…/stack`，均失败；按 itemID 过滤，不依赖夹具背包总行数。

### R2 [P2] 任务事件已在数据库去重，新增局内通知计数仍会重复累加

- **位置**：`internal/server/quests.go:308–338`；数据库去重在 `internal/store/quests.go:RecordQuestProgressOnce`。
- **问题**：`RecordQuestProgressOnce` 命中重复 eventKey 时成功返回，但没有告知调用方“本次未应用”；随后 `questMatchUnits` 无条件加 delta。因此持久化进度不变，内存进度和里程碑通知却改变了。
- **复现**：任务 Goal=100、Notice.Goal=2，每分钟加 1。对同一个 stat 连续投递两次 `Key="review862/minute/1"`。DB units=1，内存 questUnits=2，并发出一条不该出现的 `2/100` 通知。
- **可达路径**：`tickQuestTime` 仅在整个 `applyQuestEvent` 成功后推进 questMinutes；多任务逐个写入时，后一个任务出错会让已经成功写入的前一个任务在下次 tick 重放相同 key。新增局内计数也未进入重连快照，重连后通知累计会重新开始，这是同一份派生状态的另一个维护缺口。
- **分类**：Go 新增的事件幂等/派生状态一致性问题。基线没有 Go 的 eventKey 重试机制，本条不要求撤销按基线改正的取模通知规则。
- **建议**：让记录接口返回是否实际应用或实际增量，只在首次应用时推进局内计数；同时明确任务部分失败和重连时如何恢复该计数。
- **探针**：`TestReview862RepeatedQuestEventDoesNotRepeatNoticeProgress`，失败：`units=2 notices=1, persisted=1`。

## 二、新的整体性审查

扩大检查了在线奖励、GM 发奖、目录奖励领取、扭蛋共享发放、任务分钟重试、重连快照、道具编码及相关缓存更新。抽查了新旧探针是否真实断言返回补丁，而不只断言数据库。以下为新确认问题；已登记的家族战名单口径、掉落箱系统文案、购买请求客户端例外和登录包序没有重复列级。

### N1 [P1] 在线目录奖励及 GM 发奖落库后不刷新角色缓存，也未推送货币/经验变化

- **位置**：`internal/server/login_extra.go:52–75` 的 `grantCatalogReward`；相同遗漏见 `internal/server/gm.go:gmGrantReward`。场景掉落入口 `internal/server/host.go:grantDropBox` 调用前者。
- **基线**：`manager/itemmanager.cpp:GiveReward`（约 1915–1932）对 exp/points/honorPoints 分别调用 `UpdateExp` / `UpdatePoints` / `UpdateHonorPoints`；这些用户更新方法发送 `UpdateClientUserInfo`。166 则经 `AddItem → UpdatePasswordBoxes` 更新角色计数。
- **问题**：`Store.GrantReward` 已修改角色，调用方只发送库存 154 和奖励通知 67，没有重新读取 `c.character`，也没有 157。相比之下，`finishRewardClaim` 和商店成功路径已经实现角色重读及 157，扭蛋 166 的相同问题也已在前轮修过。
- **复现**：以当前完整角色作为在线缓存，将目录奖励 3000 设为固定 Points=50，经 `grantCatalogReward(c,3000)` 发放。DB points 增加 50，缓存不变，队列没有 157。
- **实测**：`persisted=50 cache=0 sent157=false`。
- **影响**：场景掉落箱、在线 GM 发奖的货币/经验/等级/密码箱等角色字段与客户端和在线缓存不同步；后续用户信息广播还可能重新发送旧值。奖励通知不能代替基线的角色字段更新。
- **建议**：复用统一的发奖后角色刷新/通知流程，覆盖在线目录奖励和 GM 发奖；仍在登录链上的离线奖励应单独保持既定登录包序，不能无差别插包。本条没有声称所有奖励入口都缺失更新，选择型领取路径已有处理。
- **探针**：`TestReview862CatalogRewardRefreshesOnlineCurrency`，失败。

### 验证记录与复现命令

- 使用 `GOTOOLCHAIN=local`。根目录 `go build ./...`、`go vet ./...`、`go test ./...` 全部通过；完整测试在允许本机回环连接的环境运行。
- 上轮八条 `TestReviewHead*`、存储层九条 `TestStoreProbe*` 全部通过；S-3 零载荷探针仍按上述理由预期失败。
- 本轮新增三项测试，其中购买测试有两个子场景：两个服务器测试及两个存储子场景均在预期断言处失败。
- 新探针已 gofmt，overlay vet 通过（exit 0）。未进行新的长时间 fuzz 或客户端实测，不复述此前长跑次数为本轮覆盖。
- 仅新增 `code-review/probes/review-86212b7/` 及本文件，未改数据、业务代码和历史探针。

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay code-review/probes/review-75cddae/overlay.json ./internal/server ./internal/store -run '^TestReviewHead' -count=1
go test -overlay code-review/probes/review-75cddae-store/overlay.json ./internal/store -run '^TestStoreProbe' -count=1
go test -overlay code-review/probes/review-86212b7/overlay.json ./internal/server ./internal/store -run '^TestReview862' -count=1
go vet -overlay code-review/probes/review-86212b7/overlay.json ./internal/server ./internal/store
```
