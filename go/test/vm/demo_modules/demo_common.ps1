# ═══════════════════════════════════════════════════════════════════
# demo_common.ps1 — vbr_vm 데모 공통 함수 (요청, 조회 대기, 판정, 입력)
# 구조는 ncc-xen backend/demo_modules/demo_common.ps1 (@043c6d5, CP-VM-13)을 따른다.
# 바뀐 점: 결과를 Middleware 로 받는 대신 조회 API(GET /api/v1/jobs/{taskId})로 기다린다.
#          토큰·암호를 소스에 두지 않는다(ncc-xen demo.ps1 의 DEFAULT_API_TOKEN 같은 값을 두지 않는다).
# demo.ps1 이 dot-source 로 읽는다. $script:VBR_URL, $script:VBR_TOKEN 은 demo.ps1 이 정한다.
# ═══════════════════════════════════════════════════════════════════

$script:PassCount = 0
$script:FailCount = 0

# Invoke-Vbr 는 vbr_vm API 를 부르고 {Status, Body} 를 돌려준다(HTTP 오류도 예외 없이 돌려준다).
function Invoke-Vbr {
    param(
        [Parameter(Mandatory)][string]$Method,
        [Parameter(Mandatory)][string]$Path,
        $Body = $null,
        [switch]$NoAuth,
        [string]$Token = $script:VBR_TOKEN
    )
    $headers = @{}
    if (-not $NoAuth -and $Token) { $headers['X-Internal-Token'] = $Token }
    $p = @{ Uri = "$script:VBR_URL$Path"; Method = $Method; Headers = $headers; SkipHttpErrorCheck = $true; TimeoutSec = 30 }
    if ($null -ne $Body) {
        $p.Body = if ($Body -is [string]) { $Body } else { $Body | ConvertTo-Json -Depth 10 -Compress }
        $p.ContentType = 'application/json; charset=utf-8'
    }
    try {
        $r = Invoke-WebRequest @p
    } catch {
        return [pscustomobject]@{ Status = 0; Body = @{ error = $_.Exception.Message } }
    }
    $obj = $null
    try { $obj = $r.Content | ConvertFrom-Json -AsHashtable } catch { $obj = @{ raw = $r.Content } }
    return [pscustomobject]@{ Status = [int]$r.StatusCode; Body = $obj }
}

# New-TaskId 는 접두어가 붙은 taskId 를 만든다(1~64자, 영문·숫자·-·_).
function New-TaskId { param([string]$Prefix = 'demo') return "$Prefix-" + [guid]::NewGuid().ToString('N').Substring(0, 12) }

# New-VbrJob 은 규약 1 요청 본문을 만든다.
function New-VbrJob {
    param(
        [Parameter(Mandatory)][string]$TaskId,
        [Parameter(Mandatory)][ValidateSet('export', 'restore', 'verify')][string]$Action,
        [hashtable]$Options = @{},
        [string]$TargetUUID = $script:VM_UUID,
        [string]$MasterIP = $script:MASTER_IP,
        [string]$Password = $script:XAPI_PASSWORD_VALUE
    )
    $payload = [ordered]@{ targetType = $(if ($Action -eq 'export') { 'VM' } else { 'FILE' }); options = $Options }
    if ($Action -eq 'export') { $payload.targetUUID = $TargetUUID }
    if ($Action -ne 'verify') {
        $payload.masterIp = $MasterIP
        $payload.credentials = @{ username = $script:XAPI_USER; password = $Password }
    }
    return [ordered]@{ taskId = $TaskId; jobType = 'VM_BACKUP'; action = $Action; executionType = 'async'; payload = $payload }
}

# Send-VbrJob 은 요청을 보내고 202 면 참이다. 요청 본문은 암호를 가려 보여 준다.
function Send-VbrJob {
    param([Parameter(Mandatory)]$Job, [string]$Desc = '')
    if ($Desc) { Write-Host "  → $Desc" -ForegroundColor White }
    $shown = $Job | ConvertTo-Json -Depth 10 | ConvertFrom-Json -AsHashtable
    if ($shown.payload.credentials) { $shown.payload.credentials.password = '***' }
    Write-Host "  ┌─ Request" -ForegroundColor Gray
    ($shown | ConvertTo-Json -Depth 10) -split "`n" | ForEach-Object { Write-Host "  │ $_" -ForegroundColor Gray }
    Write-Host "  └─" -ForegroundColor Gray
    $r = Invoke-Vbr -Method POST -Path '/api/v1/jobs' -Body $Job
    if ($r.Status -eq 202) {
        $msg = if ($r.Body.message) { " ($($r.Body.message))" } else { '' }
        Write-Host "  ✔ 202 ACCEPTED$msg" -ForegroundColor Green
        return $true
    }
    Write-Host "  ✗ HTTP $($r.Status) $($r.Body.code) $($r.Body.error)" -ForegroundColor Red
    return $false
}

# Wait-VbrJob 은 작업이 끝날 때까지(또는 Until 상태가 될 때까지) 조회하고 마지막 상태를 돌려준다.
function Wait-VbrJob {
    param([Parameter(Mandatory)][string]$TaskId, [int]$TimeoutSec = 7200, [string[]]$Until = @(), [switch]$Quiet)
    $deadline = (Get-Date).AddSeconds($TimeoutSec)
    $last = ''
    while ((Get-Date) -lt $deadline) {
        $r = Invoke-Vbr -Method GET -Path "/api/v1/jobs/$TaskId"
        if ($r.Status -ne 200) { Write-Host "  ✗ 조회 실패 HTTP $($r.Status) $($r.Body.code)" -ForegroundColor Red; return $r.Body }
        $b = $r.Body
        $line = "$($b.status) $($b.step) $($b.percent)%"
        if (-not $Quiet -and $line -ne $last) {
            Write-Host "    · $line  $($b.payload.message)" -ForegroundColor DarkGray
            $last = $line
        }
        if ($Until -contains $b.status) { return $b }
        if ($Until.Count -eq 0 -and @('succeeded', 'failed', 'canceled', 'interrupted') -contains $b.status) { return $b }
        Start-Sleep -Milliseconds 500
    }
    Write-Host "  ✗ 제한 시간($TimeoutSec 초) 안에 끝나지 않음" -ForegroundColor Red
    return $null
}

# Show-VbrResult 는 작업 상태를 보기 좋게 출력한다.
function Show-VbrResult {
    param($Body)
    if (-not $Body) { return }
    $color = switch ($Body.status) { 'succeeded' { 'Green' } 'canceled' { 'Yellow' } default { 'Red' } }
    Write-Host "  ┌─ Result: $($Body.status) ($($Body.result))  $($Body.payload.message)" -ForegroundColor $color
    if ($Body.error) { Write-Host "  │ error.code   = $($Body.error.code)" -ForegroundColor $color; Write-Host "  │ error.detail = $($Body.error.detail)" -ForegroundColor $color }
    foreach ($k in ($Body.payload.options.Keys | Sort-Object)) {
        $v = $Body.payload.options[$k]
        if ($v -is [System.Collections.IDictionary] -or $v -is [System.Array]) { $v = $v | ConvertTo-Json -Depth 5 -Compress }
        Write-Host "  │ $k = $v" -ForegroundColor Gray
    }
    Write-Host "  └─" -ForegroundColor $color
}

# Test-Step 은 판정 한 줄을 출력하고 집계한다.
function Test-Step {
    param([Parameter(Mandatory)][string]$Name, [Parameter(Mandatory)][bool]$Ok, [string]$Detail = '')
    if ($Ok) { $script:PassCount++; Write-Host ("  PASS  {0}  {1}" -f $Name, $Detail) -ForegroundColor Green }
    else { $script:FailCount++; Write-Host ("  FAIL  {0}  {1}" -f $Name, $Detail) -ForegroundColor Red }
}

function Get-Input {
    param([string]$Prompt, [string]$Default = '')
    try { $Host.UI.RawUI.FlushInputBuffer() } catch { }   # 메뉴 번호가 다음 입력에 새는 것을 막는다(ncc-xen 2026-08-13)
    if ($Default -ne '') {
        $v = (Read-Host "  ▷ $Prompt [$Default]").Trim()
        if ($v -eq '') { return $Default }
        return $v
    }
    return (Read-Host "  ▷ $Prompt").Trim()
}

function Confirm-Run { param([string]$Msg = '실행할까요? (Enter=예 / n=아니오)') $a = (Read-Host "  ▷ $Msg").Trim(); return ($a -eq '' -or $a -match '^[yY]') }

function Write-MenuHeader {
    param([string]$Title)
    Write-Host ''
    Write-Host '  ╔══════════════════════════════════════════════════╗' -ForegroundColor Cyan
    Write-Host "  ║  $Title" -ForegroundColor Cyan
    Write-Host '  ╚══════════════════════════════════════════════════╝' -ForegroundColor Cyan
}

# Read-Secret 은 화면에 보이지 않게 입력받아 평문을 돌려준다(메모리에만 둔다).
function Read-Secret { param([string]$Prompt) $s = Read-Host "  ▷ $Prompt" -AsSecureString; return [System.Net.NetworkCredential]::new('', $s).Password }

# Protect-Password 는 vbr_secret encrypt 로 ENC[...] 를 만든다. 평문은 표준 입력으로만 넘긴다.
function Protect-Password {
    param([Parameter(Mandatory)][string]$Plain, [Parameter(Mandatory)][string]$Keyring)
    $exe = Join-Path $script:BIN 'vbr_secret.exe'
    if (-not (Test-Path $exe)) { throw "vbr_secret.exe not found: $exe (go\build.ps1 all 로 빌드)" }
    $enc = $Plain | & $exe encrypt $Keyring
    if ($LASTEXITCODE -ne 0 -or -not ($enc -like 'ENC`[*')) { throw "vbr_secret encrypt failed" }
    return $enc
}

function Write-Summary {
    Write-Host ''
    $color = if ($script:FailCount -eq 0) { 'Green' } else { 'Red' }
    Write-Host ("  ══ 결과: PASS {0}, FAIL {1}" -f $script:PassCount, $script:FailCount) -ForegroundColor $color
}
