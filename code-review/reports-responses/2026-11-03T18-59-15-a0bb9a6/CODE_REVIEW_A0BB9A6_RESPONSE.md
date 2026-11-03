# 对 a0bb9a6 审查与覆盖记录的处理

提交：`5b6c6f4`（a0bb9a6 的 5 项），追加的黑盒修复见 `8fa33d5`。`go vet`、`go test ./...` 全绿；本轮 overlay（`review-a0bb9a6`）四条探针全部通过，黑盒 5 例两边均通过。

## 审查发现（4 P1 + 1 P2）

| 编号 | 问题 | 处理 |
|---|---|---|
| R1 | 登录扫描跳过推送时也跳过了缓存重读，登录链仍按过期背景初始化 | `refreshCharacterCache` 拆出 `reloadCharacter`（只读不发包）；`sweepExpiredItems` 对 skip 账号调它，包序不变、缓存已更新 |
| R2 | 强化材料删除后不清理外观引用 | `EnhanceItem` 对每个被消费材料调 `clearItemReference`；两条强化入口下发补丁后 `refreshCharacterCache` |
| R3 | 职业加成用对局开始时的快照，已到期删除的职业物品仍加成 | 新增 `Store.HasActiveItem`（复刻 `GetFirstActiveItemByItemID`：`status=1 AND inUse=1`），`endGame` 结算时查库 |
| R4 | 同款材料只把单件经验翻倍 | 改成基线的 `totalExp += exp; 同ID 时 totalExp *= 2`；抽出纯函数 `enhanceMaterialExp` 并加确定性用例 |
| R5 | Voxel 内层保留位 18/20 被当作 U32 消费 | `parseVoxel` 只对 11/12/13/15/16/17 消费 U32，18/20 不消费载荷 |

## 实现说明

- **R1 的取舍**：不能只给 skip 账号"什么都不做"——数据库清理已经把它装备的背景清空，缓存不同步就会让登录链继续下发失效背景。也不能对它广播：150 还没发。所以拆成"重读"与"重读+广播"两个函数，skip 走前者。重连走同一条 `/login` → `loginPackets` 路径，同样覆盖。
- **R3 的查询语义**：基线 `SELECT * FROM UserInventory WHERE userID=? AND itemID=? AND status=1 AND inUse=1 LIMIT 1`，没有过期或数量过滤，我按原样实现（`HasActiveItem`），不额外加 `liveItemSQL`，否则会与基线差一个"已过期但还没被扫描"的窗口。
- **R3 与装备加成的区分**：职业加成现在严格按基线查库；装备加成是显式扩展（`ItemBonusEnabled`，默认关），继续用对局快照，这个时间语义差异已写进 MIGRATION。
- **R4 的拆分**：把经验累加抽成 `enhanceMaterialExp(targetID, materials, gradeOf)`，Grade 解析留在 `materialGrade`（含 8481/8482/8566 映射）。这样"先累加后翻倍"可以被确定性测试钉住，不再靠随机抽取间接证明。
- **R5 的边界**：保留位只是不消费字节，flag 本身照样保留在 `settings.flags` 里并原样回写，因此回包与基线一致。

## 新增回归

`internal/server/review_a0_fixes_test.go`

- `TestLoginSweepReloadsExpiredSelection`（R1）
- `TestEnhanceCosmeticMaterialClearsSelection`（R2）
- `TestEnhanceMaterialDoublesRunningTotal`（R4，纯函数算例 `(500+10)*2=1020`）
- `TestExpiredClassItemLosesBonus`（R3）
- `TestVoxelReservedBitsCarryNoPayload`（R5，bit 18/20 两个子用例）

夹具调整：`rewardMatch` 现在把职业物品写成数据库行（`status=1,inUse=1`），因为加成改按基线查库。

## 探针复跑

九批 overlay 全绿（`44b5eed`、`expanded`、`b6afe1f`、`overall-f6eda20`、`154b2fe`、`33e9ec6`、`3e6fa14`、`9d33c2d`、`a0bb9a6`）。

`review-b4cc191` 的两条 server 探针仍红，且**不是本轮引入**：

- `TestReviewBonusBeforeCap` 用纯内存 `stat.items` 期待职业加成生效，而 R3 已确认基线在结算时查库；
- `TestReviewBonusRetryPercent` 期待装备加成默认开启，R6 已明确默认关闭（基线那段是注释掉的）。

两条的前提都被后续审查自己的结论取代，对应的正确行为由 `TestBonusUsesUncappedBase`、`TestEquipmentBonusRetryIsIdempotent`、`TestExpiredClassItemLosesBonus` 覆盖。

## 未做

`FuzzItemRequest`/`FuzzMetadata`、全模式结果包逐字段金标准、`zsDifficulty` 值/编码职责拆分、等级表口径、完整事务调用图（本轮覆盖记录给出的是文本候选扫描 + 两个 helper 的人工确认）。


---

## 追加：黑盒对比（同日晚，提交 `7691112`、`8fa33d5`）

### 先纠正一处判断

上一节说"metadata 各段大小差异属于数据差异"，**这是错的**。30003 那个基线实例的来路已经确认：

- 仓库 `~/repos/hm-gamesrv/counter-strike-online`（分支 `main`），容器 `cso-baseline` 里跑的是 `/app/csnz-server`（C++ 基线），由 `scripts/start-baseline.sh` 启动：`rsync -a --delete .data/ .data-test/` 后把 `.data-test` 挂成 `/data`。
- 逐文件 sha256 对比：**45/45 个文件完全相同**（含 `ServerConfig.json` `612f3893…`、`MapList.csv`、`GameModeList.csv`、`Item.csv`）。

所以两边的差异全部是代码差异。

### 新增 `blackbox/`（Python，纯 socket）

不 import Go 包、不读数据库，只看服务器发出的字节；同一套脚本可以打 30002（我们的 Go）和 30003（基线）。

| 文件 | 作用 |
|---|---|
| `cso_proto.py` | U 帧编解码、字段读写 |
| `cso_client.py` | version 握手、`/login`、按空闲超时收包 |
| `test_smoke.py` | 5 个用例：greeting、版本握手、登录进大厅、登录包序、错误密码被拒 |
| `trace.py` | 一次登录的包序列 dump，供两边逐包 diff |
| `metadata.py` | 把 91 帧逐个解压后比内容（zip 大小受压缩器影响，内容才是判据） |

两边都通过全部 5 个冒烟用例。

### 实际发现并修复的差异：元数据条目顺序

解压对比后发现 32 个类型里有 3 个"集合相同、顺序不同"：

| 类型 | 修复前 |
|---|---|
| 4 ItemBox | 同一 box 的档位组顺序不同（我们 `sort.Strings` 排） |
| 6 WeaponPaints | 我们从 paint 1 开始，基线从 4 开始（文件序） |
| 16 RandomWeaponList | 我们从 item 1 开始，基线从 8330 开始（`Version` 之后第一个） |

基线按 **JSON 文件顺序**遍历；我们解码进 map 后按数字排序。修复：新增 `internal/server/jsonorder.go`（流式 decoder 保序解析，并复用 `config.StripComments` 处理注释/BOM），三个 builder 改为按文件顺序遍历，嵌套的 `Rate`/`mode` 对象同样保序。

验证：`metadata.py` 现在 **32/32 类型内容一致**；`TestMetadataKeepsFileOrder` 用"文件序/数字序/字典序互不相同"的夹具钉住。提交 `8fa33d5`。

### 仍然存在的结构差异（未处理）

| 包 | 我们(30002) | 基线(30003) | 性质 |
|---|---|---|---|
| `157` | 303B，flags `0c4d03eb` | 398B，flags `ffffffff` | 我们只置位实际写出的可选段；基线置满 |
| `74`/`76` | 74 → 76 | 76 → 74 | 基线先发配置再发封禁表 |
| `103` | 登录链末尾 | 紧跟 91 之后 | `sendVoxelURLs` 调用位置 |
| `151` | 2 条 | 1 条 | 疑似 `roomList` 被调两次 |
| `153` | 412B | 314B | 大厅用户列表长度 |
| `180` | 在 `153`/`151` 之后 | 在它们之前 | 联赛计量条时序 |
| `72` | 32B `"Tes…"` | 34B `"Rec…"` | 同一份 `Shop.json`，取的字段不同 |
| `67` | 少 2 条 | 多 168B/28B | 欢迎语/公告条数，配置相同 |

`91` 各段的 **zip 大小**仍不同，那是 Go flate 与基线 zlib 的压缩差异，解压后内容一致，不作为缺陷处理。

