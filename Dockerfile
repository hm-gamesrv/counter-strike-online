# syntax=docker/dockerfile:1
#
# Counter-Strike Online (CSN:Z) 主服务器 —— Linux 构建
# 上游: JusicP/CSNZ_Server @ 12644f2（2024-11-29，SUPPORTED_CLIENT_BUILD=30.11.24）

# =================
# 上游源码
# =================
FROM debian:bookworm AS source

ARG DEBIAN_FRONTEND=noninteractive
ARG APT_MIRROR=deb.debian.org
RUN if [ "$APT_MIRROR" != "deb.debian.org" ]; then \
        sed -i "s|deb.debian.org|$APT_MIRROR|g" /etc/apt/sources.list.d/debian.sources; \
    fi \
    && apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl tar patch \
    && rm -rf /var/lib/apt/lists/*

# CODELOAD_BASE：源码下载地址，默认 GitHub 官方；国内网络可换成镜像，
# 例如 --build-arg CODELOAD_BASE=https://gh-proxy.com/https://codeload.github.com
ARG CODELOAD_BASE=https://codeload.github.com

COPY scripts/fetch-upstream.sh /usr/local/bin/fetch-upstream.sh
RUN chmod +x /usr/local/bin/fetch-upstream.sh \
    && fetch-upstream.sh /upstream

COPY patches/ /patches/

WORKDIR /upstream
# 上游是 Windows 优先的工程（README 自称 Linux 需自测），GCC 下无法直接编译。
# 补丁只修可编译性与两处 Linux 崩溃，不改协议与业务逻辑，
# 逐项说明见 README「构建补丁」一节。
RUN patch -p1 --batch --forward < /patches/linux-build.patch \
    && sed -i 's/ -Werror//g' src/thirdparty/KeyValues/CMakeLists.txt

# ===================
# 编译
# ===================
FROM debian:bookworm AS build

ARG DEBIAN_FRONTEND=noninteractive
ARG APT_MIRROR=deb.debian.org
RUN if [ "$APT_MIRROR" != "deb.debian.org" ]; then \
        sed -i "s|deb.debian.org|$APT_MIRROR|g" /etc/apt/sources.list.d/debian.sources; \
    fi \
    && apt-get update \
    && apt-get install -y --no-install-recommends build-essential cmake ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build
COPY --from=source /upstream /build

# -DSERVER_USE_GUI=OFF : CMake 里写的是 GUI/，实际目录是 gui/（Linux 大小写敏感），
#                        且需要 Qt6，容器里不需要管理界面。
# -DSERVER_DBSQLITE=ON : SQLite 后端（客户端包里的 UserDatabase.db3 就是它建的）。
# -Wno-error           : wolfSSL 5.x 会往 CMAKE_C_FLAGS 里塞 -Werror，
#                        GCC 12 会踩到 tls.c 的 stringop-overflow 误报。
# 产物固定写到 <repo>/bin/CSNZ_Server（见 src/CMakeLists.txt）。
RUN cmake -S src -B build \
        -DCMAKE_BUILD_TYPE=Release \
        -DSERVER_USE_GUI=OFF \
        -DSERVER_DBSQLITE=ON \
        -DBUILD_TESTKV=OFF \
        -DCMAKE_C_FLAGS="-Wno-error" \
        -DCMAKE_CXX_FLAGS="-Wno-error" \
    && cmake --build build --parallel "$(nproc)"

# ===================
# 主镜像
# ===================
FROM debian:bookworm-slim

ARG DEBIAN_FRONTEND=noninteractive
ARG APT_MIRROR=deb.debian.org
RUN if [ "$APT_MIRROR" != "deb.debian.org" ]; then \
        sed -i "s|deb.debian.org|$APT_MIRROR|g" /etc/apt/sources.list.d/debian.sources; \
    fi \
    && apt-get update \
    && apt-get install -y --no-install-recommends libstdc++6 ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -g 1000 gamesrv \
    && useradd -u 1000 -g gamesrv -M -d /data -s /usr/sbin/nologin gamesrv \
    && mkdir -p /app/default /data

# 应用在 /app、数据在 /data：挂数据卷不会盖住程序
COPY --from=build /build/bin/CSNZ_Server /app/csnz-server
# 仓库 bin/ 里的默认配置与建库脚本；首次启动由 init.sh 灌进空的数据目录
COPY --from=build /build/bin/*.json /app/default/
COPY --from=build /build/bin/Data /app/default/Data
COPY init.sh /init.sh

RUN chmod 755 /init.sh && chown -R gamesrv:gamesrv /app /data

WORKDIR /data
VOLUME ["/data"]

# 主服务器 TCP + UDP 同端口（ServerConfig.json -> "Port"）
EXPOSE 30002/tcp 30002/udp

USER gamesrv
ENTRYPOINT ["/init.sh"]
CMD ["/app/csnz-server"]
