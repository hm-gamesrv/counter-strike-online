#!/bin/bash
# 首次启动：把镜像内置的默认配置与建库脚本写进数据目录。
# 已有文件一律不覆盖——数据目录的内容优先级高于镜像。
set -euo pipefail

if [ ! -f /data/ServerConfig.json ]; then
    echo "[init] /data 尚无配置，写入镜像内置默认值 ..."
    cp -a /app/default/. /data/
fi

if [ ! -w /data ]; then
    echo "[init] 错误: /data 不可写。容器以 uid 1000 运行，" >&2
    echo "       请让宿主机挂载目录对该用户可写（宿主 uid 非 1000 时执行 chown 1000:1000）。" >&2
    exit 1
fi

# 日志目录必须可写。不可写时服务端不会报错，只是静默不写日志，排查会很痛苦，
# 所以这里直接失败退出。
mkdir -p /data/Logs 2>/dev/null || true
if [ ! -w /data/Logs ]; then
    echo "[init] 错误: /data/Logs 不可写（多半是目录被 root/其他 UID 创建过）。" >&2
    echo "       执行 chown -R 1000:1000 <数据目录> 后重试。" >&2
    exit 1
fi

# 正常情况镜像已内置全套元数据表；出现这种情况说明数据目录是“半成品”
# （比如只挂了 ServerConfig.json），服务端会因缺表直接退出。
if [ ! -f /data/Data/ClientTable.csv ]; then
    echo "[init] 警告: /data/Data 缺少客户端元数据表（ClientTable.csv / Item.csv / MapList.csv ...）。" >&2
    echo "       请把客户端包 CSNZ/Server 目录整体复制到数据目录，否则服务端会因" >&2
    echo "       Failed to load metadata 退出。" >&2
fi

exec "$@"
