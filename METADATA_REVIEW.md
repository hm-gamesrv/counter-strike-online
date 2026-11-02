# 元数据审查结果

日期：2026-11-03。审查工程提交 `00c4459`，重点为 `48300f6`、`915782c` 之后的实现。审查范围限于所附请求的六项问题，不是全仓库代码审查。

**快照说明**：收尾时检测到其他工作正在把参考基线改为 `e5721ed9bbd6bd16abdd7133f412316bc14d3cf0`，并同步默认数据、修改缺失文件处理。本报告的两个边界复现、zip 字节数和逐函数比较对应改动前快照及明确指定的 `12644f2`/`3aac2d2` Git 对象，不是新默认数据的验收结果。并发改动未被覆盖，也不在本次审查结论内。

已执行 `pwsh scripts/check-ref.ps1`，确认 `.ref/CSNZ_Server` 为 `12644f2adc0708e2d8b74f7dd6a4c3c29d3e1140`。后版通过 `git show 3aac2d2:<path>` 读取，没有切换或改写参考工作树。本轮未操作游戏、未重启服务器、未修改业务代码。附件中的实机实验作为用户提供的证据；本轮独立验证的是源码差异、数据大小和 Go 边界行为。

## 1. 需要处理的代码问题

### [P2] 基线单帧判断少算了 3 字节元数据头

位置：`internal/server/metadata.go:46`、`:143`。

`protocol.MaxPayload=65531` 是整个元数据 payload 的上限，已经包含 `U8(type)+U16(zipLen)`。当前判断只检查 zip 长度不超过 65531，因而错误接受 65529–65531 字节的压缩流；随后 `loadMetadata` 检查报错，服务器初始化失败，而不是按既定策略跳过超限条目并警告。

复现：压缩流长度 65528 时生成 payload 65531，Codec 接受；长度 65529 时生成 payload 65532，Codec 返回 `frame too large`。所以当前 Go 传输层下，正确的 zip 上限是 **65528**，不是 65535 或 65531。建议改用 `protocol.MaxPayload-3` 并覆盖 65528/65529 两个边界。

这不是已知当前默认数据闪退的原因，但更新运行数据后能触发启动回归。

### [P2] 四块以上的 zip 被静默截断，并标记为完成

位置：`internal/server/metadata.go:55–72`；`internal/server/metadata_test.go:65–67`。

`MetadataZipTypeByte=true` 时，256001 字节输入只产生四块，共 256000 字节，第 4 块仍标记为最后一块。缺失 zip 尾部的流被作为完整流发送。现有测试反而要求截断后恰好四块，固化了错误行为。

这是后版参考实现也存在的限制，不能因为与其一致就认为数据完整。建议在分块前拒绝超过 `64000*4` 的输入，返回明确错误或显式跳过；测试应验证不发送截断流。只以当前客户端为目标时，也可直接移除这条不适用的后版协议分支，避免维护并未验收的兼容选项。

默认关闭此选项时不会触发本问题。不能把它描述为默认路径的闪退原因。

## 2. 两个版本的完整 sender 对照

基线共 39 个 `SendMetadata*` 函数，后版 44 个：删除 3 个、新增 8 个。对两个 `packetmanager.cpp` 按函数提取并比较全部写入语句，结果见文末逐函数表。

- 共有的 23 个 zip sender：新增 `U8(5)`；超过 64000 字节改走分块，块编号为 1/2/3，末块标记为 4。除此之外，类型和原有写入字段不变。`ProgressUnlock` 另删了一条注释，没有额外格式变化。
- `ItemBox(4)`：仅 count 前增加 `U8(0)`；每条 26 字节记录不变。后版 sender **没有增加 U16 长度前缀**。当前 Go 注释中“later private build added both”不准确。
- `WeaponPaints(6)`、`ZombieWarWeaponList(15)`、`RandomWeaponList(16)`：只有 C++ 整数显式转换，字段宽度和顺序未变。
- 原样 blob 的 9 个 sender 完全不变：Unk3、Unk8、Unk20、ReinforceItemsExp、Unk31、Unk43、Unk49、Unk54、Unk55。
- 后版删除 ModeList(2)、Encyclopedia(23)、Hash sender。基线 Hash 实际只发送空 Metadata payload，数据写入被注释；不建议为了“完整”把这个空占位补回 Go。
- 后版新增 WeaponAscend(56)、Unk57(57)、PerkParam(58)、Synthesis(63)、Unk64(64)、VoxelConfigList(65)，以及附件漏提的 VoxelList(33)、VoxelItem(34)。

因此附件“基线会发送 56–65 那批类型”及其基线顺序尾部需要更正：它们不在 `12644f2` 的 sender 和调用序列中。VoxelConfigList 是嵌套 URL/IP/端口结构，不是读取一个同名文件后直接发送。它们不属于本次基线迁移遗漏，不应从后版随意补发。

这些结论仅比较 sender 的字段布局；不代表两个版本的表内容、枚举及客户端语义完全相同，也不代表 `MetadataZipTypeByte=true` 已完整模拟后版协议（例如 ItemBox 仍然采用基线布局）。

## 3. ItemBox 解析和延迟崩溃

能确认的是基线 sender 生成 `04 + U32(count) + 26*count`，并且附件报告该布局实机稳定。没有取得目标 `client.dll`/`hw.dll` 的接收函数反汇编与运行时栈，不能据此声称已经确认客户端实际每次读取的顺序，更不能确定 40–55 秒延迟的内部原因。

有一个可检验的解释：若按基线布局读取，145 条记录的错误前缀 `04 00 91 00 00 00 ...` 会把 count 读成 `0x9100=37120`，而不是 145。零条记录则仍读成 0，所以“空表稳定”不能证明这个额外字节合法。这只能解释错位风险，不能单独证明延迟崩溃的调用链。

带长度前缀的实验同样不能作为合法协议证据。若 body 为 `U8(0)+U32(145)+3770`，bodyLen=3775；按基线读取会得到另一个巨大 count `0x91000ebf`。它存活可能来自长度校验拒绝、hook 截获、忽略或其他分支，现有证据不能选定其中一种。dump 也可能发生在实际客户端解析前。

确认方法：先定位与现场二进制对应的 hook Metadata 分派，区分“dump 前”“交给原接收函数”“原函数返回”；再在隔离测试中记录 type 4 接收函数的读偏移、count、进入/返回状态和第一次 fatal exception 的原始栈。按同一开关单独发送 0/1/2 条有效记录，每次重启进程，检查实际功能使用。仅等待 60 秒或检查 dump 文件数不足以证明条目已生效。

补充：`U8(5)` 被旧布局误读时，读到的 U16 是“5 + 原 zipLen 低字节左移 8”，并不是固定读取 5 字节。例如 zipLen=787 时，`type 05 13 03 50 4b...` 会被读成长度 `0x1305`，zip 起点也错一字节。README/注释中“读成长度 5、解析五字节 zip”的解释需修正，尽管删掉类型字节的修复方向成立。

## 4. 顺序

Go `metadataFiles` 的“mirrors SendMetadata order”注释不成立：除了四种构造元数据最后追加，Item/CodisData/WeaponProp 以及 ZBCompetitive 等顺序也与基线不同。

从 master sender 看不到客户端依赖图或必须相邻的机制；附件实机结果仅说明当前数据和这次操作未暴露问题，不能证明所有消费路径顺序无关。因此建议直接按基线调度顺序组织统一条目，特殊类型在各自位置构造，并对全开配置的 type 序列做测试。成本低，也能减少排查变量，但目前不把顺序差异断言为已证实的闪退 bug。

基线调用序列（Hash 为可选空占位，可保持禁用）：

```text
0,1,2,3,4,6,8,9,15,17,18,20,23,24,27,28,29,30,31,
36,37,38,39,40,48,43,49,45,32,35,42,[Hash],16,50,51,52,53,54,55
```

## 5. Item(32) 和大包

使用当前 `classicZip("Item.csv", bytes)` 函数实测：

| 数据来源 | 原文件字节 | zip 字节 |
| --- | ---: | ---: |
| `assets/defaults/Data/Item.csv` | 437373 | 56245 |
| `.data/Data/Item.csv` | 437373 | 56245 |
| `.ref/CSNZ_Server/bin/Data/Item.csv` | 440301 | 56259 |

这三份都能容纳在基线 Go 单帧内。**原 CSV 大于 64 KiB，不等于 zip 大于 64 KiB**；`Item=false` 不能证明这个类型根本发不出去，也不能推导默认关闭的作者意图。仍需独立验收 Item 覆盖对客户端语义的影响。本轮没有读取到目标客户端根目录的 `Data/Item.csv`，不以服务器配套表代替客户端实际加载文件。

真正的超限：基线 `extendedsocket.cpp` 的 Send 在完整帧超过 `PACKET_MAX_SIZE=65536` 时直接拒发，并记录日志。故不能仅凭 `WriteUInt16` 就断言超大 Item 必然被送到客户端且发生 U16 回绕。当前 Go 的同一帧预算为 `5+3+zipLen<=65536`。

在所审查的基线中未发现官方分块/自动重组协商；`3aac2d2` 的分块实现只能证明后版服务端采用了另一协议，不能证明当前客户端支持，更不能称为“官方方案”。TCP 分段也不能解决应用层帧长限制。不应通过启用已知会让当前客户端崩溃的类型字节开关来解决大包。

## 6. 低成本语义验收

1. 独立复制运行数据和测试账号，关闭其他 Metadata，只启用待测类型；先留存原始数据与完整 dump。每个 A/B 用全新客户端进程，避免表缓存影响。
2. MapList 选择现存、可进入且菜单可见的地图，保持 ID/列数/编码/文件名不变，仅将 `map_name` 从本地化 key 改为 ASCII 标记 `META_REVIEW_A`。先确认解压后的接收内容含标记，再检查建房地图选择界面是否显示标记。原名是 `#CSO_*`，直接写非本地化文本是否受支持也需要验证；没有变化不能立即判定整表被忽略。
3. 若显示文字由其他资源控制，可用同一现有地图的 `map_sort` 做 A/B 或选择已知能影响列表的可见字段，但一次仅改一个值。不要改 ID、地图文件名或引入未安装地图来判断表是否生效。
4. GameModeList 同理测试既有模式的 `mode_select_ui_order`；不要改变服务端房间逻辑、计分规则或武器属性，否则无法区分 UI 元数据与业务代码效果。
5. ItemBox 在实现了对应预览/奖池展示路径的界面核对特定 `(box,item)` 是否出现；实际抽奖结果由服务器控制，不能拿它单独证明客户端用了元数据。
6. 必须同时具备：收到的内容正确、相关消费界面出现可逆变化、还原后变化消失。测试服关闭目标下发项也应做负对照，排除其他包和本地文件覆盖。存活检查保留，但只是附加判据。

## 7. 处理建议和验证范围

优先修正上述两个长度问题与错误测试，再按当前唯一客户端目标处理不适用的后版选项。随后统一基线顺序，做一项可逆语义 A/B。56–65、33/34 不应作为基线缺口补齐。

本轮复现脚本与逐函数原始 diff 保存在忽略目录 `.dist/metadata-review/`；它们从当前 Go 函数和指定 Git 对象读取，没有启动服务器或访问游戏。构建/检查统一设置 `GOTOOLCHAIN=local`。不把本轮静态审查和聚焦测试表述为新的实机验收。

元数据聚焦测试 `go test ./internal/server -run 'TestMetadata|TestLoadMetadata' -count=1` 通过（5.167 秒）；现有测试通过并不消除前述已复现的边界缺陷。
`go vet ./internal/server` 通过。本轮未修改 Go 生产代码或测试代码，未运行会改写现有源码的格式化操作。

## 附：逐函数清单

下表“类型字节/分块”指共有 23 个 zip sender 的统一变化；没有发现它们另有字段增删。`ProgressUnlock` 的额外差异只是删除注释。

| 函数 | 12644f2 → 3aac2d2 |
| --- | --- |
| `SendMetadataClientTable` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataCodisData` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataEncyclopedia` | 后版删除 |
| `SendMetadataEventShop` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataFamilyTotalWar` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataFamilyTotalWarMap` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataGameModeList` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataHash` | 后版删除 |
| `SendMetadataHonorMoneyShop` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataItem` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataItemBox` | count 前新增 U8(0)；26 字节条目相同 |
| `SendMetadataItemExpireTime` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataMaplist` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataMatchOption` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataMileageShop` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataModeEvent` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataModelist` | 后版删除 |
| `SendMetadataPPSystem` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataPerkParam` | 后版新增；基线不存在 |
| `SendMetadataProgressUnlock` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataRandomWeaponList` | 仅增加整数类型显式转换；字段宽度/顺序不变 |
| `SendMetadataReinforceItemsExp` | 完全相同 |
| `SendMetadataReinforceMaxEXP` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataReinforceMaxLvl` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataScenarioTX_Common` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataScenarioTX_Dedi` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataShopItemList_Dedi` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataSynthesis` | 后版新增；基线不存在 |
| `SendMetadataUnk20` | 完全相同 |
| `SendMetadataUnk3` | 完全相同 |
| `SendMetadataUnk31` | 完全相同 |
| `SendMetadataUnk43` | 完全相同 |
| `SendMetadataUnk49` | 完全相同 |
| `SendMetadataUnk54` | 完全相同 |
| `SendMetadataUnk55` | 完全相同 |
| `SendMetadataUnk57` | 后版新增；基线不存在 |
| `SendMetadataUnk64` | 后版新增；基线不存在 |
| `SendMetadataUnk8` | 完全相同 |
| `SendMetadataVoxelConfigList` | 后版新增；基线不存在 |
| `SendMetadataVoxelItem` | 后版新增；基线不存在 |
| `SendMetadataVoxelList` | 后版新增；基线不存在 |
| `SendMetadataWeaponAscend` | 后版新增；基线不存在 |
| `SendMetadataWeaponPaints` | 仅增加整数类型显式转换；字段宽度/顺序不变 |
| `SendMetadataWeaponParts` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataWeaponProp` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataZBCompetitive` | 新增类型字节/分块，其余写入相同 |
| `SendMetadataZombieWarWeaponList` | 仅增加整数类型显式转换；字段宽度/顺序不变 |
