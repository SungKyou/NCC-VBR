# ═══════════════════════════════════════════════════════════════════
# demo_auto.ps1 — 자동 모드 (메뉴 A, 또는 demo.ps1 -Auto)
# 가짜 XAPI(vm-fakexapi) 두 개와 vbr_vm 을 임시 폴더에서 띄우고 시나리오를 돌려 PASS/FAIL 을 센다.
# 실제 XenServer 에는 접속하지 않는다. 끝나면 띄운 프로세스를 모두 끈다.
# 시험 번호는 docs/vm/06-test-plan-report.md 3장.
# ═══════════════════════════════════════════════════════════════════

# vm-fakexapi 의 시험용 암호(go/internal/vm/vmtest.Password). 실제 암호가 아니다.
$script:FAKE_PASSWORD = 'vbr-Secret-#1'

function Get-FreePort {
    $l = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
    $l.Start(); $port = $l.LocalEndpoint.Port; $l.Stop()
    return $port
}

function Start-Fake {
    param([string]$Work, [string]$Name, [string[]]$FakeArgs = @())
    $out = Join-Path $Work "$Name.out"
    $exe = Join-Path $script:BIN 'vm-fakexapi.exe'
    $p = Start-Process -FilePath $exe -ArgumentList $FakeArgs -RedirectStandardOutput $out -RedirectStandardError "$out.err" -WindowStyle Hidden -PassThru
    for ($i = 0; $i -lt 50; $i++) {
        Start-Sleep -Milliseconds 100
        $line = Get-Content $out -ErrorAction SilentlyContinue | Select-String '^ADDR=' | Select-Object -First 1
        if ($line) { return [pscustomobject]@{ Process = $p; Addr = $line.Line.Substring(5) } }
    }
    throw "vm-fakexapi did not start ($Name)"
}

function Write-AutoConfig {
    param([string]$Path, [string]$Work, [int]$Port, [string]$Token, [string]$Locale = 'ko')
    $w = $Work -replace '\\', '/'
    @"
server:
  port: $Port
  listen: 127.0.0.1
api_auth:
  enabled: true
  VBR_VM_API_TOKEN: "$Token"
crypto:
  enabled: false
locale: $Locale
logging:
  level: info
  format: custom
  output: file
  file_name: "$w/vbr_vm.log"
  max_size_mb: 50
  max_backups: 5
  max_age_days: 28
  compress: false
  local_time: true
  time_format: "2006-01-02 15:04:05.000"
  time_zone: "local"
  log_pattern: "[{time}][{level}] {msg} {attrs}"
  service_name: vbr_vm
backup:
  base_path: "$w/backup"
  timeout_hours: 1
concurrency:
  max_jobs: 2
xapi:
  progress_interval_sec: 1
task_store:
  dir: "$w/tasks"
"@ | Set-Content -Path $Path -Encoding utf8NoBOM
}

function Invoke-AutoTest {
    $script:PassCount = 0; $script:FailCount = 0
    foreach ($exe in 'vbr_vm.exe', 'vm-fakexapi.exe') {
        if (-not (Test-Path (Join-Path $script:BIN $exe))) { Write-Host "  ✗ $exe 가 없습니다. go\build.ps1 all 로 빌드하세요 ($script:BIN)" -ForegroundColor Red; return }
    }
    $work = Join-Path ([IO.Path]::GetTempPath()) ("vbr-vm-demo-" + (Get-Date -Format 'yyyyMMdd-HHmmss'))
    New-Item -ItemType Directory -Force $work | Out-Null
    Write-MenuHeader "자동 모드 — 작업 폴더 $work"
    $saved = @{ URL = $script:VBR_URL; TOKEN = $script:VBR_TOKEN; MASTER = $script:MASTER_IP; VM = $script:VM_UUID; PW = $script:XAPI_PASSWORD_VALUE; USER = $script:XAPI_USER }
    $procs = @()
    try {
        $fast = Start-Fake -Work $work -Name 'fake'
        $slow = Start-Fake -Work $work -Name 'fake-slow' -FakeArgs @('-slow')
        $procs += $fast.Process, $slow.Process
        $port = Get-FreePort
        $token = [guid]::NewGuid().ToString('N')
        $cfg = Join-Path $work 'sysconfig.yaml'
        Write-AutoConfig -Path $cfg -Work $work -Port $port -Token $token
        $svc = Start-Process -FilePath (Join-Path $script:BIN 'vbr_vm.exe') -ArgumentList @('-config', $cfg) `
            -RedirectStandardOutput (Join-Path $work 'vbr_vm.out') -RedirectStandardError (Join-Path $work 'vbr_vm.err') -WindowStyle Hidden -PassThru
        $procs += $svc
        $script:VBR_URL = "http://127.0.0.1:$port"; $script:VBR_TOKEN = $token
        $script:MASTER_IP = $fast.Addr; $script:VM_UUID = '42097417-b9ee-2f97-8eeb-3612efbcb25d'
        $script:XAPI_USER = 'root'; $script:XAPI_PASSWORD_VALUE = $script:FAKE_PASSWORD
        $up = $false
        for ($i = 0; $i -lt 50; $i++) { Start-Sleep -Milliseconds 200; if ((Invoke-Vbr -Method GET -Path '/health' -NoAuth).Status -eq 200) { $up = $true; break } }
        Test-Step 'U70 /health' $up "port $port"
        if (-not $up) { Get-Content (Join-Path $work 'vbr_vm.err') -ErrorAction SilentlyContinue | Select-Object -Last 10 | ForEach-Object { Write-Host "    $_" -ForegroundColor DarkGray }; return }

        $r = Invoke-Vbr -Method GET -Path '/api/v1/jobs' -NoAuth
        Test-Step 'U80 토큰 없음 401' ($r.Status -eq 401) "$($r.Body.error)"
        $bad = New-VbrJob -TaskId (New-TaskId 'bad') -Action export -Options @{}
        $r = Invoke-Vbr -Method POST -Path '/api/v1/jobs' -Body $bad
        Test-Step 'U02 필수 값 없음 400' ($r.Status -eq 400 -and $r.Body.code -eq 'MISSING_FIELD') "$($r.Body.code) $($r.Body.field)"

        $id = New-TaskId 'full'
        $null = Send-VbrJob (New-VbrJob -TaskId $id -Action export -Options @{ file_name = 'full' }) 'I01 전체 백업(Halted, auto)'
        $b = Wait-VbrJob $id -TimeoutSec 60 -Quiet
        Test-Step 'I01 전체 백업' ($b.status -eq 'succeeded' -and $b.payload.options.snapshot_used -eq $false) "size=$($b.payload.options.file_size_bytes) sha256=$($b.payload.options.checksum_sha256.Substring(0,12))…"
        $fullPath = $b.payload.options.file_path

        $r1 = Invoke-Vbr -Method POST -Path '/api/v1/jobs' -Body (New-VbrJob -TaskId $id -Action export -Options @{ file_name = 'full' })
        Test-Step 'U03 같은 taskId' ($r1.Status -eq 202 -and $r1.Body.message -like 'duplicate taskId*') "$($r1.Body.message)"

        $id = New-TaskId 'part'
        $null = Send-VbrJob (New-VbrJob -TaskId $id -Action export -Options @{ file_name = 'part'; disks = @(0); compression = 'gzip' }) 'I02 디스크 0만'
        $b = Wait-VbrJob $id -TimeoutSec 60 -Quiet
        Test-Step 'I02 디스크 선택 백업' ($b.status -eq 'succeeded' -and $b.payload.options.snapshot_used -eq $true -and "$($b.payload.options.disks)" -eq '0') "exported_uuid=$($b.payload.options.exported_uuid)"

        $id = New-TaskId 'zstd'
        $null = Send-VbrJob (New-VbrJob -TaskId $id -Action export -Options @{ file_name = 'z'; compression = 'zstd'; verify_after_export = $true }) 'U23·U41 zstd + 백업 뒤 검증'
        $b = Wait-VbrJob $id -TimeoutSec 60 -Quiet
        Test-Step 'U41 zstd + 검증' ($b.status -eq 'succeeded' -and $b.payload.options.verify.verified -eq $true -and $b.payload.options.verify.compression_detected -eq 'zstd') "$($b.payload.options.file_path)"
        $zPath = $b.payload.options.file_path

        $id = New-TaskId 'verify'
        $null = Send-VbrJob (New-VbrJob -TaskId $id -Action verify -Options @{ file_path = $fullPath }) 'I05 검증'
        $b = Wait-VbrJob $id -TimeoutSec 60 -Quiet
        Test-Step 'I05 XVA 검증' ($b.status -eq 'succeeded' -and $b.payload.options.verified -eq $true) "chunk_count=$($b.payload.options.chunk_count)"

        foreach ($f in @($fullPath, $zPath)) {
            $id = New-TaskId 'restore'
            $null = Send-VbrJob (New-VbrJob -TaskId $id -Action restore -Options @{ file_path = $f; sr_uuid = 'e4138437-7e20-480f-08b1-25e5b8b6027b'; restore_original_uuid = $true }) "I03·I04 복구 ($(Split-Path $f -Leaf))"
            $b = Wait-VbrJob $id -TimeoutSec 60 -Quiet
            Test-Step "I03 복구 $(Split-Path $f -Leaf)" ($b.status -eq 'succeeded' -and $b.payload.options.vm_uuid -and $b.payload.options.is_a_template_fixed -eq $true) "vm_uuid=$($b.payload.options.vm_uuid)"
        }

        $id = New-TaskId 'xerr'
        $null = Send-VbrJob (New-VbrJob -TaskId $id -Action export -Options @{ file_name = 'x' } -TargetUUID '00000000-0000-0000-0000-000000000000') 'I08 없는 VM'
        $b = Wait-VbrJob $id -TimeoutSec 60 -Quiet
        Test-Step 'I08 없는 VM' ($b.status -eq 'failed' -and $b.error.code -eq 'XAPI_ERROR' -and $b.error.detail -like '*UUID_INVALID*') "$($b.error.detail)"
        $id = New-TaskId 'auth'
        $null = Send-VbrJob (New-VbrJob -TaskId $id -Action export -Options @{ file_name = 'y' } -Password 'wrong') 'I08 틀린 암호'
        $b = Wait-VbrJob $id -TimeoutSec 60 -Quiet
        Test-Step 'I08 틀린 암호' ($b.error.code -eq 'XAPI_AUTH_FAILED') "$($b.error.code)"

        $id = New-TaskId 'cancel'
        $null = Send-VbrJob (New-VbrJob -TaskId $id -Action export -Options @{ file_name = 'c' } -MasterIP $slow.Addr) 'I07 처리 중 취소'
        $b = Wait-VbrJob $id -TimeoutSec 30 -Until @('running') -Quiet
        Start-Sleep -Milliseconds 800
        $c = Invoke-Vbr -Method POST -Path "/api/v1/jobs/$id/cancel"
        $b = Wait-VbrJob $id -TimeoutSec 60 -Quiet
        Test-Step 'I07 처리 중 취소' ($c.Status -eq 202 -and $b.status -eq 'canceled' -and -not (Test-Path (Join-Path $work 'backup\c.xva.partial'))) "cancel HTTP $($c.Status), status=$($b.status)"

        $r = Invoke-Vbr -Method GET -Path '/api/v1/jobs?limit=1000'
        Test-Step 'U50 목록 조회' ($r.Status -eq 200 -and $r.Body.count -ge 9) "count=$($r.Body.count)"

        Write-AutoConfig -Path $cfg -Work $work -Port $port -Token $token -Locale 'en'
        Start-Sleep -Seconds 2
        $id = New-TaskId 'locale'
        $null = Send-VbrJob (New-VbrJob -TaskId $id -Action verify -Options @{ file_path = $fullPath }) 'U71 설정 즉시 반영(locale=en)'
        $b = Wait-VbrJob $id -TimeoutSec 60 -Quiet
        Test-Step 'U71 locale 즉시 반영' ($b.payload.message -like 'XVA verification passed*') "$($b.payload.message)"

        $log = Get-Content (Join-Path $work 'vbr_vm.log') -Raw -ErrorAction SilentlyContinue
        $tasks = Get-Content (Join-Path $work 'tasks\jobs.jsonl') -Raw -ErrorAction SilentlyContinue
        Test-Step 'U82 암호가 로그·기록에 없음' (-not ($log -like "*$($script:FAKE_PASSWORD)*") -and -not ($tasks -like "*$($script:FAKE_PASSWORD)*")) 'vbr_vm.log, tasks\jobs.jsonl'
    } catch {
        Test-Step '자동 모드 실행' $false $_.Exception.Message
    } finally {
        foreach ($p in $procs) { if ($p -and -not $p.HasExited) { Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue } }
        $script:VBR_URL = $saved.URL; $script:VBR_TOKEN = $saved.TOKEN; $script:MASTER_IP = $saved.MASTER
        $script:VM_UUID = $saved.VM; $script:XAPI_PASSWORD_VALUE = $saved.PW; $script:XAPI_USER = $saved.USER
        Write-Summary
        Write-Host "  로그와 작업 기록: $work" -ForegroundColor Gray
    }
}
