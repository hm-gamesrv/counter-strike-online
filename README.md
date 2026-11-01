# Counter-Strike Online Server

## 1. 简述

反恐精英 Online（官方全球版名 CSN:Z / Counter-Strike Nexon: Studio）的
**主服务器（master server）**，负责登录、频道、房间、匹配、商店、背包、
任务、排行榜等，用于自建私服。游戏客户端与游戏资源不包含在本项目中。

上游：[JusicP/CSNZ_Server](https://github.com/JusicP/CSNZ_Server)，
固定在提交 `12644f2`（2024-11-29）。该提交的
`SUPPORTED_CLIENT_BUILD` 是 `30.11.24`，对应客户端包里的
`30.12.24` 客户端 —— 登录时服务端会打一条版本告警，但不影响使用。

> *Counter-Strike Online / CSN:Z 归 NEXON / Valve 所有，本项目仅用于学习。*

**特点：**

- **源码在构建期拉取**：固定 commit + 7 个固定子模块，仓库本身不含 C/C++ 源码
- **原生 Linux 编译**：二阶段构建，运行镜像只依赖 `libstdc++`
- **数据目录与二进制分离**：二进制在 `/app/`，挂载 `/data` 不会盖住它
- **首次启动自动就绪**：空数据目录会写入默认配置、全套元数据表与建库脚本
- **GitHub Actions 自动构建**：推送镜像到 GHCR + 阿里云 ACR

### 1.1. 镜像

| 仓库 | tag |
| ---- | --- |
| `ghcr.io/hm-gamesrv/counter-strike-online` | `latest`、`yyyy-MM-dd` |

## 2. 资源占用信息

### 2.1. 端口

| 端口号 | 协议 | 说明 |
| ------ | ---- | ---- |
| 30002  | TCP  | 游戏主连接（登录、大厅、房间列表） |
| 30002  | UDP  | 联机通信（登录后的 UDP 打洞） |

### 2.2. 持久卷

| 容器路径 | 说明 |
| -------- | ---- |
| `/data` | 服务器工作目录：`ServerConfig.json`、`Data/` 元数据表、`UserDatabase.db3`、`Logs/` |

容器以 **UID/GID 1000** 运行。宿主机目录必须对该用户可写；宿主 uid 不是 1000 时
需要 `sudo chown 1000:1000 data`。

### 2.3. 环境变量

无。所有配置都在 `ServerConfig.json` 里。

## 3. 数据准备

**镜像开箱即用**。上游仓库 `bin/` 里的默认配置与完整元数据表都被打进镜像：

- 8 个 JSON：`ServerConfig.json`、`Shop.json`、`ItemBox.json`、`ItemRewards.json`、
  `EventQuests.json`、`RandomWeaponList.json`、`WeaponPaints.json`、`ZombieWarWeaponList.json`
- `Data/` 下 35 个元数据文件 + `SQL/` 目录：`Item.csv`、`MapList.csv`、`ClientTable.csv`、`Metadata_*.bin` 等
  元数据表，以及 `Data/SQL/` 建库脚本

空数据目录首次启动时会自动写入。想换成客户端包里的更新版本时，把客户端
`Server` 目录覆盖到 `data/` 即可（**数据目录优先，已有文件不会被镜像覆盖**）：

```bash
cp -a /path/to/CSNZ/Server/. ./data/
```

要点：

- `Data/` 下的 35 个元数据文件是启动**硬依赖**：代码在 `CPacketManager::Init` 里
  无条件加载，缺任意一张就 `Failed to load metadata` 退出。
  `ServerConfig.json` 里的 `Metadata.XXX` 开关只决定“发不发客户端”，**不代表可以删文件**。
- `Data/SQL/`（6 个 `.sql`）只在建新库 / 升级库时执行；已有 v4 的库可以不要，
  但**没有库又没有 SQL 就建不起来**。
- `UserDatabase.db3` 缺失会自动新建空库（并轮转备份为 `UserDatabase_年_月_日.db3`），
  删掉即清档；升级前请先备份它。
- 日志写在 `data/Logs/`，每个进程一份。

## 4. 快速开始

### 4.1. Docker

```bash
docker build -t csnz:temp .
docker run --rm -it \
    -p 30002:30002/tcp \
    -p 30002:30002/udp \
    -v ./data:/data \
    csnz:temp
```

> 构建需要访问 GitHub（codeload.github.com）和 Debian 源。国内网络下两者都可能很慢，可以同时换源：
>
> ```bash
> docker build --build-arg APT_MIRROR=mirrors.aliyun.com --build-arg CODELOAD_BASE=https://gh-proxy.com/https://codeload.github.com -t csnz:temp .
> ```

### 4.2. Docker Compose

```bash
mkdir -p data        # 必须以当前用户建好，见 2.2
docker compose up -d
```

### 4.3. 首次启动与维护

- 首次启动：数据目录为空 → 写入默认配置 + `Data/SQL` → 启动
- 每次启动：检查 `/data` 可写；`Data/ClientTable.csv` 缺失时打印告警
- 清空玩家数据：`rm -rf data/Logs data/UserDatabase*.db3` 后重启
- 更新元数据表：用新版客户端包的 `Server/Data` 覆盖 `data/Data`
- 日志：`data/Logs/`（每个进程一份）或 `docker logs`

## 5. 客户端连接

客户端启动器（`CSOLauncher.exe`，放在客户端的 `Bin` 目录）支持：

```bat
CSOLauncher.exe -ip <服务器IP> -port 30002
```

自动登录（账号密码以聊天命令 `/login` 发给服务端）：

```bat
CSOLauncher.exe -ip <服务器IP> -port 30002 -login <账号> -password <密码> -disableauthui
```

- `-disableauthui` 跳过登录框；账号密码错误时也看不到提示，建议先手动登录确认
- 注册：去掉 `-disableauthui`，在登录框里点注册；或加 `-register`
- 密码是明文参数，且走未加密 TCP，不要复用重要密码

## 6. 构建补丁

上游是 Windows 优先的工程（README 自称 Linux “requires tests”），源码在
GCC 下**完全无法直接编译**。`patches/linux-build.patch` 只做可编译性修复，
不改协议与业务逻辑：

| 类别 | 内容 |
| ---- | ---- |
| 平台类型 / 头文件 | `SOCKET`、`WSAPOLLFD` typedef，`<unistd.h>`、`<cstring>`、`<signal.h>` 等 |
| MSVC 扩展 | 不透明枚举前置声明补 `: int`、临时 `vector` 绑非 const 引用、range-for 变量遮蔽 |
| Linux 崩溃 | `CCompositeLogger` 传 NULL `va_list` 给 glibc；stdin 是 EOF 时控制台线程死循环 |
| 构建图 | 关掉 `src/test`；KeyValues 的 `-Werror`（会传染给主目标） |

维护方式：上游升版本时，把 `.ref/CSNZ_Server` 换成新快照，重做补丁后
用 `docker build` 验证。补丁必须保持 LF 行尾（见 `.gitattributes`）。

## 7. 已知限制

- `GetMemoryInfo` 是 Windows API，Linux 下日志固定 `0.00mb` + `not implemented`
- 管理 GUI 关闭（CMake 写的是 `GUI/`，实际目录 `gui/`，且需要 Qt6）
- 单元测试不编译（`src/test` 是 Windows 向的）
- 私有构建里内嵌的活动任务数据不在公开源码中，活动回落到 `EventQuests.json`
- SSL 路径未实测（客户端包默认 `SSL=false`）

## 8. 原项目

- 主服务器：[JusicP/CSNZ_Server](https://github.com/JusicP/CSNZ_Server)（GPL-3.0）
- 专用服务器：[SmilexGamer/HLDS_CSNZ](https://github.com/SmilexGamer/HLDS_CSNZ)
- 客户端启动器：[JusicP/Launcher_CSNZ](https://github.com/JusicP/Launcher_CSNZ)
