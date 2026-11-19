# ═══════════════════════════════════════════════════════════════════
# NCC-Xen Backend — 대화형 테스트 스크립트
# 실행: .\demo.ps1
# ═══════════════════════════════════════════════════════════════════

# ── 기본 환경 설정 ────────────────────────────────────────────────
$BACKEND      = "http://localhost:8080"
$MASTER_IP    = "192.168.102.51"
$POOL_UUID    = "bc114e8e-4ecd-6021-42f2-981218351a06"
$VM_UUID      = "a04a6e8a-802f-a634-b722-00cf1f4eb76e"
$SR_UUID      = "da6445bc-9e47-c18c-e75c-cbfde2f90322"
$NETWORK_UUID = "aa2e831f-8154-a2e2-3e23-75f99a92156f"
# 아래 UUID들은 DEVICE_QUERY 조회 후 설정하세요
$PIF_UUID      = ""
$VIF_UUID      = ""
$VBD_UUID      = ""
$PBD_UUID      = ""
# 아래 UUID들은 VDI_QUERY / SNAPSHOT_QUERY 조회 후 설정하세요
$VDI_UUID      = ""
$SNAPSHOT_UUID = ""

# ── API 인증 토큰 (🆕 2026-10-29, 🔄 2026-11-02 키 이름 변경) ──────
# Backend가 api_auth.enabled=true(sysconfig.yaml)인 경우 /api/v1/*, /debug/* 요청에
# X-Internal-Token 헤더가 필요합니다(값 = api_auth.NCC_XEN_API_TOKEN, Middleware → Backend 토큰).
# 우선순위: 환경 변수 NCC_XEN_API_TOKEN → ..\sysconfig.yaml의 api_auth.NCC_XEN_API_TOKEN
# → 없음(인증 없이 호출). sysconfig 값이 암호문(ENC[...])이면 demo가 복호화할 수 없으므로
# 직접 입력받습니다(Enter=헤더 없이 진행). 토큰 값은 화면에 출력하지 않습니다.
# 🆕 2026-11-03 테스트 환경 기본 토큰 — sysconfig 값이 암호문(ENC)이거나 비어 있으면 입력 대신 이 값을 사용.
# 환경 변수 NCC_XEN_API_TOKEN이 있으면 그 값이 우선. 빈 문자열로 두면 기존처럼 입력받음.
$DEFAULT_API_TOKEN = "5aa6f28cf5c2c8adabaff54a975188f6768786486ebd8f094d6f4ff599e52a47"
$API_TOKEN = $env:NCC_XEN_API_TOKEN
if (-not $API_TOKEN) {
    $sysCfg = Join-Path $PSScriptRoot "..\sysconfig.yaml"
    if (Test-Path -LiteralPath $sysCfg) {
        $inAuth = $false
        foreach ($line in Get-Content -LiteralPath $sysCfg -Encoding UTF8) {
            if ($line -match '^api_auth:\s*$') { $inAuth = $true; continue }
            if ($inAuth -and $line -match '^\S') { break }   # 다음 최상위 키 → 블록 끝
            if ($inAuth -and $line -match '^\s+NCC_XEN_API_TOKEN:\s*["'']?([^"''#\s]*)') { $API_TOKEN = $Matches[1]; break }
        }
    }
    if ($DEFAULT_API_TOKEN -and (-not $API_TOKEN -or $API_TOKEN -like 'ENC`[*')) {
        $API_TOKEN = $DEFAULT_API_TOKEN
        Write-Host "  demo.ps1 기본 토큰(DEFAULT_API_TOKEN) 사용" -ForegroundColor Gray
    }
    if ($API_TOKEN -like 'ENC`[*') {
        Write-Host "  sysconfig.yaml의 NCC_XEN_API_TOKEN이 암호문이라 demo에서 읽을 수 없습니다." -ForegroundColor DarkYellow
        Write-Host "  (환경 변수 NCC_XEN_API_TOKEN을 설정하면 이 입력을 건너뜁니다)" -ForegroundColor Gray
        $sec = Read-Host "  ▷ API 토큰 입력 (Enter=헤더 없이 진행)" -AsSecureString
        $API_TOKEN = [System.Net.NetworkCredential]::new("", $sec).Password
    }
}
# 🆕 2026-11-03: 붙여넣기 실수 방어 — 앞뒤 공백·따옴표, 로그에서 함께 복사된 "NCC_XEN_API_TOKEN=" 접두어 제거
if ($API_TOKEN) {
    $API_TOKEN = $API_TOKEN.Trim().Trim('"', "'")
    if ($API_TOKEN -like 'NCC_XEN_API_TOKEN=*') { $API_TOKEN = $API_TOKEN.Substring('NCC_XEN_API_TOKEN='.Length).Trim().Trim('"', "'") }
}
$API_HEADERS = @{}
if ($API_TOKEN) { $API_HEADERS["X-Internal-Token"] = $API_TOKEN }
# 🆕 2026-11-03: 입력 확인용 — 값 전체 대신 길이와 앞뒤 4자만 표시(서버 로그 값과 비교)
if ($API_TOKEN) {
    $tokHint = if ($API_TOKEN.Length -ge 8) { $API_TOKEN.Substring(0, 4) + "..." + $API_TOKEN.Substring($API_TOKEN.Length - 4) } else { "(8자 미만)" }
    Write-Host "  API 토큰 적용: $($API_TOKEN.Length)자 $tokHint" -ForegroundColor Gray
} else {
    Write-Host "  API 토큰 없음 — X-Internal-Token 헤더 없이 호출" -ForegroundColor DarkYellow
}


# ─────────────────────────────────────────────────────────────────
# 모듈 로드 (🆕 2026-09-01: 기능별 분리 — 이 파일들은 demo_modules\ 하위에
# 위치해야 합니다. $PSScriptRoot 기준 상대경로라 실행 위치와 무관하게 동작.)
# ─────────────────────────────────────────────────────────────────
$ModuleDir = Join-Path $PSScriptRoot "demo_modules"
. (Join-Path $ModuleDir "demo_common.ps1")
. (Join-Path $ModuleDir "demo_vm_create.ps1")
. (Join-Path $ModuleDir "demo_query.ps1")
. (Join-Path $ModuleDir "demo_power_control.ps1")
. (Join-Path $ModuleDir "demo_vm_backup.ps1")
. (Join-Path $ModuleDir "demo_vm_control.ps1")
. (Join-Path $ModuleDir "demo_infra_control.ps1")
. (Join-Path $ModuleDir "demo_pool_network.ps1")
. (Join-Path $ModuleDir "demo_analysis_query.ps1")

function Show-MainMenu {
    Clear-Host
    Write-Host ""
    Write-Host "  ╔══════════════════════════════════════════════════════╗" -ForegroundColor Cyan
    Write-Host "  ║   NCC-Xen Backend  대화형 테스트                    ║" -ForegroundColor Cyan
    Write-Host "  ╠══════════════════════════════════════════════════════╣" -ForegroundColor Cyan
    Write-Host "  ║  $BACKEND  /  $MASTER_IP" -ForegroundColor Gray
    Write-Host "  ╠══════════════════════════════════════════════════════╣" -ForegroundColor Cyan
    Write-Host "  ║  [ VM 생성 ]                                        ║" -ForegroundColor Yellow
    Write-Host "  ║   0. VM_CREATE        — clone 기반 VM 생성          ║" -ForegroundColor Yellow
    Write-Host "  ║  [ 동기화 ]                                         ║" -ForegroundColor Cyan
    Write-Host "  ║   1. INFRA_SYNC       — Pool+VM+Host+SR+Net+Device  ║" -ForegroundColor White
    Write-Host "  ║  [ 상세 조회 (신규) ]                               ║" -ForegroundColor Green
    Write-Host "  ║   2. DEVICE_QUERY     — PIF/VIF/VBD/PBD 상세 조회  ║" -ForegroundColor Green
    Write-Host "  ║  [ 조회 ]                                           ║" -ForegroundColor Cyan
    Write-Host "  ║   3. POOL_QUERY       — Pool 상세 조회              ║" -ForegroundColor White
    Write-Host "  ║   4. HOST_QUERY       — Host 목록 조회              ║" -ForegroundColor White
    Write-Host "  ║   5. VM_QUERY         — VM 목록 조회                ║" -ForegroundColor White
    Write-Host "  ║   6. SR_QUERY         — Storage 조회               ║" -ForegroundColor White
    Write-Host "  ║   7. NETWORK_QUERY    — Network+PIF 조회            ║" -ForegroundColor White
    Write-Host "  ║  [ 제어 ]                                           ║" -ForegroundColor Cyan
    Write-Host "  ║   8. POWER_CONTROL    — VM 전원 제어                ║" -ForegroundColor White
    Write-Host "  ║   9. VM_CONTROL       — VM 관리                     ║" -ForegroundColor White
    Write-Host "  ║  10. HOST_CONTROL     — Host 관리                   ║" -ForegroundColor White
    Write-Host "  ║  11. SR_CONTROL       — SR+VDI 생성/삭제/탐색/관리 ║" -ForegroundColor White
    Write-Host "  ║  12. NETWORK_CONTROL  — Network/VIF 관리            ║" -ForegroundColor White
    Write-Host "  ║  [ 신규 조회 ]                                      ║" -ForegroundColor Green
    Write-Host "  ║  13. VDI_QUERY        — VDI 목록/상세 조회          ║" -ForegroundColor Green
    Write-Host "  ║  14. SNAPSHOT_QUERY   — 스냅샷 조회/삭제/복원       ║" -ForegroundColor Green
    Write-Host "  ║  15. VLAN_QUERY       — VLAN 조회                   ║" -ForegroundColor Green
    Write-Host "  ║  16. BOND_QUERY       — Bond NIC 조회               ║" -ForegroundColor Green
    Write-Host "  ║  [ Pool 관리 ]                                      ║" -ForegroundColor Magenta
    Write-Host "  ║  17. POOL_MANAGE      — Pool 등록/삭제/목록         ║" -ForegroundColor Magenta
    Write-Host "  ║  18. POOL_CONTROL     — Pool 설정/마이그망/join/eject/HA (신규)║" -ForegroundColor Magenta
    Write-Host "  ║  [ 백업 (신규) ]                                    ║" -ForegroundColor Green
    Write-Host "  ║  19. VM_BACKUP        — XVA export/verify/restore, OVA export/restore ║" -ForegroundColor Green
    Write-Host "  ║  [ 운영 분석 (파일럿, 신규) ]                       ║" -ForegroundColor Magenta
    Write-Host "  ║  20. ANALYSIS_QUERY   — 진단 결과/규칙/상태/이력 조회, 즉시 실행 ║" -ForegroundColor Magenta
    Write-Host "  ║  [ 기타 ]  h)HealthCheck  g)고루틴확인(Debug)  c)환경설정  q)종료 ║" -ForegroundColor Gray
    Write-Host "  ╚══════════════════════════════════════════════════════╝" -ForegroundColor Cyan
    Write-Host "     ※ 18-1(set_config)은 POOL_STATE_CHANGED 이벤트 검증용으로도 사용" -ForegroundColor Gray
}

# ─────────────────────────────────────────────────────────────────
# 진입점
# ─────────────────────────────────────────────────────────────────
Clear-Host
Write-Host "  NCC-Xen Backend 테스트 도구 시작 중..." -ForegroundColor Gray
try {
    $h = Invoke-RestMethod "$BACKEND/health" -ErrorAction Stop
    Write-Host "  ✔ Backend 정상 — Pool $($h.pools.Count)개 연결됨" -ForegroundColor Green
    Start-Sleep -Milliseconds 600
} catch {
    Write-Host "  ✗ Backend에 연결할 수 없습니다." -ForegroundColor Red
    $c = (Read-Host "  ▷ (y=계속 / Enter=종료)").Trim()
    if ($c -ne "y" -and $c -ne "Y") { exit 1 }
}

while ($true) {
    Show-MainMenu
    $choice = (Read-Host "  ▷ 번호 선택").Trim().ToLower()
    switch ($choice) {
        "0"  { Invoke-VMCreate }
        "1"  { Invoke-InfraSync }
        "2"  { Invoke-DeviceQuery }
        "3"  { Invoke-PoolQuery }
        "4"  { Invoke-HostQuery }
        "5"  { Invoke-VMQuery }
        "6"  { Invoke-SRQuery }
        "7"  { Invoke-NetworkQuery }
        "8"  { Invoke-PowerControl }
        "9"  { Invoke-VMControl }
        "10" { Invoke-HostControl }
        "11" { Invoke-SRControl }
        "12" { Invoke-NetworkControl }
        "13" { Invoke-VDIQuery }
        "14" { Invoke-SnapshotQuery }
        "15" { Invoke-VLANQuery }
        "16" { Invoke-BondQuery }
        "17" { Invoke-PoolManage }
        "18" { Invoke-PoolControl }
        "19" { Invoke-VMBackup }
        "20" { Invoke-AnalysisQuery }
        "h"  { Invoke-HealthCheck }
        "g"  { Invoke-GoroutineCheck }
        "c"  { Invoke-SetConfig }
        "q"  { Write-Host "  종료합니다." -ForegroundColor Gray; exit 0 }
        default { Write-Host "  잘못된 선택입니다." -ForegroundColor Red }
    }
    # 🆕 2026-08-19 버그 수정: inspect(REST 즉시 응답) 등 네트워크 호출 후 메뉴로
    # 복귀할 때 콘솔 입력 버퍼에 남아있던 값이 아래 Enter 프롬프트나 다음 루프의
    # "번호 선택" 프롬프트로 새어 들어가 이후 선택이 무시되는 현상을 방어.
    # Show-MainMenu는 Clear-Host만 하고 입력 버퍼는 비우지 않으므로 여기서 처리.
    $Host.UI.RawUI.FlushInputBuffer()
    Write-Host ""; Read-Host "  ▷ 메뉴로 돌아가려면 Enter"
}
