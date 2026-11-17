# go test ./... во всех модулях (GOOS=windows).
# Модули находятся автоматически: каталог верхнего уровня с go.mod.
# Использование: .\scripts\test.ps1 [-Tag integration]

param(
    [string]$Tag = ''
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'dev-env.ps1')

$root = Split-Path -Parent $PSScriptRoot
$failed = @()

# Модули ищем на глубину до двух уровней: сервисы в корне и tools/loadgen.
$modules = Get-ChildItem $root -Recurse -Depth 2 -Filter 'go.mod' -File -ErrorAction SilentlyContinue |
    Where-Object { $_.FullName -notmatch '\\\.(gocache|gomodcache|gotmp|git)\\' } |
    ForEach-Object { $_.Directory } |
    Sort-Object FullName -Unique

foreach ($m in $modules) {
    Write-Host "== test: $($m.FullName.Substring($root.Length + 1)) =="
    Push-Location $m.FullName
    try {
        if ($Tag) { go test "-tags=$Tag" ./... } else { go test ./... }
        if ($LASTEXITCODE -ne 0) { $failed += $m.FullName.Substring($root.Length + 1) }
    } finally { Pop-Location }
}

if ($failed.Count -gt 0) {
    Write-Host ("TEST FAILED: " + ($failed -join ', '))
    exit 1
}
Write-Host 'TEST OK'