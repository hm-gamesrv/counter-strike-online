# 差距分析：Go 重写版与上游 C++ 实现

基准：本分支 `go-rewrite`（2026-11-02）对照上游 `JusicP/CSNZ_Server@12644f2adc0708e2d8b74f7dd6a4c3c29d3e1140`。默认嵌入数据也使用该提交的 `bin/` 快照。

方法：以 `CServerInstance::OnPackets`（`src/serverinstance.cpp:352`）的入口分发表为准，逐项对照 Go `internal/server/server.go` 的 `dispatch`，再下钻到各 manager 的子类型处理；只统计上游实际实现的功能，上游为桩的模块单独列出（第二节）。

## 结论

- 入口覆盖：上游 33 个入口中 Go 已接 28 个（含 UDP）；未接的 5 个在上游是空实现或桩（`15`、`83`、`88`、`90`、`164`）；`103 Voxel` 已接登录 URL 与占位回包。
- 业务覆盖：上一版列出的道具养成、军团、武器解放、房间设置校验、称号、屏蔽名单、问卷、重连恢复、结算加成与排名等均已实现（见附录）。
- 剩余缺口集中在：Studio 房间与 `65/34` Voxel 房间列表、GM 道具/奖励命令、房间高级设置校验与 SSL。Bingo 与 Voxel `103` 基础通路已接。
- 规模：C++ 核心约 4.6 万行（不含 GUI/thirdparty/test）；Go 非测试 13,864 行、含测试 21,057 行。

## 〇、默认数据与目标客户端

- 默认嵌入数据已回退到上游 `12644f2` 的 `bin/`，不是 `S:\.temp\CSNZ` 的客户端定制数据。
- 对比基线，`S:\.temp\CSNZ` 的 `Item.csv` 多 60 个 ID、`MapList.csv` 多 28 个、`GameModeList.csv` 多 8 个；其中 `Item.csv` 的 ID 集合对应上游 2025-02-19 元数据，地图/模式另有私服追加。
- 当前默认配置的 `Bingo.Active`、`WeaponRelease.Active` 均为 `false`，`MainMenuSkinEvent` 为 0；因此现成默认数据不会暴露这些活动。
- 目标客户端仍是 `30.12.24`；如果要用它的活动/商店/道具，必须在运行目录替换 `ServerConfig.json` 和 `Data`，或重新制作嵌入数据。默认上游数据与目标客户端存在内容差异，这是部署缺口，不是协议入口缺口。

## 一、剩余功能缺口

### 1.1 Bingo 小游戏（已实现，待实机验收）

上游 `minigamemanager.cpp:51` 的 update（0）、reset（3）、shuffle（4）可用，open-random（1）为注释状态。Go 已接 `89/0/0,1,3,4`：初始化 25 格/12 个奖项，登录下发事件位 `1<<0` 和桌面，支持开格、完成连线发奖、洗牌限制和重置；状态落在三张 Bingo 表。

限制：上游 open-random 本身被注释，Go 采用“从未打开的桌面数字中随机选一个”的简化规则；客户端是否按这个流程刷新、发奖提示和开格动画是否完整，尚未实机验收。

### 1.2 Studio / Voxel 房间（`103` 已接，`65/34` 未接）

上游在登录时下发 Voxel URL（`SendVoxelURLs`），并对 8 个 Voxel 请求返回固定占位包。Go 已接 `103` 的 URL 下发和 `4/8/9/10/38/46/47/58` 占位回包。Voxel/Studio 房间列表 `65/34` 仍未接，地图 254 的 Studio 房间创建仍拒绝；因此 Studio 实际玩法还不能完整使用。

### 1.3 GM 管理命令（命令骨架已实现，道具类未接）

上游 `channelmanager.cpp:372` 起实现 31 个聊天命令：`/ban`、`/ipban`、`/hban`、`/unban`、`/additem`、`/giveitem`、`/removeitem`、`/addpoints`、`/givepoints`、`/addexp`、`/addallitems`、`/givereward`、`/giverewardtoall`、`/addgroom`、`/addwpnreleasechar`、`/sendnotice`、`/sendgmnotice`、`/bingo`、`/weaponrelease`、`/setiteminuse`、`/setitemstatus`、`/status`、`/getuid`、`/getfreeslots`、`/disconnect`、`/reqsnapshot`、`/tournament`、`/help` 等。

Go 已按 `UserCharacterExtended.gameMaster` 鉴权，实现 `/help`、`/version`、`/disconnect`、`/status`、`/getfreeslots`、`/getuid`、`/addpoints`、`/givepoints`、`/sendnotice`、`/sendgmnotice`、`/ban`、`/hban`、`/ipban`、`/unban`。IP 封禁在 TCP accept、HWID 封禁在登录握手（`U` 类型 3）处拦截；三者都会断开在线目标，`/ban type=1` 向全服广播封禁原因。

仍缺道具与奖励类命令（`/additem`、`/giveitem`、`/removeitem`、`/addallitems`、`/setitemstatus`、`/setiteminuse`、`/addexp`、`/givereward`、`/giverewardtoall`、`/addwpnreleasechar`）以及 `PUBLIC_RELEASE` 之外的管理命令（`/addgroom`、`/reqsnapshot`、`/tournament`）。上游的 Qt 管理 GUI（账号/角色/封禁编辑）也未迁移。

### 1.4 房间子项（`65`）

| 子类型 | 上游 | Go |
| --- | --- | --- |
| 34 Voxel 房间列表 | 按 `voxel_id` 过滤并返回房间 | 未处理，回通用未实现提示 |
| 35 ZB addon 设置 | 校验 `zbsaddonitem`、消耗道具并保存 addon | 已实现（替换 `UserAddon`，跳过未知/未拥有/受保护道具） |
| 37 军团战踢人 | 踢出对方军团成员并回 `KickClan` | 已实现（仅家族战房主、非对局中） |
| 32 / 33 | 固定空包（`SendRoomUnk32/33`） | 已回同结构空包 |

### 1.5 房间高级设置校验

Go 在 `Room.ValidateSettings` 下已校验人数、地图人数上限、胜负/击杀/时间/回合范围与步长；家族战已支持双军团建房/加入/开局与 `116` 通知。仍缺：

- `mapPlaylist` 数量、重复、首图、地图合法性与模式互斥
- `zbLimit` 与 `fireBomb` 的联动
- `superRoom` 标记校验
- ZS 难度上限、模式专属必填项
- 家族战踢人（`65/37`）和更完整的家族战人数/准备校验
- 地图是否允许该模式的完整矩阵

### 1.6 SSL

上游支持 `SSL`（wolfSSL）；Go 在配置校验阶段直接拒绝 `SSL=true`。

## 二、与上游一致、无需实现

- `73 Rank`：上游在 PUBLIC_RELEASE 下直接回"未实现"，Go 行为一致
- `88 Quest` 界面与领取：上游 `SendQuests` 为 `TODO: reverse`，领取分支被注释
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

1. **GM 道具与奖励命令**：`/additem`、`/giveitem`、`/removeitem`、`/addallitems`、`/addexp`、`/givereward`、`/giverewardtoall`；封禁与 `/sendnotice` 已接。
2. **房间子项**：`65/34,35,37` 与 `65/32,33` 回包；`65/34` 决定 Voxel 房间列表能否使用。
3. **SSL**：按部署需要决定。
4. **Bingo/Voxel 实机验收**：开格/连线发奖/洗牌/重置和 Voxel 占位回包的客户端流程。
5. **验证**：真实 HLDS、多人实机与客户端界面回归。

## 附：已实现能力（本轮复核）

- 道具：解码器开箱、强化（含降级与重置）、分解、涂装、部件、服装与纹身、延期、快捷购买
- 玩家：昵称/位置/聊天色、屏蔽名单与聊天过滤、Messenger、问卷、Addon、称号、公告、到期与离线奖励通知、会话记录
- 军团：协议请求、成员与解散规则、仓库转移、军团聊天
- 小游戏：武器解放、Bingo 基础桌面/开格/洗牌/重置
- 房间与对局：房间设置校验、superRoom 踢人、家族战房间、场景道具消耗、掉落箱奖励、断线重连与会话接管、结算道具加成与模式排名
- 其他：主菜单皮肤事件、军团/武器解放/家族战；默认数据回退到上游基线
