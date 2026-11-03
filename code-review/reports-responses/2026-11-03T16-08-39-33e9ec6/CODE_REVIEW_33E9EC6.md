# 33e9ec6 修复复核

针对 `.tmp/review-response-4.md` 核对提交 `33e9ec6`。确认 **2 项 P2 未完全修复**；上轮原始 4 条探针均通过，插件材料重复使用与战队审批超员的原问题可关闭。

未修改业务代码、现有测试或运行数据，未操作客户端。整体检查的独立新增发现见 `CODE_REVIEW_OVERALL_33E9EC6.md`。

## 1. [P2] 数据库清掉背景引用后，在线角色缓存仍会下发旧背景

- 位置：`internal/server/inventory.go:590`，关联 `internal/server/server.go:613`、`internal/server/update_info.go:37`。
- `DeleteItems` 现在会在数据库清掉 nameplateID，但服务端分解入口只发送库存更新，既不重读 `c.character`，也不发布角色信息更新。到期扫描同样没有同步在线角色缓存。
- **复现**：通过 `toggleNameplate` 启用背景 7001，再经正常 `itemRequest` 分解它。数据库重读的 `NameplateID=0`，在线 `c.character.NameplateID` 却仍为 7001。
- 后续位置更新等调用 `broadcastUserInfo(c)` 时，仍从缓存把已删除背景下发给自己及其他玩家；本次删除也没有立即发送角色背景变更。重新登录会恢复正常，因此不能说数据库修复无效，但在线路径尚不完整。
- 建议：库存移除导致角色字段变化后，同步对应在线角色缓存并发布必要的角色信息更新。到期扫描需能定位受影响用户；避免只靠下次登录纠正。
- 探针：`TestReview33RemovalRefreshesCharacterCache`，输出 `cache=7001, database=0`。

## 2. [P2] 存在同款休眠副本时，已到期的装备引用仍然残留

- 位置：`internal/store/references.go:10–14`；关联 `internal/store/costume.go:225–229`。
- `clearItemReference` 只要找到任意同 ID 且 count>0 的行就跳过清理，未确认这份副本是否实际装备、是否仍有效，也没有把替代副本切到装备状态。`AutoEquipCostume` 的防御检查也把休眠副本视为槽位仍被占用。
- **复现**：持有两份同 ID 的限时 HeadCostume，装备第一份，第二份保持休眠；第一份到期后执行扫描。loadout.head 仍为 471，但库存中已没有任何对应的 `inUse=1,status=1` 副本，只剩休眠副本。
- 旧引用仍会让该装饰槽被当作占用，后续发放另一件同槽装饰时自动装备仍受阻。这延续了上轮“到期引用挡自动装备”的问题，只是触发条件从无剩余副本变为剩余休眠副本。
- 建议：按选择类型检查剩余副本的实际状态；合法接替时需要更新副本状态及必要的时长表示，否则清掉引用。不能将“有一行同 itemID”直接等同于“存在有效装备”。同时覆盖删除/到期及同款休眠、已激活未装备、过期副本。
- 探针：`TestReview33DormantCopyDoesNotKeepEquippedReference`。

## 验证

- `GOTOOLCHAIN=local` 下，现有完整测试和 `go vet ./...` 通过。
- 上轮 `.dist/review-154b2fe/overlay.json` 的 `TestReview154*` 全部通过，确认原探针覆盖的条件已经修复。
- 新增探针 `.dist/review-33e9ec6/{server_test,store_test}.go` 已 gofmt，overlay 加入测试编译，没有改动现有测试；上述两条复现失败。

```powershell
$env:GOTOOLCHAIN = 'local'
go test -overlay .dist/review-33e9ec6/overlay.json ./internal/server ./internal/store -run '^TestReview33(RemovalRefreshesCharacterCache|DormantCopyDoesNotKeepEquippedReference)$' -count=1
```
