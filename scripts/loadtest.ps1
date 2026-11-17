# Нагрузочная проверка стека Wallet2.
#
# Что проверяется:
#   1. Kafka: отправка N событий и скорость их обработки консьюмерами
#      (дренаж оценивается по счётчику received в analytics и по lag групп).
#   2. HTTP: задержка ручки кошелька — p50/p95/p99 под параллельной нагрузкой.
#
# Пороговые значения по ТЗ: не менее 1000 сообщений/с и p95 HTTP не более 200 мс.
#
# Примеры:
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\loadtest.ps1
#   ... -Events 100000 -HttpRequests 5000 -HttpConcurrency 100
#   ... -SkipKafka      # только HTTP
#   ... -SkipHttp       # только Kafka

[CmdletBinding()]
param(
    [int]$Events = 50000,
    [int]$HttpRequests = 2000,
    [int]$HttpConcurrency = 50,
    [int]$Rate = 0,
    [int]$TargetMsgPerSec = 1000,
    [int]$TargetP95Ms = 200,
    [string]$WalletUrl = 'http://127.0.0.1:8080',
    [string]$AnalyticsUrl = 'http://127.0.0.1:8083',
    [string]$KafkaExternal = 'localhost:19092',
    [switch]$SkipKafka,
    [switch]$SkipHttp
)

$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
$loadgen = Join-Path $root 'tools\loadgen'

$script:failed = 0
$script:passed = 0

function Ok([string]$message) {
    Write-Host "  ok   $message" -ForegroundColor Green
    $script:passed++
}

function Bad([string]$message) {
    Write-Host "  FAIL $message" -ForegroundColor Red
    $script:failed++
}

function Assert([bool]$condition, [string]$message) {
    if ($condition) { Ok $message } else { Bad $message }
}

function Call {
    param([string]$Method, [string]$Url, $Body, [string]$Token)

    $headers = @{}
    if ($Token) { $headers['Authorization'] = "Bearer $Token" }

    try {
        $params = @{ Method = $Method; Uri = $Url; Headers = $headers; TimeoutSec = 30 }
        if ($null -ne $Body) {
            $params['ContentType'] = 'application/json'
            $params['Body'] = ($Body | ConvertTo-Json -Compress)
        }
        $response = Invoke-WebRequest @params
        return @{ Code = [int]$response.StatusCode; Data = ($response.Content | ConvertFrom-Json) }
    }
    catch {
        $code = 0
        if ($_.Exception.Response) { $code = [int]$_.Exception.Response.StatusCode }
        return @{ Code = $code; Data = $null }
    }
}

Write-Host 'Wallet2 loadtest' -ForegroundColor Cyan
Write-Host "  пороги: >= $TargetMsgPerSec msg/s, p95 <= $TargetP95Ms ms"

# Окружение Go берём из общего скрипта (кэши, офлайн-прокси, GOOS).
. (Join-Path $PSScriptRoot 'dev-env.ps1') | Out-Null

# --- 1. Kafka -------------------------------------------------------------
if (-not $SkipKafka) {
    Write-Host "1. Kafka: $Events событий в wallet.transactions"

    $before = Call -Method 'GET' -Url "$AnalyticsUrl/stats"
    if ($before.Code -ne 200) { Bad "analytics недоступна ($($before.Code))" }
    $baseline = [int]$before.Data.received

    $produceSeconds = 0
    $produceLine = ''

    Push-Location $loadgen
    try {
        $rateArgs = @()
        if ($Rate -gt 0) { $rateArgs = @('-rate', "$Rate") }
        $output = & go run . -mode kafka -events $Events -brokers $KafkaExternal @rateArgs 2>&1
        $produceLine = ($output | Out-String)
    }
    finally { Pop-Location }

    Write-Host "     $($produceLine.Trim())"

    if ($produceLine -match 'elapsed=([\d\.]+)s') { $produceSeconds = [double]$Matches[1] }
    Assert ($produceLine -match 'kafka: produced=') 'события отправлены'

    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    $drained = $false
    $processed = 0

    for ($i = 0; $i -lt 900; $i++) {
        $now = Call -Method 'GET' -Url "$AnalyticsUrl/stats"
        if ($now.Code -eq 200) {
            $processed = [int]$now.Data.received - $baseline
            if ($processed -ge $Events) { $drained = $true; break }
        }
        Start-Sleep -Milliseconds 200
    }
    $sw.Stop()

    Assert $drained "консьюмеры обработали $processed из $Events событий за $([math]::Round($sw.Elapsed.TotalSeconds,1)) c"

    $tailSeconds = [math]::Max($sw.Elapsed.TotalSeconds, 0.001)
    $cycleSeconds = $produceSeconds + $tailSeconds
    $cycleRate = [math]::Round($Events / [math]::Max($cycleSeconds, 0.001))

    Write-Host ("     отправка: {0:N3} c; дообработка остатка: {1:N2} c; полный цикл: {2:N3} c => {3} msg/s" -f `
            $produceSeconds, $tailSeconds, $cycleSeconds, $cycleRate)

    if ($Rate -gt 0) {
        # Режим поддержания темпа: важнее выдержать заданную скорость без роста lag.
        Assert ($produceSeconds -ge ($Events / $Rate * 0.9)) "темп удержан: $([math]::Round($produceSeconds,2)) c при $Rate msg/s"
    }
    else {
        Assert ($cycleRate -ge $TargetMsgPerSec) "полный цикл $cycleRate msg/s >= $TargetMsgPerSec"
    }

    if (Get-Command docker -ErrorAction SilentlyContinue) {
        try {
            $lagOutput = docker compose -f (Join-Path $root 'docker-compose.yml') exec -T kafka `
                /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group gw-analytics 2>$null
            $lag = 0
            foreach ($row in ($lagOutput | Where-Object { $_ -match '^gw-analytics\s' })) {
                $lag += [int](($row -split '\s+')[5])
            }
            Assert ($lag -eq 0) "lag группы gw-analytics = $lag"
        }
        catch { Write-Host '     lag не проверен (docker недоступен)' -ForegroundColor Yellow }
    }
    else {
        Write-Host '     docker не найден — проверка lag пропущена' -ForegroundColor Yellow
    }
}

# --- 2. HTTP --------------------------------------------------------------
if (-not $SkipHttp) {
    Write-Host "2. HTTP: $HttpRequests запросов, $HttpConcurrency потоков"

    $user = "load_$(Get-Date -Format 'HHmmss')"
    $null = Call -Method 'POST' -Url "$WalletUrl/api/v1/register" -Body @{ username = $user; password = 'secret123'; email = "$user@example.com" }
    $login = Call -Method 'POST' -Url "$WalletUrl/api/v1/login" -Body @{ username = $user; password = 'secret123' }

    if ($login.Code -ne 200) {
        Bad "логин для нагрузки не удался ($($login.Code))"
    }
    else {
        $null = Call -Method 'POST' -Url "$WalletUrl/api/v1/wallet/deposit" -Token $login.Data.token -Body @{ amount = 100; currency = 'USD' }

        Push-Location $loadgen
        try {
            $output = & go run . -mode http -url "$WalletUrl/api/v1/balance" -token $login.Data.token `
                -n $HttpRequests -c $HttpConcurrency 2>&1
            $httpLine = ($output | Out-String)
        }
        finally { Pop-Location }

        Write-Host "     $($httpLine.Trim())"

        Assert ($httpLine -match 'http: requests=') 'нагрузка выполнена'
        Assert ($httpLine -match 'errors=0') 'ошибок нет'

        if ($httpLine -match 'p95=(\d+)ms') {
            $p95 = [int]$Matches[1]
            Assert ($p95 -le $TargetP95Ms) "p95 = $p95 ms <= $TargetP95Ms ms"
        }
        else {
            Bad 'не удалось разобрать p95'
        }
    }
}

# --- Итог -----------------------------------------------------------------
Write-Host ''
if ($script:failed -eq 0) {
    Write-Host "LOADTEST OK: проверок $($script:passed), ошибок 0" -ForegroundColor Green
    exit 0
}

Write-Host "LOADTEST FAIL: проверок $($script:passed), ошибок $($script:failed)" -ForegroundColor Red
exit 1
