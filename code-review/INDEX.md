# 代码审查档案

仓库 `hm-gamesrv/counter-strike-online`（本地 `C:\Users\11717\Desktop\cso-server`），分支 `go-rewrite`。
本目录**进版本库**：审查报告、逐条响应、探针与分析都归档在这里，代码库与档案同步演进。

## 命名约定

`reports-responses/` 下**按轮次分目录**，目录名 = 该轮主报告的时间戳（`yyyy-MM-ddTHH-mm-ss`）+ 轮次提交短哈希，例如 `2026-11-03T06-08-43-b4cc191`；时间在前保证排序，哈希在后便于和提交对应：

```
reports-responses/
  2026-11-03T06-08-43-b4cc191/
    CODE_REVIEW_B4CC191.md
    CODE_REVIEW_B4CC191_RESPONSE.md
  2026-11-04T01-18-26-75cddae-store/
    CODE_REVIEW_75CDDAE_STORE.md
    CODE_REVIEW_75CDDAE_STORE_RESPONSE.md
  2026-11-05T02-54-52-83b1c1a-lobby/
    CODE_REVIEW_83B1C1A_LOBBY.md
    CODE_REVIEW_83B1C1A_LOBBY_RESPONSE.md
  2026-11-05T02-57-18-83b1c1a-lifecycle/
    CODE_REVIEW_83B1C1A_LIFECYCLE.md
    CODE_REVIEW_83B1C1A_LIFECYCLE_RESPONSE.md
```

同一轮并行多份报告时，目录名在哈希后加主题后缀（`-store`、`-lobby`、`-lifecycle`），保证"一个目录一份报告一份响应"。报告放进来之后**必须**在同一目录补 `_RESPONSE.md`；没有响应的目录视为未处理。

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
| `2026-11-04T00-05-40-21801e6` | 报告 + 响应（3 P1 + 5 P2，已修） |
| `2026-11-04T01-18-26-75cddae-store` | 报告 + 响应（存储层 S-1/S-2 采纳、S-3 不采纳） |
| `2026-11-04T01-24-42-75cddae` | 报告 + 响应（6 P2，已修） |
| `2026-11-04T15-24-55-86212b7` | 报告 + 响应（R1/N1 两处 P1、R2 一处 P2，2026-11-05 补修） |
| `2026-11-05T02-54-52-83b1c1a-lobby` | 报告 + 响应（2 P1 + 6 P2，已修；同轮关掉 P0-6 的 8 条） |
| `2026-11-05T02-57-18-83b1c1a-lifecycle` | 报告 + 响应（6 P2，已修） |

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

- `go vet` / `go test ./...` 全绿，199 个 Test/Fuzz/Benchmark。
- `blackbox/`（Python，纯 socket，**在仓库里**）5 个冒烟用例在自建服务端与基线实例上均通过；32 个元数据类型解压后内容一致。
- 2026-11-05：登录链与房间出站报文已和 30003 基线做过逐字节对照。登录 60 包只差 19 条元数据帧的压缩方式与 1 条商店推荐标题（已批准偏离）；建房/更新/房间列表/进房包集合与基线逐字节一致（房间 id 与对端 IP 属环境差异）。`blackbox/test_login_order.py` 固化登录包序。
- 未完成：全模式结果包逐字段金标准、`zsDifficulty` 值/编码职责拆分、等级表口径、完整事务调用图。
