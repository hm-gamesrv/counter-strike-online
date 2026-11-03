# 4308aae 审查报告

审查源码 HEAD：`4308aae`，业务改动截至 `8fa33d5`；工作树原有 `compose.yaml` 修改未触碰。按用户最新说明，以 30002 为 WSL 重写版、30003 为 Windows 原版，覆盖请求中已过时的实例部署说明。两服使用同样数据由用户确认，本轮未重新计算全部数据文件哈希，也未独立证明运行二进制的构建提交。

唯一报告文件，分为下面两节。探针与网络证据均在 `.code-review/probes/review-4308aae/`。未修改业务代码和已有测试；黑盒执行了测试账号正常登录、临时建房及断线退出，会产生正常会话记录，没有直接编辑服务数据文件。

## 一、对上次修复回复的再审查

依据 `reports-responses/CODE_REVIEW_A0BB9A6_RESPONSE.md` 核对 `5b6c6f4`、`8fa33d5`，参考树通过 `scripts/check-ref.ps1` 确认为 `4859a6e`。

| 原问题 | 复核结果 |
|---|---|
| R1 登录扫描 skip 导致旧背景缓存 | 原探针通过；skip 分支无广播重读角色数据 |
| R2 强化消耗外观材料不清引用 | 原探针通过；Store 清引用，Server 两条强化路径刷新缓存 |
| R3 职业加成使用旧库存快照 | 原探针通过；HasActiveItem 查询与基线一致，未额外加入数量/到期条件 |
| R4 同款材料翻倍公式 | `enhanceMaterialExp` 已按先累计再翻倍；现有确定性回归通过 |
| R5 Voxel 保留 bit 18/20 | 原两个子用例通过；128 个外层字段空值宽度矩阵通过 |
| 元数据 JSON 顺序 | 实测两服 32 类元数据解压内容全部一致，包含类型 4/6/16；不能据此推定全部协议已一致 |

本轮未确认上述修复的新回归。历史两条基于“仅内存职业库存”和“装备加成默认开”的旧探针，前提已被后续决定替代，不将其失效判为当前实现回归。

验证：本地 `GOTOOLCHAIN=local`，build、vet、现有完整测试通过；上一轮 `review-a0bb9a6` 的 `TestReviewA0*` 全通过。两个运行实例各通过现有 5 个冒烟用例。元数据运行结果为 `metadata types: 32, identical content: 32, differing: 0`。

## 二、新的整体性审查

新增确认 **1 项 P1、2 项 P2**，三条隔离探针均复现失败。优先检查“服务器实际允许消耗什么”、库存级联的返回补丁，以及可选角色字段的帧预算。没有向运行中的原版发送破坏性消耗请求；这类负例使用临时数据库白盒复现。

### N1 [P1，偏离基线] host 的场景消耗入口能删除已选中的武器

- 位置：`internal/server/host.go:443`、`internal/store/consume.go:19–31`。
- host 子类型 100 验证了玩家在同一房间，却没有限制目标为实际可使用的道具，随后 ConsumeItem 仅检查 ID、数量和保护列表，直接减数/删除。
- 基线对应：`manager/hostmanager.cpp::OnUseInGameItem` 调用 `CItemManager::UseItem`，不是直接 OnItemUse；`itemmanager.cpp:1351` 的 CanUseItem 要求 status=0，随后还有 ClassName/具体物品的用途分派。status=1 的选中武器会被拒绝。
- **复现**：真正调用 hostRequest，向同房间玩家的非保护、status=1 武器发送 kind100。武器库存行被删除。探针使用 ID12、空保护列表的有效简化配置，避免把默认保护列表的偶然拦截当作用途验证。
- 影响：有权发送 host 包的客户端可以消费用途不符的同房玩家道具；“这个入口只会消耗喇叭/掉落箱，所以不需要级联”的判断不成立。这里不声称任意未认证客户端都能调用。
- 建议：复刻 UseItem 的可用状态和用途分派，只允许该入口支持的消耗动作；不能只补 clearItemReference 来让非法消耗变得一致。仅加 status=0 也不能替代用途校验。
- 探针：`TestReview4308HostCannotConsumeSelectedWeapon`。

### N2 [P2，帧预算健壮性] 大厅人数上限忽略战队等可选字段

- 位置：`internal/server/limits.go:16–25`，发送入口 `internal/server/lobby.go::joinLobby`。
- maxUserInfoSize 仅以无战队、无背景、无 tournament 的角色编码计算预算，名字虽取最大长度，其他可选段仍被省略。validateWireLimits 因此接受不能保证安全的 MaxPlayers。
- **复现**：配置允许的 MaxPlayers=202，每个角色使用合法长度的用户名/角色名，战队名仅 `ReviewClan`。joinLobby 生成 153 包 **69693 字节**，超过 MaxPayload=65531。现有边界测试仅断言计算器接受自己算出的边界，没编码实际的最坏角色内容。
- Codec.Write 会拒绝超长帧，正常服务路径中的发送协程随之关闭连接。这里是可配置人数下的可用性缺陷，不是已经实测的客户端崩溃。
- 基线对应 `packetmanager.cpp::SendLobbyJoin`、`packethelper_fulluserinfo.cpp::Build` 也包含可选/变长字段；本项不声称 C++ 大人数场景一定安全，也不以减少字段后的 Go 帧较小为证明。问题是 Go 自身“该人数保证能装进帧”的配置校验不成立。
- 建议：预算涵盖所有实际可下发的可选段及字符串长度上限，或在生成名单时按协议支持的方式拆分/限制；若没有续包协议，应采用真正保守的人数上限。room joined 等复用该大小估计的预算也应一起复核。
- 探针：`TestReview4308LobbyBudgetIncludesClanFields`。

### N3 [P2，Go 级联同步不完整] 公仓存入后接替状态只落库、不下发

- 位置：`internal/store/clan_storage.go:127–137`；调用方 `internal/server/clan.go::clanStorageGive`。
- GiveClanItem 新增 promoteItemSelection 后会把同款剩余副本置 status=1，却仍只返回被存入槽位的墓碑。服务端照这个返回值发 154，refreshCharacterCache 不会发送库存选择状态。
- **复现**：同款武器在槽位2、3，3被选中；将3存入公仓。数据库中2已成为 status=1，但返回补丁仅有 `{Slot:3,ID:0}`，客户端中的2仍是旧选择状态。
- 基线 `clanmanager.cpp::OnClanStorageGiveItemRequest` 只发移除记录，不能直接拿它来证明 Go 新增的接替更新也无需发送。本项针对已接受的 Go 级联语义与其网络结果不一致，不冒充“基线本来已完整实现接替”的新协议偏差。
- 建议：返回墓碑和真正被改动的接替副本，再由同一提交后的路径发送；保持事务内状态与事务后补丁一致。
- 探针：`TestReview4308ClanDepositReturnsPromotedCopy`，数据库接替断言先通过，再断言缺少更新而失败。

### 本轮两服黑盒实测：纠正旧差异表

同一账号、单个大厅用户场景，30002 登录共58包，30003共60包。下面均为本次抓取事实，不是套用旧回复里的方向。尤其原请求 P0-6 中不少条目正反写颠倒，不能照旧表直接给 Go 增删调用。

| 项目 | 30002 重写版 | 30003 原版 | 判断 |
|---|---|---|---|
| 74/76 | 74→76 | 76→74 | 仍有包序差异；基线源码先配置再封禁设置 |
| 103 URLs | 登录索引43，背包前 | 索引59，69之后 | 旧表的方向相反 |
| 151 房间列表 | 1条 | 2条 | 不能按旧表“删 Go 多发的一条” |
| 180 联赛 | 153/151之前 | 153/151之后 | 旧表方向相反 |
| 153 大厅 | 314B，label=`tester1` | 412B，label=`test_lobby` | 长度不能仅归因于人数；本次两边 count 都是1 |
| 72/15 推荐 | 34B，`Recommended`/空串 | 32B，`Test`/`Test2` | 与当前 Go 和固定 C++ 源码分别吻合；不是相同 JSON 读出不同推荐商品，而是标题常量不同 |
| 157 角色 | 303B，low flags=`0x0c4d03eb` | 398B，low flags=`0xffffffff` | 已知 flag/可选段策略差异，非本轮新发现 |
| 67 提示 | 2条 | 3条 | 基线多一条168B固定欢迎语；公告相对顺序也不同 |
| 临时建房 | 65包407B、6B | 65包6B、18B、18B、634B | 同一请求均有响应，尚未逐字段归因；不把长度差直接定为错位或崩溃 |

推荐标题源码位置：Go `shop.go:203–204`，基线 `packetmanager.cpp:2142` 起；大厅 label 源码为基线 `SendLobbyJoin` 中的固定 `test_lobby`。这些是已知但尚未批准的字节差异；若继续要求严格等价，应修复或明确批准例外。本轮不把这张既有差异表中的每一行再包装成独立新 bug。

关于157：flag真实描述所带字段可以在解析上自洽，但自洽不等于基线字节一致。Rank单条回包不实现，并不自动授权改全部角色信息 flags；保留现状需要显式记录批准的例外，不能将它描述为已经完全复刻。压缩器导致的 zip 字节差异则建议以解压内容一致为判据，本轮32类均一致。

### 证据、复跑及范围

- 单份报告：本文件。探针路径：`.code-review/probes/review-4308aae/`，包含 server_test.go、store_test.go、overlay.json，Go代码已 gofmt。
- 网络证据：trace-30002.txt、trace-30003.txt；live_compare.py 与 live-comparison.json 保存正常登录/临时建房的包内容（不保存登录密码）。通过 Client 上下文关闭连接退出临时房间，没有重启或替换两服。
- overlay 目标为 `internal/server/review_4308_test.go`、`internal/store/review_4308_test.go`，使用绝对路径增补测试，不改已有源码。

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay .code-review/probes/review-4308aae/overlay.json ./internal/server ./internal/store -run '^TestReview4308' -count=1
python .code-review/probes/review-4308aae/live_compare.py
python blackbox/metadata.py --a 30002 --b 30003
```

覆盖：修复 diff、元数据顺序、登录/正常建房网络对照、host消耗入口、公仓补丁、大厅帧预算。build/vet/完整测试通过，独立问题探针三条按预期失败；两服冒烟各5例通过。没有做游戏 GUI 端测、HLDS/NAT验证或全仓等价证明。未完成的模糊测试与结果包金标准仍未完成。
