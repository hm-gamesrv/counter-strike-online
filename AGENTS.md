# AGENTS.md

本项目为 CSO 私服主服务端（master server）CSNZ_Server 的 Go 重写，开发过程使用 AI 辅助。

## 1. 相关项目

### 1.1. CSNZ_Server

**仓库**：<https://github.com/JusicP/CSNZ_Server>

CSN:Z 的私服服务端（master server），由 JusicP 维护，实现登录、大厅与房间、道具、元数据下发等客户端协议。

**与本项目的关系**：本项目的重写目标。功能与协议以实现为准，当前基线为 `JusicP/CSNZ_Server@12644f2adc0708e2d8b74f7dd6a4c3c29d3e1140`。

### 1.2. counter-strike-online

**仓库**：<https://github.com/hm-gamesrv/counter-strike-online>

CSNZ_Server 的 Linux 容器化封装，提供 Docker 镜像构建流程与上游 Linux 兼容性补丁。

**与本项目的关系**：部署形态与容器约定的参考来源，重写版沿用其镜像仓库、数据卷与 CI 约定。

### 1.3. Launcher_CSNZ

**仓库**：<https://github.com/JusicP/Launcher_CSNZ>

客户端启动器（hook 版），替换客户端的 `Bin/CSOLauncher.exe`，通过命令行参数指定 master server 地址与端口（默认 `127.0.0.1:30002`），并支持自动登录等开关。

**与本项目的关系**：客户端的连接入口；联调与实机验证时用于切换服务器地址。

### 1.4. csnz-launcher

**仓库**：<https://github.com/hiromuraki/csnz-launcher>

面向玩家的图形界面启动器（WPF/.NET），管理 CSNZ 与 CSO2 的客户端目录、服务器地址与账号，按 Launcher_CSNZ 的参数格式启动客户端。

### 1.5. HLDS_CSNZ

**仓库**：<https://github.com/SmilexGamer/HLDS_CSNZ>

CSN:Z 的专用服务器（dedicated server），由 SmilexGamer 维护，负责对局内的房间逻辑与玩家同步。

**与本项目的关系**：多人对局的必要组件，需与 CSNZ_Server 同时运行。

## 2. 代码规范

本文件记录本仓库的协作约定。修改代码时请同时遵守现有测试、格式化和静态检查要求。

### 2.1. Go 结构体字段

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

### 2.2. Go 复合字面量

多字段复合字面量也要求一行一个字段，嵌套结构体要展开。不要把多个字段写在同一个字面量行里。

不要写：

```go
c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "DisassembleUser"}, version: true, queue: make(chan outbound, 8)}
```

要写：

```go
c := &client{
    user: store.User{
        ID: 1,
    },
    character: store.Character{
        ID:   1,
        Name: "DisassembleUser",
    },
    version: true,
    queue:   make(chan outbound, 8),
}
```

单字段字面量可以保持单行，但只要同一字面量包含多个字段，就按上面的方式展开。新增或修改的测试与生产代码都适用；改动旧字面量时顺手规范化。

## 3. 构建和测试

本机 Go 默认的 `GOTOOLCHAIN=auto` 会在解析工具链时访问网络，构建、测试和 `go vet` 可能因此多等约一分钟。仓库要求 Go 1.27.0，本机 1.27.2 已满足，统一使用本地工具链：

```powershell
$env:GOTOOLCHAIN = "local"
```

需要长期生效可执行 `go env -w GOTOOLCHAIN=local`。后续所有构建和测试命令都应在这个设置下运行。
