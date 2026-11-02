# 差距分析：Go 重写版与上游 C++ 实现

基准：本分支 `go-rewrite`（2026-11-02）对照上游 `JusicP/CSNZ_Server@12644f2adc0708e2d8b74f7dd6a4c3c29d3e1140`。默认嵌入数据也使用该提交的 `bin/` 快照。

方法：以 `CServerInstance::OnPackets`（`src/serverinstance.cpp:352`）的入口分发表为准，逐项对照 Go `internal/server/server.go` 的 `dispatch`，再下钻到各 manager 的子类型处理；只统计上游实际实现的功能，上游为桩的模块单独列出（第二节）。

## 结论

- 入口覆盖：上游 33 个入口中 Go 已接 28 个（含 UDP）；未接的 5 个在上游是空实现或桩（`15`、`83`、`88`、`90`、`164`）；`103 Voxel` 已接登录 URL 与占位回包。
- 业务覆盖：上一版列出的道具养成、军团、武器解放、房间设置校验、称号、屏蔽名单、问卷、重连恢复、结算加成与排名等均已实现（见附录）。
- 剩余缺口集中在：Studio 房间创建（依赖 Voxel 槽位 HTTP 服务）、少数 GM 命令、可选房间字段的模式清洗与 SSL。Bingo 与 Voxel `103` 基础通路已接。
- 规模：C++ 核心约 4.6 万行（不含 GUI/thirdparty/test）；Go 非测试 15,133 行、含测试 23,126 行。

## 〇、默认数据与目标客户端

- 默认嵌入数据已回退到上游 `12644f2` 的 `bin/`，不是 `S:\.temp\CSNZ` 的客户端定制数据。
- 对比基线，`S:\.temp\CSNZ` 的 `Item.csv` 多 60 个 ID、`MapList.csv` 多 28 个、`GameModeList.csv` 多 8 个；其中 `Item.csv` 的 ID 集合对应上游 2025-02-19 元数据，地图/模式另有私服追加。
- 当前默认配置的 `Bingo.Active`、`WeaponRelease.Active` 均为 `false`，`MainMenuSkinEvent` 为 0；因此现成默认数据不会暴露这些活动。
- 目标客户端仍是 `30.12.24`；如果要用它的活动/商店/道具，必须在运行目录替换 `ServerConfig.json` 和 `Data`，或重新制作嵌入数据。默认上游数据与目标客户端存在内容差异，这是部署缺口，不是协议入口缺口。

## 一、剩余功能缺口

### 1.1 Bingo 小游戏（已实现，待实机验收）

上游 `minigamemanager.cpp:51` 的 update（0）、reset（3）、shuffle（4）可用，open-random（1）为注释状态。Go 已接 `89/0/0,1,3,4`：初始化 25 格/12 个奖项，登录下发事件位 `1<<0` 和桌面，支持开格、完成连线发奖、洗牌限制和重置；状态落在三张 Bingo 表。

限制：上游 open-random 本身被注释，Go 采用“从未打开的桌面数字中随机选一个”的简化规则；客户端是否按这个流程刷新、发奖提示和开格动画是否完整，尚未实机验收。

### 1.2 Studio / Voxel 房间（`103` 与 `65/34` 已接，建房未接）

上游在登录时下发 Voxel URL（`SendVoxelURLs`），并对 8 个 Voxel 请求返回固定占位包。Go 已接 `103` 的 URL 下发和 `4/8/9/10/38/46/47/58` 占位回包，`65/34` 也已按 `voxel_id` 过滤并回房间列表。

Studio 房间创建（地图 254）仍拒绝：上游 `CRoomSettings::CheckSettings` 会调用 `CVoxelManager::GetSlotDetails` 访问 `Data/VoxelConfigList.json` 里配置的 Voxel HTTP 服务（`GET /v6/slots/detail/<slot>`），用返回的资源标题、人数、模式覆盖房间设置；本仓库的配置只有 `VoxelVxlURL`/`VoxelVmgURL`，没有该 HTTP 服务地址，也没有可用的服务实例，因此尚未实现。现在 `65/34` 会返回空列表而不是“未实现”提示，客户端 Studio 房间浏览器可以正常打开。

### 1.3 GM 管理命令（公开命令已实现）

上游 `channelmanager.cpp:372` 起实现 31 个聊天命令：`/ban`、`/ipban`、`/hban`、`/unban`、`/additem`、`/giveitem`、`/removeitem`、`/addpoints`、`/givepoints`、`/addexp`、`/addallitems`、`/givereward`、`/giverewardtoall`、`/addgroom`、`/addwpnreleasechar`、`/sendnotice`、`/sendgmnotice`、`/bingo`、`/weaponrelease`、`/setiteminuse`、`/setitemstatus`、`/status`、`/getuid`、`/getfreeslots`、`/disconnect`、`/reqsnapshot`、`/tournament`、`/help` 等。

Go 已按 `UserCharacterExtended.gameMaster` 鉴权，实现 `/help`、`/version`、`/disconnect`、`/status`、`/getfreeslots`、`/getuid`、`/addpoints`、`/givepoints`、`/addexp`、`/sendnotice`、`/sendgmnotice`、`/additem`、`/giveitem`、`/removeitem`、`/addallitems`、`/setitemstatus`、`/setiteminuse`、`/givereward`、`/giverewardtoall`、`/addwpnreleasechar`、`/ban`、`/hban`、`/ipban`、`/unban`；`/bingo`、`/weaponrelease` 按上游对全部玩家开放。IP 封禁在 TCP accept、HWID 封禁在登录握手（`U` 类型 3）处拦截；三者都会断开在线目标，`/ban type=1` 向全服广播封禁原因。

剩余只有 `PUBLIC_RELEASE` 之外的管理命令（`/addgroom`、`/reqsnapshot`、`/tournament`）。GM 发奖复用 `ItemRewards.json` 目录奖励，选择型奖励仍需要客户端选择 UI；上游的 Qt 管理 GUI（账号/角色/封禁编辑）也未迁移。

### 1.4 房间子项（`65`）

| 子类型 | 上游 | Go |
| --- | --- | --- |
| 34 Voxel 房间列表 | 按 `voxel_id` 过滤并返回房间 | 已实现（房间条目复用全量列表编码） |
| 35 ZB addon 设置 | 校验 `zbsaddonitem`、消耗道具并保存 addon | 已实现（替换 `UserAddon`，跳过未知/未拥有/受保护道具） |
| 37 军团战踢人 | 踢出对方军团成员并回 `KickClan` | 已实现（仅家族战房主、非对局中） |
| 32 / 33 | 固定空包（`SendRoomUnk32/33`） | 已回同结构空包 |

### 1.5 房间高级设置校验

Go 在 `Room.ValidateSettings` 下已校验人数、地图人数上限、胜负/击杀/时间/回合范围与步长，并补齐：

- 模式/地图矩阵：按 `MapList.csv` 的模式列（`map_original`、`map_zombi`、`map_Zombie_Exterminate` 等）与 `map_FunMode`/`playroom_modeID` 判定
- `mapPlaylist`：2..5 张、禁止重复、首图必须是房间地图、每张地图合法且无默认武器限制、模式允许 playlist
- `randomMap` 的模式限制与大于 2 时夹紧
- ZS 难度：模式 15 必须有难度字段，且不超过地图 `ZSmaxDifficulty`
- 家族战：仅限上游允许的模式
- `superRoom`：改由房主持有的道具 8357 决定，不再信任客户端字段

在 `ValidateSettings` 开启时还按上游 `LoadNewSettings` 做可选字段清洗：`voxel` 块（非地图 254 清除）、`fireBomb`、`mutationRestrict`/`mutationLimit`、`zbLimit`（模式白名单、去重、道具存在性、标志夹紧）、`teamSwitch`、`zbRespawn`/`zbBalance`、`gameRule`、`zbAutoHunting`、`integratedTeam`、`enhanceRestrict`、`floatingDamageSkin`、`playerOneTeam`、`weaponBuyCoolTime`、`zbRebalance`、`changeShot`/`zombieReviveZHC`、`kdRule`/`mannerLimit`、`leagueRule`、`statusSymbol`/`movingShot`/`mapPlaylistIndex`。

对局结束后按 `mapPlaylist` 轮换地图（`CRoom::EndGame`）：非强制结束切到下一张并回绕，强制结束回到第一张，同时更新 `mapId`/`mapId2`/`mapPlaylistIndex` 并广播设置更新；专用服务器模式下轮换未回到第一张时保留专服。

房主道具相关设置也已接：`superRoom`（道具 8357）、`sd` 大头事件（道具 439）、`c4Timer` 音效（道具 112）；`buyTime`/`startingCash` 取值校验、`teamBalance`/`friendlyFire` 默认值比对、`viewFlag` 位清理、`displayNickname`/`hostageKillLimit`/`freezeTime`/`flashLight`/`voiceChat` 夹紧，以及上游固定丢弃的 `levelLimit`/`status` 等字段。

bot 数量/难度夹紧（模式 3/4/5/24 上限、其他模式默认值比对）、`ballNumber` 模式与地图上限校验、`weaponRestrict` 夹紧和地图默认武器限制下发也已接。房间设置校验到这里已基本对齐，剩余只有 Studio 房间（依赖 Voxel 槽位服务）和实机验收。`ValidateSettings` 关闭时这些字段仍按客户端原样保留（与上游一致）。

### 1.6 SSL

上游支持 `SSL`（wolfSSL）；Go 在配置校验阶段直接拒绝 `SSL=true`。

## 二、与上游一致、无需实现

- `73 Rank`：上游在 PUBLIC_RELEASE 下直接回"未实现"，Go 行为一致
- `88 Quest` 界面与领取：上游 `SendQuests` 为 `TODO: reverse`，领取分支被注释
- 军团任务：上游 `CQuestManager::LoadClanQuests` 是空函数
- 每日/荣誉任务：没有独立的任务表或处理器，相关入口在上游为空
- `65/8,9` 投票踢人：`CRoom::VoteKick` 未实现
- `67/5` 队伍聊天：上游声明 party 未实现
- `67/3` 军团聊天：上游仅打印警告（军团聊天走 `71/113`）
- `83 Report`（`#if 0`）、`164 Alarm`（注释）、`15 MileageBingo`（空分支）、`90 Hack`（空分支）
- `78/8` 每日奖励（仅日志）、`78/5,6` 武器/材料组装（空 `break`）
- `68/4,6,15`、`68/19` ZBS 结果（仅日志）、`76/2`（空分支）
- 多频道：上游 `channelmanager.cpp:23` 自述未实现
- 仅服务端使用或未启用的表：`UserRestore`、`RelationProducts`、`SuspectAction`、`TimeConfig`

## 三、数据库

`internal/store/schema.sql` 与上游一致的 45 张表中，12 张尚未被 Go 代码引用：

- 设计差异：`UserQuest*` 5 张，Go 使用 `GoQuest*` 扩展表
- 上游桩或未使用：`UserRank`、`UserDailyReward`、`UserDailyRewardItems`、`UserRestore`、`RelationProducts`、`SuspectAction`、`TimeConfig`

## 四、验证缺口

真实 HLDS 对局、多人实机、模式矩阵、Bingo / Voxel / GM 命令的客户端验收、SSL，以及 `30.12.24` 客户端各界面回归。

## 五、建议顺序

1. **Studio 房间创建**：接入 Voxel 槽位 HTTP 服务后放开地图 254。
2. **剩余字段清洗**：`sd`/`c4Timer`/`startingCash`/bot 数量等依赖道具和模式默认值的夹紧。
3. **SSL**：按部署需要决定。
4. **Bingo/Voxel/GM/房间校验实机验收**：开格与发奖、Voxel 列表、GM 封禁与发奖、`Room.ValidateSettings` 开启后的建房流程。
5. **验证**：真实 HLDS、多人实机与客户端界面回归。

## 附：已实现能力（本轮复核）

- 道具：解码器开箱、强化（含降级与重置）、分解、涂装、部件、服装与纹身、延期、快捷购买
- 玩家：昵称/位置/聊天色、屏蔽名单与聊天过滤、Messenger、问卷、Addon、称号、公告、到期与离线奖励通知、会话记录
- 军团：协议请求、成员与解散规则、仓库转移、军团聊天
- 小游戏：武器解放、Bingo 基础桌面/开格/洗牌/重置
- 房间与对局：模式/地图矩阵、playlist、ZS 难度、家族战模式与 superRoom 校验、superRoom 踢人、家族战房间、场景道具消耗、掉落箱奖励、断线重连与会话接管、结算道具加成与模式排名
- 其他：主菜单皮肤事件、军团/武器解放/家族战；默认数据回退到上游基线
