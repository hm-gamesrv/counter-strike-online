# 代码审查档案

仓库 `hm-gamesrv/counter-strike-online`（本地 `C:\Users\11717\Desktop\cso-server`），分支 `go-rewrite`。
本目录**进版本库**：审查报告、逐条响应、探针与分析都归档在这里，代码库与档案同步演进。

## 命名约定

`reports-responses/` 下**按轮次分目录**，目录名 = 该轮主报告的时间戳（`yyyy-MM-ddTHH-mm-ss`）+ 轮次提交短哈希，例如 `2026-11-03T06-08-43-b4cc191`；时间在前保证排序，哈希在后便于和提交对应：

```
reports-responses/
  2026-11-03T06-08-43/
    CODE_REVIEW_B4CC191.md
    CODE_REVIEW_B4CC191_RESPONSE.md
  2026-11-03T14-13-48/
    CODE_REVIEW_44B5EED.md
    CODE_REVIEW_44B5EED_EXPANDED.md
    CODE_REVIEW_44B5EED_RESPONSE.md
  ...
  2026-11-03T20-31-27-4308aae/
    CODE_REVIEW_4308AAE.md
```

目录内文件名：

```
CODE_REVIEW_<哈希>.md            审查报告（目录名来源）
CODE_REVIEW_<哈希>_RESPONSE.md   我对它的逐条处理
CODE_REVIEW_<哈希>_OVERALL.md    同一轮附带的跨模块检查（若有，旧范式）
CODE_REVIEW_<哈希>_COVERAGE.md   同一轮附带的覆盖记录（若有，旧范式）
CODE_REVIEW_<哈希>_EXPANDED.md   同一轮附带的扩大范围审查（若有，旧范式）
```

**新的一轮开始后，审查结果只写一个文件**：`CODE_REVIEW_<哈希>.md`，不再拆 OVERALL/COVERAGE；我对应写 `CODE_REVIEW_<哈希>_RESPONSE.md`。同一轮若有附属报告（如 `F6EDA20_OVERALL` 属于 `B6AFE1F` 那轮），连同主报告一起放进同一个日期目录。

## 目录

```
code-review/
  review-request.md      发给审查者的请求（目标、范围、重点、非目标、开放问题）
  INDEX.md               本文件
  reports-responses/     报告与响应（见上）
  probes/                审查者附的 overlay 探针集，每条复现都能直接跑
  analysis/              主动做的分析记录（库存级联梳理、元数据帧问题）
```

交给审查者（GPT）的文件只有三个：`review-request.md`、`reports-responses/<最新时间戳>/CODE_REVIEW_<最新哈希>.md`、以及同目录的 `_RESPONSE.md`；其余是历史。

## 现有轮次

| 目录 | 内容 |
|---|---|
| `2026-11-03T06-08-43-b4cc191` | 报告 + 响应 |
| `2026-11-03T14-13-48-44b5eed` | 报告 + `_EXPANDED` + 响应 |
| `2026-11-03T14-59-34-b6afe1f` | 报告 + `CODE_REVIEW_F6EDA20_OVERALL`（同一轮的整体审查）+ 响应 |
| `2026-11-03T15-56-51-154b2fe` | 报告 + `_OVERALL` + 响应 |
| `2026-11-03T16-08-39-33e9ec6` | 报告 + `_OVERALL` + 响应 |
| `2026-11-03T16-42-02-3e6fa14` | 报告 + `_OVERALL` + 响应 |
| `2026-11-03T18-06-18-9d33c2d` | 报告 + `_OVERALL` + 响应 |
| `2026-11-03T18-59-15-a0bb9a6` | 报告 + `_COVERAGE` + 响应 |
| `2026-11-03T20-31-27-4308aae` | 报告 + 响应（1 P1 + 2 P2，已修） |
| `2026-11-03T21-15-03-04608be` | 报告 + 响应（4 P1，已修） |
| `2026-11-03T22-24-51-70bd62e` | 报告 + 响应（1 P0 + 3 P1，已修） |

所有路径均在 `reports-responses/<时间戳>-<哈希>/` 下。

## 跑探针

`probes/review-<短哈希>/` 里是探针源码与 `overlay.json`；overlay 用绝对路径把探针“虚拟”进源码树，不改仓库文件：

```powershell
$env:GOTOOLCHAIN = "local"
go test -overlay code-review/probes/review-a0bb9a6/overlay.json ./internal/server ./internal/store `
    -run '^(TestReview|TestAudit|TestOverall)' -count=1
```

十批全跑：

```powershell
Get-ChildItem code-review/probes -Directory | ForEach-Object {
    Write-Output "== $($_.Name)"
    go test -overlay "$($_.FullName)\overlay.json" ./internal/server ./internal/store `
        -run '^(TestReview|TestAudit|TestOverall)' -count=1
}
```

`review-b4cc191` 那批的两条 server 探针预期失败：其前提（职业加成用内存快照、装备加成默认开启）已被后续审查自己的结论取代，理由见 `CODE_REVIEW_B4CC191_RESPONSE.md` 与 `CODE_REVIEW_A0BB9A6_RESPONSE.md`。

## 当前状态

- `go vet` / `go test ./...` 全绿，197 个 Test/Fuzz/Benchmark。
- `blackbox/`（Python，纯 socket，**在仓库里**）5 个冒烟用例在自建服务端与基线实例上均通过；32 个元数据类型解压后内容一致。
- 未完成：`FuzzItemRequest`/`FuzzMetadata`、全模式结果包逐字段金标准、`zsDifficulty` 值/编码职责拆分、等级表口径、完整事务调用图，以及 `review-request.md` §5 P0-6 的 8 条黑盒差异（`74`/`76` 顺序、`103` 位置、`151` 条数、`180` 位置、`153` 长度、`72` 内容、`67` 条数、`157` flag 策略）。
