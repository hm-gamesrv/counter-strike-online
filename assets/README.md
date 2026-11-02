# 默认数据来源

客户端基准：`S:.tempCSNZ`，当前配套客户端 `30.12.24`。

- `defaults/Data` 中同名文件复制自该客户端的 `Server/Data`。
- `Shop.json`、`ItemBox.json`、`ItemRewards.json`、`EventQuests.json`、`RandomWeaponList.json`、`WeaponPaints.json`、`ZombieWarWeaponList.json` 复制自该客户端的 `Server`。
- `ServerConfig.json` 复制自该客户端的 `Server/ServerConfig.json`，不再使用上游默认配置。
- `Metadata_ModeList.bin`、`Metadata_Encyclopedia.bin` 在该客户端 `Server/Data` 中没有独立文件，保留从上游 `packet_metadata_data.h` 提取的固定数据块；其余元数据全部以该客户端文件为准。

参考源码固定在 `.upstream` 所记录的提交。本项目不包含客户端可执行程序或游戏地图资源。
