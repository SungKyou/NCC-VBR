# ═══════════════════════════════════════════════════════════════════
# demo_common.ps1 — 공통 헬퍼 (Send-Job, Get-Input, 환경설정 등)
# 🆕 2026-09-01: demo.ps1에서 기계적으로 분리됨 (로직 변경 없음).
# demo.ps1에서 dot-source로 로드되며, $BACKEND 등 전역 변수는 demo.ps1이
# 이미 스크립트 스코프에 설정한 뒤 이 파일을 dot-source하므로 그대로 참조 가능.
# ═══════════════════════════════════════════════════════════════════

# ─────────────────────────────────────────────────────────────────
# 공통 헬퍼
# ─────────────────────────────────────────────────────────────────

function Send-Job {
    param([string]$Desc, [string]$Body)
    Write-Host ""
    Write-Host "  → $Desc" -ForegroundColor White
    Write-Host "  ┌─ Request ──────────────────────────────────────" -ForegroundColor Gray
    $Body | ConvertFrom-Json | ConvertTo-Json -Depth 10 | ForEach-Object {
        Write-Host "  │ $_" -ForegroundColor Gray
    }
    Write-Host "  └────────────────────────────────────────────────" -ForegroundColor Gray
    try {
        $resp = Invoke-WebRequest -Uri "$BACKEND/api/v1/jobs" `
            -Method POST -ContentType "application/json" `
            -Body $Body -Headers $API_HEADERS -ErrorAction Stop
        Write-Host "  ✔ 202 Accepted" -ForegroundColor Green
        return $true
    } catch {
        $code = $_.Exception.Response.StatusCode.value__
        Write-Host "  ✗ HTTP $code — $($_.ErrorDetails.Message)" -ForegroundColor Red
        return $false
    }
}

function Get-Input {
    param([string]$Prompt, [string]$Default = "")
    # 이전 프롬프트(메뉴 번호 선택 등)에서 빠르게 입력된 키가 콘솔 입력 버퍼에
    # 남아있다가 다음 Read-Host로 새어 들어가는 경우 방지 (2026-08-13 실기 확인:
    # 메뉴에서 "5" 선택 직후 Master IP 프롬프트에 그 "5"가 그대로 들어간 사례)
    $Host.UI.RawUI.FlushInputBuffer()
    if ($Default -ne "") {
        $val = (Read-Host "`n  ▷ $Prompt [$Default]").Trim()
        if ($val -eq "") { return $Default }
        return $val
    }
    return (Read-Host "`n  ▷ $Prompt").Trim()
}

function Confirm-Run {
    param([string]$Msg = "실행하시겠습니까? (Enter=예 / n=취소)")
    $ans = (Read-Host "`n  ▷ $Msg").Trim()
    return ($ans -eq "" -or $ans -eq "y" -or $ans -eq "Y")
}

function Write-MenuHeader {
    param([string]$Title)
    Write-Host ""
    Write-Host "  ╔══════════════════════════════════════════════════╗" -ForegroundColor Cyan
    Write-Host "  ║  $Title" -ForegroundColor Cyan
    Write-Host "  ╚══════════════════════════════════════════════════╝" -ForegroundColor Cyan
}

# 🆕 2026-08-19: OVF/OVA 파일을 로컬에서 직접 파싱해 디스크/NIC 목록을 미리 보여줍니다.
# VM_BACKUP restore(OVF/OVA)는 fire-and-forget(202 Accepted 후 실제 결과는 Middleware
# 쪽으로 비동기 전달)이라 콘솔에서 결과를 바로 볼 수 없으므로, network_map/disk_sr_map
# 입력 전 SK님이 실제 OVF 서술자 내용(디스크 diskId, NIC 개수/연결 네트워크 이름)을
# 바로 확인할 수 있도록 demo.ps1 자체에서 파싱합니다(백엔드 왕복 불필요, 재빌드 불필요).
# ⚠ file_path가 백엔드 서버 전용 경로(base_path 하위 상대경로 등)라 이 스크립트를
#   실행하는 로컬 머신에서 접근 불가능하면 조용히 건너뜁니다(치명적 에러 아님).
function Show-OVFPreview {
    param([string]$FilePath)

    if (-not (Test-Path -LiteralPath $FilePath)) {
        Write-Host "  ※ 로컬에서 파일을 찾을 수 없어 미리보기를 건너뜁니다 ($FilePath) — 백엔드 서버 전용 경로인 경우 정상입니다" -ForegroundColor Gray
        return
    }

    $isOVFLoose = $FilePath.ToLower().EndsWith(".ovf")
    [xml]$ovfXml = $null
    try {
        if ($isOVFLoose) {
            $ovfXml = [xml](Get-Content -LiteralPath $FilePath -Raw)
        } else {
            # .ova(tar/tar.gz) 내부 첫 .ovf 엔트리를 파일로 풀지 않고 stdout으로 추출
            # (Windows 10/11 기본 내장 tar.exe = bsdtar/libarchive — 패턴 인자를 파일명
            # 위치에 직접 넘기면 fnmatch로 매칭됨. GNU tar 전용인 --wildcards는 bsdtar에
            # 없는 옵션이라 쓰지 않음. gzip 압축은 bsdtar가 자동 인식)
            $ovfContent = & tar -xOf $FilePath "*.ovf" 2>$null
            if (-not $ovfContent) {
                Write-Host "  ※ .ova 내부에서 .ovf 서술자를 추출하지 못해 미리보기를 건너뜁니다 (tar 명령 확인 필요)" -ForegroundColor Gray
                return
            }
            $ovfXml = [xml]($ovfContent -join "`n")
        }
    } catch {
        Write-Host "  ※ OVF 파싱 실패로 미리보기를 건너뜁니다: $($_.Exception.Message)" -ForegroundColor Gray
        return
    }

    # 네임스페이스 접두어(ovf:/rasd: 등)가 파일마다 달라 local-name() 기반 XPath로 매칭
    $disks = $ovfXml.SelectNodes('//*[local-name()="Disk"]')
    $networks = $ovfXml.SelectNodes('//*[local-name()="Network"]')
    $items = $ovfXml.SelectNodes('//*[local-name()="Item"]')

    Write-Host ""
    Write-Host "  ┌─ OVF 미리보기 ───────────────────────────────────" -ForegroundColor Cyan
    Write-Host "  │ 디스크 ($($disks.Count)개, disk_sr_map의 key로 사용):" -ForegroundColor White
    foreach ($d in $disks) {
        $diskId = $d.GetAttribute("diskId")
        $cap = $d.GetAttribute("capacity")
        $units = $d.GetAttribute("capacityAllocationUnits")
        Write-Host "  │   - $diskId (capacity=$cap $units)" -ForegroundColor Gray
    }
    if ($networks.Count -gt 0) {
        Write-Host "  │" -ForegroundColor White
        Write-Host "  │ NetworkSection 선언 네트워크 ($($networks.Count)개):" -ForegroundColor White
        foreach ($n in $networks) {
            Write-Host "  │   - $($n.GetAttribute('name'))" -ForegroundColor Gray
        }
    }
    $nics = @()
    foreach ($item in $items) {
        $rtNode = $item.SelectSingleNode('*[local-name()="ResourceType"]')
        if ($rtNode -ne $null -and $rtNode.InnerText -eq "10") {
            $connNode = $item.SelectSingleNode('*[local-name()="Connection"]')
            $elNode = $item.SelectSingleNode('*[local-name()="ElementName"]')
            $nics += [PSCustomObject]@{
                ElementName = if ($elNode) { $elNode.InnerText } else { "" }
                Connection  = if ($connNode) { $connNode.InnerText } else { "" }
            }
        }
    }
    Write-Host "  │" -ForegroundColor White
    Write-Host "  │ NIC / Ethernet Adapter ($($nics.Count)개 — network_map에 이만큼 항목 필요):" -ForegroundColor Yellow
    if ($nics.Count -eq 0) {
        Write-Host "  │   (NIC 없음 — network_map 입력 불필요)" -ForegroundColor Gray
    }
    foreach ($nic in $nics) {
        Write-Host "  │   - $($nic.ElementName) → connection=`"$($nic.Connection)`"  ⟶ network_map 예시: $($nic.Connection)=<network_uuid>" -ForegroundColor Gray
    }
    Write-Host "  └────────────────────────────────────────────────" -ForegroundColor Cyan
}

function New-TaskId { param([string]$Prefix)
    return "$Prefix-$([DateTimeOffset]::UtcNow.ToUnixTimeSeconds())"
}

function Show-EnvConfig {
    Write-Host ""
    Write-Host "  ┌─ 환경 설정 ─────────────────────────────────────" -ForegroundColor Gray
    Write-Host "  │  Backend  : $BACKEND"      -ForegroundColor Gray
    Write-Host "  │  Master   : $MASTER_IP"    -ForegroundColor Gray
    Write-Host "  │  Pool     : $POOL_UUID"    -ForegroundColor Gray
    Write-Host "  │  VM       : $VM_UUID"      -ForegroundColor Gray
    Write-Host "  │  SR       : $SR_UUID"      -ForegroundColor Gray
    Write-Host "  │  Network  : $NETWORK_UUID" -ForegroundColor Gray
    Write-Host "  │  PIF      : $(if ($PIF_UUID) { $PIF_UUID } else { '(미설정)' })" -ForegroundColor Gray
    Write-Host "  │  VIF      : $(if ($VIF_UUID) { $VIF_UUID } else { '(미설정)' })" -ForegroundColor Gray
    Write-Host "  │  VBD      : $(if ($VBD_UUID) { $VBD_UUID } else { '(미설정)' })" -ForegroundColor Gray
    Write-Host "  │  PBD      : $(if ($PBD_UUID) { $PBD_UUID } else { '(미설정)' })" -ForegroundColor Gray
    Write-Host "  │  VDI      : $(if ($VDI_UUID) { $VDI_UUID } else { '(미설정)' })" -ForegroundColor Gray
    Write-Host "  │  Snapshot : $(if ($SNAPSHOT_UUID) { $SNAPSHOT_UUID } else { '(미설정)' })" -ForegroundColor Gray
    Write-Host "  └────────────────────────────────────────────────" -ForegroundColor Gray
}

# ─────────────────────────────────────────────────────────────────
# 환경 설정 변경
# ─────────────────────────────────────────────────────────────────
function Invoke-SetConfig {
    Write-MenuHeader "환경 설정 변경"
    Show-EnvConfig
    Write-Host "  변경할 항목만 입력하고 나머지는 Enter를 누르세요." -ForegroundColor DarkYellow
    $script:BACKEND      = Get-Input "Backend URL  " $BACKEND
    $script:MASTER_IP    = Get-Input "Master IP    " $MASTER_IP
    $script:POOL_UUID    = Get-Input "Pool UUID    " $POOL_UUID
    $script:VM_UUID      = Get-Input "VM UUID      " $VM_UUID
    $script:SR_UUID      = Get-Input "SR UUID      " $SR_UUID
    $script:NETWORK_UUID = Get-Input "Network UUID " $NETWORK_UUID
    $script:PIF_UUID     = Get-Input "PIF UUID     " $PIF_UUID
    $script:VIF_UUID     = Get-Input "VIF UUID     " $VIF_UUID
    $script:VBD_UUID     = Get-Input "VBD UUID     " $VBD_UUID
    $script:PBD_UUID      = Get-Input "PBD UUID      " $PBD_UUID
    $script:VDI_UUID      = Get-Input "VDI UUID      " $VDI_UUID
    $script:SNAPSHOT_UUID = Get-Input "Snapshot UUID " $SNAPSHOT_UUID
    Write-Host "  ✔ 환경 설정 업데이트 완료" -ForegroundColor Green
}

# ─────────────────────────────────────────────────────────────────
# Health Check
# ─────────────────────────────────────────────────────────────────
function Invoke-HealthCheck {
    Write-MenuHeader "Health Check"
    try {
        $h = Invoke-RestMethod "$BACKEND/health"
        Write-Host "  ✔ Backend 정상 — Pool $($h.pools.Count)개 연결됨" -ForegroundColor Green
        foreach ($p in $h.pools) {
            Write-Host "    $($p.pool_uuid)  status=$($p.status)  ready=$($p.is_ready)" -ForegroundColor Gray
        }
    } catch { Write-Host "  ✗ Backend에 연결할 수 없습니다." -ForegroundColor Red }
}

# ─────────────────────────────────────────────────────────────────
# 고루틴 수 확인 (Debug, 수동 조회)
# 🆕 2026-09-17: 2026-09-16 세션에서 제거된 TEMP-DEBUG(worker.go 누수 진단용,
# 30초 주기 자동 로깅 + /health 상시 노출)를 되살리는 게 아니라, 필요할 때만
# 수동으로 1회 조회하는 별도의 상시 debug 엔드포인트(GET /debug/goroutines)를
# 호출합니다. 평소엔 백엔드가 아무것도 로깅/노출하지 않다가 이 메뉴를 선택한
# 순간에만 현재 goroutine 수를 확인합니다.
# ─────────────────────────────────────────────────────────────────
function Invoke-GoroutineCheck {
    Write-MenuHeader "고루틴 수 확인 (Debug)"
    try {
        $r = Invoke-RestMethod "$BACKEND/debug/goroutines" -Headers $API_HEADERS -ErrorAction Stop
        Write-Host "  ✔ 현재 고루틴 수: $($r.goroutines)" -ForegroundColor Green
        Write-Host "    조회 시각(UTC): $($r.timestamp)" -ForegroundColor Gray
    } catch {
        Write-Host "  ✗ 조회 실패 — $($_.Exception.Message)" -ForegroundColor Red
    }
}

# ─────────────────────────────────────────────────────────────────
# 0. VM_CREATE
# ─────────────────────────────────────────────────────────────────
