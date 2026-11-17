# Сквозная проверка стека Wallet2.
#
# Проверяет: пробы, регистрацию/логин, баланс, пополнение (малое и крупное),
# вывод, курсы, обмен, негативные коды (401/400) и доставку события о крупной
# операции в MongoDB (notification) и ClickHouse (analytics).
#
# Пример:
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\smoke.ps1
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\smoke.ps1 -SkipStorageChecks

[CmdletBinding()]
param(
    [string]$WalletUrl = 'http://127.0.0.1:8080',
    [string]$NotificationUrl = 'http://127.0.0.1:8082',
    [string]$AnalyticsUrl = 'http://127.0.0.1:8083',
    [string]$ClickHouseUrl = 'http://127.0.0.1:8123',
    [int]$EventWaitSeconds = 8,
    [switch]$SkipStorageChecks
)

$ErrorActionPreference = 'Stop'
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
        $params = @{ Method = $Method; Uri = $Url; Headers = $headers; TimeoutSec = 20 }
        if ($null -ne $Body) {
            $params['ContentType'] = 'application/json'
            $params['Body'] = ($Body | ConvertTo-Json -Compress)
        }
        $response = Invoke-WebRequest @params
        $data = $null
        if ($response.Content) { $data = $response.Content | ConvertFrom-Json }
        return @{ Code = [int]$response.StatusCode; Data = $data }
    }
    catch {
        $code = 0
        if ($_.Exception.Response) { $code = [int]$_.Exception.Response.StatusCode }
        return @{ Code = $code; Data = $null }
    }
}

Write-Host "Wallet2 smoke: $WalletUrl" -ForegroundColor Cyan

# --- 1. Пробы -------------------------------------------------------------
Write-Host '1. Пробы'
foreach ($probe in @(
        @{ Url = "$WalletUrl/health"; Name = 'wallet /health' },
        @{ Url = "$NotificationUrl/health"; Name = 'notification /health' },
        @{ Url = "$NotificationUrl/ready"; Name = 'notification /ready' },
        @{ Url = "$AnalyticsUrl/health"; Name = 'analytics /health' },
        @{ Url = "$AnalyticsUrl/ready"; Name = 'analytics /ready' })) {
    $r = Call -Method 'GET' -Url $probe.Url
    Assert ($r.Code -eq 200) "$($probe.Name) -> $($r.Code)"
}

# --- 2. Регистрация и логин ----------------------------------------------
Write-Host '2. Регистрация и логин'
$user = "smoke_$(Get-Date -Format 'HHmmss')"
$password = 'secret123'

$r = Call -Method 'POST' -Url "$WalletUrl/api/v1/register" -Body @{ username = $user; password = $password; email = "$user@example.com" }
Assert ($r.Code -eq 201) "register -> $($r.Code)"

$r = Call -Method 'POST' -Url "$WalletUrl/api/v1/register" -Body @{ username = $user; password = $password; email = "$user@example.com" }
Assert ($r.Code -eq 400) "повторная регистрация -> 400 ($($r.Code))"

$r = Call -Method 'POST' -Url "$WalletUrl/api/v1/login" -Body @{ username = $user; password = $password }
Assert ($r.Code -eq 200 -and $r.Data.token) 'login -> 200 + token'
$token = $r.Data.token

$r = Call -Method 'POST' -Url "$WalletUrl/api/v1/login" -Body @{ username = $user; password = 'wrong' }
Assert ($r.Code -eq 401) "неверный пароль -> 401 ($($r.Code))"

# --- 3. Баланс и операции -------------------------------------------------
Write-Host '3. Баланс и операции'
$r = Call -Method 'GET' -Url "$WalletUrl/api/v1/balance" -Token $token
Assert ($r.Code -eq 200 -and $r.Data.balance) 'balance -> 200'

$r = Call -Method 'POST' -Url "$WalletUrl/api/v1/wallet/deposit" -Token $token -Body @{ amount = 100; currency = 'USD' }
Assert ($r.Code -eq 200) "deposit 100 USD -> $($r.Code)"

$r = Call -Method 'POST' -Url "$WalletUrl/api/v1/wallet/withdraw" -Token $token -Body @{ amount = 50; currency = 'USD' }
Assert ($r.Code -eq 200) "withdraw 50 USD -> $($r.Code)"

$r = Call -Method 'POST' -Url "$WalletUrl/api/v1/wallet/withdraw" -Token $token -Body @{ amount = 1000000; currency = 'USD' }
Assert ($r.Code -eq 400) "вывод больше баланса -> 400 ($($r.Code))"

$r = Call -Method 'GET' -Url "$WalletUrl/api/v1/exchange/rates" -Token $token
Assert ($r.Code -eq 200 -and $r.Data.rates.USD -gt 0 -and $r.Data.rates.RUB -eq 1) "rates -> $($r.Code) USD=$($r.Data.rates.USD)"

$r = Call -Method 'POST' -Url "$WalletUrl/api/v1/exchange" -Token $token -Body @{ from_currency = 'USD'; to_currency = 'EUR'; amount = 10 }
Assert ($r.Code -eq 200) "exchange 10 USD -> EUR -> $($r.Code)"

$r = Call -Method 'GET' -Url "$WalletUrl/api/v1/balance"
Assert ($r.Code -eq 401) "balance без токена -> 401 ($($r.Code))"

$r = Call -Method 'POST' -Url "$WalletUrl/api/v1/wallet/deposit" -Token $token -Body @{ amount = 1; currency = 'GBP' }
Assert ($r.Code -eq 400) "deposit в GBP -> 400 ($($r.Code))"

# --- 4. Событие о крупной операции ---------------------------------------
Write-Host '4. Крупная операция (45000 RUB > 30000)'
$r = Call -Method 'POST' -Url "$WalletUrl/api/v1/wallet/deposit" -Token $token -Body @{ amount = 500; currency = 'USD' }
Assert ($r.Code -eq 200) "deposit 500 USD -> $($r.Code)"

Write-Host "   ждём доставку события (${EventWaitSeconds}s)"
Start-Sleep -Seconds $EventWaitSeconds

$n = Call -Method 'GET' -Url "$NotificationUrl/stats"
$nom = 0
if ($n.Data) { $nom = [int]$n.Data.received }
Assert ($nom -ge 1) "notification получил событий: $nom"

$a = Call -Method 'GET' -Url "$AnalyticsUrl/stats"
$anom = 0
if ($a.Data) { $anom = [int]$a.Data.received }
Assert ($anom -ge 1) "analytics получил событий: $anom"

$s = Call -Method 'GET' -Url ('{0}/api/v1/analytics/summary?period=5m' -f $AnalyticsUrl)
Assert ($s.Code -eq 200 -and [int]$s.Data.total -ge 1) "summary.total = $($s.Data.total)"

$t = Call -Method 'GET' -Url ('{0}/api/v1/analytics/timeseries?period=5m&step=30s' -f $AnalyticsUrl)
Assert ($t.Code -eq 200) "timeseries -> $($t.Code)"

$bad = Call -Method 'GET' -Url ('{0}/api/v1/analytics/summary?period=42' -f $AnalyticsUrl)
Assert ($bad.Code -eq 400) "неизвестный период -> 400 ($($bad.Code))"

$bad = Call -Method 'GET' -Url ('{0}/api/v1/analytics/timeseries?period=1m&step=5m' -f $AnalyticsUrl)
Assert ($bad.Code -eq 400) "шаг больше периода -> 400 ($($bad.Code))"

# --- 5. Проверка хранилищ -------------------------------------------------
if (-not $SkipStorageChecks) {
    Write-Host '5. Хранилища'

    $chUrl = "$ClickHouseUrl/?query=" + [uri]::EscapeDataString('SELECT count() FROM analytics.events')
    try {
        $count = Invoke-RestMethod -Uri $chUrl -TimeoutSec 10
        Assert ([int]$count -ge 1) "ClickHouse analytics.events: строк = $count"
    }
    catch { Bad "ClickHouse недоступен: $($_.Exception.Message.Split([char]10)[0])" }

    if (Get-Command docker -ErrorAction SilentlyContinue) {
        try {
            $doc = docker compose exec -T mongo mongosh notification --quiet --eval 'db.transfers.countDocuments({})' 2>$null
            if ($LASTEXITCODE -eq 0 -and $doc) {
                Assert ([int]$doc -ge 1) "MongoDB notification.transfers: документов = $doc"
            }
            else {
                Bad 'MongoDB: не удалось выполнить mongosh (стек поднят?)'
            }
        }
        catch { Bad "MongoDB: $($_.Exception.Message.Split([char]10)[0])" }
    }
    else {
        Write-Host '   docker не найден — проверка MongoDB пропущена' -ForegroundColor Yellow
    }
}

# --- Итог -----------------------------------------------------------------
Write-Host ''
if ($script:failed -eq 0) {
    Write-Host "SMOKE OK: проверок $($script:passed), ошибок 0" -ForegroundColor Green
    exit 0
}

Write-Host "SMOKE FAIL: проверок $($script:passed), ошибок $($script:failed)" -ForegroundColor Red
exit 1
