# 默认数据来源

当前嵌入数据固定使用上游 `JusicP/CSNZ_Server@12644f2adc0708e2d8b74f7dd6a4c3c29d3e1140` 的 `bin/` 快照，不再使用 `S:\.temp\CSNZ` 客户端目录中的定制配置和数据。

- `ServerConfig.json`、`Shop.json`、`ItemBox.json`、`ItemRewards.json`、`EventQuests.json`、`RandomWeaponList.json`、`WeaponPaints.json`、`ZombieWarWeaponList.json` 复制自上游 `bin/`。
- `defaults/Data` 复制自上游 `bin/Data`，只保留 Go 服务端实际读取的文件；`Data/SQL` 由 Go 内嵌 schema 取代，不随运行目录生成。
- `Metadata_ModeList.bin`、`Metadata_Encyclopedia.bin` 不在上游该提交的 `bin/Data` 中；上游 `ServerConfig.json` 也关闭了对应元数据，因此不嵌入。

参考源码固定在 `.upstream` 所记录的提交。本项目不包含客户端可执行程序或游戏地图资源。
