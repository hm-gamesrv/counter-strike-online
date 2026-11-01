#!/usr/bin/env bash
# 拉取上游 JusicP/CSNZ_Server 的固定提交，以及 src/thirdparty/ 下 7 个子模块。
#
# 用法: fetch-upstream.sh <目标目录>
#
# 全部走 codeload 的 tar.gz，不依赖 git / submodule：上游 .gitmodules 只记录
# 子模块地址，具体提交记录在上游仓库的 gitlink 里，因此这里把 SHA 全写死，
# 保证 CI 与本地构建拿到的是同一份源码。
#
# 环境变量:
#   CODELOAD_BASE  默认 https://codeload.github.com
#                  国内网络可换镜像，例如
#                  https://gh-proxy.com/https://codeload.github.com
set -euo pipefail

DEST="${1:?用法: fetch-upstream.sh <目标目录>}"
CODELOAD_BASE="${CODELOAD_BASE:-https://codeload.github.com}"
CODELOAD_BASE="${CODELOAD_BASE%/}"

UPSTREAM_REPO="JusicP/CSNZ_Server"
# 2024-11-29「Update metadata」。该提交的 SUPPORTED_CLIENT_BUILD 仍是
# 30.11.24，对应客户端包里的 30.12.24 客户端（会告警但可登录）。
UPSTREAM_SHA="12644f2adc0708e2d8b74f7dd6a4c3c29d3e1140"

# src/thirdparty/<名字> <仓库> <提交>
SUBMODULES=(
    "KeyValues  JusicP/KeyValues     95364960fdabd56f95ad5b8f012bd0261bf51330"
    "SQLiteCpp  SRombauts/SQLiteCpp  bcb4c78fed6628f150c28fa6377c2b0430f7f99b"
    "json       nlohmann/json        6eab7a2b187b10b2494e39c1961750bfd1bda500"
    "wolfssl    wolfSSL/wolfssl      008d4958bffcafa9e22f21abcf20268b6bbd632a"
    "zip        kuba--/zip           d15aa0c3789fdd6765b010886a09e9eac021b06f"
    "rapidcsv   d99kris/rapidcsv     f032c527caa2533a3b5b17d11d9c29feaa1cca06"
    "doctest    doctest/doctest      ae7a13539fb71f270b87eb2e874fbac80bc8dda2"
)

fetch_tarball() { # <仓库> <提交> <目标目录>
    local repo=$1 sha=$2 dest=$3 url tmp attempt
    url="${CODELOAD_BASE}/${repo}/tar.gz/${sha}"
    tmp="$(mktemp /tmp/upstream-XXXXXX.tgz)"

    # -C -：断点续传（重试时接着下）；speed-limit/speed-time：连接“挂着不动”
    # 超过 120 秒就中断重试，而不是傻等到天荒地老。
    for attempt in 1 2 3; do
        if curl -fsSL -C - --connect-timeout 20 --retry 2 --retry-delay 3 \
                --speed-limit 1024 --speed-time 120 -o "$tmp" "$url"; then
            break
        fi
        echo "   下载失败（第 ${attempt} 次），重试: ${url}" >&2
        if [ "$attempt" = "3" ]; then
            echo "无法下载: ${url}" >&2
            exit 1
        fi
        sleep 5
    done

    mkdir -p "$dest"
    tar xzf "$tmp" --strip-components=1 -C "$dest"
    rm -f "$tmp"
    echo "   完成: ${dest}"
}

rm -rf "$DEST"
echo "==> ${UPSTREAM_REPO}@${UPSTREAM_SHA:0:10} -> ${DEST}"
fetch_tarball "$UPSTREAM_REPO" "$UPSTREAM_SHA" "$DEST"

for entry in "${SUBMODULES[@]}"; do
    read -r name repo sha <<< "$entry"
    echo "==> src/thirdparty/${name} (${repo}@${sha:0:10})"
    fetch_tarball "$repo" "$sha" "${DEST}/src/thirdparty/${name}"
done

echo "上游源码已就绪: ${DEST}"
