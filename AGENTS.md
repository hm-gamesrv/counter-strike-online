# AGENTS.md

本文件记录本仓库的协作约定。修改代码时请同时遵守现有测试、格式化和静态检查要求。

## Go 结构体字段

结构体字段必须一行一个，每个字段单独写类型；不要把多个字段名合并到同一行。

不要写：

```go
type itemDefinition struct {
	Category, UseType, RewardID, Level int
	InGame                             bool
	ClassName                          string
}
```

要写：

```go
type itemDefinition struct {
	Category  int
	UseType   int
	RewardID  int
	Level     int
	InGame    bool
	ClassName string
}
```

新增或修改的结构体都按此规则书写；如果改动到旧结构体，应顺手把它规范为一行一个字段。

## 构建和测试

本机 Go 默认的 `GOTOOLCHAIN=auto` 会在解析工具链时访问网络，构建、测试和 `go vet` 可能因此多等约一分钟。仓库要求 Go 1.27.0，本机 1.27.2 已满足，统一使用本地工具链：

```powershell
$env:GOTOOLCHAIN = "local"
```

需要长期生效可执行 `go env -w GOTOOLCHAIN=local`。后续所有构建和测试命令都应在这个设置下运行。
