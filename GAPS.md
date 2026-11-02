# 差距分析：Go 重写版与上游 C++ 实现

基准：本分支 `go-rewrite`（2026-11-02，`e23fa60`）对照上游 `JusicP/CSNZ_Server@12644f2adc0708e2d8b74f7dd6a4c3c29d3e1140`。

方法：以 `CServerInstance::OnPackets`（`src/serverinstance.cpp:352`）的入口分发表为准，逐项对照 Go `internal/server/server.go` 的 `dispatch`，再下钻到各 manager 的子类型处理；只统计上游实际实现的功能，上游为桩的模块单独列出（第二节）。本轮复核了上一版之后新增的 39 个提交。

## 结论

- 入口覆盖：上游 33 个入口中 Go 已接 27 个（含 UDP）；未接的 6 个里 5 个在上游是空实现或桩（`15`、`83`、`88`、`90`、`164`），实际功能缺口只有 `103 Voxel`。
- 业务覆盖：上一版列出的道具养成、军团、武器解放、房间设置校验、称号、屏蔽名单、问卷、重连恢复、结算加成与排名等均已实现（见附录）。
- 剩余缺口集中在三处：Bingo 小游戏、Voxel/Studio、GM 管理命令；另有少量房间子项、占位回包与 SSL。
- 规模：C++ 核心约 4.6 万行（不含 GUI/thirdparty/test）；Go 非测试 12,808 行、含测试 19,378 行。

## 一、剩余功能缺口

### 1.1 Bingo 小游戏（MiniGame `89/0`）

上游 `minigamemanager.cpp:51` 处理 Bingo 页面的 update（0）、reset（3）、shuffle（4），open-random（1）为注释状态；状态落在 `UserMiniGameBingo`、`UserMiniGameBingoSlot`、`UserMiniGameBingoPrizeSlot` 三张表。

Go 只处理 `89/6`（武器解放），其余请求仅写 debug 日志，三张表未被引用。

### 1.2 Voxel / Studio（`103`）

上游在登录时下发 Voxel URL（`SendVoxelURLs`，`voxelVxlURL` / `voxelVmgURL`），并对 8 个 Voxel 请求返回固定占位包。Go 未处理 `103`，登录也不下发 URL，Studio 入口不可用。

### 1.3 GM 管理命令

上游 `channelmanager.cpp:372` 起实现 31 个聊天命令：`/ban`、`/ipban`、`/hban`、`/unban`、`/additem`、`/giveitem`、`/removeitem`、`/addpoints`、`/givepoints`、`/addexp`、`/addallitems`、`/givereward`、`/giverewardtoall`、`/addgroom`、`/addwpnreleasechar`、`/sendnotice`、`/sendgmnotice`、`/bingo`、`/weaponrelease`、`/setiteminuse`、`/setitemstatus`、`/status`、`/getuid`、`/getfreeslots`、`/disconnect`、`/reqsnapshot`、`/tournament`、`/help` 等。

Go 仅实现 `/login`、`/register`、`/version`；`HWIDBanList`、`IPBanList` 两张表未被引用，`/hban`、`/ipban` 无对应实现。

### 1.4 房间子项（`65`）

| 子类型 | 上游 | Go |
| --- | --- | --- |
| 34 Voxel 房间列表 | 返回房间列表 | 未处理，回通用未实现提示 |
| 35 感染模式 addon 选择 | 设置 addon | 未处理 |
| 37 军团战踢人 | 实现 | 未处理（38 军团公告已实现） |
| 32 / 33 | 固定空包（`SendRoomUnk32/33`） | 不回包 |

### 1.5 SSL

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

`internal/store/schema.sql` 与上游一致的 45 张表中，17 张尚未被 Go 代码引用：

- 待实现功能：Bingo 3 张（`UserMiniGameBingo`、`UserMiniGameBingoSlot`、`UserMiniGameBingoPrizeSlot`）、封禁 2 张（`HWIDBanList`、`IPBanList`）
- 设计差异：`UserQuest*` 5 张，Go 使用 `GoQuest*` 扩展表
- 上游桩或未使用：`UserRank`、`UserDailyReward`、`UserDailyRewardItems`、`UserRestore`、`RelationProducts`、`SuspectAction`、`TimeConfig`

## 四、验证缺口

真实 HLDS 对局、多人实机、模式矩阵、Bingo / Voxel / GM 命令的客户端验收、SSL，以及 `30.12.24` 客户端各界面回归。

## 五、建议顺序

1. **Bingo**：客户端入口可见，涉及 3 张表与 3 个请求，工作量中等。
2. **Voxel 通路**：登录下发 URL 与 8 个占位回包，改动小。
3. **GM 命令与封禁管理**：运维需要，可先做 `/ban`、`/ipban`、`/hban`、`/unban`、`/additem`、`/givereward`、`/sendnotice`。
4. **房间子项**：`65/34,35,37` 与 `65/32,33` 回包。
5. **SSL**：按部署需要决定。
6. **验证**：真实 HLDS、多人实机与客户端界面回归。

## 附：上一版缺口在本轮的实现情况

- 道具：解码器开箱、强化（含降级与重置）、分解、涂装、部件、服装与纹身、延期、快捷购买
- 玩家：昵称/位置/聊天色、屏蔽名单与聊天过滤、Messenger、问卷、Addon、称号、公告、到期与离线奖励通知、会话记录
- 军团：协议请求、成员与解散规则、仓库转移、军团聊天
- 小游戏：武器解放
- 房间与对局：房间设置校验、superRoom 踢人、家族战房间、场景道具消耗、掉落箱奖励、断线重连与会话接管、结算道具加成与模式排名
- 其他：主菜单皮肤事件、客户端配套数据更新
