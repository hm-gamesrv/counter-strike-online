# 默认数据来源

嵌入数据固定取上游 `JusicP/CSNZ_Server@4859a6e73451ccbfce167b1d28deeb0457468ac3` 的 `bin/`，不使用 `S:\.temp\CSNZ` 客户端目录中的定制配置和数据。

- 参考仓库按 `AGENTS.md` 约定克隆在 `./.ref/CSNZ_Server`；执行 `scripts/sync-defaults.ps1` 会从基线提交重新生成 `defaults/`。
- `ServerConfig.json`、`Shop.json`、`ItemBox.json`、`ItemRewards.json`、`EventQuests.json`、`RandomWeaponList.json`、`WeaponPaints.json`、`ZombieWarWeaponList.json` 复制自上游 `bin/`。
- `defaults/Data` 是上游 `bin/Data` 的完整副本（含 `Data/SQL` 脚本）；这些 SQL 只作为上游快照随附，Go 端 schema 由 `internal/store/schema.sql` 维护。
- `Metadata_ModeList.bin`、`Metadata_Encyclopedia.bin` 不在上游该提交的 `bin/Data` 中（基线的 ModeList/Encyclopedia sender 用 `src/packet/packet_metadata_data.h` 里的硬编码数组）；上游 `ServerConfig.json` 默认也关闭这两项。脚本按发送格式把它们生成为可直接下发的 payload 一并嵌入，运维开启对应元数据时才有数据可用。

脚本只改写内容不同的文件，重复运行不会弄脏工作区；上游该提交里没有的文件会从 `defaults/` 删除。本项目不包含客户端可执行程序或游戏地图资源。
