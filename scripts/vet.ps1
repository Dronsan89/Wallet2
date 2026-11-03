# go vet ./... во всех модулях.
# Модули находятся автоматически: каталог верхнего уровня с go.mod.
# Использование: .\scripts\vet.ps1

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'dev-env.ps1')

$root = Split-Path -Parent $PSScriptRoot
$failed = @()

$modules = Get-ChildItem $root -Directory |
    Where-Object { Test-Path (Join-Path $_.FullName 'go.mod') } |
    Sort-Object Name

foreach ($m in $modules) {
    Write-Host "== vet: $($m.Name) =="
    Push-Location $m.FullName
    try {
        go vet ./...
        if ($LASTEXITCODE -ne 0) { $failed += $m.Name }
    } finally { Pop-Location }
}

if ($failed.Count -gt 0) {
    Write-Host ("VET FAILED: " + ($failed -join ', '))
    exit 1
}
Write-Host 'VET OK'