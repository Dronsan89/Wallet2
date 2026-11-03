# Локальное окружение сборки и тестов.
# Использование:  . .\scripts\dev-env.ps1
#
# Почему это обязательно в текущем окружении:
#  * в профиле `go env` прописан GOOS=linux — локальные сборки/тесты падают;
#  * кэши Go в AppData недоступны на запись, поэтому кэши лежат в репозитории;
#  * интернета нет — зависимости берём из локального file:// прокси.
#
# Важно:
#  * GOWORK=off, потому что в корне лежит go.work (workspace-режим несовместим с -mod=mod);
#  * GOPRIVATE не выставляем: он отключает file://-прокси (Go уходит в direct и висит);
#  * `go mod download` без аргументов здесь не работает (тянет весь граф, включая
#    нескачанные zip). go.sum наполняется обычным `go build ./...`.

$root = Split-Path -Parent $PSScriptRoot

$env:GOMODCACHE  = Join-Path $root '.gomodcache'
$env:GOCACHE     = Join-Path $root '.gocache'
$env:GOTMPDIR    = Join-Path $root '.gotmp'
$env:GOPROXY     = 'file:///C:/Users/user/go/pkg/mod/cache/download'
$env:GOSUMDB     = 'off'
$env:GOFLAGS     = '-mod=mod'
$env:GOWORK      = 'off'
$env:GOTOOLCHAIN = 'local'
$env:GOOS        = 'windows'
$env:GOARCH      = 'amd64'

foreach ($d in @($env:GOMODCACHE, $env:GOCACHE, $env:GOTMPDIR)) {
  if (-not (Test-Path $d)) { New-Item -ItemType Directory -Force -Path $d | Out-Null }
}

Write-Host "dev-env: GOOS=$env:GOOS GOARCH=$env:GOARCH GOWORK=$env:GOWORK"
Write-Host "dev-env: GOMODCACHE=$env:GOMODCACHE"
Write-Host "dev-env: GOPROXY=$env:GOPROXY"
