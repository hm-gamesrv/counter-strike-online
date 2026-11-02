# 校验 .ref 参照仓库是否停在 AGENTS.md 指定的基线提交。
# 用法：pwsh scripts/check-ref.ps1
$baseline = "12644f2adc0708e2d8b74f7dd6a4c3c29d3e1140"
$dir = Join-Path (Split-Path -Parent $PSScriptRoot) ".ref/CSNZ_Server"
if (-not (Test-Path $dir)) {
    Write-Error "缺少参照仓库: $dir"
    exit 1
}
$head = (git -C $dir rev-parse HEAD).Trim()
if ($head -ne $baseline) {
    Write-Error ("参照仓库不在基线: {0} != {1}`n修复: git -C .ref/CSNZ_Server checkout {1}" -f $head, $baseline)
    exit 1
}
"参照仓库已在基线 {0}" -f $baseline.Substring(0, 7)