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

$modules = Get-ChildItem $root -Directory |
    Where-Object { Test-Path (Join-Path $_.FullName 'go.mod') } |
    Sort-Object Name

foreach ($m in $modules) {
    Write-Host "== test: $($m.Name) =="
    Push-Location $m.FullName
    try {
        if ($Tag) { go test "-tags=$Tag" ./... } else { go test ./... }
        if ($LASTEXITCODE -ne 0) { $failed += $m.Name }
    } finally { Pop-Location }
}

if ($failed.Count -gt 0) {
    Write-Host ("TEST FAILED: " + ($failed -join ', '))
    exit 1
}
Write-Host 'TEST OK'