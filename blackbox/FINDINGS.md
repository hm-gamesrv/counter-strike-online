# 黑盒对照发现（30002 Go vs 30003 基线）

这里只记**行为差异**，不记测试自身的问题。每条都能用下面的命令复跑：

```powershell
python -m unittest discover -s blackbox -p "test_*.py"                       # 打 30002
$env:CSO_PORT = "30003"; python -m unittest discover -s blackbox -p "test_*.py"  # 打基线
python blackbox\test_cross.py                                               # 两台一起对照
```

README 里已列出的差异（metadata 压缩字节、`157` flags、`74`/`76` 顺序、`103` 位置、`151` 条数、`180` 位置、`153` label、`72` 标题、`67` 欢迎语条数）不重复。下面都是这次新发现的。

## 半开帧会让整台基线服务器停止服务

- 用例：`blackbox/test_protocol.py::FrameLengthTest.test_half_open_frame_does_not_block_other_clients`（Go 独有：在基线上跑就是复现事故，所以 skip）
- 30002（Go）：一个只发了 11 / 64 字节 body 的帧只让**该连接**等待；期间新建连接完成 greeting 与登录（用例断言 `150` 到达）。
- 30003（基线）：`CExtendedSocket::Read()` 在单线程 poll 循环里按声明长度做阻塞 `recv`（`.ref/CSNZ_Server/src/net/extendedsocket.cpp:255-268`，循环在 `.ref/CSNZ_Server/src/net/tcpserver.cpp:176-275`）。实测：连上后只发 4 字节头、声明 65533 字节，另一台客户端 4 秒内连 greeting 都拿不到（`accept` 也被这一帧挡住）；关掉那个 socket 后立刻恢复。
- 判断：新发现，非已知差异。属于基线缺陷（单线程 + 内联阻塞读），我们不需要对齐。副作用提醒：**不要把"半开帧/撒谎长度"探测指向 30003**，会短暂打断它。

## 非法帧长度：我们立即断开，基线不校验

- 用例：`blackbox/test_protocol.py::FrameLengthTest.test_zero_length_is_refused`、`test_length_past_the_frame_limit_is_refused`、`test_payload_one_byte_over_max_payload_is_refused`（均 Go 独有）
- 30002（Go）：读完 4 字节头后判定 `length < 1` 或 `length + 4 > 65536` 就断开（`internal/protocol/packet.go:46-48`）。声明长度 0、65533、65535，以及 payload = `MAX_PAYLOAD + 1` 的帧全部断开。
- 30003（基线）：没有长度上下界校验。`length = 0` 被当成"空 payload 的包 0"，回 `55 01 02 00 00 00`（version ack）；`length = 65533` 则一直阻塞等数据（见上一条）。
- 判断：新发现，非已知差异。我们更严格，无需对齐；但要注意两边的"帧上限"契约其实不同（我们 65536 总长 / payload ≤ 65531，基线能把 `length` 读到 65535）。

## 超长大厅聊天：我们断线，基线照常广播

- 用例：`blackbox/test_chat.py::OverlongMessageTest.test_message_above_1024_bytes_drops_the_connection`（Go 独有）；边界由共享用例 `ChannelChatTest.test_message_at_1024_bytes_is_accepted` 钉住
- 30002（Go）：聊天文本 > 1024 字节 → dispatch 返回错误 → 断开连接（`internal/server/lobby.go:167-169`）。恰好 1024 字节正常广播。
- 30003（基线）：没有长度上限，1025 字节消息照常广播（对端收到 1035 字节的 `67`）。
- 判断：新发现，非已知差异。行为差异 + 易用性风险：客户端（或插件/宏）发一条长消息就会被踢下线，而基线不会。若要更稳妥，建议截断或静默丢弃而不是断开；至少值得在代码注释里写明这是有意的防护。

## 已登录时重复 /login：基线把明文密码广播进大厅

- 用例：`blackbox/test_commands.py::GoCommandReplyTest.test_second_login_replies_with_code_4`、`GoCommandReplyTest.test_second_login_does_not_leak_the_password_to_the_lobby`（Go 独有）；共享侧只断言"不产生第二个会话"（`LoggedInCommandTest.test_second_login_on_the_same_connection_is_refused`）
- 30002（Go）：已认证时再发 `/login ...` 只回 `packet 1`，`payload[0] = 4`（`internal/server/lobby.go:230-234`）；大厅里其他用户收不到任何东西（用例断言监听方收到 0 条聊天）。
- 30003（基线）：`OnCommandHandler` 只在 `user == NULL` 时处理 `/login`（`.ref/CSNZ_Server/src/manager/channelmanager.cpp:372-446`），已登录时该行落到普通大厅聊天（同文件 248-257 的广播）。实测另一个登录用户收到 `67 kind 1 "Tester1" "/login tester1 secret1"`——**密码明文进大厅**。
- 判断：新发现，安全相关。我们的行为正确，基线这是缺陷；共享用例只断言"没有第二个会话"，所以两边都过，泄密断言保留为 Go 独有。

## 未登录时的聊天与房间请求

- 用例：`blackbox/test_commands.py::UnauthenticatedTest.test_chat_before_login_is_not_broadcast`（共享）、`GoCommandReplyTest.test_room_request_before_login_drops_the_connection`（Go 独有）
- 30002（Go）：未登录发聊天 → 回 `67 kind 11` "Enter /login <username> <password> or /register <username> <password>."；未登录发包 `65`（房间请求）→ 断开（"authentication and character required"，`internal/server/server.go:464-467`）。
- 30003（基线）：未登录发聊天 → 完全没有回应；未登录发包 `65` → 静默忽略并保持连接。
- 判断：新发现，非已知差异。两边都不会把未登录消息送进大厅（这正是共享用例的判据），差异只在"回不回提示 / 断不断连接"。断开是更严格的选择；若某个客户端在握手后会先发 `65`，我们会把它踢掉而基线不会。

## 私聊不存在用户的通知 shape 不同

- 用例：`blackbox/test_chat.py::WhisperTest.test_whisper_to_an_unknown_user_reports_not_found`
- 30002（Go）：`67 kind 10`（notice）+ `MSG_TELL_USER_NOT_FOUND\0`（`internal/server/lobby.go:309-311`）。
- 30003（基线）：`67 kind 20`（system reply red）+ `MSG_TELL_USER_NOT_FOUND\0` + `U8(1)` + 目标名（`.ref/CSNZ_Server/src/manager/channelmanager.cpp:273-278`）。
- 判断：新发现。文本一致、kind 与尾部不同。共享用例只断言"有 `67` 且含该文本"，两边都过；若客户端按 kind 分派，我们的 kind 10 会显示成普通提示，基线的 kind 20 是"红色系统回复"。

## 命令文案与校验顺序

- 用例：`blackbox/test_commands.py::CommandUsageTest.test_login_without_arguments_asks_for_them`、`test_register_rejects_a_short_username`、`test_register_rejects_a_numeric_only_password`（共享，只断言公共子串）
- 30002（Go）：`/login <username> <password>`；"Username must be 5..15 English letters or digits."；"Password must be 5..15 characters and not only numbers."；先校验用户名、后校验密码（`internal/store/store.go:141-147`）。
- 30003（基线）：`/login arguments: <username> <password>`；"Username must contain at least 5 characters and not more than 15, English letters."；"Password must contain at least 5 characters and not more than 15, not only numbers"（无句号）；先密码后用户名（`.ref/CSNZ_Server/src/manager/usermanager.cpp:1024-1029`），所以两个字段都非法时，`/register ab 12345` 在基线报密码错、在我们这里报用户名错。
- 判断：新发现，语义一致、文案与顺序不同。用例故意只断言 `<username>`/`<password>`、首单词与 "5"/"15" 这类公共部分，避免把文案当协议。若要和客户端/基线的提示逐字一致，照抄基线文案即可。

## 私聊自己与大小写查找（未固化为用例）

- 用例：无。刻意不写成用例，避免把可疑行为写进规格；探测方法见 `test_chat.py::WhisperTest` 的同类构造。
- 30002（Go）：私聊自己 → 给发送者回两份（direction 0 与 1）；目标名查找大小写不敏感（`tester2` 能找到 `Tester2`，`internal/server/lobby.go:302-307` 用 `strings.EqualFold`）。
- 30003（基线）：私聊自己 → 只回 `67 kind 11` "You can't send whisper to yourself."；查找大小写敏感（`tester2` 找不到 `Tester2`）。
- 判断：新发现。大小写不敏感看起来是刻意的、更宽容，可以保留；"私聊自己回两份"更像副作用（客户端可能显示两遍），建议改成一份或按基线拒绝。两条都不影响现有用例。

## 备注：两侧一致的部分

这次顺带确认了一批"应当一致且确实一致"的点，写在这里便于以后回归时区分"新问题"和"老一致"：

- `150`、`152`、`154`、`77`、`99`、`74`、`76`、`69`、`159`、`180`、`103`、`0`、`1` 的 payload 逐字节相同；
- `91` 的 32 个类型顺序完全一致，解压内容一致（`metadata.py` 复核：differing 0）；
- 大厅聊天与私聊的 `67` 帧逐字节相同；`153` 的加入/离开事件类型与 user info 块一致；
- 会话接管（同账号二次登录踢旧 socket）、错误密码/不存在账号的文案、`sequence` 255→0 回绕、跨 TCP 写分片重组、`MAX_PAYLOAD` 帧被接受，两边一致。
