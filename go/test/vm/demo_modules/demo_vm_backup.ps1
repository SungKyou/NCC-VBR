# ═══════════════════════════════════════════════════════════════════
# demo_vm_backup.ps1 — 실기 모드 메뉴 1~9 (실제 XenServer 풀)
# 구조는 ncc-xen backend/demo_modules/demo_vm_backup.ps1 (@043c6d5, CP-VM-13)을 따른다.
# 바뀐 점: 결과를 조회 API 로 기다려 바로 보여 준다. dom0 확인·정리 명령은 출력만 하고 실행하지 않는다
#          (풀 정리는 사용자가 dom0 에서 직접 한다).
# 시험 번호는 docs/vm/06-test-plan-report.md 3장(T-VM-I01~I10).
# ═══════════════════════════════════════════════════════════════════

$script:Created = [System.Collections.Generic.List[string]]::new()   # 이 데모가 만든 VM uuid

function Show-Dom0 { param([string[]]$Lines) Write-Host '  [dom0 에서 확인]' -ForegroundColor Yellow; $Lines | ForEach-Object { Write-Host "    $_" -ForegroundColor Yellow } }

function Read-ExportOptions {
    param([switch]$Disks)
    $o = [ordered]@{}
    $o.file_name = Get-Input '파일 이름(.xva 자동)' ("vbrvm-" + (Get-Date -Format 'yyyyMMdd-HHmmss'))
    $p = Get-Input 'export_path(비우면 서버 backup.base_path)' $script:EXPORT_PATH
    if ($p) { $o.export_path = $p }
    $c = Get-Input 'compression none/gzip/zstd (zstd 는 XenCenter 로 복구 불가)' 'none'
    if ($c -ne 'none') { $o.compression = $c }
    $m = Get-Input 'snapshot_mode auto/always' 'auto'
    if ($m -ne 'auto') { $o.snapshot_mode = $m }
    if ($Disks) { $o.disks = @((Get-Input '담을 디스크 번호(쉼표)' '0') -split ',' | ForEach-Object { [int]$_.Trim() }) }
    if ((Get-Input '백업 뒤 검증 y/n' 'n') -eq 'y') { $o.verify_after_export = $true }
    return $o
}

function Invoke-ExportMenu {
    param([switch]$Disks)
    Write-MenuHeader $(if ($Disks) { '3. 디스크 선택 백업 (I02)' } else { '2. 전체 백업 (I01)' })
    $vm = Get-Input 'VM uuid' $script:VM_UUID
    $o = Read-ExportOptions -Disks:$Disks
    $id = New-TaskId 'export'
    if (-not (Send-VbrJob (New-VbrJob -TaskId $id -Action export -Options $o -TargetUUID $vm))) { return }
    $b = Wait-VbrJob $id
    Show-VbrResult $b
    Test-Step "export $id" ($b.status -eq 'succeeded') "$($b.payload.options.file_path)"
    Show-Dom0 @("xe snapshot-list snapshot-of=$vm params=uuid,name-label   # 임시 스냅샷이 남지 않아야 한다",
        'xe task-list params=uuid,name-label,status')
}

function Invoke-RestoreMenu {
    Write-MenuHeader '4. 복구 (I03, I04)'
    $o = [ordered]@{ file_path = Get-Input 'file_path(서버 기준)'; sr_uuid = Get-Input 'SR uuid' $script:SR_UUID }
    if ((Get-Input 'MAC·uuid 보존(restore_original_uuid) y/n — 원본 VM 이 같은 풀에 있으면 거부됨' 'n') -eq 'y') { $o.restore_original_uuid = $true }
    $n = Get-Input 'name_label(비우면 파일 속 이름)' ''
    if ($n) { $o.name_label = $n }
    $id = New-TaskId 'restore'
    if (-not (Send-VbrJob (New-VbrJob -TaskId $id -Action restore -Options $o))) { return }
    $b = Wait-VbrJob $id
    Show-VbrResult $b
    if ($b.status -eq 'succeeded') {
        $u = $b.payload.options.vm_uuid
        $script:Created.Add($u)
        Show-Dom0 @("xe vm-list uuid=$u params=name-label,power-state,is-a-template",
            "xe vif-list vm-uuid=$u params=device,MAC", "xe vbd-list vm-uuid=$u type=Disk params=userdevice")
        Write-Host '  복구한 VM 은 켜지 마세요(원본과 IP 가 같을 수 있습니다).' -ForegroundColor Yellow
    }
    Test-Step "restore $id" ($b.status -eq 'succeeded') "$($b.payload.options.vm_uuid)$($b.error.detail)"
}

function Invoke-VerifyMenu {
    Write-MenuHeader '5. 검증 (I05)'
    $id = New-TaskId 'verify'
    if (-not (Send-VbrJob (New-VbrJob -TaskId $id -Action verify -Options @{ file_path = (Get-Input 'file_path(서버 기준)') }))) { return }
    $b = Wait-VbrJob $id
    Show-VbrResult $b
    Test-Step "verify $id" ($b.status -eq 'succeeded' -and $b.payload.options.verified -eq $true) "verified=$($b.payload.options.verified)"
}

function Invoke-ConcurrentMenu {
    Write-MenuHeader '6. 동시 작업과 목록 (I06)'
    $n = [int](Get-Input '동시에 보낼 백업 수' '3')
    $ids = @()
    for ($i = 1; $i -le $n; $i++) {
        $id = New-TaskId "multi$i"
        if (Send-VbrJob (New-VbrJob -TaskId $id -Action export -Options @{ file_name = "vbrvm-multi$i-" + (Get-Date -Format 'HHmmss'); overwrite = $true })) { $ids += $id }
    }
    Start-Sleep -Seconds 2
    $r = Invoke-Vbr -Method GET -Path '/api/v1/jobs?status=queued,running'
    Write-Host "  처리 중·대기: $($r.Body.count)" -ForegroundColor White
    $r.Body.items | ForEach-Object { Write-Host "    $($_.taskId)  $($_.status)  $($_.percent)%" -ForegroundColor Gray }
    foreach ($id in $ids) { $b = Wait-VbrJob $id -Quiet; Test-Step "multi $id" ($b.status -eq 'succeeded') "$($b.payload.options.file_size_bytes) bytes, $($b.payload.options.duration_seconds)s" }
}

function Invoke-CancelMenu {
    Write-MenuHeader '7. 처리 중 취소 (I07)'
    $vm = Get-Input 'VM uuid' $script:VM_UUID
    $id = New-TaskId 'cancel'
    if (-not (Send-VbrJob (New-VbrJob -TaskId $id -Action export -Options @{ file_name = 'vbrvm-cancel'; snapshot_mode = 'always'; overwrite = $true } -TargetUUID $vm))) { return }
    $null = Wait-VbrJob $id -Until @('running') -Quiet
    Write-Host '  내보내기가 시작되기를 15초 기다린 뒤 취소합니다.' -ForegroundColor Gray
    Start-Sleep -Seconds 15
    $c = Invoke-Vbr -Method POST -Path "/api/v1/jobs/$id/cancel"
    Write-Host "  취소 응답 HTTP $($c.Status) step=$($c.Body.step)" -ForegroundColor White
    $b = Wait-VbrJob $id
    Show-VbrResult $b
    Test-Step "cancel $id" ($b.status -eq 'canceled') "$($b.status)"
    Show-Dom0 @("xe snapshot-list snapshot-of=$vm params=uuid,name-label   # vbrvm 임시 스냅샷이 남지 않아야 한다",
        'xe task-list params=uuid,name-label,status')
}

function Invoke-ErrorMenu {
    Write-MenuHeader '8. 오류 응답 (I08)'
    $cases = @(
        @{ name = '없는 VM'; job = (New-VbrJob -TaskId (New-TaskId 'err') -Action export -Options @{ file_name = 'x' } -TargetUUID '00000000-0000-0000-0000-000000000000'); code = 'XAPI_ERROR' },
        @{ name = '없는 복구 파일'; job = (New-VbrJob -TaskId (New-TaskId 'err') -Action restore -Options @{ file_path = 'none-' + [guid]::NewGuid().ToString('N') + '.xva'; sr_uuid = $script:SR_UUID }); code = 'FILE_IO_ERROR' },
        @{ name = '틀린 암호'; job = (New-VbrJob -TaskId (New-TaskId 'err') -Action export -Options @{ file_name = 'x' } -Password $(if ($script:CRYPTO_ON) { $script:WRONG_PASSWORD_ENC } else { 'wrong' })); code = 'XAPI_AUTH_FAILED' }
    )
    foreach ($c in $cases) {
        if (-not (Send-VbrJob $c.job $c.name)) { continue }
        $b = Wait-VbrJob $c.job.taskId -Quiet
        Test-Step $c.name ($b.error.code -eq $c.code) "$($b.error.code) $($b.error.detail)"
    }
    $r = Invoke-Vbr -Method POST -Path '/api/v1/jobs' -Body (New-VbrJob -TaskId (New-TaskId 'err') -Action export -Options @{})
    Test-Step '필수 값 없음' ($r.Status -eq 400 -and $r.Body.code -eq 'MISSING_FIELD') "$($r.Body.code) $($r.Body.field)"
}

function Invoke-CleanupMenu {
    Write-MenuHeader '9. 정리 안내'
    if ($script:Created.Count -eq 0) { Write-Host '  이 데모에서 복구한 VM 이 없습니다.' -ForegroundColor Gray; return }
    Write-Host '  이 데모에서 복구한 VM 입니다. uuid 와 이름을 확인한 뒤 dom0 에서 지우세요.' -ForegroundColor White
    Write-Host "  시험 VM($script:VM_UUID)과 기존 스냅샷은 지우지 마세요." -ForegroundColor Yellow
    foreach ($u in $script:Created) {
        Show-Dom0 @("xe vm-list uuid=$u params=uuid,name-label,power-state", "xe vm-uninstall uuid=$u force=true")
    }
}
