# 元数据下发导致客户端闪退：问题汇总（给 Astra）

日期：2026-11-03　仓库：`hm-gamesrv/counter-strike-online`，分支 `go-rewrite`　服务端端口 30002

## 1. 目标与背景

- 把 `JusicP/CSNZ_Server`（C++ 私服 master server）重写成 Go。`AGENTS.md` 指定基线为 `JusicP/CSNZ_Server@12644f2adc0708e2d8b74f7dd6a4c3c29d3e1140`（2024-11-29，`SUPPORTED_CLIENT_BUILD=30.11.24`）。
- 同一仓库 `main` 分支的 Dockerfile 也是 `CODELOAD` 拉取 `12644f2` 构建，`docker compose up` 起来后**客户端可以正常游玩**（用户实测）。所以 12644f2 + 该客户端 = 已知可用组合。
- 客户端：`S:\.temp\CSNZ`（README 记为 `30.12.24`；`Bin\hw.dll`、`Bin\client.dll` 修改时间 2025-02-20）。通过 hook 版 `Bin\CSOLauncher.exe` 启动，`launcher.json` 指向 `10.8.1.100:30002`、账号 `tester1`。
- hook 版启动器自带调试开关（从二进制字符串提取并实测可用）：`-debug`、`-dumpmetadata`、`-dumpitem`、`-dumpall`、`-writemetadata`、`-ignoremetadata`、`-usessl`、`-window/-novid` 等。`-dumpmetadata` 会把收到的每个元数据包写到 `Bin\MetadataDump\`（命名如 `Metadata_MapList.csv.zip`、`Metadata_Unk23.bin`、`WeaponPaints.json`），因此可以精确知道客户端收到了哪些包。

## 2. 症状

- 元数据下发开启 → 客户端登录后闪退；元数据全关 → 正常进大厅。
- 崩溃码 `0xE0005916`（自抛致命异常）；`Bin\Error.log` 为空；`Bin\Dump\baseinfo.json` 里 `EXCEPTION_CODE=e0005916`，无可用符号/调试器（本机没有 cdb/windbg）。

## 3. 关键实验与观察（全部在实机客户端上跑）

前提：客户端就在本机，可用 `Bin\CSOLauncher.exe -ip 10.8.1.100 -port 30002 -login tester1 -password secret1 -window -novid -dumpmetadata` 自动登录；服务端用 `-debug`，每个元数据包会打印 `metadata type=<id> len=<n>`。判据 = 进程是否存活 + `MetadataDump\` 里收到的包数。

E1. **帧格式 A/B（决定性）**
   - 按 `U8(id) + U8(5) + U16(size) + zip` 发：客户端收到第一个 zipped 元数据（MapList 或 ClientTable）后立刻崩，`MetadataDump` 只留下 1 个文件。
   - 与内容无关：把 `MapList.csv` 砍到只剩表头 + 3 行（服务端下发 `len=1179`）仍旧秒崩。
   - 把客户端自己 `Data\Metadata_ModeList.bin` 里那段 zip 原样抽出（787 字节），按带 `5` 的帧当 type 0 发：照样秒崩。→ 排除压缩器/内容/大小。
   - 去掉这个字节（`U8(id) + U16(size) + data`）：客户端把全部 36–38 个包收下并 dump，存活 60 秒以上，不再闪退。

E2. **type 字节位置**
   - 把 `U16(len)` 放在 `U8(type)` 之前发送，客户端把它当 `type=191` 并 dump 成 `Metadata_Unk191.bin`。→ 分派依据是 payload 首字节，type 必须第一。

E3. **ItemBox（type 4）**
   - `U8(4) + U8(0) + U32(count) + 26×count`（3aac2d2 写法，145 条）：客户端收到后约 40–55 秒崩（延迟崩，早期判据太短导致误判）。
   - 同一写法但 `count=0`：不崩。
   - `U8(4) + U16(bodyLen) + U8(0) + U32(count) + 26×count`：不崩。
   - `U8(4) + U32(count) + 26×count`（12644f2 写法）：不崩（当前实现，已实机确认）。
   - ItemBox 里的 145 条 (box,item) 与 `ItemBox.json` 一致，item id 全在 `Item.csv` 里，所以不是"未知道具"。

## 4. 根因：对照错了上游版本

`.ref/CSNZ_Server` 当时检出在 `3aac2d2`（更晚的私服构建），而项目和容器用的是 `12644f2`。两者元数据形状不同：

| 项 | `12644f2`（基线，客户端接受） | `3aac2d2`（本轮误用） |
| --- | --- | --- |
| zipped 元数据帧 | `U8(id) + U16(size) + data`（无宏、无分块） | `U8(id) + U8(ZIPMETADATA_FULL_ZIP=5) + U16(size) + data`，超 64000 分块（`CHUNK_SIZE=64000`、`MAX_CHUNKS=4`） |
| ItemBox(4) | `U8(4) + U32(count) + 26×count` | 多一个 `U8(0)` |
| ModeList(2) | 有 sender：`U8(2) + U16(sizeof(metaData2)=787) + metaData2` | `SendMetadata` 无该分支 |
| Encyclopedia(23) | 有 sender：直接下发 `metaData_Encyclopedia[697]`（无长度、无类型前缀，blob 自带 `17`） | 无该分支 |

`12644f2` 的 `SendMetadata` 顺序：MapList, ClientTable, ModeList, Unk3, ItemBox, WeaponPaints, Unk8, MatchOption, ZombieWarWeaponList, WeaponParts, MileageShop, Unk20, Encyclopedia, GameModeList, ProgressUnlock, ReinforceMaxLvl, ReinforceMaxEXP, ReinforceItemsExp, Unk31, HonorMoneyShop, ItemExpireTime, ScenarioTX_Common, ScenarioTX_Dedi, ShopItemList_Dedi, ZBCompetitive, Unk43, Unk49, PPSystem, Item, CodisData, WeaponProp, RandomWeaponList, ModeEvent, EventShop, FamilyTotalWarMap, FamilyTotalWar, Unk54, Unk55, WeaponAscend, Unk57, PerkParam, Synthesis, Unk64, VoxelConfigList（文件缺失的 sender 会直接 return）。

另外已逐字节核对：仓库嵌入的 `Metadata_ModeList.bin`（790 B）= `02 + U16(787) + metaData2[787]`，`Metadata_Encyclopedia.bin`（697 B）= `metaData_Encyclopedia[697]`，两者取自 `12644f2:src/packet/packet_metadata_data.h`，比对完全一致，所以这两个文件本身就是可直接下发的完整 payload。

## 5. 当前 Go 实现状态（已提交）

- 默认帧 = `U8(id) + U16(size) + data`；`MetadataZipTypeByte=true` 才切到 `3aac2d2` 的帧与分块（用于适配更新的私服构建）。旧帧下单帧 > `U16` 上限的条目跳过并 `Warn`。
- ItemBox 用基线 5 字节头；ModeList/Encyclopedia 恢复下发（原样文件）。
- 实机验收通过：全部元数据下发、客户端 `MetadataDump` 全收、60 秒存活。`go build`、`go vet`、`go test ./...` 全绿。
- 提交：`94aaed2` 端口顺序修复、`48300f6` 帧修复、`7904b7c` 文档、`915782c` 恢复 12644f2 形状。

## 6. 想请 Astra 分析/确认的问题

1. **12644f2 → 3aac2d2 之间元数据协议的完整变更清单**：除"帧头多 `U8(5)` + 分块"和"ItemBox 多 `U8(0)`"，其余 `SendMetadata*`（WeaponPaints/ZombieWar/RandomWeapon/ModeEvent/…）是否还有形状变化？建议按函数逐个 diff。
2. **客户端到底怎么解析 ItemBox**：实测 3 种写法都能存活（`U8+U32(count)`、`U8+U16(len)+U8(0)+U32(count)`、空表），只有 `U8+U8(0)+U32(count)`（145 条）会延迟崩。是否有办法确认客户端真正的读取顺序（以及为什么是延迟崩：约 40–55 秒后）。
3. **下发顺序是否有强约束**：Go 目前把 ItemBox/WeaponPaints/ZombieWar/RandomWeapon 追加在表尾，基线插在中间；客户端实测不崩，但语义是否正确未知。
4. **`Item`（type 32）的大包问题**：基线 `msg->WriteUInt16(m_pItemZip->GetBufSize())`，而 `Item.csv` 约 437 KB（zip 后仍可能 > 65535）；12644f2 没有分块，`U16` 会截断/回绕。基线的 `Item` 元数据默认关闭（`"Item": false`），是否意味着"该类型在旧协议下根本发不出去"？新客户端是否有官方分块方案？
5. **语义验收方法**：目前只验证了"不闪退 + 包被接收并 dump"。如何低成本确认客户端**真的用上了**服务端下发的模式/地图/道具表（例如临时改一个地图显示名，看大厅是否变化）？
6. `WeaponAscend(56)`、`Unk57(57)`、`PerkParam(58)`、`Synthesis(63)`、`Unk64(64)`、`VoxelConfigList(65)` 在基线里会发（文件存在时），Go 未实现、默认数据也没有对应文件；这些是否需要补齐？

## 7. 可复用信息

- 服务端启动：`.\.dist\cso-server.exe -data ./.data -debug`（TCP/UDP 30002）。
- 客户端启动：`S:\.temp\CSNZ\Bin\CSOLauncher.exe -ip 10.8.1.100 -port 30002 -login tester1 -password secret1 -window -novid -dumpmetadata`；收到包会落在 `S:\.temp\CSNZ\Bin\MetadataDump\`。
- 自动化判据脚本思路：起服务端 → 起客户端 → 轮询 `MetadataDump` 计数 → 等待 → 查 `Get-Process CSOLauncher` 是否存活。
- 崩溃信息：`S:\.temp\CSNZ\Bin\Dump\baseinfo.json`（`EXCEPTION_CODE`、`PLAY_TIME`）、`CrashReportLog.txt`。
- 参考数据/配置：`.data\Data\*`（上游 12644f2 的表与 bin）、`.data\ServerConfig.json`、`.ref\CSNZ_Server`（注意：需要 `git checkout 12644f2` 才是基线，别用 HEAD）。