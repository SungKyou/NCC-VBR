# ═══════════════════════════════════════════════════════════════════
# demo_vm_backup.ps1 — VM_BACKUP (XVA export/verify/restore, OVA export/restore,
#                                  zstd/gzip 압축 자동 감지)
# 🆕 2026-09-01: demo.ps1에서 기계적으로 분리됨 (로직 변경 없음).
# 다음 zstd 관련 테스트 메뉴 수정은 이 파일만 보면 됩니다
# (백엔드 쪽 zstd 로직은 vm_backup_compress.go 참고).
# 🆕 2026-09-28: 메인 jobType payload에 poolUUID=$POOL_UUID 추가 (사용자 요청 —
#   demo.ps1 모든 API 호출에 poolUUID 포함. 기존 필드/로직 변경 없음, 추가만 함)
#   ※ 6)inspect는 jobType이 아닌 순수 REST 호출(GET /api/v1/ovf/inspect)이라
#     poolUUID/payload 구조 자체가 없으므로 대상에서 제외했습니다.
# ═══════════════════════════════════════════════════════════════════

function Invoke-VMBackup {
    Write-MenuHeader "VM_BACKUP"
    Write-Host "  1)export     — VM을 XVA 파일로 내보내기 (Halted=직접 / Running=임시 스냅샷 기반 / Suspended=disk 기준, memory 미포함)" -ForegroundColor White
    Write-Host "  2)verify     — export된 XVA 파일 무결성 검증 (tar 구조 + 내부 checksum + SHA256, gzip/zstd 압축 자동 감지)" -ForegroundColor Green
    Write-Host "  3)restore    — XVA 파일을 VM으로 복원 (실기검증 완료, zstd 압축 파일은 업로드 전 자동 압축해제)" -ForegroundColor Yellow
    Write-Host "  4)export OVA/OVF — VM을 OVF/OVA 파일로 내보내기 (OVF 낱개파일 지원, 실기검증 완료 — KVM/virt-v2v 호환 목적, VHD 디스크)" -ForegroundColor Cyan
    Write-Host "  5)restore OVA/OVF — OVF/OVA 파일을 VM으로 복원 (OVF 낱개파일 지원, 실기검증 완료 — VHD + VMDK(streamOptimized)/network_map(VIF)/boot_mode=auto/name_description 전부 확인)" -ForegroundColor Cyan
    Write-Host "  6)inspect OVF/OVA — 🆕 REST API로 디스크/NIC 목록 즉시 조회 (jobType 아님 — Send-Job과 달리 결과가 바로 이 콘솔에 표시됨, restore 전 network_map/disk_sr_map 사전 확인용)" -ForegroundColor Magenta
    $Host.UI.RawUI.FlushInputBuffer()
    $sel = (Read-Host "`n  ▷ 번호 선택").Trim()

    # 🆕 2026-08-19: inspect(6)는 jobType/masterIp/action/options 파이프라인과 무관한
    # 순수 REST 조회(GET /api/v1/ovf/inspect)라 아래 "연결 정보(Master IP)" 프롬프트도
    # 필요 없습니다 — 별도로 분기해서 여기서 바로 처리하고 return합니다.
    if ($sel -eq "6") {
        Write-Host ""
        Write-Host "  ── OVF/OVA 미리보기 (REST API — jobType 아님, 즉시 응답) ──────" -ForegroundColor Gray
        $fp6 = (Read-Host "  ▷ file_path (.ova 파일 또는 .ovf 서술자 파일 — 절대경로 또는 base_path 하위 상대경로)").Trim()
        if ($fp6 -eq "") { Write-Host "  file_path는 필수입니다" -ForegroundColor Red; return }
        $uri = "$BACKEND/api/v1/ovf/inspect?file_path=$([uri]::EscapeDataString($fp6))"
        Write-Host ""
        Write-Host "  → GET $uri" -ForegroundColor White
        try {
            $resp = Invoke-RestMethod -Uri $uri -Method GET -Headers $API_HEADERS -ErrorAction Stop
            Write-Host "  ✔ 200 OK" -ForegroundColor Green
            Write-Host ""
            Write-Host "  ┌─ OVF 정보 ───────────────────────────────────────" -ForegroundColor Cyan
            Write-Host "  │ file_path         : $($resp.file_path)" -ForegroundColor Gray
            Write-Host "  │ name_label        : $($resp.name_label)" -ForegroundColor Gray
            Write-Host "  │ vcpus             : $($resp.vcpus)" -ForegroundColor Gray
            Write-Host "  │ memory_mb         : $($resp.memory_mb)" -ForegroundColor Gray
            Write-Host "  │ efi_firmware_hint : $($resp.efi_firmware_hint)" -ForegroundColor Gray
            Write-Host "  │" -ForegroundColor White
            Write-Host "  │ 디스크 ($($resp.disk_count)개, disk_sr_map의 key로 사용):" -ForegroundColor White
            foreach ($d in $resp.disks) {
                Write-Host "  │   - $($d.disk_id) (format=$($d.format), capacity_bytes=$($d.capacity_bytes))" -ForegroundColor Gray
            }
            if ($resp.declared_networks -and $resp.declared_networks.Count -gt 0) {
                Write-Host "  │" -ForegroundColor White
                Write-Host "  │ NetworkSection 선언 네트워크 ($($resp.declared_networks.Count)개):" -ForegroundColor White
                foreach ($n in $resp.declared_networks) {
                    Write-Host "  │   - $n" -ForegroundColor Gray
                }
            }
            Write-Host "  │" -ForegroundColor White
            Write-Host "  │ NIC / Ethernet Adapter ($($resp.nic_count)개 — network_map에 이만큼 항목 필요):" -ForegroundColor Yellow
            if ($resp.nic_count -eq 0) {
                Write-Host "  │   (NIC 없음 — network_map 입력 불필요)" -ForegroundColor Gray
            }
            foreach ($nic in $resp.nics) {
                Write-Host "  │   - $($nic.element_name) → connection=`"$($nic.connection)`"  ⟶ network_map 예시: $($nic.connection)=<network_uuid>" -ForegroundColor Gray
            }
            Write-Host "  └────────────────────────────────────────────────" -ForegroundColor Cyan
        } catch {
            $code = $_.Exception.Response.StatusCode.value__
            Write-Host "  ✗ HTTP $code — $($_.ErrorDetails.Message)" -ForegroundColor Red
        }
        # 🆕 2026-08-19 버그 수정: Invoke-RestMethod 대기 중 콘솔에 쌓인 입력이
        # 메인 루프의 "메뉴로 돌아가려면 Enter" 프롬프트로 새어 들어가 이후 메뉴
        # 선택이 무시되는 현상 확인 — Get-Input과 동일한 FlushInputBuffer 패턴 적용.
        $Host.UI.RawUI.FlushInputBuffer()
        return
    }

    Write-Host ""
    Write-Host "  ── 연결 정보 ─────────────────────────────────────" -ForegroundColor Gray
    $masterIp = Get-Input "Master IP" $MASTER_IP
    $action = ""; $vmUUID = ""; $options = @{}
    switch ($sel) {
        "1" {
            $action = "export"
            $vmUUID = Get-Input "VM UUID" $VM_UUID
            $fn = (Read-Host "  ▷ file_name (예: DAAS-WEB02_20260716, .xva 자동 부여)").Trim()
            if ($fn -eq "") { Write-Host "  file_name은 필수입니다" -ForegroundColor Red; return }
            $options["file_name"] = $fn
            Write-Host "  ※ export_path: 절대경로(/mnt/... 또는 C:\...)는 그대로 사용, 상대경로는 base_path 하위" -ForegroundColor Gray
            $ep = (Read-Host "  ▷ export_path (Enter=base_path 루트)").Trim()
            if ($ep -ne "") { $options["export_path"] = $ep }
            $cp = (Read-Host "  ▷ compression? none/gzip/zstd (Enter=none)").Trim()
            if ($cp -ne "") { $options["compression"] = $cp }
            if ($cp -eq "zstd") {
                Write-Host "  ※ zstd는 클라이언트(backend) 측 로컬 압축 — XAPI 네이티브 미지원(XCP-ng 전용, Citrix XenServer엔 없음)" -ForegroundColor Gray
                Write-Host "  ※ zstd로 압축된 파일은 XenCenter 등 타 도구로 직접 import 불가 — 반드시 이 스크립트(restore)로 복원해야 함" -ForegroundColor DarkYellow
            }
            $ks = (Read-Host "  ▷ keep_snapshot? Running VM 임시 스냅샷 보존 (y/N)").Trim()
            if ($ks -eq "y") { $options["keep_snapshot"] = $true }
            $ow = (Read-Host "  ▷ overwrite? 동일 파일 덮어쓰기 (y/N)").Trim()
            if ($ow -eq "y") { $options["overwrite"] = $true }
        }
        "2" {
            $action = "verify"
            Write-Host "  ※ gzip/zstd 압축 여부는 파일 매직바이트로 자동 감지됩니다 — 옵션 지정 불필요" -ForegroundColor Gray
            $fp = (Read-Host "  ▷ file_path (절대경로 또는 base_path 하위 상대경로)").Trim()
            if ($fp -eq "") { Write-Host "  file_path는 필수입니다" -ForegroundColor Red; return }
            $options["file_path"] = $fp
        }
        "3" {
            $action = "restore"
            Write-Host "  ※ zstd로 압축된 파일(.xva.zst 등)은 매직바이트로 자동 감지되어 업로드 전 backend가 임시파일로 압축해제합니다 — 옵션 지정 불필요 (gzip은 XAPI가 서버측에서 자동 인식, 기존과 동일)" -ForegroundColor Gray
            $fp = (Read-Host "  ▷ file_path (복원할 XVA 절대경로 또는 base_path 하위 상대경로)").Trim()
            if ($fp -eq "") { Write-Host "  file_path는 필수입니다" -ForegroundColor Red; return }
            $options["file_path"] = $fp
            $sr = (Read-Host "  ▷ sr_uuid (복원 대상 SR UUID)").Trim()
            if ($sr -eq "") { Write-Host "  sr_uuid는 필수입니다" -ForegroundColor Red; return }
            $options["sr_uuid"] = $sr
            $nl = (Read-Host "  ▷ name_label (Enter=미변경, XVA 내 원본 이름 사용)").Trim()
            if ($nl -ne "") { $options["name_label"] = $nl }
            $ru = (Read-Host "  ▷ restore_original_uuid? 원본 UUID 보존 시도 — DR용, 충돌 가능성 있음 (y/N)").Trim()
            if ($ru -eq "y") { $options["restore_original_uuid"] = $true }
            $hs = (Read-Host "  ▷ home_server_uuid — 복원 후 Home Server 지정 (Enter=생략)").Trim()
            if ($hs -ne "") { $options["home_server_uuid"] = $hs }
            $fo = (Read-Host "  ▷ force? (y/N)").Trim()
            if ($fo -eq "y") { $options["force"] = $true }
        }
        "4" {
            $action = "export"
            Write-Host "  [ 형식 선택 ]  1)OVA (tar 단일 파일)  2)OVF (신규 — .ovf/.mf/디스크 파일을 디렉토리에 낱개 저장, DMTF 표준)" -ForegroundColor DarkCyan
            $fmtSel = (Read-Host "  ▷ 선택 (Enter=OVA)").Trim()
            $isOVFExport = ($fmtSel -eq "2")
            $options["format"] = if ($isOVFExport) { "ovf" } else { "ova" }
            $vmUUID = Get-Input "VM UUID" $VM_UUID
            if ($isOVFExport) {
                $fn = (Read-Host "  ▷ file_name (예: DAAS-WEB02_20260728 — 이 이름으로 디렉토리가 만들어지고 그 안에 .ovf/.mf/디스크 파일 저장)").Trim()
            } else {
                $fn = (Read-Host "  ▷ file_name (예: DAAS-WEB02_20260728, .ova 자동 부여)").Trim()
            }
            if ($fn -eq "") { Write-Host "  file_name은 필수입니다" -ForegroundColor Red; return }
            $options["file_name"] = $fn
            Write-Host "  ※ export_path: 절대경로(/mnt/... 또는 C:\...)는 그대로 사용, 상대경로는 base_path 하위" -ForegroundColor Gray
            $ep = (Read-Host "  ▷ export_path (Enter=base_path 루트)").Trim()
            if ($ep -ne "") { $options["export_path"] = $ep }
            $ks = (Read-Host "  ▷ keep_snapshot? Running VM 임시 스냅샷 보존 (y/N)").Trim()
            if ($ks -eq "y") { $options["keep_snapshot"] = $true }
            $ow = (Read-Host "  ▷ overwrite? 동일 파일 덮어쓰기 (y/N)").Trim()
            if ($ow -eq "y") { $options["overwrite"] = $true }
        }
        "5" {
            $action = "restore"
            Write-Host "  ✔ VHD + VMDK(streamOptimized) 디스크, network_map(VIF 자동생성), boot_mode=auto, name_description 전부 실기검증 완료(2026-08-19)" -ForegroundColor Green
            Write-Host "  ※ .ova(또는 .tar/.tar.gz)면 OVA(tar 묶음), .ovf면 OVF(디렉토리 낱개 파일) 모드로 확장자 기준 자동 판단됩니다" -ForegroundColor Gray
            Write-Host "  ※ 디스크 포맷(VHD/VMDK)은 OVF 서술자의 ovf:format 값으로 디스크별 자동 판단됩니다 — 별도 지정 불필요" -ForegroundColor Gray
            $fp = (Read-Host "  ▷ file_path (.ova 파일 또는 .ovf 서술자 파일 — 절대경로 또는 base_path 하위 상대경로)").Trim()
            if ($fp -eq "") { Write-Host "  file_path는 필수입니다" -ForegroundColor Red; return }
            $options["file_path"] = $fp
            $options["format"] = if ($fp.ToLower().EndsWith(".ovf")) { "ovf" } else { "ova" }
            Show-OVFPreview -FilePath $fp
            $bt = (Read-Host "  ▷ base_template_uuid (복제 기준 template UUID — VM.create 대신 재사용)").Trim()
            if ($bt -eq "") { Write-Host "  base_template_uuid는 필수입니다" -ForegroundColor Red; return }
            $options["base_template_uuid"] = $bt
            $sr = (Read-Host "  ▷ sr_uuid (기본 대상 SR UUID)").Trim()
            if ($sr -eq "") { Write-Host "  sr_uuid는 필수입니다" -ForegroundColor Red; return }
            $options["sr_uuid"] = $sr
            Write-Host "  ※ disk_sr_map: 디스크별로 다른 SR을 쓰려면 diskId=sr_uuid 형식, 콤마로 여러 개 구분 (예: vmdisk1=<uuid>,vmdisk2=<uuid>)" -ForegroundColor Gray
            $dsm = (Read-Host "  ▷ disk_sr_map (Enter=전체 sr_uuid 기본값 사용)").Trim()
            if ($dsm -ne "") {
                $map = @{}
                foreach ($pair in ($dsm -split ",")) {
                    $kv = $pair -split "=", 2
                    if ($kv.Length -eq 2) { $map[$kv[0].Trim()] = $kv[1].Trim() }
                }
                if ($map.Count -gt 0) { $options["disk_sr_map"] = $map }
            }
            Write-Host "  ※ network_map: 위 미리보기의 'connection' 값을 key로 사용, ovfName=network_uuid 형식, 콤마로 여러 개 구분 (예: nic0=<uuid>,nic1=<uuid>) — 미지정 시 VIF 생성 안 함(base_template 상속에 의존)" -ForegroundColor Gray
            $nm = (Read-Host "  ▷ network_map (Enter=VIF 생성 안 함)").Trim()
            if ($nm -ne "") {
                $netMap = @{}
                foreach ($pair in ($nm -split ",")) {
                    $kv = $pair -split "=", 2
                    if ($kv.Length -eq 2) { $netMap[$kv[0].Trim()] = $kv[1].Trim() }
                }
                if ($netMap.Count -gt 0) { $options["network_map"] = $netMap }
            }
            $nl = (Read-Host "  ▷ name_label (Enter=OVF 서술자 내 이름 사용)").Trim()
            if ($nl -ne "") { $options["name_label"] = $nl }
            $nd = (Read-Host "  ▷ name_description — VM 설명 (Enter=OVF 서술자 내 OS Description 사용, 그것도 없으면 template 설명 유지)").Trim()
            if ($nd -ne "") { $options["name_description"] = $nd }
            $vc = (Read-Host "  ▷ vcpus (Enter=OVF 서술자 값 사용)").Trim()
            if ($vc -ne "") { $options["vcpus"] = [int]$vc }
            $mm = (Read-Host "  ▷ memory_mb (Enter=OVF 서술자 값 사용)").Trim()
            if ($mm -ne "") { $options["memory_mb"] = [int]$mm }
            Write-Host ""
            Write-Host "  [ Boot Mode (신규) ]  1)BIOS  2)UEFI  3)UEFI Secure Boot  4)Auto — 🆕 OVF의 vmw:Config firmware/secureboot 선언을 읽어 자동 결정 (선언 없으면 BIOS로 안전하게 강제)" -ForegroundColor DarkCyan
            Write-Host "  ※ OVF는 firmware 정보를 담지 않으므로, base_template의 firmware가 원본 디스크의" -ForegroundColor Gray
            Write-Host "     부트 방식(BIOS/MBR vs UEFI/GPT)과 다르면 부팅 실패(UEFI Shell 등) 가능 — 그 경우 명시 지정" -ForegroundColor Gray
            Write-Host "     (Auto는 위 미리보기에서 vmw:Config를 직접 보여주지 않으므로, 실제 반영값은 백엔드 로그의 vm_backup.l053 참조)" -ForegroundColor Gray
            $bootModeSel = (Read-Host "  ▷ 선택 (Enter=template 기본값 유지)").Trim()
            $bootMode = switch ($bootModeSel) {
                "1" { "bios" }
                "2" { "uefi" }
                "3" { "uefi_secure_boot" }
                "4" { "auto" }
                default { "" }
            }
            if ($bootMode -ne "") { $options["boot_mode"] = $bootMode }
            $hs = (Read-Host "  ▷ home_server_uuid — 복원 후 Home Server 지정 (Enter=생략)").Trim()
            if ($hs -ne "") { $options["home_server_uuid"] = $hs }
            $fo = (Read-Host "  ▷ force? manifest 체크섬 불일치 무시 (y/N)").Trim()
            if ($fo -eq "y") { $options["force"] = $true }
        }
        default { Write-Host "  잘못된 선택" -ForegroundColor Red; return }
    }
    if (-not (Confirm-Run)) { return }
    $tt = "VM"; if ($action -eq "verify" -or $action -eq "restore") { $tt = "FILE" }
    Send-Job "VM_BACKUP / $action" (@{
        taskId="$(New-TaskId "backup")"; jobType="VM_BACKUP"; action=$action; executionType="async"
        payload=@{ targetType=$tt; targetUUID=$vmUUID; masterIp=$masterIp; poolUUID=$POOL_UUID; options=$options }
    } | ConvertTo-Json -Depth 5)
}

# ─────────────────────────────────────────────────────────────────
# 9. VM_CONTROL
# ─────────────────────────────────────────────────────────────────
