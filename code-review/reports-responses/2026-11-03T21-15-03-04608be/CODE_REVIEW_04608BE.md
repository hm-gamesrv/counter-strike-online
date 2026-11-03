# 04608be 审查报告

源码 HEAD：`04608be`；上轮业务修复为 `8827f33`。审查依据：`2026-11-03T20-31-27-4308aae/CODE_REVIEW_4308AAE_RESPONSE.md`。参考树已通过 check-ref 确认为 `4859a6e`。

本轮确认 **4 项 P1**：1 项是 host 修复分支仍未等价，3 项是整体检查发现（含归档改名引入的构建检查回归）。未修改业务代码、已有测试或历史报告；未操作游戏客户端，未重跑两服黑盒。不把旧轮网络结果当作本轮二进制验证。

## 一、对上轮修复回复的再审查

原 `review-4308aae` 三条探针全部通过：选中武器不会被 kind100 删除，大厅最坏角色字段的旧复现不再超限，公仓补丁包含接替副本。N2/N3 可按原复现场景关闭；N1 的拒绝分支已修，但激活分支仍有下面的等价缺口。

### R1 [P1，偏离基线] host 激活分支对 inUse=1/status=0 的物品直接无操作返回

- 位置：`internal/server/host.go:527`；关联 `internal/store/inventory.go:64–65`。
- `useInGameItem` 通过 CanUseItem 后调用 ChangeItem(ActivateItem)。后者只要 InUse 非零就直接返回，不再把 Status 置1。这是 SwitchInUse 的幂等约束，不能直接代替 UseItem 的用途分派。
- 基线：`itemmanager.cpp:1402–1442` 的 UseItem 默认分支，在 category<9 或 category==11 时写 `status=1`，不因为 inUse 已为1而提前返回。
- **复现**：正常发放一个 category8、count=2 的 Unselected 物品，数据库自然得到 `inUse=1,status=0`；发送真正的 host kind100 请求。Go 回复成功但状态仍为0；基线该分支应置1，数量不变。
- 当前新增测试只覆盖了 `status=0,inUse=0` 的激活场景，所以原探针全绿仍不能证明这个适配函数等价。
- 建议：为 host UseItem 分支实现其实际状态迁移，不复用具有不同前置条件的 ActivateItem；保留 selected/exhausted 的拒绝，并覆盖永久/限时、已激活未选中及同款副本情况。不要为了此入口改坏 SwitchInUse 的原有幂等语义。
- 探针：`TestReview046HostUseSelectsActivatedUnselectedItem`，结果 `status=0,inUse=1`。

### 对回复其他说明的核对

- HasActiveItem、元数据保序等上轮已关闭项，本次没有发现由该补丁引入的新回归。
- 旧黑盒差异的方向更正保留；但“153 长度差来自 label 常量”的说明不完整：上一轮实测差为98字节，其中 `tester1` 与 `test_lobby` 差3字节，157/full-user-info策略差95字节。不能只改 label 就期待314B变成412B。本轮没有把已知未处理差异再重复计数。
- 回复中全绿检查对应早先业务提交；不能沿用为本轮 `04608be` 根目录 `./...` 的验证结论，见 N1。

## 二、新的整体性审查

### N1 [P1，构建检查回归] code-review 归档进入根模块包发现，标准检查失败

- 位置：`code-review/probes/`；引入提交 `04608be` 将原 `.code-review/` 改为非隐藏目录并纳入仓库。
- `go build ./...`、`go vet ./...`、`go test ./...` 现在都会扫描归档探针。它们是用于 overlay 注入目标包的源码，不是可独立编译的包。
- 已跟踪的 `review-4308aae`、`review-154b2fe` 等目录同时含 package server 和 package store，产生 `found packages server ... and store ...`；即使某目录只有一个包，也会因 `newLoginExtraServer`、`client` 等目标包私有符号不存在而编译失败。
- **复现不依赖本轮新增探针**：上述出错的历史文件已存在于 HEAD，git ls-files 可确认。根目录三项检查本轮实跑均失败；实际项目包单独 build/vet/test 则通过。
- 建议：保留用户指定的 code-review 归档布局，但隔离探针与根模块包发现，例如在 probes 下建立独立模块边界，或将源码放到 Go 不递归扫描的 testdata 子树并同步 overlay。不要把归档探针注册为普通项目测试；仅加 build-ignore 标签也需考虑 overlay 后会一起被忽略。
- 失败证据来自标准构建命令，无需额外 Go 探针。

### N2 [P1，状态/回包不一致] 房间设置广播仍使用校正前的客户端字段

- 位置：`internal/server/rooms.go:656–660`。
- 更新路径构造 merged，执行 refreshHostRoomItems、validateRoomSettings 等校正，再把 merged 保存为 currentRoom.settings；但发出的65/4仍是原始 `st.write(w)`。
- **复现**：无8357道具的房主更新 superRoom=1。服务端有效设置正确改为0，紧接着广播却带superRoom=1。探针解析真实排队包，结果为 `broadcast=1,stored=0`。
- 影响不只“客户端多显示一个按钮”：同一路径的模式默认值、字段夹紧及道具关联字段均可能只在服务端生效，房间客户端收到的则是另一组设置。这里未端测具体游戏表现，不推断一定闪退。
- 基线：`channelmanager.cpp:1793–1798 OnRoomUpdateSettings` 在 UpdateSettings 后，以当前 roomSettings 的有效值和更新标志发送，而不是复写未经处理的请求载荷。
- 建议：从最终有效设置构造增量回包，同时包含服务端随本次操作改动的字段；维护实际应发送的 mask。不要为此重新引入曾被撤销的“按空载荷清 flag”归一化，保留位仍应遵守基线。
- 探针：`TestReview046SettingsBroadcastUsesEffectiveValues`。

### N3 [P1，偏离基线] 换房主后不刷新房主道具权益

- 位置：`internal/server/rooms.go:754–771`，leaveRoom 的房主迁移分支。
- 新房主选出后只发65/5并迁移对局，不重新计算 superRoom、c4Timer，以及适用模式的sd。房间沿用旧房主留下的设置。
- **复现**：旧房主拥有的设置为 superRoom=1、c4Timer=1；旧房主退出，新房主没有这些道具，两项仍为1。反过来，新房主有道具而旧房主没有时，也不会按新房主更新。
- superRoom不仅是显示字段，`kick.go::kickRequest` 直接根据房间的58号字段决定是否允许超级房主踢人，因此旧房主的权益会被继续使用。
- 基线：`room.cpp:969 UpdateHost` 在更新房主后调用 CheckForHostItems；`:899–930` 按新房主重算并广播三类道具设置。Go 在创建/手动改设置时已有检查，但自动迁移漏接。
- 建议：将新房主选择、按其有效道具重算设置、广播以及迁移做成统一流程；检查等待房间与对局中两条路径，也检查有/无专服情况。对应查询应遵守基线的激活状态语义，不能仅复用任意持有副本的判定就认为完全等价。
- 探针：`TestReview046HostMigrationRefreshesHostItems`，迁移后仍得到 `superRoom=1,c4Timer=1`。

### 验证、复跑与范围

使用 `GOTOOLCHAIN=local`。上轮三条 TestReview4308 全通过；本轮三条 TestReview046 均复现失败。Go探针已 gofmt，目标虚拟路径为 `internal/server/review_046_test.go`，源文件位于 `code-review/probes/review-04608be/server_test.go`。

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay code-review/probes/review-4308aae/overlay.json ./internal/server ./internal/store -run '^TestReview4308' -count=1
go test -overlay code-review/probes/review-04608be/overlay.json ./internal/server -run '^TestReview046' -count=1
```

当前实际结果：

| 命令范围 | 结果 |
|---|---|
| 根目录 `go build ./...` / `go vet ./...` / `go test ./...` | 失败，归档探针进入包发现；不能报告全绿 |
| `go build ./cmd/... ./internal/... ./assets/...` | 通过 |
| `go vet ./cmd/... ./internal/... ./assets/...` | 通过 |
| `go test ./cmd/... ./internal/... ./assets/...` | 通过 |

本轮新增报告一份，按时间戳+提交归档。没有修业务实现，没有调整历史探针/模块结构，也未提交 Git。覆盖上轮修复分支、包发现边界与房间设置/房主生命周期；不是全仓无缺陷证明。旧包序/157策略等未处理项、模糊测试和全模式结果包金标准仍未完成。
