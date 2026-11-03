# a0bb9a6 覆盖记录与开放问题答复

本轮按 `.tmp/review-request.md` 进行跨模块静态审查与定向复现。具体发现见 `CODE_REVIEW_A0BB9A6.md`。这不是 25k 行代码已经完成逐函数等价证明的声明，不能据此认证“100% 复刻”。

## 优先范围结论

| 范围 | 结果及边界 |
|---|---|
| 道具路由/槽位 | 已核对当前 switch、装饰优先分支及主要游戏槽位转换。发现强化材料类别假设不成立；未完成所有 kind 的逐字节金标准。请求中“78 种 kind”应区分外层 Packet ID 78 与其子类型数量。 |
| count | 基线 UseItem 会把 itemCount 作为 additionalArg 交给部分消耗/奖励路径，并非所有分支都只用 1；不能据请求中的概括直接判 Go 批量奖励有误。 |
| 房间 128 bit | 对照构造函数、WriteSettings 的外层宽度，新增 128 个单 bit 空值矩阵全部通过；确认 Voxel 内层保留位误消费。difficulty 保持客户端例外。复杂非空数组尚未全部穷举。 |
| 对局/结算 | 检查 startGame、hostRequest、endGame、迁移及基线对应路径。LeagueGauge 发给发起者与基线一致。确认职业加成快照偏差；没有重做全部模式结果包金标准。 |
| 库存级联 | 公仓新级联已接入；强化材料仍只接选择接替，未接引用清理，判断表不能标为全覆盖。其他特定材料入口仍需结合服务器实际类别校验，不能只凭 UI 推定。 |
| 到期/登录 | 上轮删除墓碑、先 UserStart 后背包的探针通过；新确认 skip 导致登录缓存未重读。全局 s.mu 使已检查的登录/定时扫描调用互斥，不把二者简单视为同时提交的竞态。 |
| 元数据 | 默认帧预算 `65536-5-3=65528` 正确；zipType 分块为显式配置例外，不应在本客户端默认开启。Hash 是否被当前客户端接受仍缺端测，不作保证。metadataOrder 与基线 SendMetadata 顺序核对未发现新顺序偏差。 |
| 重连/断线 | users 删除前验证指针归属，旧连接 defer 不会直接删除已接管的新会话。恢复 stat.items 会保留旧快照，不能据此保证过期职业加成正确。未完成完整并发状态机证明。 |
| ValidateSettings | 基线 CheckSettings 的主要校验在 validateSettings 条件内，LoadNewSettings 的大量清理同样受此条件控制。因此不能仅凭 Go 顶层短路断言开关放错；未完成全部模式 sanitize 值域的逐项证明。 |
| cmd/配置/文档 | 抽查启动地址、同端口 TCP/UDP、工具链约定、基线哈希及配置加成范围；未重统计所有能力/数据行数，不能确认请求中“191”仍是当前完整测试数量。风格遗留不作为阻塞发现。 |

## 单连接事务检查

扫描 internal/store 非测试文件中含 `s.DB.Begin()` 的 Store 方法，其直接 Store 方法调用清单如下（接收者为 s，不把 errors.Is、slices.Contains 混入）：

| 事务方法 | 再调用的 Store 方法 |
|---|---|
| JoinClan | joinClanTx |
| ApproveClanJoin | clanMembership、joinClanTx |
| LeaveClan、InviteToClan、SetClanGrade、KickClanMember、SetClanMark、DissolveClan、DelegateClanMaster | clanMembership |

`clanMembership(tx,...)`、`joinClanTx(tx,...)` 都使用传入的 tx，不再 Begin，也不从 s.DB 申请连接。这组直接调用未发现单连接自等待阳性。扫描结果保存在 `.dist/review-a0bb9a6/transaction-calls.txt`。

这是文本候选扫描加人工检查这两个 helper，不是完整 AST/动态分发/全仓调用图证明；事务外持有未关闭 Rows 再查库的路径也不能仅由这张表排除。不能把“这组未发现”写成“死锁已完全证明不存在”。

## 数值与已知差异

- PlayerCoop：基线虽有计算循环，但全 src 搜索 bonusPlayerCoop 仅找到定义和读取，没有找到填充配置的代码；当前基线正常加载时是空表。因此不能把缺少 Go 配置入口当成已经观察到的默认加成偏差。未来若启用它，应作为显式功能扩展。
- 结果包 classBonusPoints 笔误：基线 packetmanager.cpp:2903、2911 两处都写 ClassBonusPoints，Go 在经验区写 ClassExp。当前要求严格复刻且该处不在客户端已验证例外中，应照基线或明确记录获准偏离；这是请求已经指出的开放问题，不重复充作“新发现”。
- 截断设置拒绝并回 notice 比应用半包可靠；属于已决定的健壮性偏离，应保留记录。尾部只记日志与此不矛盾。

## 七个开放问题

1. **等级表**：保持基线服务器等级表。结果页的 9000 不足以证明完整阈值；如另做客户端显示适配，应独立配置并有全表证据。
2. **difficulty**：当前首字节保留实际值，另 8 字节为输出 padding。建议拆为值与编码职责；当前只针对指定客户端，无需新增未要求的版本兼容分支。
3. **发送队列**：在全局锁内改成无期限阻塞会扩大慢客户端影响；不建议直接这样改。保留有界队列和明确断连策略，并记录 user、packet ID、queue len/cap、room；除非协议证明可省略，否则不要随意丢关键包。当前没有证明正常流量必然超过 512。
4. **发布数据**：发布一个有明确来源、版本与文件校验的客户端适配数据集；纯基线数据保留为测试/对照 profile。不要把两套工作目录无说明地合并为默认数据。
5. **新增幂等表**：既已接受，维持独立 Go 表与事务边界；本轮不再争论是否允许建表，也不因此保证全部幂等路径正确。
6. **装备加成开关**：保留显式开关、默认 false 可接受；它是有文档的扩展，严格基线验证必须保持关闭。彻底删除不是复刻所必需。
7. **到期多发库存**：154 是按槽位更新，多发正确的剩余条目不会改变字段布局。应优先发送墓碑与真正变化的接替副本以减少流量；额外条目能否触发客户端额外重建未端测，不应说已验证无影响。

## 验证与未完成项

使用 `GOTOOLCHAIN=local` 完成现有 build/vet/完整测试；新增探针已 gofmt。未做客户端端测、跨 NAT 联机、SSL、HLDS 实机验证、FuzzItemRequest/FuzzMetadata 或全模式结果包金标准。未改业务代码、现有测试和运行数据。
