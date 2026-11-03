# blackbox —— 黑盒行为测试

这里放**只通过 TCP 与服务器对话**的测试：不 import Go 包、不读数据库、不依赖内部函数名。判据是"服务器发出来的字节"，因此同一套脚本可以打我们自己的服务端，也可以打基线（WSL 里的 30003），用来对比行为。

## 目录

| 文件 | 作用 |
|---|---|
| `cso_proto.py` | `U` 帧编解码（`'U' + seq + u16(len) + id + payload`）、字段读写、`frame()` 造帧；明文路径，RC4 握手未实现（开发实例 `Crypt=false`） |
| `cso_client.py` | 一个最小客户端：连接、`version` 握手、`/login`、收包、按空闲超时收集；另有 `send_raw`/`send_frame`/`send_split`（畸形帧、边界帧、分片）与 `expect_closed` |
| `cso_harness.py` | 测试脚手架：环境变量、`BlackBoxTest` 基类（连接/登录/skip）、包解码器（150/153/154/157、metadata、房间列表、聊天）与"等某个包"的辅助函数 |
| `test_smoke.py` | 冒烟用例：greeting、版本握手、登录进大厅、登录包序、错误密码被拒 |
| `test_protocol.py` | 协议层健壮性：非法签名/序列号、非法长度、`MAX_PAYLOAD` 边界、跨写分片重组、序列号 255→0 回绕、半开帧隔离 |
| `test_commands.py` | 命令解析：`/login`、`/register` 的参数与校验错误、未登录聊天/房间请求、已登录时的重复 `/login` 与 `/register` |
| `test_lobby.py` | 登录后的协议流：150/157、153 大厅列表（自身条目与 user info 一致）、151 房间列表、152+154 背包、99 匹配标签、91 元数据条数与顺序、180 段位、77 收藏；两人在线时的加入/离开广播 |
| `test_chat.py` | 聊天：大厅广播（含发送者回显）、UTF-8 往返、1024 字节边界、私聊在线用户、私聊不存在用户的 67 通知 |
| `test_session.py` | 会话：错误密码/不存在账号的错误码文案、同账号二次登录踢掉旧 socket、断开后立刻重登 |
| `test_cross.py` | 跨服务器对照：同一登录脚本打两台，只比较稳定不变量（包族与条数、91 类型序列、登录阶段相对顺序、稳定 payload、背包/房间列表帧形状、加入广播、聊天帧） |
| `trace.py` | 把一次登录的包序列 dump 成可比对的文本，用于两台服务器逐包 diff |
| `metadata.py` | 把 91 帧逐个解压后比内容；zip 大小因压缩器不同而不同，内容才是有意义的判据 |
| `FINDINGS.md` | 与基线的行为不一致记录（审查素材）：半开帧阻塞基线、超长聊天、重复 `/login` 明文广播等 |

## 用法

```powershell
# 开发实例（.data-test，30002）——整套跑一遍
python -m unittest discover -s blackbox -p "test_*.py"

# 基线（WSL 容器，30003）——同一套用例，能跑的都跑，只在 Go 独有的用例上 skip
$env:CSO_PORT = "30003"; python -m unittest discover -s blackbox -p "test_*.py"
Remove-Item Env:CSO_PORT

# 单个文件
python blackbox\test_protocol.py
python blackbox\test_lobby.py

# 两台一起对照（必须打 30002，基线端口默认 30003）
python blackbox\test_cross.py

# 逐包对比两台服务器
python blackbox\trace.py --port 30002 --out .dist\trace-30002.txt
python blackbox\trace.py --port 30003 --out .dist\trace-30003.txt
python blackbox\metadata.py --a 30002 --b 30003
```

可用环境变量覆盖：

| 变量 | 默认 | 含义 |
|---|---|---|
| `CSO_HOST` | `127.0.0.1` | 服务器地址 |
| `CSO_PORT` | `30002` | 目标端口（自建实例） |
| `CSO_USER` / `CSO_PASSWORD` | `tester1` / `secret1` | 主测试账号 |
| `CSO_OTHER_USER` | `tester2` | 第二个账号（双人在线用例） |
| `CSO_BASELINE_PORT` | `30003` | `test_cross.py` 对照用的基线端口 |
| `CSO_REFERENCE` | 端口==`CSO_BASELINE_PORT` 时为 `1` | 目标是基线：Go 独有的用例 skip |
| `CSO_LOGIN_IDLE` | `0.7` | 登录收包的空闲超时（秒） |

`CSO_REFERENCE` 用于把"只在我们的实现上成立的行为"和"两边共有的行为"分开：前者在基线上 skip，并在 `FINDINGS.md` 里写明差异；直接改 `CSO_REFERENCE=0` 打 30003 只会让这些用例失败，没有意义。

## 哪些用例可以打基线，哪些只能打自建实例

**只读、可以随便打 30003（基线）**：greeting、版本握手、`/login`（含失败分支）、登录后的全部下发包（150/157/153/151/152/154/99/91/180/77）、大厅聊天、私聊、会话接管、`/register` 的**失败**分支（校验发生在写库之前，不产生账号）。这些用例在 30003 上全部通过。

**只能打 30002（自建、可丢弃）**：

| 用例 | 原因 |
|---|---|
| `test_protocol.py::FrameLengthTest` | 基线的 poll 循环对声明长度做阻塞读，非法长度/半开帧会把整台基线服务器卡住（见 `FINDINGS.md` 第一条），不适合拿来探测 |
| `test_chat.py::OverlongMessageTest` | 我们 >1024 字节断线，基线照常广播；断言只在我们的实现上成立 |
| `test_commands.py::GoCommandReplyTest` | 只属于我们的回复：重复 `/login` 回 reply 4、`/register` 回 "Already logged in."、未登录房间请求断线、重复 `/login` 不把密码广播进大厅 |
| `test_cross.py` | 需要两台同时在线 |

**不会**写基线的用例：本套件没有任何"注册成功、建房间、买东西、改角色数据"的用例。登录本身会写会话记录与最后登录时间（现有 `test_smoke.py` 早已如此），这是黑盒登录无法避免的。请仍然只把写操作指向 30002。

补充约定：

- `test_lobby.py::LoginChainTest.test_metadata_type_order` 的期望类型列表对应本仓库 `.data/ServerConfig.json` 里启用的那批元数据；改配置就要同步改该常量。
- `test_lobby.py::LoginChainTest.test_room_list_header_and_empty_lobby` 假定实例上没有开着的房间（房间只存在于内存，正常安静实例就是 0 条）；若有人开着房间，会以 "the test instance should not have rooms open" 失败而不是误报。
- 时间/随机/并发调度都不作为判据：等待一律用"收到某个包或超时"，不断言绝对耗时。

## 与 Go 测试的分工

- `go test ./...`（白盒）：函数级、事务级、逐字段编码，能直接构造夹具。
- `blackbox/`（黑盒）：真实 socket、真实登录流程、真实包序。它唯一能证明的是"外部可见行为"，也正好是审查报告里最容易出现争议的那一层。

黑盒用例默认只读：登录会写会话/最后登录时间，所以**只打可丢弃的数据目录**（`.data-test`）或基线实例，不要打 `.data`。

## 当前已知差异（2026-11-03，30002 Go vs 30003 基线）

两台都通过整套黑盒用例（基线上 10 个用例 skip：4 个长度校验、1 个超长聊天、4 个 Go 独有命令回复、1 个 `test_cross` 整类，见上表）。逐包对比的稳定差异：

| 差异 | 说明 |
|---|---|
| `157` 长度 303 vs 398 | 我们的 `userInfo` 只置位并序列化实际写入的可选段（代码注释已说明），基线把所有 flag 置满。属既有、有意的取舍，需要时再逐字段核对 |
| `74`/`76` 顺序 | 我们是 `76` 后 `74`，基线是先 `74`（`0302`）后 `76` |
| `103` 位置 | 基线紧跟在 91 元数据之后，我们排在登录链末尾（`69` 附近） |
| `151` 次数 | 我们发两条，基线一条 |
| `180` 位置 | 我们早于 `153`/`151`，基线在它们之后 |
| `91` 各段大小 | **不是数据差异**：数据目录逐文件一致。zip 大小不同只因压缩器不同（Go flate vs 基线 zlib），`metadata.py` 解压后 32 个类型内容全部相同 |
| `67` 通知条数 | 基线多两条欢迎/公告，来自它自己的 `ServerConfig.json` |

前五条属于**包序/字段数**，是真正需要对照基线源码确认的部分；metadata 与通知条数是数据/配置差异，换同一份数据就会消失。

不在上表、需要单独看的行为不一致（协议拒绝策略、超长聊天、重复 `/login` 的明文广播、私聊通知 shape、命令文案等）都记在 [`FINDINGS.md`](FINDINGS.md)。
