#Requires -Version 7
# ═══════════════════════════════════════════════════════════════════
# vbr_vm 대화형 시험 스크립트 (docs/vm/06-test-plan-report.md 9장)
#   .\demo.ps1          메뉴
#   .\demo.ps1 -Auto    자동 모드만 실행(가짜 XAPI, 실제 풀에 접속하지 않음)
# 구조는 ncc-xen backend/demo.ps1 (@043c6d5, CP-VM-13)을 따른다.
# 토큰과 XAPI 암호는 소스에 두지 않는다. 실행할 때 환경 변수나 화면 입력으로 받고 화면·파일에 남기지 않는다.
# ═══════════════════════════════════════════════════════════════════
param([switch]$Auto)

# ── 기본 환경(필요하면 고친다) ─────────────────────────────────────
$script:VBR_URL     = $(if ($env:VBR_VM_URL) { $env:VBR_VM_URL } else { 'http://127.0.0.1:8090' })
$script:MASTER_IP   = '192.168.102.53'
$script:VM_UUID     = '42097417-b9ee-2f97-8eeb-3612efbcb25d'   # 시험 VM(지우지 않는다)
$script:SR_UUID     = 'e4138437-7e20-480f-08b1-25e5b8b6027b'
$script:EXPORT_PATH = ''                                       # 비우면 서버 backup.base_path
$script:XAPI_USER   = 'root'
$script:KEYRING     = ''                                       # vbr_vm 의 crypto.keyring_file 과 같은 key.yaml
$script:BIN         = (Resolve-Path (Join-Path $PSScriptRoot '..\..\bin\windows-amd64') -ErrorAction SilentlyContinue).Path
if (-not $script:BIN) { $script:BIN = Join-Path $PSScriptRoot '..\..\bin\windows-amd64' }

$ModuleDir = Join-Path $PSScriptRoot 'demo_modules'
. (Join-Path $ModuleDir 'demo_common.ps1')
. (Join-Path $ModuleDir 'demo_auto.ps1')
. (Join-Path $ModuleDir 'demo_vm_backup.ps1')

if ($Auto) { Invoke-AutoTest; return }

# ── 토큰과 암호(실기 모드) ────────────────────────────────────────
function Initialize-Session {
    $script:VBR_URL = Get-Input 'vbr_vm 주소' $script:VBR_URL
    $h = Invoke-Vbr -Method GET -Path '/health' -NoAuth
    if ($h.Status -ne 200) { Write-Host "  ✗ $script:VBR_URL/health 응답 없음 (HTTP $($h.Status))" -ForegroundColor Red; return $false }
    Write-Host "  vbr_vm $($h.Body.version)  처리 중 $($h.Body.running), 대기 $($h.Body.queued), 인증 $($h.Body.authEnabled), 암호화 $($h.Body.cryptoEnabled)" -ForegroundColor Gray
    $script:VBR_TOKEN = $env:VBR_VM_API_TOKEN
    if ($h.Body.authEnabled -and -not $script:VBR_TOKEN) { $script:VBR_TOKEN = Read-Secret 'API 토큰(VBR_VM_API_TOKEN, 화면에 보이지 않음)' }
    $t = Invoke-Vbr -Method GET -Path '/api/v1/jobs?limit=1'
    if ($t.Status -ne 200) { Write-Host "  ✗ 토큰 확인 실패 HTTP $($t.Status) $($t.Body.error)" -ForegroundColor Red; return $false }
    $script:MASTER_IP = Get-Input '풀 마스터 주소' $script:MASTER_IP
    $script:XAPI_USER = Get-Input 'XAPI 계정' $script:XAPI_USER
    $plain = Read-Secret "XAPI 암호($script:XAPI_USER, 화면에 보이지 않음)"
    $script:CRYPTO_ON = [bool]$h.Body.cryptoEnabled
    if ($script:CRYPTO_ON) {
        $script:KEYRING = Get-Input 'key.yaml 경로(vbr_vm 과 같은 키링)' $script:KEYRING
        $script:XAPI_PASSWORD_VALUE = Protect-Password -Plain $plain -Keyring $script:KEYRING
        $script:WRONG_PASSWORD_ENC = Protect-Password -Plain ('wrong-' + [guid]::NewGuid().ToString('N')) -Keyring $script:KEYRING
        Write-Host '  암호를 ENC[...] 로 바꿔 보냅니다.' -ForegroundColor Gray
    } else {
        $script:XAPI_PASSWORD_VALUE = $plain
        Write-Host '  서버가 crypto.enabled=false 라 평문 암호를 보냅니다(시험용).' -ForegroundColor DarkYellow
    }
    $plain = $null
    return $true
}

function Show-Menu {
    Write-Host ''
    Write-Host "  ── vbr_vm 시험 ($script:VBR_URL)  PASS $script:PassCount / FAIL $script:FailCount" -ForegroundColor Cyan
    Write-Host '   1. 서비스 상태와 토큰        2. 전체 백업          3. 디스크 선택 백업'
    Write-Host '   4. 복구                      5. 검증               6. 동시 작업과 목록'
    Write-Host '   7. 처리 중 취소              8. 오류 응답          9. 정리 안내'
    Write-Host '   A. 자동 모드(가짜 XAPI)      0. 끝'
}

$ready = $false
while ($true) {
    Show-Menu
    $sel = Get-Input '번호'
    if ($sel -eq '0') { Write-Summary; break }
    if ($sel -match '^[aA]$') { Invoke-AutoTest; continue }
    if (-not $ready) { $ready = Initialize-Session; if (-not $ready) { continue } }
    switch ($sel) {
        '1' { $h = Invoke-Vbr -Method GET -Path '/health' -NoAuth; Test-Step 'health' ($h.Status -eq 200) ($h.Body | ConvertTo-Json -Compress) }
        '2' { Invoke-ExportMenu }
        '3' { Invoke-ExportMenu -Disks }
        '4' { Invoke-RestoreMenu }
        '5' { Invoke-VerifyMenu }
        '6' { Invoke-ConcurrentMenu }
        '7' { Invoke-CancelMenu }
        '8' { Invoke-ErrorMenu }
        '9' { Invoke-CleanupMenu }
        default { Write-Host '  모르는 번호' -ForegroundColor DarkYellow }
    }
}
