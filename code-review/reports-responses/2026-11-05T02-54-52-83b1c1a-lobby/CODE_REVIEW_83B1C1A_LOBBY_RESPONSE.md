# CODE_REVIEW_83B1C1A_LOBBY 响应

审查报告：`CODE_REVIEW_83B1C1A_LOBBY.md`（同日，HEAD 83b1c1a，探针 `probes/review-83b1c1a-lobby/`）。
本轮修复提交：`2a15e61`（登录链）、`05f5f2b`（房间出站与家族战）、`483989b`（存储/结算）。

## 一、7 条待办

报告确认 6 条在 83b1c1a 上是真偏差、第 7 条（103 的位置）也已修。这与我在同一时间做的实机对照结论一致：我先按 30003 的实测字节改了登录链（`2a15e61`），审查者在纯净 83b1c1a worktree 上跑 14 条探针得到 7 通过/7 失败，与我的修复范围吻合。

一并接受报告的方向更正：**74 = PacketId::Ban、76 = Option，房间列表是 151**。之前的待办表把 74/76 写成房间列表相关，是按字节推断错了对象，语义没变（Ban 在 Option 之后）。

现状：`login_payloads.py` 对 30003 与我们的实例比较 74/76/150/153/157/180/103 **逐字节相同**；登录 60 包只剩 19 条元数据帧的压缩方式与 1 条商店推荐标题（已批准偏离）。

## 二、8 条新发现

| # | 结论 | 修法 |
|---|---|---|
| N1 (P1) 65/0 只带子集 flags/字段 | 采纳 | 新增 `loadDefaultSettings` 逐字段镜像 `CRoomSettings::LoadDefaultSettings`（`roomsettings.cpp:1210`）：四个 flag 字置 `ROOM_*_ALL`、mapId≠254 清 `ROOM_LOWMID_VOXEL`、每个字段写零值默认。65/0 从 504B 变为 636B、设置区 60B → 192B，字段表 96 项逐项与基线相同。更新路径仍只发请求改动的字段。 |
| N5 (P2) 回滚包是空 mask | 采纳 | `revertRoomSettings` 改为 `U8(4) + settings.write()`，与 `WriteSettings` 在 mask 全 0 时回退到房间 flags 等价（193B）。 |
| N2 (P1) 151/65-34 条目只写 6 个字段 | 采纳 | `writeRoomListEntry` 按 `BuildRoomInfo(..., RLFLAG_ALL, RLHFLAG_ALL)` 重写：条目 19B → 85B、151 从 51B → 117B，含 STATUSSYMBOL、PLAYERS、等级/武器限制、PLAYLIST、SD/难度、ZBLIMIT、家族战 clanID 等，map 254 的 Studio 段一实现。 |
| N3 (P2) 建房缺 65/5 与两条 65/4，多一条 65/3 | 采纳 | 建房路径改为 `65/5` → `65/4 superRoom` → `65/4 c4Timer`（模式 3/4/5/15/24 再加 `65/4 sd`）→ `65/0`；建房不再发 65/3。 |
| N4 (P2) 进房缺 65/13 | 采纳 | 广播 65/1 之后对除进房者外的成员发 `U8(13)+U32(userID)+U8(team)`。 |
| N6 (P2) 153 顺序来自 map 遍历 | 采纳 | 新增 `client.lobbySeq`/`Server.lobbySeq`，153 的 count 与条目都按入频道顺序输出。重连后顺序与基线一致（`[1,2]` → `[2,1]`）。 |
| N7 (P2) 打洞前网络信息为 0 | 采纳 | 登录首行 `c.initNetwork()`：IP 取 TCP 对端、端口 27015/27005，UDP 打洞后覆盖。 |
| N8 (P2) 空 URL 不发 103 | 采纳 | 去掉提前 return，两个空串也发 `103`（`U8(20)+""+""`）。 |

## 三、验收

- 30003 与我们的实例，同一份数据、同一账号、同一个建房请求，四组场景（默认 mode0/map0、mode3、mode15、playlist+zbLimit）**逐字节一致**，只掩掉房间 id 与成员 IP（基线在容器里看到的是网关地址）。
- 建房回包 `[('05',6),('04',18),('04',18),('00',636)]`；65/0 flags `ffffffff/fffbffff/ffffffff/ffffffff`、192B、members offset 205；151 len 117 / 字段 85B；进房 guest `[('00',1066),('03',6),('03',6)]`、host `[('01',431),('0d',6)]`。
- 提交后重跑：**14/14 探针通过**，`go test ./...` 与 `go vet` 全绿，62 例黑盒套件全绿，`blackbox/test_login_order.py` 在 30002 与 30003 上都通过。

## 四、未覆盖 / 保留

1. **没有真实客户端**。判据是基线源码加 30003 的实际字节，不能证明客户端渲染成什么样；按字段语义推断的显示差异没有实机确认。
2. **map 254（Studio）的房间列表 Voxel 段**按 `BuildRoomInfo` 实现，但两侧都无法在没有 Voxel HTTP 服务时建 Studio 房，未做线上对照。
3. **`ValidateSettings` 分支**：仓库配置是 `false`（基线同配置也是 `false`）。为让 ALL flags 不把“未请求字段”当成请求，更新路径的校验/清理改成只针对“请求 + 服务端派生”的 mask；这条分支没有线上对照，已在 `docs/MIGRATION.md` 记录。
4. 报告 §七 提到的“带 playlist/zbLimit/家族战的 151 条目未逐一实测”，本轮补测了 playlist+zbLimit 房（一致）；家族战房间条目的 clanID 段仍只有源码对照。
