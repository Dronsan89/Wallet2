# Сборка всех модулей репозитория.
#  * сервис (есть каталог cmd/) -> linux/amd64 бинарь `main` (команда из ТЗ);
#  * библиотечный модуль (proto-exchange) -> проверка компиляции.
# Модули находятся автоматически: каталог верхнего уровня с go.mod.
# Использование: .\scripts\build.ps1 [-Windows]

param(
    [switch]$Windows
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'dev-env.ps1')

$root = Split-Path -Parent $PSScriptRoot
$targetOS = if ($Windows) { 'windows' } else { 'linux' }
$failed = @()

$modules = Get-ChildItem $root -Directory |
    Where-Object { Test-Path (Join-Path $_.FullName 'go.mod') } |
    Sort-Object Name

foreach ($m in $modules) {
    Push-Location $m.FullName
    try {
        if (Test-Path (Join-Path $m.FullName 'cmd')) {
            Write-Host "== $($m.Name) ($targetOS/amd64) =="
            $env:GOOS = $targetOS
            $env:GOARCH = 'amd64'
            $bin = if ($targetOS -eq 'windows') { 'main.exe' } else { 'main' }
            go build -trimpath -ldflags='-s -w' -o $bin ./cmd
        } else {
            Write-Host "== $($m.Name) =="
            go build ./...
        }

        if ($LASTEXITCODE -ne 0) { $failed += $m.Name }
    } finally {
        $env:GOOS = 'windows'
        Pop-Location
    }
}

if ($failed.Count -gt 0) {
    Write-Host ("BUILD FAILED: " + ($failed -join ', '))
    exit 1
}
Write-Host 'BUILD OK'