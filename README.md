# Counter-Strike Online — Go 迁移版

这是原 C++ 主服务器的 Go 重写工程，目前实现基础服务、账号、大厅、房间、基础商店，以及客户端/专用服务器对局协议和基础结算。**尚未完成全功能重写；真实客户端已跑通登录、建房、机器人对局、正常结算和返回房间。**

参考仓库按 `AGENTS.md` 约定放在 `./.ref/<repo>`：`CSNZ_Server` 是服务端源码基线 `JusicP/CSNZ_Server@4859a6e73451ccbfce167b1d28deeb0457468ac3`，`counter-strike-online` 是部署封装。Go 程序独立编译运行，不调用 C++ 程序。客户端与游戏资源不包含在本工程中。

## 运行

需要 Go 1.27 或更新版本。

```powershell
go run ./cmd/cso-server -data ./.data
```

默认监听 `0.0.0.0:30002` 的 TCP 和 UDP；`-listen 127.0.0.1:30002` 可覆盖监听地址。启动时逐个补齐缺失的配置与元数据，已有文件不会覆盖。`-init` 仅初始化默认文件，`-debug` 开启调试日志。关闭时使用 Ctrl+C，服务会关闭连接并退出。

构建输入目录为 `assets`、`cmd`、`internal`。参考代码、运行数据和构建产物分别使用 `.ref`、`.data`、`.dist`，均已加入忽略规则。

```powershell
go build -o ./.dist/cso-server.exe ./cmd/cso-server
go fmt ./...
go test ./...
go vet ./...
```

Linux 构建：

```bash
CGO_ENABLED=0 go build -o ./.dist/cso-server ./cmd/cso-server
docker compose up --build -d
```

Docker 使用 UID/GID 1000，Compose 默认使用命名卷。绑定宿主目录时需保证 UID 1000 可写。Docker 构建流程尚未在本环境执行验证。

## 默认数据

嵌入的默认配置和元数据固定取上游基线 `4859a6e` 的 `bin/`（`Metadata_ModeList.bin`、`Metadata_Encyclopedia.bin` 由基线 `packet_metadata_data.h` 里的硬编码数组生成）。重新生成需要参考仓库：

```powershell
git clone https://github.com/JusicP/CSNZ_Server ./.ref/CSNZ_Server
./scripts/sync-defaults.ps1
```

脚本只改写内容不同的文件，并删除基线中不存在的文件；结果提交在 `assets/defaults`，构建时嵌入。

## GM 命令

`UserCharacterExtended.gameMaster` 为 1 的角色可使用 GM 聊天命令：

```sql
UPDATE UserCharacterExtended SET gameMaster=1 WHERE userID=1;
```

改完需重新登录，`/help` 会列出已实现的命令。`/ban` 的 term 单位为天，0 表示永久；`/ipban`、`/hban` 使用该账号最近一次登录记录的 IP/HWID。

## 当前迁移范围

| 模块 | 实现情况 |
| --- | --- |
| 网络 | 原版 `~SERVERCONNECTED` 欢迎消息、`U` 数据帧、长度限制、拆包/粘包、序号循环、读写超时、有界发送队列、退出清理 |
| 加密 | 可选原版 HWID/MD5 派生 RC4 双向流与切换时机；`SSL=true` 时用 TLS 1.3（`Data/Certs/server-cert.pem`/`server-key.pem`），UDP 不受影响 |
| 账号 | `/register`、`/login`、重复登录时关闭旧会话，并在 5 分钟内恢复原房间/对局、注册 IP 限额、账号封禁检查、角色创建 |
| GM 命令 | 按 `UserCharacterExtended.gameMaster` 鉴权；`/help`、`/version`、`/disconnect`、`/status`、`/getfreeslots`、`/getuid`、`/addpoints`、`/givepoints`、`/addexp`、`/sendnotice`、`/sendgmnotice`、`/additem`、`/giveitem`、`/removeitem`、`/addallitems`、`/setitemstatus`、`/setiteminuse`、`/givereward`、`/giverewardtoall`、`/addwpnreleasechar`、`/ban`、`/hban`、`/ipban`、`/unban`；`/bingo`、`/weaponrelease` 对所有玩家开放；IP 封禁在连接入口、HWID 封禁在登录握手处拦截，封禁会断开在线目标 |
| 数据 | SQLite v4 表结构、PBKDF2 密码摘要、玩家设置、背包读取、登录会话与历史；账号/背包/封禁日期按客户端的分钟时间戳保存 |
| 元数据 | CSV/JSON ZIP 与原始 bin、武器皮肤、随机武器、僵尸武器、ItemBox 列表；按配置开关下发 |
| 大厅 | 单服务器/单频道、玩家进出通知、聊天与私聊、房间列表；`67/6` 全服喇叭消耗道具 204 并向所有在线玩家广播 |
| 屏蔽名单 | `74` 列表、添加/移除、屏蔽设置、列表上限与登录下发；聊天、私聊和房间加入过滤已接入 |
| 玩家资料 | 改名卡改名（校验唯一性/长度/字符并消耗道具）、位置、聊天颜色设置；教程标记按上游忽略 |
| 资料查询 | `93 Messenger` 按昵称查询角色资料并返回完整 `157` 风格用户信息 |
| 称号 | `84` 设置昵称前缀、设置/移除 5 个称号槽；按 `achievementList` 校验已解锁称号，随 `157` 下发 128 字节解锁位图与槽位列表 |
| 军团 | `71` 列表/筛选/详情、创建扣 30,000 积分、申请/审批/拒绝/邀请/退出、成员列表与在线状态、军团聊天、公告/加入方式/地区/标记设置、解散与转让；登录回放成员、申请与军团状态；仓库 5 页 × 20 格支持存取、删除与页权限；军团任务上游是空实现 |
| 小游戏 | `89/6` 武器解放：读取 `MiniGames.WeaponRelease` 配置行与字母，登录下发事件位和进度，支持选字、消耗字母、重复/缺字状态、集齐发奖。`89/0` Bingo：初始化 25 格/12 个奖项，登录下发事件位和桌面，支持开格、完成连线发奖、洗牌限制和重置；open-random 上游本身被注释，Go 采用随机开未开格 |
| Voxel | `103` 登录下发 `VoxelVxlURL`/`VoxelVmgURL`，并回 `4/8/9/10/38/46/47/58` 固定占位包；`65/34` 按 `voxel_id` 过滤回房间列表；Studio 房间创建（地图 254）依赖 Voxel 槽位 HTTP 服务，仍未接 |
| 问卷 | `87` 登录时下发未答问卷；类型 11 校验问卷/问题/选项并事务保存到 `UserSurveyAnswer`，回复 0/1/2；类型 12 按上游忽略 |
| 公告 | 登录时按 `ServerConfig.json` 的 `Notices` 下发 `67` 类型 65 公告包，ID 偏移 10000，时间从秒换算为分钟 |
| 登录通知 | 登录时补发 `UserRewardNotice` 中的固定奖励并清理已发记录；`UserExpiryNotice` 以 `67` 类型 62 下发后清空；定时清理过期道具并入队提醒；`League` 请求返回 5 个联赛占位条目；`MainMenuSkinEvent` 以 `159/3` 下发 |
| 房间 | 创建、密码验证、人数限制、加入、退出、准备、换队、房主迁移、邀请、房主踢人、断线重连回到原房间/对局、设置增量更新；按配套模式表补齐计时、击杀/回合限制，补齐机器人、购买时间等基础默认值；`Room.ValidateSettings` 开启时校验模式/地图矩阵、mapPlaylist、randomMap、ZS 难度、家族战模式与地图人数上限，并按模式下清洗 `zbLimit`/`fireBomb`/`mutation`/`buyTime`/`startingCash`/bot/`ballNumber`/`weaponRestrict` 等可选字段；`superRoom`/`sd`/`c4Timer` 由房主道具决定；家族战支持双军团建房/加入、禁止换队、双方到齐才能开局、`116` 军团通知和 `65/37` 踢出对方军团；`65/35` ZB addon 消耗并保存，`65/32/33` 回固定空包；对局结束按 `mapPlaylist` 轮换地图，专服模式下保留到轮换结束 |
| 对局 | 房主客户端承载、加入主机、主机背包、击杀/死亡/分数与阵营计数、存档、换图、断线主机迁移；`68/100` 场景道具使用消耗并同步背包 |
| 专用服务器 | IP 白名单注册、空闲池分配、强制/优先专服模式、玩家连接指令、断线强制结束；尚未接真实 HLDS |
| 结算 | 按模式计算经验/积分、角色/装备加成、经验封顶与升级、部分模式战绩更新；个人模式按分数、回合模式按击杀排序；事务内写入去重记录，避免同一对局重复发奖；场景掉落箱 `3000–3023` 即时发奖并推送通知 |
| UDP | 已登录用户打洞应答与端口登记，校验来源 IP；不转发游戏流量 |
| 商店 | 商品/推荐/热门下发、当前客户端 32 位商品编号购买、整包发放与扣款原子事务；普通武器发放即可用、计数道具堆叠、密码箱计数；服装发放到空槽位时自动装备 |
| 背包操作 | 普通武器/角色启用、同款武器选中副本切换、道具锁定/解锁、增量下发；保护默认道具，检查所属玩家和有效期 |
| 道具延期 | `78/16` 消耗延长道具 676，支持活跃道具按天延长、休眠道具相对天数延长和永久化；目标与消耗在同一事务内完成 |
| 武器涂装 | `78/17` 获取并消耗涂装道具，`78/24` 切换或移除涂装；校验 `WeaponPaints.json` 兼容列表，更新 `paintID`/`paintIDList` 并同步背包 |
| 武器部件 | `78/21` 装配/卸下 category 13 部件，校验武器类型、消耗部件并同步 `partSlot1/2` |
| 武器强化 | `78/14` 类型 0：按 `ReinforceMaxLv`/`ReinforceMaxEXP` 与 `ItemGrade` 消耗材料注入经验并按概率升级；类型 2：防降级/清除单项或全部强化；更新 `enhancementLevel/Exp/Value` |
| 分解 | `78/7` 删除选中的背包物品并同步背包；上游同样只删除、不发奖励 |
| 服装 | `9/15` 头/背/臂/骨盆/脸/纹身/宠物装备与替换，按槽位更新 `UserCostumeLoadout` 和 `inUse`；ZB 皮肤按 `ZombieSkin` 类型独立保存到 `UserZBCostumeLoadout` |
| 超级房间 | `112` superRoom 房主踢人，校验房主/同房/超级房间标记，广播踢人包并记录禁止重入 |
| 通用发放与奖励 | 商店/礼包共用事务发放，支持来源消耗、物品/积分/经验/荣誉点、选择与随机奖励；领取凭据持久化、失败重试不重抽、领取去重；补给包、金币券和配件武器自选箱已实机验证 |
| 活动任务事件 | 读取 EventQuests，接入登录、升级、击杀、分钟、胜利及部分主机事件；保存进度、冻结完成奖励并去重领取；当前配套表缺失奖励 1111/1112，保留完成进度等待配置补齐；任务界面上游为 `TODO: reverse`，每日/荣誉/军团任务上游均为桩 |
| 收藏 | 读取配装/购买菜单/书签，保存购买菜单/书签/当前配装编号、武器配装和大厅背景角色；校验物品持有、有效期、类别及当前配装 |
| 快捷购买 | `77/1` 保存 5 组、每组 11 个商品的快捷购买预设，按槽位更新或插入 |
| 附加内容 | `115 Addon` 从 `UserAddon` 下发附加内容列表，空列表也返回计数 0 |
| 尚未迁移 | Studio 房间创建；任务界面/每日/荣誉/军团任务、排行在上游本身是桩，不计入迁移缺口 |

以用户提供的 `30.12.24` 客户端为唯一目标，不保留旧客户端协议分支。已跑通圣诞大厦的虚拟个人决战（模式 4、地图 277）：机器人入场、击杀、死亡/复活、达到 20 次击杀后正常结束、结果窗口及返回房间。首局实际发放 230 经验和 200 积分，数据库仅有一条对应结算记录。新角色的初始 MP5 也已在真实地图中验证。其他模式、多人网络环境和真实 HLDS 仍需逐项验收。

## 客户端基准与默认数据

默认数据固定嵌入上游 `JusicP/CSNZ_Server@4859a6e73451ccbfce167b1d28deeb0457468ac3` 的 `bin/` 快照：`ServerConfig.json`、商店/奖励/活动/皮肤/武器列表和 `bin/Data` 下 Go 服务端读取的表与二进制数据。`S:\.temp\CSNZ` 的客户端定制数据暂不使用。新运行目录直接初始化即可，无需再复制客户端目录：

```powershell
go run ./cmd/cso-server -data ./.data -init
go run ./cmd/cso-server -data ./.data -listen 127.0.0.1:30012
```

初始化不会覆盖已有文件；已有运行目录更新资源需自行替换对应数据文件。默认嵌入数据对应上游基线，未包含 `S:\.temp\CSNZ` 的客户端定制配置；需要时手工把对应 `ServerConfig.json`/`Data` 放入运行目录。本次联调数据在 `.dist/client-test-data`，与客户端原服数据分开。

`Room.HostConnectingMethod` 为 `0` 时使用房主客户端，`1` 时必须有空闲专用服务器，`2` 时优先专用服务器、无可用专服时回落到房主客户端。专服连接需命中 `DedicatedServerWhitelist`。

## 数据目录与存储

配置采用配套 `ServerConfig.json`，支持注释；`Port` 使用字符串（例如 `"30002"`）。元数据默认按目标客户端解析的长度帧 `U8(id) + U16(size) + data` 下发；`MetadataZipTypeByte=true` 才切回上游私服 build 2063 的 `U8(id) + U8(5) + U16(size) + data` 帧，该字节会让本客户端把 `5` 当成长度并闪退。当前只读取已实现模块涉及的配置项，其他选项不会自动获得对应业务能力。`SSL=true` 需要 `Data/Certs/server-cert.pem` 与 `server-key.pem`，缺失时启动失败。

SQLite 使用 `UserDatabase.db3`、v4 表结构、事务、外键与 WAL，不支持其他数据库版本。默认目录为 `./.data`。密码只接受本项目生成的 PBKDF2 摘要；账号、道具和封禁日期直接使用 Unix 分钟，不再自动转换早期 Go 构建的秒时间戳。Go 结算审计表的 `settledAt` 字段使用秒。

领取记录、活动任务进度、事件去重与待投递事件保存在 `GoRewardClaim`、`GoQuestTaskProgress`、`GoQuestEvent`、`GoQuestEventOutbox` 扩展表中。消耗来源道具与发奖同一事务；升级和正常结算产生的任务事件也与角色数值一起提交。任务完成奖励在登录、返回大厅或关闭对局结果后领取，每次恢复最多 8 项；背包不足时保留原奖励。更改任务目标需使用新的任务编号，避免重置旧任务后再次发奖。

## 验证与后续工作

自动测试包含协议拆包/加密/序号循环/错误帧、配置注释、注册竞争、密码摘要校验、数据库持久化、购买回滚，以及 TCP 注册→登录→角色→大厅→房间→开局→背包→结算和 UDP 打洞。对局链路包含明文、RC4、模拟专服三种场景，覆盖结算重试和客户端主机断线迁移。协议与房间解析还包含模糊测试入口。

最近一轮完整自动链路测试和竞态检查通过，并完成当前客户端自动登录、M24 三天商品购买扣款/发放、仓库锁定/解锁、机器人对局、结果页余额和返回房间复验。界面余额与数据库一致，结算仅一条记录。启动时会检查大厅人数和主机背包能否放入单帧：当前布局人数最多 370；50 个默认物品时，持有背包上限 3226，默认配置无需改动。清理、修复和各轮验证记录见 [STATIC_REVIEW.md](STATIC_REVIEW.md)。

剩余迁移顺序与源码映射见 [MIGRATION.md](MIGRATION.md)。

## 许可证

GPL-3.0，见 `LICENSE`。实现参考上述上游固定提交，配置和 SQL 保留上游来源；配套表及资源来自当前客户端目录，来源见 `assets/README.md`。CSO/CSNZ 名称与游戏资源归原权利人所有。
