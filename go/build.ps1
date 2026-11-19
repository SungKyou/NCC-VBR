# NCC-VBR Go 빌드 (Windows PowerShell용). 원본 c1 스크립트를 대신한다.
#   .\build.ps1            : linux/amd64 와 windows/amd64 를 모두 만든다
#   .\build.ps1 linux      : linux/amd64 만 만든다
#   .\build.ps1 test       : 단위 시험만 실행한다
# 산출물: bin\<os>-<arch>\vbr_cifs, vbr_remove, vbr_agent, vbr_vm, vbr_secret
param([ValidateSet('all', 'linux', 'windows', 'test')][string]$Target = 'all')

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
$commands = @('vbr_cifs', 'vbr_remove', 'vbr_agent', 'vbr_vm', 'vbr_secret')
$testTools = @('fakexapi', 'driver')

function Invoke-Build([string]$Os, [string]$Arch) {
    $ext = if ($Os -eq 'windows') { '.exe' } else { '' }
    $out = "bin/$Os-$Arch"
    New-Item -ItemType Directory -Force $out | Out-Null
    $env:CGO_ENABLED = '0'
    $env:GOOS = $Os
    $env:GOARCH = $Arch
    try {
        foreach ($cmd in $commands) {
            Write-Host "build $Os/$Arch $cmd"
            go build -trimpath -ldflags '-s -w' -o "$out/$cmd$ext" "./cmd/$cmd"
            if ($LASTEXITCODE -ne 0) { throw "build failed: $cmd" }
        }
        # vbr_vm demo.ps1 자동 모드용 가짜 XAPI(시험 도구)
        Write-Host "build $Os/$Arch vm-fakexapi (test tool)"
        go build -trimpath -o "$out/vm-fakexapi$ext" "./test/vm/fakexapi"
        if ($LASTEXITCODE -ne 0) { throw "build failed: vm-fakexapi" }
        if ($Os -eq 'linux') {
            foreach ($tool in $testTools) {
                Write-Host "build $Os/$Arch $tool (test tool)"
                go build -trimpath -o "$out/$tool" "./test/agent-parity/$tool"
                if ($LASTEXITCODE -ne 0) { throw "build failed: $tool" }
            }
        }
    }
    finally {
        Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
    }
}

switch ($Target) {
    'test' { go vet ./...; go test ./... }
    'linux' { Invoke-Build linux amd64 }
    'windows' { Invoke-Build windows amd64 }
    'all' { go vet ./...; Invoke-Build linux amd64; Invoke-Build windows amd64 }
}
