#requires -Version 7.0
<#
.SYNOPSIS
    从参考仓库重新生成 assets/defaults。

.DESCRIPTION
    默认数据固定取 JusicP/CSNZ_Server 的基线提交，避免手工拷贝导致漂移。
    运行前需先克隆参考仓库到 .ref/CSNZ_Server（见 README）。

    只改写内容不同的文件，因此重复运行不会弄脏工作区；基线里不存在的
    文件会从 assets/defaults 中删除。

.PARAMETER Baseline
    上游提交；默认值即 AGENTS.md 记录的基线。

.PARAMETER Repo
    参考仓库路径；默认为项目下的 .ref/CSNZ_Server。
#>
[CmdletBinding()]
param(
    [string]$Baseline = '12644f2adc0708e2d8b74f7dd6a4c3c29d3e1140',
    [string]$Repo = ''
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrEmpty($Repo)) {
    $Repo = Join-Path $root '.ref/CSNZ_Server'
}
if (-not (Test-Path (Join-Path $Repo '.git'))) {
    throw "找不到参考仓库 $Repo；请先执行 git clone https://github.com/JusicP/CSNZ_Server .ref/CSNZ_Server"
}

git -C $Repo cat-file -e "${Baseline}^{commit}" 2>$null
if ($LASTEXITCODE -ne 0) {
    throw "参考仓库中没有提交 $Baseline；请先执行 git -C .ref/CSNZ_Server fetch"
}

$worktree = Join-Path $root '.tmp/upstream-defaults'
if (Test-Path $worktree) {
    git -C $Repo worktree remove --force $worktree 2>$null
    Remove-Item -Recurse -Force $worktree -ErrorAction SilentlyContinue
}
New-Item -ItemType Directory -Force (Split-Path -Parent $worktree) | Out-Null
git -C $Repo -c core.autocrlf=false worktree add --detach $worktree $Baseline | Out-Null
if ($LASTEXITCODE -ne 0) {
    throw "无法创建 $Baseline 的工作树"
}

$target = Join-Path $root 'assets/defaults'
$files = git -C $worktree ls-tree -r --name-only HEAD -- bin
$expected = @{}
foreach ($file in $files) {
    $relative = $file.Substring(4)
    $expected[$relative] = (git -C $worktree rev-parse "HEAD:$file").Trim()
}

New-Item -ItemType Directory -Force $target | Out-Null
$removed = 0
foreach ($item in (Get-ChildItem -Recurse -File -LiteralPath $target)) {
    $relative = $item.FullName.Substring($target.Length + 1).Replace('\', '/')
    if (-not $expected.ContainsKey($relative)) {
        Remove-Item -LiteralPath $item.FullName
        $removed++
    }
}

$changed = 0
foreach ($relative in $expected.Keys) {
    $destination = Join-Path $target $relative
    if (Test-Path -LiteralPath $destination) {
        $sha = (git -C $root hash-object -- $destination).Trim()
        if ($sha -eq $expected[$relative]) {
            continue
        }
    }
    New-Item -ItemType Directory -Force (Split-Path -Parent $destination) | Out-Null
    Copy-Item -LiteralPath (Join-Path $worktree ('bin/' + $relative)) -Destination $destination -Force
    $changed++
}

git -C $Repo worktree remove --force $worktree | Out-Null
Write-Host ("已按 {0} 校验 {1} 个文件：写入 {2}，删除 {3}" -f $Baseline.Substring(0, 7), $expected.Count, $changed, $removed)
