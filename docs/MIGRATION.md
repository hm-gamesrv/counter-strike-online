# 迁移记录

基准：`.ref/counter-strike-online@947fee4966381e05a4051cce8e24f23b351f1aaa`、`.ref/CSNZ_Server@12644f2adc0708e2d8b74f7dd6a4c3c29d3e1140`（部署形态与协议/数据基线）。

| 上游 | Go 实现 | 剩余工作 |
| --- | --- | --- |
| `net/{extendedsocket,sendpacket,receivepacket}.cpp` | `internal/protocol` | SSL、实客户端抓包验证 |
| `serverconfig.cpp` | `internal/config` | 随业务迁移补齐配置项 |
| `manager/userdatabase_sqlite.cpp` | `internal/store` | 其余角色字段、会话历史、迁移工具；账号封禁与 HWID/IP 封禁已实现 |
| `manager/usermanager.cpp` | `internal/server/{server,lobby,ban,update_info,messenger,addon,kick}.go` | 完整 UserInfo、装备；改名/位置/聊天色、Messenger、快捷购买与 Addon、superRoom 踢人、登录问卷、全服喇叭已实现，屏蔽名单已实现列表/设置/登录下发及聊天、私聊、房间加入过滤 |
| `manager/channelmanager.cpp`、`room/roomsettings.cpp` | `internal/server/{rooms,room_defaults,room_social,host,gm}.go` | 已实现邀请、房主踢人及踢出名单，以及 `ValidateSettings` 下的人数/时间/胜负/地图上限校验；仍缺 playlist、zbLimit、投票踢人、特殊模式限制与地图轮换 |
| `manager/packetmanager.cpp` | `internal/server/{metadata,lobby,rooms,shop}.go` | 随各业务逐项实现出站结构 |
| `manager/shopmanager.cpp` | `internal/server/shop.go`、`internal/store/purchase.go` | 当前客户端购买、计数堆叠和密码箱计数已实现；完整延期规则、礼包校验仍缺 |
| `manager/itemmanager.cpp` | `internal/server/{inventory,rewards,reward_claims,reward_random}.go`、`internal/store/{inventory,purchase,rewards,reward_claims,item_extend,paint,part,costume}.go` | 已实现武器/角色启用、选择副本、锁定/解锁、固定/选择/随机奖励与领取去重、开箱、延期、武器涂装、部件、普通服装、ZB 皮肤、强化（含防降级）与删除式分解；仍缺完整合并 |
| `manager/{host,dedicatedserver}manager.cpp`、`room/gamematch.cpp` | `internal/server/{host,dedicated,catalog}.go`、`internal/store/{match,levels}.go` | 已实现开局、主机背包、计数、存档迁移、专服池、场景道具使用、场景掉落箱奖励、装备加成与基础结算；尚需真实客户端/HLDS 对局验收、ZBS 名次奖励及特殊模式规则 |
| `manager/questmanager.cpp`、`quest/questevent.*` | `internal/server/{quests,title}.go`、`internal/store/{quests,quest_events,title}.go` | 已接活动任务条件与持久化进度、完成奖励，以及 `84` 称号前缀/槽位；尚缺任务界面、每日/荣誉/军团任务及称号解锁、VIP 事件与完整实机验收 |
| `manager/{clan,rank,minigame,luckyitem,voxel}manager.cpp` | `internal/server/{clan,minigame,bingo}.go`、`internal/store/{clan,clan_storage,weapon_release,bingo}.go` | 军团 7 张表、协议与仓库已接；武器解放与 Bingo 基础流程已接；仍缺军团任务、军团战踢人、排行、抽奖、Voxel 房间列表与 Studio 房间 |

当前已跑通真实客户端机器人对局、正常结算和返回房间。后续优先级按用户要求执行下表；每项应以原版出入站协议样本、当前客户端和数据库前后快照校验。

## 后续优先级（2026-11-02 确认）

1. **通用道具发放/使用**：提取商店之外也能使用的事务发放入口，完善计数消耗、归属/有效期/保护检查，以及物品变化同步；再补合并、延期和装备规则。
2. **奖励系统**：读取配套 `ItemRewards.json`，实现等级限制、物品/货币/经验奖励、礼包使用与领取，再处理选择和随机奖励。扣除来源道具与整包发奖必须同一事务，失败完整回滚。
3. **任务事件联动**：读取任务和活动条件，将登录、升级、击杀、对局时间/结束等事件接入进度，持久化进度并保证领取不重复发奖。
4. **房间与频道**：优先补完整设置校验、特殊模式规则、地图轮换、投票踢人和多频道。
5. **对局与结算**：优先补道具加成、任务事件奖励、模式专用统计与结算，并扩大多人实机和真实 HLDS 验证。

以下差距保留在待办中，排在上述工作之后：

| 范围 | 剩余内容 |
| --- | --- |
| 武器养成 | 完整道具合并规则 |
| 抽奖和小游戏 | 密码箱实际开启、概率抽奖、武器集字兑换；Bingo 桌面/开格/洗牌已接，仍待实机验收 |
| 军团 | 创建/申请/审批、成员管理、仓库及军团任务 |
| 玩家外围 | 屏蔽名单、问卷、通知/举报、Addon、完整玩家资料、快捷购买配置 |
| 管理与运维 | 管理 GUI/命令、IP/HWID 封禁管理、SSL |
| 上游未完整实现的模块 | 排行公开版为占位/禁用状态；Studio 仅有部分协议和 HTTP 对接，需要独立设计和当前客户端验证，不能当成现成功能直接搬运 |

内置配置、元数据或数据库表不代表对应业务已实现。专服分配、主机迁移已有 Go 实现但仍缺真实 HLDS/多人实机验收，须与“尚未重写”区分。

### 第一批进展：通用发放与固定礼包

- 商店扣款与物品发放分开，抽出共用的事务发放逻辑；商店和礼包统一按当前物品表决定可用、选中、休眠、堆叠状态。
- 新增内部 `GrantReward` 入口，支持物品、积分、经验、荣誉点奖励。来源道具扣除、整包物品和角色数值在同一事务中提交；满背包、写入失败、锁定、过期、跨账号及等级不足均不会消耗来源道具。
- 接入当前客户端物品使用请求和 `Item.csv.rewardID`，读取 `ItemRewards.json`；支持固定物品礼包与配置中的货币/经验候选值，单次最多 200 份，并在分配重复奖励数组前检查通知帧上限。
- 等级使用限制以当前 `Item.csv.Level` 为准，与客户端一致。奖励 JSON 中旧的 `LvlRestriction` 编号与当前物品表不一致，C++ 使用路径也未据此限制使用，不能用它阻止客户端已达到等级的礼包。
- 自动测试覆盖事务中途失败、整包空间不足、并发重复使用只成功一次、跨账号/锁定/过期/保护校验、低等级拒绝、来源槽位删除、下一档礼包和通知；现有明文、RC4、模拟专服链路继续通过。
- 实机：`GoValid2` 使用 1 级补给包（1001），来源槽位 29 删除，11 项奖励进入槽位 31–41；随后在 3 级使用物品 1002 成功，得到 5 级礼包 1003。金币券 1030 合并为 6 张，使用一张后剩余 5 张，余额 `100399 → 100899`，界面与数据库一致。
- 测试库备份 `.dist/client-before-rewards-20261102.db3`；实机日志 `.dist/rewards-qa.log`、`.dist/rewards-next-qa.log`。
- 最终 `go fmt ./...`、`go vet ./...`、`go test -race ./...` 和 Windows 构建均通过；最新可执行文件为 `.dist/cso-server-next.exe`。当前运行的实机服为 `.dist/cso-server-rewards-next.exe`，与最新构建的差异仅为最后补充的批量上限 200 和通知长度预检查；实机已验证的使用数量均为 1。
- 已知显示问题：当前客户端未解析部分等级礼包标题/描述键（如 `LEVEL1_GIFTBOX_TITLE`），奖励列表与发放正常；需要补当前资源的文案，不能视为完整本地化验收。

### 第二批进展：选择/随机奖励与领取记录

- 迁移当前客户端 UMsg 67 选择界面及其回包；候选项绑定玩家持有道具和服务端生成的领取凭据，不能直接发送奖励编号领取任意物品。取消不扣道具，重登可恢复；旧凭据、越界选择、跨账号和重复领取均拒绝。
- 发奖前持久化完整结果。背包不足、断线、进程重启或重复使用不会重新抽取物品/货币；确认领取、扣来源、整包发奖在同一事务中提交。已过期或消失的来源会使旧凭据失效，锁定来源保留待领取状态。
- 随机奖励按原版顺序逐个判断概率桶、最后一个桶兜底，并支持随机时长和永久标记；当前配套奖励表没有 RandomItems 条目，此部分以合成配置自动测试验证，不能算实机随机奖池验收。
- 实机自选箱 2214：槽位 47 消耗，选择第 1 项后只发放物品 270 到槽位 48；客户端界面显示金红双蝎，库中领取凭据 `3341577777` 状态为已领取。确认当前客户端能完整回传高位为 1 的 uint32 凭据。测试前快照 `.dist/client-before-claims-20261102.db3`，日志 `.dist/claims-qa.log`。

### 第三批进展：活动任务事件

- 读取 EventQuests.json；支持模式、地图、参战人数、枪械与持有检查、双方阵营、击杀类型、机器人/真人、连续击杀及怪物类型过滤。不认识的条件/字段拒绝加载，避免漏掉条件后放宽任务要求。
- 接入登录、升级、击杀、每分钟、正常对局胜利，以及爆破/拆弹/人质逃脱/怪物/蚊子/风筝等主机事件。修正参考实现中枪械查找范围为空、Bot 三元表达式及连续击杀计数偏一的问题；死亡会清除连续击杀计数，平局和强制结束不记作胜利。
- 进度上限、完成奖励快照和唯一领取凭据事务保存；事件投递失败可以重试，部分任务已计数时不会重复累加。升级及结算事件与角色数值在同一事务中进入持久队列，重启后登录可继续处理。对局期间只记进度并排队奖励，在关闭结果/返回大厅后发放，避免破坏结果页的余额缓存。
- 当前客户端配套与上游的活动表均引用奖励 1111/1112，但两份奖励表均没有定义；启动会明确警告，完成进度保留，补齐配置后可恢复发奖。没有自行编造这两项奖励，也没有修改原客户端表。用于自动验证的补充奖励仅存在于临时测试配置中。
- 自动验证覆盖真实主机击杀帧解析、非主机/截断拒绝、枪械持有与机器人过滤、进度上限、缺失奖励恢复、分钟去重、事务故障回滚、并发完成、重启领取、重复领奖及事件部分投递重试。任务界面、每日/荣誉/军团任务、VIP 事件和活动任务实机完整流程仍待实现或验收。
- 本轮自选箱实机验证后，用户以物理 Esc 停止 Computer Use，随即停止界面操作；未切换运行中的测试服。当前实机服仍为 `.dist/cso-server-claims.exe`（选择奖励版本），任务事件代码对应新构建 `.dist/cso-server-next.exe`，不能将两者的验证范围混同。
- 最终代码 `go fmt ./...`、`go vet ./...`、`go test -race ./...` 及 Windows 构建通过；本批完整 `go test ./...` 也已通过。最后的竞态检查包含连续击杀恰好达标及死亡重置测试。最终构建为 `.dist/cso-server-next.exe`。

**后续优先级**：继续补任务验收和已记录的数据缺口，然后优先房间/频道设置校验、特殊模式、地图轮换、投票踢人与多频道，以及对局道具加成和模式结算。完整道具合并/延期/装饰装备仍在待办中。上述大项尚未全部完成。

## 本次对局迁移验证

- 自动链路测试覆盖明文、RC4 和模拟专用服务器：注册、登录、创建角色、建房、加入、准备、开局、主机背包、击杀计数、结算、第二局和断线清理。
- 结算在 SQLite 事务内写入 `GoMatchSettlement` 扩展表，以 `(matchID,userID)` 去重；写库失败时整批回滚，重试不重复发奖。保留上游经验上限 `30069814` 与等级阈值。
- 真实客户端路径为 `S:\.temp\CSNZ\CSNZ`。本机测试服使用 `127.0.0.1:30012` 和 `.dist/client-test-data`，已跑通自动登录、大厅、建房、圣诞大厦地图（模式 4、地图 277）、机器人入场与击杀、死亡/复活、正常结算和返回房间。首局 matchID 为 `681fd48ab47b18e1cc337364fc4c9a69`，获得 230 经验和 200 积分，角色升至 2 级；数据库积分为 `100200`，结算表仅一条记录。
- 客户端配套 `Server/Data` 表与固定上游不同，旧表导致菜单入口缺失和模式名称异常。将配套表复制到独立测试数据目录后恢复；未修改原客户端表或原服玩家数据库。
- 修复已登录客户端在大厅空闲两分钟就断线的问题：读取超时仅限制未登录连接，已登录用户与注册专服不再套用该超时。
- 进图发现缺少服务器端默认房间设置，导致计时为零、没有机器人。现已按配套 `GameModeList.csv` 读取默认限制，并迁移基础计时、机器人、购买时间、初始资金等设置；切换模式时重置这些设置。真实对局已确认机器人和计时恢复；测试覆盖默认值、已有击杀限制保留、模式切换及字段序列化往返。
- 新角色初始武器的 `inUse` 标记修正为可用，默认配装读取按配置的 1–4 编号映射到数据库的 slot0–slot3。新测试角色 `GoValid2` 已在真实地图拿到 MP5（30/120），三种自动对局场景也检查主机背包包含 MP5。此修复不自动改写旧角色已有背包和配装。
- 结果页会将单局积分加到客户端缓存的余额上。首局发现提前同步最新余额导致总额显示重复增加；现改为先同步经验/等级，关闭结果页时再同步余额与累计战绩。数据库去重逻辑不变，自动测试验证同步字段和时机，真实界面复核进行中。
- 已迁移房间邀请列表/邀请通知、房主踢人、踢出后禁止重新加入与房主迁移时清空踢出名单；测试覆盖非房主、自踢和游戏中踢人的拒绝，以及截断邀请不产生部分发送。
- 已迁移武器配装和大厅背景角色保存，检查物品持有、有效期、类别和当前配装编号；未持有、已过期、错误类别/配装及截断请求均有测试。装饰装备及完整道具激活/合并规则仍未迁移。
- 普通购买武器修正为可用，同款武器切换选中副本；计数型道具使用可用但未选中状态，待激活/装饰物品保持休眠。切换旧副本、发放整包和扣款同处事务，注入失败测试确认旧武器选择和余额一起回滚。完整堆叠、延期、装饰自动装备规则仍待迁移。
- 第二局在困难机器人设置下正常开局，新角色持 MP5 参与对局，随后服务器记录正常结算（matchID `9dbcc02a117cc83272cef353ae78deee`、`forced=false`）；数据库确认第二个角色同样获得 230 经验和 200 积分，只有一条对应结算记录。界面复核期间 Computer Use 被物理 Esc 停止，本轮不再操作客户端；余额显示修复仍需最终实机复核。
- 真实 HLDS、多人实机、其他游戏模式仍未验收。

## 当前客户端与道具迁移

- 根据用户要求，仅以 `S:\.temp\CSNZ\CSNZ` 的 `30.12.24` 客户端为目标；购买请求只接受其 uint32 商品编号格式，不保留旧版两字节格式。
- 真实采集的 M24 三天商品请求为 `01 38 00 00 00`，商品编号 56。旧解析把编号读成 0，客户端仍显示兑换成功，但未扣款、未发物品。现已修正解析及失败回复标记，测试覆盖准确扣款、发放、有效期、未知商品失败及错误长度不扣款。实机复验时 Computer Use 被物理 Esc 中止，修复后尚未完成 GUI 验收。
- 内置配套 `Data` 和商店、奖励、皮肤、武器列表更新为当前客户端文件。新数据目录直接初始化；来源说明见 `assets/README.md`。参考源码仅作为业务逻辑参照。
- 新增道具启用、同款武器选中副本切换、锁定/解锁和增量响应。事务内校验所属玩家和有效期，保护默认道具；重复启用不会延长时限。测试验证跨账号拒绝、过期拒绝、切换失败回滚及并发选择仅保留一个选中副本。
- 计数道具可在满背包内合并已有堆叠，数量超出协议上限时整笔回滚；密码箱（166）写入角色计数，不占背包格。完整延期和道具奖励使用仍未完成。
- 核对上游确认账号、道具和封禁使用 Unix 分钟。新写入改为分钟；打开库时将早期 Go 构建的秒时间戳原子转换，重复打开不会重复转换。待启用物品的相对天数保留到启用时转换。独立测试库迁移前备份：`.dist/client-before-minutes.db3`。
- 真实客户端仓库显示 `GoValid2` 余额 `100200`，与第二局结算后的库一致；结果页本身的总额显示仍待单独复核。未将客户端提示作为购买入账成功的依据。

最新 Windows 产物使用 `.dist/cso-server-next.exe`；本地测试服尚为采集购买字段的诊断构建，未在 Esc 停止界面操作后强行重启客户端。

上一轮最终检查：`go fmt ./...`、`go vet ./...`、`go test ./...`、`go test -race ./...` 全部通过；使用当前客户端配套数据的 Windows/Linux amd64 构建均通过。此结果对应当时的代码，不代表后续静态审查修改已完成动态验证。

## 2026-11-01 静态审查

本轮不进行端测，也不执行自动测试；仅进行代码审查、格式化、静态检查及编译。移除了明文密码回退、早期 Go 秒时间戳自动转换、数值端口兼容解析和负时长别名，历史迁移记录中的自动转换行为已取消。修复详情及本轮验证结果见 [STATIC_REVIEW.md](STATIC_REVIEW.md)。

原仓库及上游快照仅供参考。Go 构建只需要 `assets`、`cmd`、`internal`、`go.mod`、`go.sum`。默认资源中 `Metadata_ModeList.bin`、`Metadata_Encyclopedia.bin` 是从上游 `packet_metadata_data.h` 的固定数组提取，其余资源按原样保存。

## 2026-11-02 继续审查

用户允许恢复端测。修复当前商店永久时长标记被误判为兼容别名的问题，并增加协议容量检查、房间设置转发空间检查与对局期间踢人保护。格式化、静态检查、Windows 构建和完整自动测试已通过；按配套 `启动游戏.ps1` 的参数、以 `Bin` 为工作目录直接启动客户端，已连接本机 30002 测试服并进入游戏内登录界面。详细结果见 [STATIC_REVIEW.md](STATIC_REVIEW.md)。

后续按配套启动器 README 的 `-login`、`-password`、`-disableauthui` 参数直接启动，完成自动登录。M24 三天商品购买后余额 `100200 → 100199`，物品进入槽位 30，分钟有效期差为 4320；仓库锁定/解锁同步成功。机器人对局 `26021ab370763f6b44b0024c234f43b7` 正常结束，发放 230 经验、200 积分，数据库只有一条结算记录；结果页和返回后的商店均显示 `100399`，正常返回房间并清理空房间。此前购买与结果页余额同步的待复核项已完成实机验证。本轮再次 `go fmt`、`go vet` 通过，多人、真实 HLDS 及其他模式仍未验收。

## 2026-11-02 密码箱开箱与强化兜底

- 接入 `Item` 子类型 4（OpenDecoder）：读取配套 `ItemBox.json`，按上游规则排序并归一化概率，服务端生成每一项奖励；扣钥匙与整包发奖在同一 SQLite 事务内完成，背包不足或写库失败会整体回滚，不消耗钥匙。
- 返回与上游一致的 `[4, 1, boxID, mileage, 0xFAD, count, awards...]`；永久期限按客户端惯例写 `-1`（uint16 `0xFFFF`），普通期限写天数。
- 上游 `OpenDecoder(user, ReadUInt16(), ReadUInt16())` 的实参求值顺序未固定，因此同时识别 `slot,count` 与 `count,slot` 两种排列，并以实际持有的 `LotteryKeyItem` 槽位锚定；两者都匹配时取较大的槽位值。
- 子类型 14（EnhanceReq）暂未迁移，但改为立即返回 `[14, 5]`（`ENHANCE_SYSTEMERROR`），避免强化界面一直等待。
- 新增测试覆盖正常开箱、未知箱子不扣钥匙、两种字段顺序、背包满回滚和强化错误回包。`go test -race ./...`、`go vet ./...` 和 Windows 构建通过；尚未用真实客户端点击开箱，界面提示与实际包顺序仍待实机复核。

## 2026-11-02 屏蔽名单管理

- 接入 `74 Ban` 的列表、添加、移除、屏蔽设置和列表上限请求；登录时下发 `UserBanList` 与 `banSettings`。
- 添加时按上游返回 `-1` 目标角色不存在、`-2` 已屏蔽、`-3` 超出 `BanListMaxSize`；移除不存在返回 `-1`，通过 `67` 通知返回对应本地化键。
- `UserCharacterExtended.banSettings` 持久化；`BanListMaxSize` 加入 `ServerConfig.json` 读取与校验，默认 300。
- 接入 `banSettings` 的聊天/私聊与房间加入过滤：位 1 阻止被屏蔽者加入房主房间，位 2 过滤大厅/房间聊天，位 4 拒绝全部私聊；私聊同时检查双方设置。
- 新增测试覆盖添加/重复/不存在、列表上限、设置、上限查询、移除/不存在、登录下发，以及聊天、私聊、房间加入的过滤策略。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 改名卡、位置与聊天色

- 接入 `69 UpdateInfo` 的改名请求：校验 4..16 位、唯一性和字符集，消耗 `65` 改名卡；改名、扣道具和唯一性检查在同一事务内完成。
- 成功回 `69 [2, 2, ""]`，失败按上游返回 `-1/-2/-3/-4/8` 及对应本地化键；改名后同步背包并广播 `157` 用户信息。
- 接入位置 `69/4`：保存 `nation/city/town` 并在 `157` 用户信息中按 `UFLAG_LOW_LOCATION` 序列化；教程标记按上游只读取、不持久化。
- 接入聊天颜色 `69/15`：校验物品资源名包含 `chatcolor_` 且玩家实际持有，更新 `UserCharacter.chatColorID` 并广播。
- 新增测试覆盖改名成功/重复名/缺道具、位置保存/广播及聊天色持有校验。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 Messenger 资料查询

- 接入 `93 Messenger` 类型 1：按昵称查询目标角色，返回 `[1, userID, "22223123132", FullUserInfo]`。
- 查询使用 `COLLATE NOCASE`；找不到时返回 `userID=0` 和请求中的昵称，不泄露其他账号信息。
- 新增测试覆盖命中目标角色和不存在昵称。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 道具延期

- 接入 `78/16 ItemExtend`：目标槽位按天延长，消耗 `676` 延长道具；目标有效期为 0（永久）时拒绝，避免误扣。
- 活跃道具按 `days * 1440` 分钟延长；休眠道具按相对天数延长；达到 999 天规则时转为永久。
- 目标更新和延长道具扣减在同一 SQLite 事务内完成，失败整体回滚；返回背包增量，延长道具耗尽时发送空槽。
- 新增测试覆盖正常延期和永久目标不扣道具。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 快捷购买与 Addon 列表

- 接入 `77/1` 快捷购买：保存槽位 0..4，每组名称最长 32 字节、11 个 uint16 商品编号；已有槽位更新，不存在时插入。
- 接入 `115 Addon`：按 `UserAddon` 下发 uint16 数量与 itemID 列表，空列表返回 0 项。
- 新增测试覆盖快捷购买首次写入/同槽更新不重复，以及 Addon 列表包。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 武器涂装

- 读取 `WeaponPaints.json`，建立武器到可用涂装的兼容表。
- 接入 `78/17`：校验武器与涂装道具归属及兼容性，追加 `paintIDList`、设置 `paintID`，并消耗一个涂装道具；全部在同一事务内完成。
- 接入 `78/24`：切换已拥有的涂装，`paintID=0` 移除涂装；回复 `[17, 1, 1, 1]` 并同步背包。
- 新增测试覆盖获取涂装、涂装道具消耗、切换和移除。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 武器部件

- 接入 `78/21`：类型 1 卸下部件，类型 2 校验武器类别（1–6/11）和部件类别 13 后装配。
- 部件装配消耗部件道具并更新 `partSlot1/partSlot2`；装配和扣减在同一事务内完成。
- 装配成功后返回背包增量，并按上游发送 `21` 部件检查包。
- 新增测试覆盖装配、部件消耗和卸下。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 superRoom 踢人

- 接入 `112` 类型 1：仅允许 superRoom 房主踢同房间成员，校验目标存在、非房主本人。
- 踢人前向房间广播 `[3, userID, 1]`，随后移除成员、记录禁止重入并送回大厅。
- 新增测试覆盖房主踢人、广播包、踢出记录和房间成员清理。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 普通服装

- 接入 `9/15` 的 `HeadCostume`、`BackCostume`、`ArmCostume`、`PelvisCostume`、`FaceCostume`、`Tattoo`、`PetCostume`。
- 按槽位切换 `inUse`；装备时清除同类别旧服装的 `inUse` 并替换 `UserCostumeLoadout` 字段。
- 首次装备时自动插入 `UserCostumeLoadout` 行；返回受影响的旧/新道具并同步背包。
- 新增测试覆盖首次装备、替换旧服装和卸下。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 ZB 皮肤服装

- 读取 `Item.csv` 的 `ZombieSkin` 列，按皮肤类型区分不同僵尸职业的服装槽。
- 接入 `9/15` 的 `ZombieSkinCostume`：装备/替换写入 `UserZBCostumeLoadout`，卸下时删除对应行；旧皮肤同步清除 `inUse`。
- 未配置 `ZombieSkin` 的服装道具按不可用拒绝，不写任何配装行。
- 新增测试覆盖装备、同类型替换、卸下和缺失皮肤列拒绝。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 武器强化

- 读取 `ReinforceMaxLv.csv`、`ReinforceMaxEXP.csv` 与 `Item.csv` 的 `ItemGrade`，建立可强化武器表、等级上限、属性上限和升级经验。
- 接入 `78/14` 类型 0：校验目标与材料槽，按材料品质（含 8481/8482/8566 特例）累加经验，封顶当前等级需求后计算成功率；成功提升等级并随机增加一项属性，失败清空经验，两者都消耗材料。
- 强化状态写入 `enhancementLevel/enhancementExp/enhanceValue`，属性按 7 个 4-bit 字段打包；成功回包包含槽位、等级、属性和尾部标志。
- 目标不可强化、等级已满、材料缺失或重复时返回 `ENHANCE_SYSTEMERROR`，不消耗任何物品。
- 新增测试覆盖成功升级、材料扣减/删除、多级强化和拒绝路径。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

### 防降级与清除强化

- 接入 `78/14` 类型 2：读取目标与材料槽，校验目标可强化且当前强化等级大于 0。
- 道具 5205 清除全部强化，5206–5211 清除对应属性，5250 随机清除一项已有属性；未命中的属性或无法强化时返回系统错误且不消耗材料。
- 结果分别按 `ENHANCE_LEVELDOWN`（3）和 `ENHANCE_FULLLEVELDOWN`（4）回包，包含材料槽、重算等级和属性索引。
- 新增测试覆盖单项清除、全部清除、经验保留和空属性拒绝。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 房间设置校验

- `GameModeList.csv` 现解析 `mode_minplayer`、`mode_create_minplayer` 以及胜负/击杀/时间/回合的 min|max|default|step 范围，不再只取默认值。
- 新增 `MapList.csv` 的 `max_player` 读取；`Room.ValidateSettings=true` 时在创建和设置更新时校验人数、地图人数上限、模式范围和步长，失败回 `Invalid room settings.` 且不落库。
- 开关默认关闭，保持现有房间行为不变。
- 新增测试覆盖合法设置、越界、步长、地图上限、未知地图和关闭开关。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 场景道具使用

- 接入主机包 `68/100`：读取目标用户、道具 ID 和数量，校验目标在同一房间后消耗 1 个道具。
- 消耗结果按背包增量下发给目标玩家，并向房主回 `[100, userID, itemID, 0xFF]`；道具不存在或受保护时静默忽略。
- 数量字段按上游行为忽略，始终消耗 1 个。
- 新增测试覆盖消耗、背包同步、房主回包和未知道具静默。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 场景掉落箱结算

- 接入主机事件 34（掉落箱拾取），读取 `rewardID`，仅接受上游范围 `3000–3023`。
- 按 `ItemRewards.json` 构建奖励，事务发奖，下发背包增量与奖励通知；奖励缺失、背包满或数据库失败只记录日志，不中断对局。
- 新增测试覆盖奖励领取、背包增量和通知包。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 结算装备加成

- 结算时读取 `GameMatch.CalcResults.BonusPercentage.Items`，对带入对局且已启用的道具累加经验/积分百分比。
- 加成写入结果包的 `itemExp`/`itemPoints` 字段，并计入总加成；配置百分比在启动时校验为 0–65535。
- 个人死亡竞赛模式（1/4/22）按分数优先排序，其余模式保持上游按击杀排序，第一名标记随排序结果。

## 2026-11-02 登录公告

- 读取 `ServerConfig.json` 的 `Notices`，按 ID 排序并校验类型、文本和日期范围。
- 登录时下发 `67` 类型 65 公告包：`[65, id+10000, type, url, startDate, endDate, name, description]`，日期按上游从秒换算为分钟。
- 公告 URL 使用上游固定 Discord 地址；非法类型或空名称在启动时拒绝。
- 新增测试覆盖公告排序、字段和非法类型拒绝。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 登录通知与联赛包

- 登录时读取 `UserRewardNotice`，对固定奖励事务发奖并下发背包/奖励通知；发放成功后删除对应记录，选择类奖励保留待后续接入选择界面。
- 读取 `UserExpiryNotice`，以 `67` 类型 62 下发道具 ID，随后清空记录。
- 接入 `League` 请求：类型 0 返回与上游一致的 5 个联赛占位条目，登录时也主动下发。
- 新增测试覆盖到期通知、离线奖励发放与清理、联赛包字段。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 过期道具清理与会话历史

- 每 30 秒及登录时扫描 `expiryDate` 为绝对时间且已过期的背包行，删除道具并写入 `UserExpiryNotice`；休眠道具的相对天数不受影响。
- 登录写入 `UserSession`，断线时把会话移入 `UserSessionHistory` 并记录时长；会话接管时旧连接不会覆盖新会话。
- `Rank` 请求按公开版上游行为回复“Rank system is not implemented”。
- 新增测试覆盖过期删除/通知、会话开始与历史、清理后登录通知。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。
- 上游保留该配置和结果字段但循环被注释；本次按配置启用。
- 新增测试覆盖启用、休眠道具不生效和百分比字段。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 重连时接管旧会话

- 登录发现同一账号已有会话时，关闭旧连接并移除旧映射，而不是直接回复“已登录”。
- 旧连接清理时只在仍持有该用户映射的情况下删除用户和广播离开，避免误删新会话。
- 新增测试覆盖旧连接关闭、映射清理和未知用户。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。
- 原房间与进行中对局的恢复已由后续提交补上。

## 2026-11-02 断线重连恢复房间与对局

- 连接断开时先快照房间、队伍、准备状态和当前对局统计，再走原有离开流程；5 分钟内有效。
- 重连登录成功后自动回到原房间；若对局仍在进行，恢复击杀/死亡/分数/职业/任务计时/背包快照并重新发送 `65` 房间状态、`180` 模式包和 `68` 主机连接包。
- 房间不存在、已被踢、对局已更换、专用服务器已释放或容量已满时放弃恢复，玩家留在大厅。
- 会话接管时先同步离开旧房间并迁移房主，再关闭旧连接，避免旧连接清理误删新会话的房间或统计。
- 新增测试覆盖对局统计恢复、模式/主机包、过期槽位和接管前快照。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 装饰自动装备

- 新增 `AutoEquipCostume`：新获得的普通服装、纹身或 ZB 皮肤在对应配装槽为空时自动设 `inUse=1`、`status=1` 并写入 `UserCostumeLoadout`/`UserZBCostumeLoadout`；槽位已占用时保持休眠。
- 只考虑未使用、未激活且未过期的库存行，避免误装备旧副本或过期道具。
- 商店购买成功后自动装备；奖励领取成功后自动装备，并同步修正背包增量中的 `inUse`/`status`。
- 只对已知服装类名生效，`LobbyBG` 等其它 category 12 物品不受影响。
- 新增测试覆盖普通服装、槽位占用、ZB 皮肤和缺失库存行。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 物品分解

- 接入 `78/7`：读取数量、未知字段和每个物品的槽位，删除选中物品并下发背包增量。
- 与上游一致，分解本身不发放任何奖励；未知槽位、默认/受保护物品和重复槽位被跳过。
- 新增测试覆盖多物品删除、保留未选中物品和未知槽位静默。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 称号

- 接入 `84` 类型 0/1/2：设置昵称前缀、设置称号槽、移除称号槽；槽位 0..4，未知类型只记录调试日志。
- 解析并持久化 `UserCharacter.achievementList` 与 `titles`，用 `achievementList` 校验已解锁称号，未解锁的请求静默忽略。
- `157` 用户信息新增 128 字节解锁位图（位 19）与 5 个称号槽（位 22），更新后向大厅/同房广播。
- 新增测试覆盖前缀设置、未解锁拒绝、槽位设置/移除、越界槽位和用户信息序列化。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 登录问卷

- 读取 `ServerConfig.json` 的 `Surveys`，归一化复选框与文本输入两种答案结构，并在启动时校验 ID、数量和长度范围。
- 登录时仅下发 `UserSurveyAnswer` 中尚无记录的问卷；包格式为 `[0, surveyID, title, questions...]`。
- 接入 `87` 类型 11：校验问卷、问题、选项和重复选项，按问题保存答案，事务提交；成功/非法/数据库错误分别回复 0/1/2。类型 12 按上游读取后忽略。
- 新增测试覆盖问卷包结构、复选框与文本答案、回答后不再下发，以及未知问卷/选项拒绝。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 全服喇叭

- 接入 `67/6`：按道具 204 查找持有数量，消耗 1 个并下发背包增量；数量归零时删除背包行。
- 广播格式为 `[6, senderName, text]`，发送给所有已登录且允许发送者聊天的玩家，沿用接收方 `banSettings` 位 2 与屏蔽名单过滤。
- 没有喇叭道具、道具受保护或数量不足时静默不发送，与上游一致。
- 新增测试覆盖消耗与删除、广播内容、无道具静默，以及被屏蔽接收方收不到消息。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 军团存储层

- 新增 `ClanList`/`ClanInfo`/`ClanForUser`：支持按名称、模式、时间段筛选与分页，读取军团头、最近 10 个仓库物品和编年史。
- 新增 `CreateClan`：校验重名、已有军团/待审申请和 30,000 积分，按 `UserDist.clanIDNext` 分配 ID，写入军团长、5 页 × 20 格仓库和创建编年史。
- 新增申请/审批/拒绝/邀请/退出/成员列表：默认加入方式 3 直接加入，方式 2 生成待审申请，方式 1 需先邀请；审批、退出和成员数在同一事务中更新。
- 角色读取新增 `clanID`/军团名/军团标记，为后续用户信息序列化与协议入口做准备。
- 新增测试覆盖创建扣点、仓库初始化、重名/重复军团/积分不足、审批加入、成员列表权限、邀请加入、退出与筛选。`go test ./...`、`go vet ./...` 通过；未接协议入口，未做实机验收。

## 2026-11-02 军团协议核心

- 接入 `71` 类型 0/1/2/3/4/5/6/7/8/11/12/14/15/16/27/28/29/30/113：列表筛选、详情、创建、申请、取消、审批/拒绝、退出、邀请、成员等级、踢人、加入方式/地区、公告、标记、解散、转让、成员/申请列表与军团聊天。
- 出站包按上游结构实现：`109` 成员在线状态、`29` 成员资料、`30` 申请列表、`107` 军团增量（含完整信息、编年史、仓库摘要）、`101` 公告、`106` 踢出、`112` 邀请、`113` 聊天。
- 登录时回放成员/申请/军团状态，并向同军团在线成员广播上线；创建、加入、退出、被踢、解散后刷新 `157` 用户信息。
- `157` 用户信息在 `clanID` 非零时增加位 12，序列化军团 ID、标记和名称；无军团时保持旧布局。
- 新增测试覆盖创建与筛选、直接加入、审批加入、军团聊天和踢人。`go test ./...`、`go vet ./...`、`go test -race ./...` 通过；仓库与军团任务未接，未做实机界面验收。

## 2026-11-02 军团仓库

- 接入 `71` 类型 24/25/26/31：存入、取出、分页读取、权限设置、历史读取和删除；登录时下发 `26/3` 仓库页权限。
- 存入按当前成员等级与页权限校验，整堆移动到第一个空仓库格；取出检查背包容量、永久道具重复、同 ID 军团道具重复，并清空仓库格。
- 仓库物品保留绝对到期时间与强化值；历史包返回空列表，避免上游 `SendClanStorageHistory` 的客户端崩溃路径。
- 新增测试覆盖存入扣背包、分页读取、取出入背包、删除权限、页权限、仓库满与永久/军团道具重复。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 武器解放小游戏

- 读取 `MiniGames.WeaponRelease`：用自定义 JSON 解码保留物品键顺序（该顺序就是客户端的武器槽位），支持 `Name`/`Count`/`Duration` 默认值和 `Characters` 单字节字母列表；启动时校验 uint8/uint16 范围与 8 字节以内的单词长度。
- 登录事件包 `159/1` 现在按配置发送武器解放事件位 `1<<6`，激活时在登录阶段下发 `89/6/65` 进度。
- 接入 `89/6/60/61/62`：查询进度、选字、消耗字母；校验行/槽位/字母位置、已开启、重复位、缺字和 `~` 万能字，返回上游 0/2/4/5 状态；集齐后按配置发放武器并发送奖励通知。
- 新增 `UserMiniGameWeaponReleaseItemProgress` 与 `UserMiniGameWeaponReleaseCharacters` 事务读写；进度和字母扣减同事务提交。
- Bingo 的上游更新/重置/洗牌主体均被注释，未按“现成功能”搬运。新增测试覆盖配置顺序/校验、进度位、重复位、缺字、登录进度包、完整集齐发奖和 inactive 状态。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 切换到当前客户端数据

- 嵌入数据源从上游默认配置切换为 `S:\.temp\CSNZ\Server`：`ServerConfig.json`、商店/奖励/活动/皮肤/武器列表以及 `Server/Data` 全部按当前客户端文件重取。
- 当前客户端配置带来的变化包括：每 IP 注册上限 1、主菜单皮肤事件 8、百科/荣誉商店/里程商店/活动商店元数据开启、Bingo/武器解放事件开启，以及 2025 年春节公告和新的 Voxel 地址。
- `Metadata_ModeList.bin`、`Metadata_Encyclopedia.bin` 当前客户端目录没有独立文件，继续使用从上游头文件提取的固定数据块，其余元数据以客户端 `Server/Data` 为准。
- 集成测试改为显式允许多个本地账号注册，避免被客户端的每 IP 上限 1 误伤。
- 新增 `MainMenuSkinEvent` 配置与 `159/3` 登录下发；新增测试覆盖启用/关闭。`go test ./...` 通过；未做实机界面验收。

## 2026-11-02 家族战房间

- 放开 `ROOM_HIGHMID_FAMILYBATTLE`（设置字段 70）：创建房间要求房主有军团，加入要求玩家属于房主军团或第二军团；第二个不同军团占据第二槽，第三个军团和未加入军团的玩家被拒绝。
- 家族战房间禁止换队；开局要求两个军团槽都非空且双方都有成员，否则返回 `ROOM_START_FAILED_CLAN_NOT_READY`。
- 成员离开或房主迁移后重新计算两个军团槽；房间创建/加入时向同军团在线成员发送 `71/116` 军团战通知，接入房间子类型 `38` 手动发送通知。
- Studio 房间（地图 254）仍明确拒绝；家族战踢人（子类型 37）未接。
- 新增测试覆盖军团槽分配、无军团拒绝、离开后清空、开局条件和通知包。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 默认数据回退到上游基线

- `assets/defaults` 重新取上游 `12644f2` 的 `bin/`：`ServerConfig.json`、8 个根 JSON 和 `bin/Data`；不再使用 `S:\.temp\CSNZ` 的客户端定制配置。
- 移除上游 `bin/Data` 中不存在的 `Metadata_ModeList.bin`、`Metadata_Encyclopedia.bin`；上游配置对应元数据为关闭。
- 集成测试仍显式允许多个本地账号注册，不受默认 `MaxRegistrationsPerIP` 影响。
- 同步 README/GAPS：说明默认数据是上游基线，客户端定制数据需在运行目录手工替换。

## 2026-11-02 剩余缺口复核

- 入口：Voxel `103`、GM 命令、房间高级设置校验和 SSL 仍缺；房间子项剩 `65/34`、`65/35`、`65/37`、`65/32/33`。Bingo 已在后续提交补齐。
- 数据库 45 张表中 14 张未引用：封禁 2 张、`UserQuest*` 5 张、`UserRank`/`UserDailyReward`/`UserDailyRewardItems` 3 张、`UserRestore`/`RelationProducts`/`SuspectAction`/`TimeConfig` 4 张。
- 上游桩不计缺口：Rank、Quest 界面、投票踢人、party 聊天、Report、ZBS 奖励/模式统计、每日奖励、组装等。
- 详见 `GAPS.md`；真实 HLDS、多人实机、模式矩阵和客户端界面回归仍未验收。

## 2026-11-02 Bingo 小游戏

- 读取 `MiniGames.Bingo`，要求激活时至少 12 个奖项；配置默认关闭。
- 登录事件位增加 `1<<0`，激活时下发 `89/0/0` 桌面；首次访问初始化 25 个不重复数字和 12 个奖项。
- 接入 `89/0/0,1,3,4`：桌面查询、开格、完成五连线发奖、洗牌（已开格 >11 或已开奖 >1 时拒绝）和重置。上游 open-random 本身被注释，Go 简化为从未开格中随机选择。
- 新增 `UserMiniGameBingo`、`UserMiniGameBingoSlot`、`UserMiniGameBingoPrizeSlot` 的事务读写；开格、奖项状态和发奖流程已接，发奖失败保留未开格状态。
- 新增测试覆盖初始化、开格、连线奖项、洗牌、重置和客户端更新包。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 Voxel 基础通路

- 配置新增 `Voxel` 段；登录下发 `103/20` 的 `VoxelVxlURL` 和 `VoxelVmgURL`。
- 接入 `103` 的 `4/8/9/10/38/46/47/58` 固定占位回包，与上游一致。
- Voxel/Studio 房间列表 `65/34`、地图 254 的 Studio 房间创建仍未实现；当前只恢复登录 URL 和基础 Voxel 请求通路。
- 新增测试覆盖 URL 包和 8 个占位回包。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 GM 命令与封禁

- 角色加载新增 `UserCharacterExtended.gameMaster`，聊天命令按该标记鉴权；非 GM 的 `/` 消息仍按普通聊天广播。
- 实现 `/help`、`/version`、`/disconnect`、`/status`、`/getfreeslots`、`/getuid`、`/addpoints`、`/givepoints`、`/sendnotice`、`/sendgmnotice`、`/ban`、`/hban`、`/ipban`、`/unban`，参数与提示语对照 `channelmanager.cpp:459-1200`。
- `/ban` 写 `UserBan`（term 按天换算为分钟，0 视为 999999 天），`/hban`、`/ipban` 分别写 `HWIDBanList`、`IPBanList`；三者断开在线目标，`type=1` 向全服广播封禁原因。
- 拦截点与上游一致：TCP accept 处拒绝 `IPBanList` 来源，登录握手（`U` 类型 3）处拒绝 `HWIDBanList` 中的 HWID；`/unban` 同时删除账号、IP、HWID 封禁。
- 新增测试覆盖 GM 鉴权、封禁/解封、IP/HWID 拦截与积分修改。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 房间子项 32/33/35/37

- `65/32`、`65/33` 按上游 `SendRoomUnk32/33` 回固定空包。
- `65/35` ZB addon：校验 6 个上限、只接受 `zbsaddonitem` 类道具、逐个消耗并替换 `UserAddon`，返回背包更新包；未知/未拥有/受保护道具跳过。
- `65/37` 军团战踢人：仅家族战房主可用，踢出所有非本方军团成员，记录 kicked 名单、回到大厅，并向被踢玩家发送 `KickClan`（用户 ID 列表）。
- 新增 `Store.SetAddons` 事务替换 addon 列表；测试覆盖 32/33 回包、addon 消耗与保存、军团战踢人与非家族战/非房主拒绝。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 `65/34` Voxel 房间列表

- 读取请求里的 `voxel_id`，按房间设置 Voxel 块的 `VOXELFLAG_ID` 过滤后回 `65/34`；房间条目与全量房间列表共用 `writeRoomListEntry`。
- 新增 `settings.voxelID()` 解析 group1/bit18 的 Voxel 块；无 Voxel 块或未设置 ID 的房间不匹配。
- Studio 房间创建仍拒绝：上游 `CRoomSettings::CheckSettings` 需要 `CVoxelManager::GetSlotDetails` 访问 `Data/VoxelConfigList.json` 配置的 HTTP 服务，用槽位详情覆盖房间名、人数和模式；仓库默认配置没有该服务地址。
- 测试覆盖按 `voxel_id` 过滤与房间条目编码。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 GM 道具与奖励命令

- 新增 `/additem`、`/giveitem`、`/removeitem`、`/addexp`、`/givereward`、`/giverewardtoall`。
- 道具发放走 `prepareGrants` + `Store.GrantReward`，与商城/奖励共用槽位、堆叠和过期规则；`/removeitem` 按同 ID 序号删除，禁止删除默认/伪默认道具，未带序号时列出可选序号。
- `/addexp` 复用 `GrantReward` 的经验上限与升级逻辑，升级后广播 `157`；`/givereward` 复用 `ItemRewards.json` 的目录奖励，在线目标收到背包更新与奖励通知，离线目标直接落库；`/giverewardtoall` 遍历全部账号。
- 选择型奖励（`Select`）仍拒绝并提示需要客户端选择 UI；新增测试覆盖加道具、删道具、伪默认道具保护、加经验、单人/全服发奖。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 GM 剩余道具命令

- 新增 `/addallitems`、`/setitemstatus`、`/setiteminuse`。
- `/addallitems` 按 `Item.csv` 全量发放，跳过默认/伪默认道具；`/setitemstatus`、`/setiteminuse` 直接改背包行的 status/inUse 并回单条背包更新，对应上游 `UpdateInventoryItem` 的两个字段分支。
- 新增 `Store.SetItemStatus`、`Store.SetItemInUse`（越界校验、槽位不存在返回 `ErrItemUnavailable`）；测试覆盖字段修改与全量发放。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 房间高级设置校验

- `MapList.csv` 改为整行加载（`mapRow`），保留模式列、`max_player`、`ZSmaxDifficulty`、`playroom_modeID`、`weapon_restrict_deault` 等字段。
- 新增模式/地图矩阵校验（`mapAllowsMode`，对应 `IsMapValid`）：按 `GetGameModeNameByID` 的列名查表，`map_FunMode` 额外比对 `Fun_Subtype`，`playroom` 比对 `playroom_modeID`。
- 新增 `mapPlaylist`（2..5 张、去重、首图一致、每张地图合法且无默认武器限制）、`randomMap` 模式限制与夹紧、ZS 难度上限、家族战模式白名单。
- `superRoom` 改为由房主持有的道具 8357 决定（对应 `CRoom::CheckForHostItems`），不再信任客户端字段。
- 新增 `room_validate_test.go` 覆盖矩阵、playlist、随机图、ZS 难度、家族战；`room_superroom_test.go` 覆盖 superRoom 覆盖逻辑。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 GM 武器解放字母与活动刷新命令

- 新增 `Store.AddWeaponReleaseCharacter`（累加 `UserMiniGameWeaponReleaseCharacters`），接入 GM `/addwpnreleasechar <char> <count>`。
- 按上游把 `/bingo`、`/weaponrelease` 作为所有玩家可用的刷新命令：对应活动开启时重发 `89/0` 更新或武器解放进度，未开启时静默返回。
- 测试覆盖字母累加与 GM 命令；`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 房间可选字段清洗

- 新增 `room_sanitize.go`，在 `Room.ValidateSettings` 开启时按上游 `LoadNewSettings` 清除或夹紧与模式不匹配的可选字段。
- 覆盖 `voxel`（非 254 地图）、`fireBomb`、`mutationRestrict`/`mutationLimit`、`zbLimit`（模式白名单、去重、道具存在性、`zbLimitFlag` 夹紧）、`teamSwitch`、`zbRespawn`/`zbBalance`、`gameRule`、`zbAutoHunting`、`integratedTeam`、`enhanceRestrict`、`floatingDamageSkin`、`playerOneTeam`、`weaponBuyCoolTime`、`zbRebalance`、`changeShot`/`zombieReviveZHC`、`kdRule`/`mannerLimit`、`leagueRule`、`statusSymbol`/`movingShot`/`mapPlaylistIndex`。
- 清洗是静默的：字段被清除后不再序列化，房间仍然有效；`ValidateSettings` 关闭时保持客户端原值。
- 新增测试覆盖 voxel 清除、fireBomb、mutationLimit、zbLimit 去重与保留、teamSwitch；`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 地图轮换

- 对局结束（`endGame`）按 `mapPlaylist` 轮换：非强制结束取当前地图的下一张并回绕，强制结束回到第一张。
- 更新 `mapId`(7)、`mapId2`(48)、`mapPlaylistIndex`(42) 后广播 `65/4` 设置更新（掩码 `1<<7` 与 `1<<10|1<<16`），与上游 `CRoom::EndGame` 的字段顺序一致。
- `Room.HostConnectingMethod=1`（仅专服）时，轮换未回到第一张地图前不释放专服。
- 新增 `rotateRoomMap` 与测试覆盖下一张、回绕、强制结束和无 playlist。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 房主道具设置与低层字段清洗

- `refreshSuperRoom` 扩展为 `refreshHostRoomItems`：`superRoom`（8357）、`sd` 大头事件（439）、`c4Timer` 音效（112）都按房主实际持有道具决定；`sd` 只在模式 3/4/5/15/24 生效，其余模式清除。
- 补充 `LoadNewSettings` 的低层清洗：`buyTime`/`startingCash` 取值校验，`teamBalance`/`friendlyFire` 默认值比对，`viewFlag` 位清理，`displayNickname`/`hostageKillLimit`/`freezeTime`/`flashLight`/`voiceChat` 夹紧，以及 `levelLimit`/`status` 等上游固定丢弃的字段。
- 测试覆盖房主道具应用与清除、buyTime/startingCash、teamBalance、viewFlag。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 bot/ballNumber/武器限制字段

- 房间清洗补充 bot 块（5 字节：难度/友方/敌方/平衡/追加）：模式 3/4/5/24 夹紧难度 ≤7、友方 ≤15、敌方 1..16；其他模式与默认值不一致时整块清除，难度 ≤2。
- `ballNumber` 只在地图 16（足球）模式保留，且不超过地图 `ball_max`。
- `weaponRestrict` 夹紧到 13；地图带 `weapon_restrict_deault` 时按地图默认值覆盖下发。
- 新增测试覆盖 bot 清除/夹紧、ballNumber 合法与超限、weaponRestrict 夹紧和地图默认限制。
- 测试基础设施：新增 `listenTestPair` 重试同一端口的 TCP/UDP 绑定，规避 Windows 偶发拒绝 UDP 端口的问题。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 SSL（TLS 1.3）

- 移除配置层对 `SSL=true` 的拒绝；`New` 在 `SSL=true` 时加载 `Data/Certs/server-cert.pem` 与 `server-key.pem`，构造 TLS 1.3（Min/Max 都锁 1.3，对齐上游 `wolfTLSv1_3_server_method`）。
- `Serve` 接受的 TCP 连接在 IP 封禁检查后包成 `tls.Server`，UDP 路径不变；证书缺失时启动直接报错，而不是像上游那样回落明文。
- 新增 `ssl_test.go`：自签证书握手后读取 greeting、完成版本握手；证书缺失时 `New` 返回错误。`go test ./...`、`go vet ./...` 通过；未用真实客户端验证。

## 2026-11-02 装饰道具状态切换

- `zbRespawnEffect`（复活特效）与 `CombatInfoItem`（击杀标记）按上游 `CItemManager::OnItemPacket` 的 switch-status 分支实现：激活时清掉同 ID 其他副本的 `status` 与上一个生效道具的 `inUse`，把所选道具置为 `status=1,inUse=1`，并写 `UserCharacterExtended.zbRespawnEffect`/`killerMarkEffect`。
- 新增 `Store.ToggleEffectItem`（列名白名单、槽位校验、`ErrItemUnavailable`）与 `Server.toggleEffectItem`；`itemRequest` 的 UseItem 分支按 `ClassName` 分发。
- 与上游一致，这两类装饰不发 `157` 用户信息包，只回背包更新；`LobbyBG` 名片需要 `UFLAG_LOW_NAMEPLATE`，等实机确认后再接。
- 测试覆盖激活/切换/取消、无效槽位、非法列名，以及 `itemRequest` 端到端切换。`go test ./...`、`go vet ./...` 通过；未做实机界面验收。

## 2026-11-02 名片（LobbyBG）装备切换

- `Store.ToggleNameplate` 按上游 nameplate 分支实现：同 ID 其他副本清 `status`、上一个生效道具清 `inUse`、所选道具 `status=1` 且按激活/取消设置 `inUse`，最后写 `UserCharacter.nameplateID`。
- `itemRequest` 的 UseItem 分支按 `ClassName == "LobbyBG"` 分发；切换后回背包更新并广播 `157`。
- `Character` 新增 `NameplateID`；`userInfo` 在非零时置 `UFLAG_LOW_NAMEPLATE`(bit0)，并在 flags/highFlag 之后、账号名之前写 `U8(0)+U32(nameplateID)`，与上游 `CPacketHelper_FullUserInfo` 的块顺序一致；未持有时不发该块，旧布局不变。
- 测试覆盖激活/取消、`Character` 读取、`userInfo` 名片块解析、`itemRequest` 端到端切换。`go test ./...`、`go vet ./...` 通过；名片块尚未用真实客户端验收。

## 2026-11-03 元数据按项目基线 12644f2 的形状下发（纠正对照错版本）

- 现象：元数据全开时客户端登录后闪退（`0xE0005916` 自抛致命异常），元数据全关则正常进大厅。
- 根因不在协议本身，而在对照错了版本：`.ref/CSNZ_Server` 检出在 `3aac2d2`（更晚的私服构建），而 AGENTS.md 指定的基线、也是容器化仓库 `main` 分支 Dockerfile 实际构建的提交是 `12644f2`（2024-11-29，`SUPPORTED_CLIENT_BUILD=30.11.24`，该组合实测可正常游玩）。两者元数据形状不同，见下表。

| 项 | `12644f2`（基线，客户端接受） | `3aac2d2`（本轮误用） |
| --- | --- | --- |
| zipped 元数据帧 | `U8(id) + U16(size) + data` | `U8(id) + U8(ZIPMETADATA_FULL_ZIP=5) + U16(size) + data`，超 64000 字节分块 |
| ItemBox（4） | `U8(4) + U32(count) + 26×count` | 多一个 `U8(0)` |
| ModeList（2） | 有专用 sender：`U8(2) + U16(787) + metaData2` | `SendMetadata` 无此分支 |
| Encyclopedia（23） | 有专用 sender：直接下发 `metaData_Encyclopedia[697]` | 无此分支 |

- 实测证据：按 `3aac2d2` 的帧发（多一个 `5`）时客户端秒崩，且与内容无关——把 `MapList.csv` 砍到 4 行仍崩；把客户端自己 `Data/Metadata_ModeList.bin` 里的 zip 原样抽出、用带 `5` 的帧发送同样崩。去掉该字节后，客户端把全部元数据包接收并写入其 `MetadataDump/`（hook `-dumpmetadata`），存活 60 秒以上、大厅/模式/地图正常。
- 现在默认帧 = `U8(id) + U16(size) + data`；`MetadataZipTypeByte`（默认 `false`）保留 `3aac2d2` 的帧与分块，供适配较新私服构建时使用。旧帧下单帧超过 `U16` 上限的条目跳过并告警。
- ItemBox 恢复基线的 5 字节头（`U8(4)+U32(count)`）；`3aac2d2` 的 `U8(0)` 与后续试过的 `U16` 长度都是私服构建的差异写法，本客户端虽不崩但不是基线形状，已弃用。
- ModeList/Encyclopedia 恢复下发，并逐字节确认嵌入数据就是基线 payload：`Metadata_ModeList.bin` = `02 + U16(787) + metaData2[787]`（`metaData2` 取自 `12644f2:src/packet/packet_metadata_data.h`，比对一致）；`Metadata_Encyclopedia.bin` = `metaData_Encyclopedia[697]`（比对一致）。
- type 字节必须是 payload 首字节（实测把长度写在类型之前会被客户端分派成 type=191）。
- 测试：`TestMetadataZipFrames`（两种帧 + 旧帧超限跳过）、`TestLoadMetadataZipPayload`、`TestLoadMetadataZipTypeByte`、`TestMetadataItemBoxHeader`（5 字节头 + 26 字节项）。`go test ./...`、`go vet ./...` 通过，并完成真实客户端登录验收（全部元数据下发、无闪退）。
- 尚未对齐：下发顺序（Go 把 ItemBox/WeaponPaints/ZombieWar/RandomWeapon 追加在表尾，基线插在中间）；基线会发的 `WeaponAscend/Unk57/PerkParam/Synthesis/Unk64/VoxelConfigList` 尚未实现，默认数据里也没有对应文件。

## 2026-11-02 修复元数据包缺少 zip 类型字节（已于 2026-11-03 修正）

- 上游 `SendMetadata*` 的 zipped 元数据帧是 `U8(id) + U8(ZIPMETADATA_FULL_ZIP=5) + U16(size) + data`；Go 之前漏掉了第二个字节，客户端把 size 低字节当成类型、后面整体错位，表现为房间创建界面模式列表空白、地图显示 `map_name` 等。
- 补上 `U8(5)`；超过 64000 字节的 zip 按上游切成最多 4 个 64000 字节块，块号 1..3、最后一块标记 4，避免 `Item.csv` 等大文件被截断。
- 新增 `metadata_test.go` 覆盖单帧/分块/超限截断，并校验 zip 能解出正确的条目名。
- 测试辅助 `listenTestPair` 不再要求 TCP/UDP 同端口，绕开 Windows 偶发拒绝 UDP 端口导致的假失败。
- 结论已修正：`12644f2` 的帧没有这个字节（该字节是更晚构建 `3aac2d2` 的写法），默认帧见 2026-11-03 条目。

## 2026-11-03 基线迁移到 4859a6e，元数据按下发顺序重排

- 基线从 `12644f2` 移到 `4859a6e73451ccbfce167b1d28deeb0457468ac3`（2025-03-20，PR #45 的合并点；内容与其分支头 `e5721ed` 相同，`git diff e5721ed 4859a6e` 为空，两者 tree 一致，取主线提交便于对照 `git log`）。`12644f2..4859a6e` 共 10 个提交，`git diff --name-only` 只有 `bin/Data/*` 与 `bin/*.json`，没有源码改动，协议形状与 `12644f2` 相同。`scripts/check-ref.ps1`、`scripts/sync-defaults.ps1`、`AGENTS.md`、`GAPS.md`、`README.md`、`assets/README.md` 一并更新。
- `scripts/sync-defaults.ps1` 现在还会从基线 `src/packet/packet_metadata_data.h` 生成 `Metadata_ModeList.bin`（`02 + U16(787) + metaData2`）与 `Metadata_Encyclopedia.bin`（`metaData_Encyclopedia`），两者与基线 payload 逐字节一致；脚本按字节比较，重复运行不产生无意义 diff。基线 `ServerConfig.json` 默认关闭这两项。
- `loadMetadata` 改为"先按名字收集帧、再按 `metadataOrder` 发射"。`metadataOrder` 就是基线 `CUserManager::SendMetadata` 的调度顺序（`Hash` 保持不实现、默认关闭）；此前 ItemBox/WeaponPaints/ZombieWar/RandomWeapon 被追加在表尾，与基线不符。
- 单帧预算修正为 `protocol.MaxPayload-3`（元数据头 `U8(type)+U16(size)` 占 3 字节），65528/65529 边界由测试覆盖。`MetadataZipTypeByte` 的 64000×4 分块在超限时直接跳过并告警，不再发送被截断却标记为末块的 zip。
- 缺少数据文件的条目跳过并 `Warn`（对应基线 sender 文件缺失即 return），不再让 `New` 失败。
- 后续路线：`4859a6e` 之后共 7 个提交才动协议，按顺序逐个过渡——`3b56abf`（引入 `ZIPMETADATA_FULL_ZIP` 帧与 64000 分块，需要把 `MetadataZipTypeByte` 打开）、`6e5275c`（ItemBox 增加 `U8(0)`）、`90bc74a`/`293c9dc`（Voxel 多服务器、voxelhttp 主机名）、`a6e0bf0`/`a2f0026`/`3aac2d2`（typo、content-length、P2P 换图）。`3b56abf` 起的帧本客户端不接受，只有客户端升级后才应切换默认帧。
- 验证：`go build`、`go vet`、`go test ./...` 全绿；全开配置 + 新基线数据（地图 322、模式 51、道具 3020）下真实客户端登录不闪退。


## 2026-11-03 补齐 /tournament、/addgroom、/reqsnapshot 与 Hash 元数据

- `/tournament`：按基线切 `UserCharacter.tournament`（0 ↔ 0xFF）并持久化；`userInfo` 在 `UFLAG_LOW_TOURNAMENT`（bit13）下、clan 块之后写 `U8(tournament)`（对应 `packethelper_fulluserinfo.cpp:154`），切换后补发 `157` 让客户端立即刷新，另发 "Tournament HUD is ON/OFF" 通知。上游只发通知、不补 `157`，这里多一步是为了让 HUD 立即生效。
- `/reqsnapshot`：基线在 `#ifndef PUBLIC_RELEASE` 里只回 `Not implemented`，Go 与之相同。
- `/addgroom`：按基线造一个硬编码房间（`GHOST ROOM NAME`、模式 15、地图 128、10 人），设置值重新走正常建房路径，因此默认值、校验和广播与客户端建房一致。上游那串 60 字节密码超过本项目 32 字节上限，改为固定短密码（`addgroom`）。
- `Hash`（255）元数据：基线的 `SendMetadataHash` 只发头部、数据写入被注释，所以开启时下发一个**空 payload** 的 `91` 包，位置在 `WeaponProp` 与 `RandomWeaponList` 之间（`usermanager.cpp:705`）。`metadataOrder` 增加 `Hash`，`loadMetadata` 在开关打开时放入空切片。
- 测试：`TestGMTournamentToggle`（DB 值 + `userInfo` bit13 与字节）、`TestGMRequestSnapshotAndGhostRoom`（两条命令行为、房间名）、`TestMetadataHashPacketIsEmpty`（空包 + 顺序）。`go build`、`go vet`、`go test ./...` 全绿。



## 2026-11-03 处理 b4cc191 审查（R1–R7）

审查报告把 7 项 P1 都做了可复现的隔离测试（`code-review/probes/review-b4cc191/`）。逐条处理：

- **R1 装饰装备没有激活休眠道具**：`ToggleCostume` 的两条路径（普通装饰 / 僵尸皮肤）在装备时改为 `inUse=1, status=1`，并把休眠道具保存的相对天数（`expiryDate` 为天数）换算成绝对分钟，与基线 `OnCostumeEquip` 的 `ConvertDurationToExpiryDate` 一致；被替换掉的旧装饰按基线保留 `status=1`、只清 `inUse`。
- **R2 限时新装饰无法自动装备**：`AutoEquipCostume` 的选择条件改为和 `readInventory` 相同的口径（`expiryDate=0`、已过期但仍是休眠相对天数、或未过期），激活时同样做绝对时间换算。
- **R3 ZombieSkin=0 被拒**：槽位范围改为基线的 `0..17`（`ZB_COSTUME_SLOT_COUNT_MAX=18`），此前 `<=0` 会拒掉 5 件 `ZombieSkin=0` 的合法装扮，又放行 18 以上。
- **R4 基础奖励先封顶再算加成**：`matchBaseReward` 改为返回未封顶的系数乘积（带溢出保护），职业加成与装备加成用它计算，最后才把 `exp/points` 截到 9999——与基线 `CalculateGameResult` 的顺序一致。系数 100、200 击杀、50% 职业加成现在入账 `9999+10000=19999`（此前 14998）。
- **R5 结算重试时加成百分比累加**：`endGame` 每轮先把 `bonusExp/bonusPoints/classExp/classPoints/itemExp/itemPoints` 全部清零再计算，事务失败重试不再翻倍。
- **R6 默认启用了基线注释掉的装备加成**：新增 `GameMatch.CalcResults.ItemBonusEnabled`（默认 `false`）。基线里那段循环是注释状态，开启属于经确认的扩展，需在配置里显式打开；顺带把百分比累加改为按 itemID 排序，避免 map 顺序影响展示值。
- **R7 重连把离线时间补成对局分钟**：`restoreSession` 重建 `started` 锚点为 `now - questMinutes`，`tickQuestTime` 只累计真实在线时间。

另外两处按审查建议收紧：

- **房间设置截断**：解析越界时不再"保留前半包"继续建房/改设置，改为记日志并回 `Unable to create a room due to incorrect settings`，避免服务端与客户端从第一个错位字段开始整体分歧；尾部多余字节仍只记日志。（单字节难度字段仍按`输入 1 / 输出 9`精确处理。）
- **僵尸皮肤卸下报错**：`toggleZombieSkinCostume` 在 `UserZBCostumeLoadout` 没有对应行时会把 `sql.ErrNoRows` 原样返回，导致卸下一件未装备的皮肤报 "sql: no rows in result set"；现在与普通装饰路径一致，视为"无旧行"继续。

测试：新增 `internal/store/costume_dormant_test.go`（R1/R2/R3）、`internal/server/reward_review_test.go`（R4/R5/R7）、`TestTruncatedRoomSettingsAreRejected`；更新 `TestZombieSkinCostumeToggle`（0 号槽合法、18 号拒绝）与 `TestApplyEquipmentBonus`（默认关闭、显式开启后生效）。审查者 overlay 里 R5 的探针在未打开 `ItemBonusEnabled` 时会看到 `itemExp=0`（R6 的预期结果），开启开关后的重试幂等由 `TestEquipmentBonusRetryIsIdempotent` 覆盖。


## 2026-11-03 处理 44b5eed 复核与扩大范围审查（8 项）

- **奖励回包到期时间不同步**（复核 P1）：`finishRewardClaim` 在自动装备后不再手工修补快照，改为按槽位从数据库重读并替换（`refreshItems`），154 包的 `status/expiry` 与落库值一致；来源消耗留下的墓碑条目保留。
- **Bingo 发奖与状态分离**：新增 `Store.OpenBingoAwards`，把开格号码、完成线标记与全部奖品放进同一事务；背包不足时整步回滚（号码不消耗、线不标记），腾出空间后重试不会重复发也不会丢。
- **离线奖励可能重复发**：新增 `Store.GrantRewardNotice`，在同一事务里消费 `UserRewardNotice` 行并发奖；`grantRewardNotices` 改走该入口，物品类拒绝（满包/等级/不可用）保留通知待下次登录，数据库错误照旧上抛。
- **集字活动满包丢奖**：`SetWeaponReleaseCharacterReward` 把最后一个字母、`opened` 标记与奖品发放在一个事务里完成；满包整体回滚，字母不消耗，腾出空间后可重试。
- **开箱绕过目录归一**：`openItemBox` 现在先过 `prepareGrants`，与商店/奖励共用 `Exclusive/Stackable/Dormant` 规则，同款武器不会同时选中两份。
- **强化随机漏掉 Ammo**：`enhanceAttribute` 的抽取范围从 `rand.IntN(5)` 改为 `rand.IntN(6)`（基线 `Randomer(5)` 的分布是闭区间 0..5），Ammo（索引 5）回到候选。
- **公仓拒收休眠道具**：新增共享谓词 `liveItemSQL`（永久 / 未过期 / 仍是相对天数的休眠副本），`readInventory`、`GiveClanItem` 与公仓查重统一使用；3 天休眠道具可以正常存入。
- **到期通知截断后清空余项**（P2）：`ClearExpiryNotices` 只删除实际下发的行数，260 条通知首登发 255 条后仍余 5 条；同时先过滤非法 itemID 再计数。

测试：新增 `internal/server/review_expanded_test.go`（通知余量、离线奖励幂等、集字满包重试、强化候选、奖励回包到期值）与 `internal/store/clan_dormant_test.go`；`bingo_test.go` 增加满包回滚重试用例；`itembox_test.go` 的夹具补上奖品道具（归一化现在要求道具在目录中）。`go vet`、`go test ./...` 全绿，两轮审查的 overlay 探针全部通过。


## 2026-11-03 处理 b6afe1f 复核与 f6eda20 整体审查（8 项）

- **选择型离线奖励会锁死登录**：`grantCatalogRewardNotice` 用 `errRewardDeferred` 包装"需要选择 UI / 奖励定义缺失 / 目录缺项"的失败，`rewardRejection` 认它，于是保留通知、继续登录；数据库错误仍上抛。此前返回原始错误会让`loginPackets`中断，账号每次登录都被断开。
- **到期通知按行身份删除**：新增 `Store.ExpiryNoticeRows`（rowid + itemID）与 `ClearExpiryNotices(id, rowIDs)`，只删真正下发的行；非法 itemID 的行直接清掉。此前"读取按 itemID 排序、删除按 rowid 取前 N 行"，乱序插入时会删掉未发送的、留下已发送的。
- **专服轮换无法开下一局**：`startGame` 先复用仍归本房间的 `r.dedicated`（仍在该池中且 `room==r.id`），没有再向池申请；修掉"第二张图提示没有可用专服"，也避免覆盖旧实例造成占用泄漏。
- **限时外观启用后消失**：`LobbyBG`/`zbRespawnEffect`/`CombatInfoItem` 的休眠副本在启用时通过 `activateExpiry` 把相对天数换算成绝对分钟并置 `status=1`，否则启用后立刻被当作过期过滤掉。
- **保护列表按物品 ID**：`DeleteItems`、`ConsumeItem` 改为比较 `item.ID`（原来误比 slot），受保护 ID 的副本无论在哪个槽位都不会被分解/消耗，普通 ID 也不会因槽位数字碰巧相同而被误保护。
- **ZB 插件请求原子化**：先完整解析并校验尾部，再用 `Store.SetAddonsConsuming` 在单个事务里扣材料 + 替换 `UserAddon`；截断、尾部多余、写库失败都不会再永久扣道具。
- **同款装饰两个副本同时装备**：替换分支不再要求 `oldID != target.ID`，按槽位排除目标后清掉旧副本；普通装饰与僵尸皮肤两条路径一致。
- **已卸下装饰续期按分钟算**：`ExtendItem` 只在"未激活"（`status=0 && inUse=0 && 相对天数`）时按天加，已激活后再卸下（`status=1`、绝对时间、`inUse=0`）按 `days*1440` 分钟加。

测试：新增 `internal/server/review_overall_test.go`（选择型通知、通知行身份、专服复用、限时外观、同款装饰、续期单位、保护 ID、插件失败回滚共 8 组用例）。`go vet`、`go test ./...` 全绿，四批 overlay 探针（`review-44b5eed`、`review-expanded`、`review-b6afe1f`、`review-overall-f6eda20`）全部通过。


## 2026-11-03 处理 154b2fe 复核与整体审查（4 项）

- **新插件事务重复使用已删除的材料快照**（154b2fe 引入的回归）：`SetAddonsConsuming` 删除整叠时把快照的 `Count` 置 0，并要求 DELETE/UPDATE 恰好影响 1 行；同一请求里重复的插件 ID 不会再拿已删除的行当材料（此前 1 件材料能换 6 条插件）。
- **删除/到期不清理外观与装备引用**：新增 `clearItemReference`，只在"同 ID 已无剩余副本"时清理，否则保留（选择以物品 ID 为键，还有副本时引用仍有效）。`DeleteItems` 与 `ExpireItems` 在删除行后、同一事务里清 `UserCharacter.nameplateID`、`UserCharacterExtended.zbRespawnEffect/killerMarkEffect`、`UserCostumeLoadout` 七个槽与 `UserZBCostumeLoadout`。
- **过期装饰残留引用挡住后续自动装备**：`AutoEquipCostume` 检查 loadout 指向的物品是否仍有有效库存，没有就清掉该槽继续自动装备（覆盖历史残留）。
- **审批入队申请可突破战队人数上限**：申请不预占名额，`ApproveClanJoin` 现在在事务内、删除申请与插入成员之前重查 `memberCount < maxMemberCount`；满员返回 `-6` 并保留申请，服务端映射为 `CSO_CLAN_JOIN_MAX_MEMBER`。

测试：新增 `internal/store/references_test.go`（删除清选择、过期不挡自动装备、审批限容）与 `internal/server/review_overall_test.go` 的 `TestRepeatedAddonIDConsumesOneUnit`。`go vet`、`go test ./...` 全绿，五批 overlay 探针全部通过。


## 2026-11-03 处理 33e9ec6 复核与整体检查（3 项）

- **数据库清了引用但在线缓存没刷新**：新增 `refreshCharacterCache`（重读 `Store.Character`，`nameplateID` 变化时 `broadcastUserInfo`）。分解入口在 `DeleteItems` 之后调用；到期扫描在 `removed>0` 时对全部在线会话重读，避免继续用缓存下发已删除的背景。
- **同款休眠副本仍占着装备引用**：`clearItemReference` 与 `AutoEquipCostume` 的"槽位是否被占用"判定改为"存在 `inUse<>0` 且仍有效的副本"，休眠/过期残留不再占位；合法接替仍靠真实装备的副本。
- **删除选中的武器副本后没有接替选择**：`DeleteItems` 在整批删除完成后，对每个被删且 `status=1` 的副本，找同 ID、`inUse<>0`、仍有效的副本置 `status=1`（对应基线 `RemoveItem`），并随返回值一起下发给客户端；`hostInventory` 不再漏掉这把武器。

测试：新增 `internal/store/references_promote_test.go`（接替选择、休眠副本不占位）与 `internal/server/review_overall_test.go::TestRemovalRefreshesCharacterCache`。`go vet`、`go test ./...` 全绿，六批 overlay 探针全部通过。


## 2026-11-03 处理 3e6fa14 复核与整体检查（2 项）

- **GM 删除背景没同步在线缓存**（P3）：`/removeitem` 在 `DeleteItems` 之后同样调用 `refreshCharacterCache`，与分解路径一致。
- **自然到期不接替选中的武器副本**（P2）：把 `DeleteItems` 里的接替逻辑抽成 `promoteItemSelection`，`ExpireItems` 在整批删除之后按用户清理引用并接替选择；新增 `ExpireItemsWithUsers` 返回受影响用户，在线会话会收到刷新后的 154 背包与 157 角色信息（`ExpireItems` 保留原两返回值签名，兼容既有调用）。

测试：新增 `internal/store/references_promote_test.go::TestExpiryPromotesRemainingWeapon` 与 `internal/server/review_overall_test.go::TestGMRemoveItemRefreshesCache`。`go vet`、`go test ./...` 全绿，七批 overlay 探针全部通过。

## 2026-11-03 处理 9d33c2d 复核与整体检查（2 项）

- **到期同步缺少已删除槽位的清除记录**（P2）：`Store.ExpireItemsChanged` 为每个受影响账号返回一份 `Items`：先是被删槽位的墓碑（`ID=0`，编码成 `U16(slot)+U8(0)`），再是剩余库存，与分解、消耗等路径的 154 增量补丁同形。服务端改用 `sendInventoryItems` 下发这份补丁，不再重发 152 与“剩余库存”——按槽位的增量帧无法靠“没提到”表达删除。`ExpireItems` 与 `ExpireItemsWithUsers` 保留为薄包装。
- **登录中的到期扫描提前发送背包**（P2，9d33c2d 引入的顺序回归）：`expireItems` 拆成 `sweepExpiredItems(now, skip)`。`loginPackets` 传入正在登录的账号 ID：数据库清理照常执行，其他已完成初始化的在线会话照常收到补丁，但正在登录的账号不再收到 152/154，其背包由登录链后面的正常初始化下发，UserStart 150 重新排在最前。重连走同一条 `/login` → `loginPackets` 路径，同样受保护。

测试：新增 `internal/server/expiry_sync_test.go`：`TestExpiryPushClearsDeletedSlot`（到期补丁含 slot 墓碑与存活行）、`TestLoginSweepKeepsInventoryAfterUserStart`（登录包序 + 其他在线会话仍收到到期补丁）。

九批 overlay 探针复跑时，`review-b4cc191` 的 `TestReviewBonusRetryPercent` 仍会在未打开 `ItemBonusEnabled` 时看到 `itemExp=0`。该探针写于 R6（装备加成默认关闭，对齐基线里被注释掉的循环）之前，与 5bc14c8 起的既定行为冲突，不是本轮回归；重试幂等由打开开关的 `TestEquipmentBonusRetryIsIdempotent` 覆盖。

## 2026-11-03 库存行消失后的级联状态梳理（2 处补齐）

把所有会让库存行消失的入口列全，逐个核对“外部引用 / 选择接替 / 在线缓存 / 下发”。清单与判断依据见 `.tmp/inventory-removal-audit.md`。其中两处确有可达缺口：

- **公仓存入不清引用**（`store/GiveClanItem`）：存入不检查 `status`/`inUse`，正在装备的背景、特效、装饰与选中的武器都能存进公仓。删除行之后补 `clearItemReference` 与 `promoteItemSelection`；服务端 `clanStorageGive` 在 154 补丁后调用 `refreshCharacterCache`。
- **强化材料吃掉了选中的副本**（`store/EnhanceItem`）：材料是普通库存行，可能正是某把武器的选中副本，删除后没有副本接替，`hostInventory` 会漏掉这把武器。整批删除后调用 `promoteItemSelection`；材料是武器，不涉及背景/装饰引用。

测试：新增 `internal/store/removal_cascade_test.go`（`TestClanDepositClearsEquippedNameplate`、`TestEnhanceMaterialPromotesSelectedCopy`）。其余入口（消耗、开箱钥匙、续期、喷漆、部件、改名卡、奖励来源、插件材料）逐条核对后判定不需要级联，理由写在清单里。

## 2026-11-03 处理 a0bb9a6 审查（4 P1 + 1 P2）

- **登录扫描跳过推送，也跳过了缓存重读**（R1）：`refreshCharacterCache` 拆成 `reloadCharacter`（只重读、不发包，返回是否变化）与 `refreshCharacterCache`（重读 + `nameplateID` 变化时 `broadcastUserInfo`）。`sweepExpiredItems` 对正在登录的账号只调 `reloadCharacter`：包序仍是 UserStart 150 在前，但登录链用的已经是清空后的背景选择。
- **强化材料没有做引用清理**（R2）：材料是任意库存行（服务端只校验归属、重复槽位与 Grade，不限制类别），基线经 `OnItemUse` → `RemoveItem` 处理材料。`EnhanceItem` 现在对每个被消费材料调用 `clearItemReference`，两条强化入口（强化 / 降级）在下发补丁后调用 `refreshCharacterCache`。
- **职业加成用了进入对局时的快照**（R3）：基线 `CGameMatch::CalculateGameResult` 在结算时用 `GetFirstActiveItemByItemID`（`status=1 AND inUse=1`）查库。新增 `Store.HasActiveItem`，`endGame` 逐玩家查询；已被到期扫描删除的职业物品不再给加成。装备加成是显式扩展（默认关），仍按对局快照，时间语义与基线职业加成分开说明。
- **同款材料翻倍对象错误**（R4）：基线先 `totalExp += exp`，再在同 ID 时 `totalExp *= 2`，即连之前累计的一起翻倍。抽出纯函数 `enhanceMaterialExp`（`materialGrade` 单独负责目录 Grade 与 8481/8482/8566 映射），配确定性用例 `(500+10)*2=1020`，不再依赖随机抽取证明。
- **Voxel 保留位被当成 U32 载荷**（R5）：`parseVoxel` 只对基线真正读取的 11/12/13/15/16/17 消费 U32；18、20 没有 `VOXELFLAG_` 定义、也不读字节，改为不消费，避免吞掉紧随其后的 FireBomb 字段。

测试：新增 `internal/server/review_a0_fixes_test.go`（R1/R2/R3/R4/R5 各一组）；`rewardMatch` 夹具改为把职业物品写成 `status=1,inUse=1` 的数据库行，因为加成现在按基线查库。九批 overlay 探针通过；`review-b4cc191` 的两条 server 探针仍红，它们用纯内存 `stat.items` 证明职业加成、并期待装备加成默认开启，两个前提都已被 R3/R6 取代，见该轮说明。

## 2026-11-03 黑盒对比与元数据条目顺序

WSL 里的 30003 实例确认是**原版 C++ 基线**（仓库 `~/repos/hm-gamesrv/counter-strike-online`，容器 `cso-baseline` 跑 `/app/csnz-server`，由 `scripts/start-baseline.sh` 先 `rsync -a --delete .data/ .data-test/` 再挂载启动）。逐文件 sha256 对比：`.data` 与容器 `/data` 的 45 个文件完全相同，配置亦然——两边差异全部是代码差异。

- **新增 `blackbox/`**（Python，仅用标准库与 socket）：`cso_proto.py` 帧编解码、`cso_client.py` 握手与登录、`test_smoke.py` 5 个冒烟用例、`trace.py` 包序列导出、`metadata.py` 解压后比元数据内容。同一套脚本可打 30002（Go 实现）与 30003（基线）。
- **元数据条目顺序偏差**：`ItemBox`(4)、`WeaponPaints`(6)、`RandomWeaponList`(16) 的条目集合相同但顺序不同。基线按 **JSON 文件顺序**遍历，Go 版解码进 map 后按数字排序（类型 16 从 item 1 开始而基线从 8330 开始）。新增 `internal/server/jsonorder.go`：流式 decoder 保序解析，并复用 `config.StripComments` 处理注释与 BOM；三个 builder 改为按文件顺序遍历，嵌套的 `Rate`/`mode` 对象同样保序。`metadata.py` 复验 32/32 类型内容一致。

测试：`internal/server/metadata_test.go::TestMetadataKeepsFileOrder`（夹具刻意让文件序、数字序、字典序三者互不相同）。

仍待处理的包序差异（74/76 顺序、103 位置、151 条数、180 位置、153 长度、72 内容、67 条数、157 flag 策略）记录在 `code-review/review-request.md` §5 P0-6。

## 2026-11-03 处理 4308aae 审查（1 P1 + 2 P2）

- **host 场景消耗入口能删除已选中的武器**（N1，P1）：基线的 `OnUseInGameItem` 走 `CItemManager::UseItem` 而不是直接消耗：先 `CanUseItem`（`count<>0 && status==0`）拒绝选中/耗尽的道具，再按 ClassName 与 Category 分派。`hostRequest` kind 100 现在按这个顺序处理：`LotteryKeyItem`、`Requirement`/`RewardItem`/`NicknameChange`、676 在这条消息里都不消耗（消息不带 additionalArg，`OnItemUse` 的 count 是 0），只有 `Category<9 || ==11` 会走 `ChangeItem(ActivateItem)`；道具存在时始终回 `68/100`。
- **大厅人数预算漏算可选字段**（N2，P2）：`maxUserInfoSize` 之前按“无战队、无背景、无 tournament”的角色计算，`maxLobbyPlayers` 因此高估到 202+，实测 202 人会生成 69693 字节的 153 包。现在按最坏角色计算（最长角色名 + nameplate 5B + clan 8B+20B 名+3B + tournament 1B + 固定 titles），上限降到 **181**（`maxUserInfoSize=341`），`validateRoomFrame` 复用同一预算。`MaxPlayers=100` 的配置不受影响。
- **公仓存入后的接替副本未下发**（N3，P2）：`GiveClanItem` 在 `promoteItemSelection` 之后重读库存，返回“墓碑 + 剩余库存”，与分解/到期路径同形，客户端能看到接替副本的新选中状态。

测试：`TestHostScenarioItemUse` 按基线改写（refuse / activation / unknown 三个分支）；`TestClanStorageGiveTakeDeleteAndAccess` 的补丁断言改为“首条墓碑 + 不再含该槽位有效条目”；`TestRoomSettingsReserveSpaceForEveryMember` 的夹具改为最坏角色，与预算同源。审查者的 `review-4308aae` 三条探针全过。

## 2026-11-03 处理 04608be 审查（4 P1）

- **host 激活分支遗漏 `inUse=1,status=0` 的物品**（R1）：`ChangeItem(ActivateItem)` 在 `InUse!=0` 时直接返回，那是 `SwitchInUse` 的幂等约束，不能代替 `UseItem` 的用途分派。新增 `Store.UseInGameItem` 复刻 `itemmanager.cpp:1402–1442` 的默认分支：`CanUseItem` 仍拒绝 `count==0`/`status!=0`；带相对天数的道具先换算成绝对分钟，若存在同 ID、有到期时间且已选中的副本则按 `ExtendItem(duration=false)` 延长并删除优惠券，否则把本行 `status` 置 1（连同换算后的到期时间）；不取消其它副本的选中。
- **归档目录被 `./...` 扫描导致标准检查失败**（N1）：`code-review/probes/go.mod` 建立模块边界，父模块不再进入该目录；overlay 绝对路径与归档布局保持不变。根目录 `go build/vet/test ./...` 恢复全绿。
- **房间设置保存校正值、广播原始请求值**（N2）：新增 `settings.writeMasked(w, mask)`，掩码取请求 flags、字段字节取校正后的 `merged`，与基线 `OnRoomUpdateSettings`（`newSettings` 的 flags + 房间当前设置的值）一致。房间清掉的字段按请求宽度发零值，保证帧合法，且不重新引入已撤销的 flag 归一化。
- **换房主后不刷新房主道具权益**（N3）：新增 `refreshMigratedHostItems`，在 `leaveRoom` 选出新房主后重算 superRoom / c4Timer / sd，并按基线 `CheckForHostItems` 逐字段广播（sd 仅模式 3/4/5/15/24）。`socialFixture` 补上真实 Store，因为 `leaveRoom` 现在要读新房主库存。

测试：审查者 `review-04608be` 三条探针覆盖 R1/N2/N3；`go build/vet/test ./...` 在根目录全绿；十一批历史探针通过（`review-b4cc191` 两条过期探针除外）。






