# 差距分析：Go 重写版与上游 C++ 实现

基准：本分支 `go-rewrite`（`cd3145c`）对照上游 `JusicP/CSNZ_Server@12644f2adc0708e2d8b74f7dd6a4c3c29d3e1140`。

方法：以 `CServerInstance::OnPackets`（`src/serverinstance.cpp:352`）的入口分发表为准，逐项对照 Go `internal/server/server.go` 的 `dispatch`；再下钻到各 manager 的子类型处理。只统计上游真正实现的功能，上游本身为桩的模块单独列在第五节，避免把"上游也没做"当成迁移缺口。

## 结论

- Go 版已覆盖"登录 → 大厅 → 建房 → 开局 → 结算 → 返回"完整闭环，并实现商店、背包、道具使用与奖励领取、活动任务事件、专服池、主机迁移、UDP 打洞和元数据下发。
- 外围系统基本空白：道具养成（分解/强化/ZB 皮肤）、军团、小游戏、称号、玩家资料扩展、管理能力；开箱、延期、涂装、部件与服装已接。
- 规模对照：C++ 核心源码约 4.6 万行（`src`，不含 GUI/thirdparty/test），Go 非测试代码约 5,700 行、含测试约 8,100 行。
- 协议入口：上游 33 个包入口（其中 2 个空实现、3 个桩），Go 已接 17 个（含 UDP）。

## 一、协议入口层缺口

| 包 | 功能 | 上游实现 | Go 状态 | 影响 |
| --- | --- | --- | --- | --- |
| `69 UpdateInfo` | 改昵称、位置、教程标记、聊天颜色 | `usermanager.cpp:862` | 已实现：改名/位置/聊天色；教程标记按上游忽略 | 玩家资料更新可用 |
| `71 Clan` | 军团 25 个子请求 | `clanmanager.cpp`（1,077 行） | 缺失 | 军团界面全不可用 |
| `74 Ban` | 屏蔽名单 4 个请求 | `usermanager.cpp:1244` | 已实现：列表/设置/登录下发/过滤 | 屏蔽名单可用 |
| `84 Title` | 称号设置与列表增删 | `questmanager.cpp:246` | 缺失 | 称号不可用 |
| `87 UserSurvey` | 登录问卷 11/12 | `usermanager.cpp:1216` | 缺失 | 问卷不弹 |
| `89 MiniGame` | Bingo、武器解放 | `minigamemanager.cpp`（482 行） | 缺失 | 小游戏不可用 |
| `93 Messenger` | 查询玩家公开资料 | `usermanager.cpp:1275` | 已实现：按昵称查询角色资料 | 资料查看可用 |
| `103 Voxel` | 8 个请求 + 登录下发 Voxel URL | `voxelmanager.cpp:15` | 缺失 | Studio 不可用（上游也仅占位） |
| `112 Kick` | superRoom 房主踢人 | `usermanager.cpp:1353` | 已实现：校验 superRoom/房主/同房，广播踢人包 | 超级房间踢人可用 |
| `115 Addon` | 附加内容列表下发 | `usermanager.cpp:1305` | 已实现：列表下发 | 附加内容可显示 |

## 二、已接入口内的子类型缺口

### Item（78）

已实现：0（使用）、23（切换启用）、26（锁定）、10/11/12（后台通知，无需操作）。

| 子类型 | 上游实现 | 缺口 |
| --- | --- | --- |
| 4 OpenDecoder | `luckyitemmanager.cpp:289`（396 行） | 已实现：开箱、概率奖励、事务发奖 |
| 7 Disassemble | `itemmanager.cpp:1759` | 分解 |
| 14 EnhanceReq | `itemmanager.cpp:2071`（约 460 行） | 仅错误回包，强化/升级逻辑未迁移 |
| 16 ItemExtend | `itemmanager.cpp:1726` | 已实现：延期、消耗延长道具 |
| 17 WeaponPaintReq | `itemmanager.cpp:2533` | 已实现：涂装获取、消耗涂装道具 |
| 24 WeaponPaintSwitchReq | `itemmanager.cpp:2580` | 已实现：涂装切换/移除 |
| 21 Part | `itemmanager.cpp:2628` | 已实现：部件装配/卸下、消耗部件 |
| 9 / 15 Tattoo/Costume | `itemmanager.cpp:2834` | 已实现：头/背/臂/骨盆/脸/纹身/宠物；ZB 皮肤未接 |

上游为空的子类型（不迁移）：5 WeaponAssem、6 MaterialAssem（空 `break`）、8 DailyRewards（仅日志）。

### Room（65）

已实现：0、2、3、4、5、6、7、11、15、17、19、22、23。

缺：34（Voxel 房间列表）、35（感染模式 addon 选择）、37/38（军团战踢人/军团公告）、32/33（上游空包）；8/9 投票踢人在上游 `CRoom::VoteKick` 直接输未实现。

### 其他入口

| 入口 | 缺口 |
| --- | --- |
| UMsg（67） | 缺 3 军团频道、6 全服喇叭；5 队伍频道上游未实现 |
| Favorite（77） | 已实现 0 购买菜单、1 快捷购买、2 配装、6 书签 |
| Option（76） | 只有 0（保存配置），缺 2（进入游戏时上报） |
| Host（68） | 基本齐备（0–24、101–103），缺 4、6、15、100 等少量事件 |
| UDP（70） | Go 仅处理打洞类型 0；上游 1–5 也只是日志占位，等价 |

## 三、业务模块缺口

| 模块 | 规模（上游） | 现状与差距 |
| --- | --- | --- |
| 道具养成 | itemmanager 2,986 行 + luckyitem 396 行 | 开箱、延期、涂装、部件与服装已实现；分解、强化、ZB 皮肤仍缺，是当前最大空白 |
| 军团 | clanmanager 1,077 行 + 7 张表 | 完全缺失：创建/申请/审批/成员管理/仓库/军团任务/军团聊天 |
| 小游戏 | minigamemanager 482 行 + 5 张表 | Bingo、武器解放全缺 |
| 房间设置校验 | roomsettings 2,655 行 | Go 虽读取 `Room.ValidateSettings` 配置项但代码从未使用；上游按模式/地图校验人数、胜负限制、playlist、zbLimit、家族战、superRoom 等 |
| 玩家资料与外围 | usermanager 1,541 行 | 缺问卷、公告 notices、到期提醒、离线奖励通知、会话历史、断线重连恢复到原房间；改名卡、聊天色、Messenger、快捷购买与 Addon 已接 |
| 对局结算补全 | gamematch 709 行 | 已有基础结算与等级/职业加成；缺道具加成（`GameMatch.CalcResults.BonusPercentage.Items` 已读未用）、模式统计事件（炸弹/拆弹/人质/怪物/蚊子/风筝）、场景掉落箱奖励（rewardID 3000–3023）、ZBS 名次奖励、第一名计算 |
| 多频道 | channelmanager | 上游 `channelmanager.cpp:23` 自述 `TODO: Should we implement multi channels?`，不必实现 |

## 四、数据库：45 张表中 35 张未被任何 Go 代码引用

`internal/store/schema.sql` 已与上游完全一致（45 表），缺的是读写逻辑：

- 军团 7 张：`Clan`、`ClanMember`、`ClanInvite`、`ClanMemberRequest`、`ClanStorageItem`、`ClanStoragePage`、`ClanChronicle`
- 玩家外围：`UserCostumeLoadout`、`UserZBCostumeLoadout`、`UserSurveyAnswer`、`UserExpiryNotice`、`UserRewardNotice`、`UserSession`、`UserSessionHistory`、`UserRestore`、`UserDist`、`TimeConfig`（`UserBanList`、`UserFastBuy`、`UserAddon` 已接入）
- 小游戏 5 张：`UserMiniGameBingo`、`UserMiniGameBingoPrizeSlot`、`UserMiniGameBingoSlot`、`UserMiniGameWeaponReleaseCharacters`、`UserMiniGameWeaponReleaseItemProgress`
- 任务与排行：`UserQuest*`（Go 改用 `GoQuest*` 扩展表）、`UserRank`
- 管理与风控：`HWIDBanList`、`IPBanList`、`SuspectAction`、`RelationProducts`、`UserDailyReward`、`UserDailyRewardItems`

## 五、上游本身为空壳，不构成迁移缺口

客户端界面仍在，但 C++ 同样未实现；Go 只需保持"无操作/回错误"而不是实现功能：

- 任务界面与领取：`packetmanager.cpp:3663` 的 `SendQuests` 等均为 `TODO: reverse`，`questmanager.cpp:231` 起三个分支被注释
- 排行榜：`rankmanager.cpp:23` 在 PUBLIC_RELEASE 下直接回 "Rank system is not implemented"，其余分支为注释
- 投票踢人：`room.cpp:782` 未实现
- 队伍/派对聊天：`usermanager.cpp:845` 自述 party 未实现
- 举报与反作弊：`usermanager.cpp:1163` 为 `#if 0`；Alarm 处理体被注释
- 每日奖励：`itemmanager.cpp:1975` 仅打日志
- 武器/材料组装：`itemmanager.cpp:614` 空 `break`
- 军团仓库上次物品：`userdatabase_sqlite.cpp:4781` 未实现

## 六、验证缺口

真实 HLDS 对局、多人实机、模式矩阵（目前仅验证模式 4/地图 277 等少数组合）、断线重连与换图、管理 GUI、SSL（Go 明确拒绝启动，上游支持）、以及 `30.12.24` 客户端各界面回归。

## 七、建议实施顺序

1. **P0 兜底**：未实现子类型补错误回包。当前对 78/4、78/7、78/14 等只写 debug 日志不回包，客户端开箱/强化界面会等待；上游每种失败都有明确回包（如 `SendItemOpenDecoderErrorReply`），照搬即可。
2. **P1 低成本高收益**：`84 Title`、房间设置校验（配置项已预留）。
3. **P2 道具养成**：分解（78/7）→ 强化（78/14）→ ZB 皮肤（9/15 剩余部分）。开箱（78/4）、延期（16）、涂装（17/24）、部件（21）与普通服装已接，后续继续复用现有事务与背包同步。
4. **P3 大件与验收**：军团（71 + 7 表）、小游戏（89 + 5 表）、结算补全（道具加成、模式统计、场景奖励），以及真实 HLDS 与多人验收。
