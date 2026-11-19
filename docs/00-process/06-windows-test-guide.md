# Windows 사용자 시험 절차 (cifs → remove → agent → vbr_vm)

기준일: 2026-11-18 / 대상 실행 파일: `c:\dev\ncc-vbr-go\go\bin\windows-amd64\`

이 절차는 사용자가 Windows PC에서 직접 실행하는 확인 시험이다. 2026-11-18 전환 수행자가 같은 절차를 개발 PC의 임시 디렉터리에서 먼저 실행했다. 아래 "예상 결과"는 그 실행에서 나온 값이다. 날짜, 시각, 경로가 들어간 부분은 실행할 때마다 달라진다.

## 0. 공통

- 셸: PowerShell. 시험 디렉터리는 `C:\vbrtest`로 한다(지워도 되는 곳).
- 세 프로그램 모두 실행한 디렉터리(또는 `-home` 디렉터리)에서 `node_info.cfg`를 읽고 `server.log`를 쓴다.
- 인자 없이 실행하면 원본처럼 백그라운드로 분리되고 부모는 바로 0으로 끝난다. 결과는 `server.log`로 본다. 시험에서는 `-foreground`로 실행해 종료 코드까지 본다.

| 종료 코드 | 뜻 |
|---|---|
| 0 | 정상 |
| 1 | 시작 실패 또는 중단(대상 없음 등) |
| 2 | 끝까지 처리했으나 일부 오류 |
| 255 | 인자 오류(사용법 출력) |

아래 두 변수를 **가장 먼저** 정한다. PowerShell 창을 새로 열 때마다 다시 정해야 한다. 정하지 않으면 경로가 비어 `\vbr_cifs.exe`를 찾지 못하고, 시험 폴더가 `C:\` 바로 아래에 만들어진다.

```powershell
$B = "c:\dev\ncc-vbr-go\go\bin\windows-amd64"
$W = "C:\vbrtest"
Test-Path "$B\vbr_cifs.exe"     # True 여야 한다
```

표 안의 명령은 앞뒤의 `` ` `` 기호를 빼고 안쪽만 복사한다.

## 1. cifs

### 1.1 준비

```powershell
New-Item -ItemType Directory -Force "$W\cifs","$W\src\a\deep","$W\src\b dir","$W\src\skip","$W\tgt" | Out-Null
Set-Content "$W\src\root.txt" "root file"
Set-Content "$W\src\a\one.txt" "one"
Set-Content "$W\src\a\deep\two.txt" "two"
Set-Content "$W\src\b dir\three.txt" "three"
Set-Content "$W\src\skip\x.txt" "excluded"
$other = ((Get-Date).DayOfWeek.value__ + 1) % 7     # 오늘이 아닌 요일
@"
[VBR Manager]
Full Scan Day	= $other
MAX Log size		= 100000000
MAX Thread cnt		= 2
EXCLUSION DIR	=	$W\src\skip
"@ | Set-Content "$W\cifs\node_info.cfg"
cd "$W\cifs"
function Total { (Select-String server.log -Pattern '^\[.*\]Total File' | Select-Object -Last 1).Line }
```

### 1.2 시험

백업 명령의 구성: `& "$B\vbr_cifs.exe" -foreground 원본폴더 대상폴더 일련번호`. 일련번호(17자리)는 관리 프로그램이 실행마다 붙이는 번호로 로그에만 남고 결과에는 영향이 없다. 실행을 구분하려고 끝자리를 001, 002, …로 바꾼다. `$LASTEXITCODE`는 종료 코드, `Total`은 마지막 합계 줄을 보여 준다. 아래 블록을 순서대로 붙여 넣는다.

**W-CIFS-01 버전**
```powershell
& "$B\vbr_cifs.exe" -version
```
예상: `NCC-VBR CIFS Manager version = 3.0000`

**W-CIFS-02 인자 오류**
```powershell
& "$B\vbr_cifs.exe" only-one; $LASTEXITCODE
```
예상: 사용법 한 줄, `255`

**W-CIFS-03 첫 실행(전체 복사)**
```powershell
& "$B\vbr_cifs.exe" -foreground "$W\src" "$W\tgt" 20261118100000001; $LASTEXITCODE; Total
Get-ChildItem "$W\tgt" -Recurse -Name; Get-Content history\*
```
예상: `0`, `Total File = [4], Total Dir = [4], Change file = [4], Create dir = [3], Error = [0]`. `tgt`에 `root.txt`, `a\one.txt`, `a\deep\two.txt`, `b dir\three.txt`가 있고 `skip`은 없다. 기준 시각 파일 내용은 `시작시각초 [날짜 시각]`

**W-CIFS-04 원본 변경 후 실행(증분 복사, 이전 버전 보관)**
```powershell
Set-Content "$W\src\a\one.txt" "one changed and longer"; Set-Content "$W\src\b dir\new.txt" "brand new"
& "$B\vbr_cifs.exe" -foreground "$W\src" "$W\tgt" 20261118100000002; $LASTEXITCODE; Total
Get-ChildItem "$W\tgt\a" -Name; Get-Content "$W\tgt\a\one.txt"
```
예상: `0`, `Total File = [5], … Change file = [2], Create dir = [0], Error = [0]`. `a`에 `deep`, `one.txt`, `one.txt-vbr-YYYYMMDDHHMMSS.zip`(이전 내용 gzip). `one.txt` 내용은 `one changed and longer`

**W-CIFS-05 변경 없이 실행**
```powershell
& "$B\vbr_cifs.exe" -foreground "$W\src" "$W\tgt" 20261118100000003; $LASTEXITCODE; Total
```
예상: `0`, `Change file = [0]`

**W-CIFS-06 대상만 손상시킨 뒤 실행**
```powershell
Set-Content "$W\tgt\root.txt" "target damaged, size differs"
& "$B\vbr_cifs.exe" -foreground "$W\src" "$W\tgt" 20261118100000004; $LASTEXITCODE; Total
Get-Content "$W\tgt\root.txt"
```
예상: `0`, `Change file = [0]`, 내용은 `target damaged, size differs` 그대로. 원본 파일이 지난 백업 뒤로 건드려지지 않았으면 비교하지 않는 원본 동작이다(K-CIFS-03). 아래 주의 참고

**W-CIFS-07 Full Scan Day를 오늘 요일로 바꾼 뒤 실행(전체 비교)**
```powershell
(Get-Content node_info.cfg) -replace '^Full Scan Day.*', "Full Scan Day = $((Get-Date).DayOfWeek.value__)" | Set-Content node_info.cfg
Get-Content node_info.cfg
& "$B\vbr_cifs.exe" -foreground "$W\src" "$W\tgt" 20261118100000005; $LASTEXITCODE; Total
Get-Content "$W\tgt\root.txt"; Get-ChildItem "$W\tgt" -Name
Select-String server.log -Pattern 'Full search|base time' | Select-Object -Last 2 | % Line
```
예상: 설정에 `Full Scan Day = 오늘 요일 숫자`(0=일요일). `0`, `Change file = [1]`. `root.txt`가 `root file`로 돌아오고 `root.txt-vbr-….zip`이 생긴다. 로그에 `Full search mode enable.[요일]`, `Backup base time  = [0]`

**W-CIFS-08 백그라운드 실행**
```powershell
& "$B\vbr_cifs.exe" "$W\src" "$W\tgt" 20261118100000006; $LASTEXITCODE
Start-Sleep 3; Select-String server.log -Pattern 'backup finished' | Select-Object -Last 1 | % Line
```
예상: 바로 `0`. 몇 초 뒤 `VBR CIFS backup finished.` 줄

**W-CIFS-09 대상 폴더가 없을 때**
```powershell
& "$B\vbr_cifs.exe" -foreground "$W\src" "$W\nope" 20261118100000007; $LASTEXITCODE
Select-String server.log -Pattern 'not valid|STOP RUN' | Select-Object -Last 2 | % Line
```
예상: `1`. `Target directory [...nope] is not valid.`, `STOP RUN`. 기준 시각 파일은 바뀌지 않는다

주의

- W-CIFS-06은 Windows의 마지막 접근 시각 갱신 설정에 따라 1이 나올 수 있다. Windows에는 Linux의 상태 변경 시각(ctime)이 없어 생성 시각을 대신 쓰고, 접근 시각 갱신은 시스템 설정과 1시간 단위 지연에 따라 달라진다. 1이 나오면 실패가 아니라 Windows 시각 규칙의 차이로 기록한다(단위 시험 T-CIFS-U05도 Windows에서는 건너뛴다). 운영 대상은 Linux다.
- `history` 파일 이름은 원본 규칙대로 경로의 첫 글자(드라이브 문자)를 빼고 구분자를 `_`로 바꾼다.

## 2. remove

cifs 시험이 끝난 `C:\vbrtest\tgt`를 그대로 쓴다. 이전 버전 파일 2개(`one.txt-vbr-…`, `root.txt-vbr-…`)가 오늘 시각으로 있다.

### 2.1 준비

```powershell
New-Item -ItemType Directory -Force "$W\remove" | Out-Null
cd "$W\remove"
Set-Content "$W\tgt\a\old.txt-vbr-20250101000000.zip" "old"          # 오래된 이전 버전
Set-Content "$W\tgt\a\bad-vbr-2025010100000X.zip" "bad stamp"        # 시각 자리가 숫자가 아님
Set-Content "$W\tgt\a\note-vbr-.zip" "short"                         # 형식이 맞지 않음
@"
[File Remove (BATCH) for NCC-VBR]
MAX Log size		= 100000000
MAX Thread cnt		= 2
"@ | Set-Content node_info.cfg
function Total { (Select-String server.log -Pattern '^\[.*\]Total File' | Select-Object -Last 1).Line }
```

### 2.2 시험

삭제 명령의 구성: `& "$B\vbr_remove.exe" -foreground 대상폴더 보존일수`. 보존 일수보다 오래된 이전 버전 파일(`이름-vbr-YYYYMMDDHHMMSS.zip`)이 대상이다. 설정에 `Delete Mode = DELETE`가 없으면 지우지 않고 `delete.log`에 지울 명령만 적는다(LOG 모드).

**W-RM-01 버전**
```powershell
& "$B\vbr_remove.exe" -version
```
예상: `File Remove (BATCH) for NCC-VBR version = 2.000`

**W-RM-02 잘못된 보존 일수**
```powershell
& "$B\vbr_remove.exe" "$W\tgt" abc; $LASTEXITCODE
& "$B\vbr_remove.exe" "$W\tgt" 106752; $LASTEXITCODE
```
예상: 두 번 모두 `days must be an integer between 0 and 106751`, 사용법, `255`. 백그라운드로 분리되기 전에 거부한다

**W-RM-03 LOG 모드, 보존 30일**
```powershell
& "$B\vbr_remove.exe" -foreground "$W\tgt" 30; $LASTEXITCODE; Total
Get-Content delete.log
```
예상: `0`, `Delete files = [1]`. `delete.log`에 `rm -f -- '…\a\old.txt-vbr-20250101000000.zip'` 한 줄. 파일은 지워지지 않는다

**W-RM-04 LOG 모드, 보존 0일**
```powershell
& "$B\vbr_remove.exe" -foreground "$W\tgt" 0; $LASTEXITCODE; Total
Get-Content delete.log
Get-ChildItem "$W\tgt" -Recurse -Filter *-vbr-* -Name
```
예상: `0`, `Delete files = [3]`. `delete.log`가 새로 쓰여 `root.txt-vbr-…`, `old.txt-vbr-…`, `one.txt-vbr-…` 세 줄(순서는 다를 수 있다). `bad-vbr-…X.zip`, `note-vbr-.zip`은 대상이 아니다. 파일 목록은 5개 그대로다

**W-RM-05 DELETE 모드, 보존 30일**
```powershell
Add-Content node_info.cfg "Delete Mode = DELETE"
& "$B\vbr_remove.exe" -foreground "$W\tgt" 30; $LASTEXITCODE; Total
Get-ChildItem "$W\tgt" -Recurse -Filter *-vbr-* -Name
```
예상: `0`, `Delete files = [1]`. `old.txt-vbr-20250101000000.zip`만 없어지고 4개가 남는다. `server.log`에 `File Name [...] : Delete finished.`

**W-RM-06 DELETE 모드, 보존 0일**
```powershell
& "$B\vbr_remove.exe" -foreground "$W\tgt" 0; $LASTEXITCODE; Total
Get-ChildItem "$W\tgt" -Recurse -Filter *-vbr-* -Name
Test-Path delete.log
```
예상: `0`, `Delete files = [2]`. `bad-vbr-2025010100000X.zip`, `note-vbr-.zip`만 남는다. `delete.log`는 없다(`False`, 실행 시작 때 지운다)

**W-RM-07 폴더가 없을 때**
```powershell
& "$B\vbr_remove.exe" -foreground "$W\nope" 0; $LASTEXITCODE
```
예상: `1`. `server.log`에 `TASK register : Internal error.  STOP RUN!!!`

일반 파일(`root.txt`, `one.txt` 등)은 어느 경우에도 지워지지 않아야 한다.

## 3. agent

실제 풀 대신 시험용 가짜 XAPI(`fakexapi`)를 이 PC에서 띄운다. 관리 서버 대신 PowerShell 함수(`go\test\agent\vbr-send.ps1`)로 전문을 보낸다.

가짜 XAPI가 받아들이는 값: VM uuid `22acc256-b431-f9df-6c44-b1f71e7912fc`, SR uuid는 아무 값(복구 성공 시 새 VM uuid `cd5498f2-0342-db8e-749e-53a5e86744dd`). 스냅샷 이름을 `slow`로 주면 내보내기가 약 12초 걸린다(대기·취소 시험용). XVA 내용은 확인하지 않는다(XVA 검증은 따로 한다).

agent는 모든 주소에서 수신하므로 처음 실행할 때 Windows 방화벽 창이 뜰 수 있다. 이 시험은 127.0.0.1만 쓰므로 취소해도 된다.

### 3.1 준비

```powershell
$A = "$W\agent"
New-Item -ItemType Directory -Force "$A\BACKUP" | Out-Null
cd c:\dev\ncc-vbr-go\go
go build -o "$B\fakexapi.exe" ./test/agent-parity/fakexapi
Set-Content "$W\xva-template.bin" "fake xva"
Start-Process "$B\fakexapi.exe" -ArgumentList "-listen","127.0.0.1:8443","-xva","$W\xva-template.bin","-log","$W\xapi.log" -WindowStyle Hidden

cd $A
& "$B\vbr_secret.exe" keygen "$A\agent.keyring"
$enc = "test-password" | & "$B\vbr_secret.exe" encrypt "$A\agent.keyring"
@"
[VBR Agent for Xenserver]
PORT-NO = 7102
MAX Log size = 100000000
XAPI Address = https://127.0.0.1:8443
XAPI User = root
XAPI Password = $enc
XAPI Verify SSL = N
XAPI Keyring = $A\agent.keyring
Backup Root = $A\BACKUP
Allow Client = 127.0.0.1
Async Thread = 3
"@ | Set-Content node_info.cfg
& "$B\vbr_agent.exe"
. c:\dev\ncc-vbr-go\go\test\agent\vbr-send.ps1
$vm = "22acc256-b431-f9df-6c44-b1f71e7912fc"; $sr = "9ad088e0-d03d-0269-0b7f-e40006aa5014"
```

예상: `keyring created : …`, agent 부모 종료 0. `server.log`에 `XAPI address [https://127.0.0.1:8443] user [root] backup root [...]`, `[0]~[2]th async thread starting.`, `VBR agent for Xenserver service start.`

응답 읽는 법: `B` + 전문 번호 17자리 + 결과 코드 1자리(0 정상, 9 오류, 3 처리 중, C 취소됨) + 제한 시간 3자리 + 우선순위 1자리 + 본문.

### 3.2 시험

전문 구성: `B` + 전문 번호 17자리 + 작업 구분 1자리(A 비동기 작업, Q 결과 조회, C 취소, S·T·I 미지원) + 제한 시간 3자리 + 우선순위 1자리 + 본문. 예: `B20261118100000001` `A` `000` `5` `BACKUP …`. 전문 번호는 요청마다 다르게 준다. 비동기 작업(A)은 바로 `OK`만 돌려주고, 결과는 `Wait-Vbr 전문번호`로 조회한다.

**W-AG-01·02 백업 접수와 결과**
```powershell
Send-Vbr "B20261118100000001A0005BACKUP $vm snap1 $A\BACKUP\17.xva"
Wait-Vbr 20261118100000001
Get-ChildItem "$A\BACKUP" -Name
```
예상: 접수 `B2026111810000000100005OK`. 결과는 18번째 뒤 코드 `0`, `VM BACKUP Success : VM uuid=22acc256-…, Snapshot VM uuid=…, backup filename=…\BACKUP\17.xva`. `17.xva`가 생기고 `backup.log`에 내보내기·스냅샷 삭제 기록

**W-AG-03 형식 오류**
```powershell
Send-Vbr "X20261118100000002A0005BACKUP x"
```
예상: `X20261118100000002` + `9` + `0005BACKUP x`(받은 전문의 결과 자리를 9로 바꿔 돌려준다)

**W-AG-04 잘못된 VM uuid**
```powershell
Send-Vbr "B20261118100000003A0005BACKUP 1234 s $A\BACKUP\x.xva"
Wait-Vbr 20261118100000003
```
예상: 접수 `OK`, 결과 코드 `9`, `[ERROR]VM UUID is not valid.`

**W-AG-05 허용 경로(Backup Root) 밖**
```powershell
Send-Vbr "B20261118100000004A0005BACKUP $vm s C:\Windows\Temp\out.xva"
Wait-Vbr 20261118100000004
```
예상: 결과 코드 `9`, `[ERROR]Export File Name is not allowed.` 파일이 생기지 않는다

**W-AG-06 부분 백업**
```powershell
Send-Vbr "B20261118100000005A0005PARTIAL $vm 100000000000000 p $A\BACKUP\p.xva"
Wait-Vbr 20261118100000005
```
예상: 접수 `OK`, 결과 코드 `0`, `VM BACKUP Success … backup filename=…\p.xva`

**W-AG-07 복구**
```powershell
Send-Vbr "B20261118100000006A0005RESTORE $A\BACKUP\17.xva true $sr"
Wait-Vbr 20261118100000006
```
예상: 접수 `OK`, 결과 코드 `0`, 본문 `cd5498f2-0342-db8e-749e-53a5e86744dd`(새 VM uuid)

**W-AG-08 복구 인자 오류(예약 옵션 자리를 비움, 공백 두 칸)**
```powershell
Send-Vbr "B20261118100000007A0005RESTORE $A\BACKUP\17.xva  $sr"
Wait-Vbr 20261118100000007
```
예상: 결과 코드 `9`, `[ERROR]SR UUID is not valid.`

**W-AG-09 미지원 작업(S, T, I)**
```powershell
Send-Vbr "B20261118100000008S0050df -k /"
Send-Vbr "B20261118100000009T00002 2 2"
Send-Vbr "B20261118100000010I0000TASK"
```
예상: 셋 다 결과 코드 `9`, `[ERROR]Not supported.`

**W-AG-10·11 없는 작업 조회와 취소**
```powershell
Send-Vbr "B20261118100000011Q000120261118109999999"
Send-Vbr "B20261118100000012C000120261118109999999"
```
예상: 조회는 코드 `9`, 본문은 조회한 번호. 취소는 코드 `9`, `Request message not found.(Invalid or Already finished).`

**W-AG-12 일괄 취소(대기 작업 없음)**
```powershell
Send-Vbr "B20261118100000013C000100000000000000000"
```
예상: 코드 `C`, `Multi cancel summary : total [100], cancel [0], failed [100]`

**W-AG-13 처리 중·대기 중 조회와 취소** (작업자 3개에 느린 백업 5개를 넣는다. 첫 줄 실행 뒤 바로 이어서 붙여 넣는다)
```powershell
21..25 | % { Send-Vbr "B202611181000000${_}A0005BACKUP $vm slow $A\BACKUP\s$_.xva" }; Start-Sleep -Milliseconds 1500
Send-Vbr "B20261118100000030Q000120261118100000021"
Send-Vbr "B20261118100000031Q000120261118100000024"
Send-Vbr "B20261118100000032C000120261118100000021"
Send-Vbr "B20261118100000033C000120261118100000024"
Send-Vbr "B20261118100000034Q000120261118100000024"
```
예상(차례대로):
1. 접수 `OK` 다섯 줄
2. 21번 조회: 코드 `3`(처리 중), 본문은 원래 요청
3. 24번 조회: 코드 `2`(대기 중), 본문은 원래 요청
4. 21번 취소: 코드 `9`, `Request message is processing.`(실행 중인 작업은 취소하지 않는다)
5. 24번 취소: 코드 `0`, `Request message is cancel.[20261118100000033]`
6. 24번 조회: 코드 `C`(취소됨), 본문은 원래 요청

**W-AG-14 나머지 작업의 결과**
```powershell
foreach ($s in 21,22,23,25) { Wait-Vbr "202611181000000$s" 90 }
Get-ChildItem "$A\BACKUP" -Name
```
예상: 넷 다 코드 `0`, `VM BACKUP Success`. 25번은 앞 작업이 끝난 뒤 시작해 전체 약 25초 걸린다. `s24.xva`는 생기지 않는다

### 3.3 정리

```powershell
Stop-Process -Name vbr_agent, fakexapi
```

남는 파일: `server.log`, `transaction-YYYYMM.log`(전문 단계 기록), `backup.log`, `vbr_agent.state`, `BACKUP\*.xva`.

### 3.4 이 시험이 확인하지 않는 것

실제 XenServer XAPI(인증서, 세션, 실제 내보내기·가져오기 시간), NAS 마운트, 실제 관리 서버의 접속 방식, 원격 서버에서의 `Allow Client` 거부. 이것들은 Linux agent 서버와 시험 풀에서 하는 PT/IT 시험([agent/06-test-report.md](../agent/06-test-report.md) 5장)의 범위다.

## 4. agent 실제 XenServer 시험 (Windows PC에서 원격 실행)

2026-11-18 사용자 결정: agent는 가짜 XAPI 대신 실제 시험 풀로 확인한다. 전환본은 XAPI로 원격 접속하므로(agentless) Windows PC에서 실행해도 된다. [agent/06](../agent/06-test-report.md) 5장의 PT·IT 중 Windows에서 할 수 있는 것을 합쳤다. 관리 서버 실제 연동(IT-06), 원본 agent와의 시간 비교(PT-03), 다른 주소에서의 접속 거부(IT-07)는 Linux agent 서버 단계에서 한다.

| 항목 | 값 |
|---|---|
| 풀 마스터 | `192.168.102.53`(PC에서 443 접속 확인, 2026-11-18) |
| 시험 VM | `42097417-b9ee-2f97-8eeb-3612efbcb25d` |
| 복구 SR | `e4138437-7e20-480f-08b1-25e5b8b6027b` |
| PC 작업 폴더 | `C:\temp\vbr`(남은 공간 100GB) |

전환 수행자는 풀에 접속하지 않는다. `[dom0]`로 표시한 명령은 사용자가 XenServer 콘솔(SSH)에서 실행한다. 모두 조회 명령이고, 지우는 명령은 R9 정리에만 있다.

**지켜야 할 것**
- 복구(RESTORE)는 새 VM을 만든다. 복구한 VM은 시작하지 않는다. 원본 VM과 IP가 같아 네트워크 충돌이 날 수 있다. 시작 확인이 필요하면 원본 VM을 끄거나 복구 VM의 네트워크를 뗀 뒤 한다.
- 정리(`vm-uninstall`)할 때는 **복구로 생긴 새 uuid인지** 두 번 확인한다. 시험 VM `42097417-…`을 지우면 안 된다.
- 풀 root 암호는 대화나 파일에 평문으로 남기지 않는다. R1에서 화면 입력으로 바로 암호화한다.

### R0. 사전 확인 [dom0]

```
xe vm-list uuid=42097417-b9ee-2f97-8eeb-3612efbcb25d params=name-label,power-state
xe vbd-list vm-uuid=42097417-b9ee-2f97-8eeb-3612efbcb25d type=Disk params=userdevice,vdi-uuid
xe vdi-list params=uuid,virtual-size,physical-utilisation uuid=b5adc735-0911-4c67-a811-b301ecaf13f2
xe vdi-list params=uuid,virtual-size,physical-utilisation uuid=b2408a84-c9c1-4bd0-9dbf-e546e36e38e6
xe snapshot-list snapshot-of=42097417-b9ee-2f97-8eeb-3612efbcb25d params=uuid,name-label
xe sr-list uuid=e4138437-7e20-480f-08b1-25e5b8b6027b params=name-label,physical-size,physical-utilisation
xe vif-list vm-uuid=42097417-b9ee-2f97-8eeb-3612efbcb25d params=device,MAC
```
기록: 디스크 수(R5 부분 백업은 2개 이상일 때만), 디스크 사용량(백업 파일 크기와 시간을 가늠한다. R6에서 백업 3개를 동시에 두므로 사용량 × 3이 100GB 안이어야 한다), 기존 스냅샷 목록(R2 뒤 비교 기준), SR 남은 공간(복구 VM이 들어갈 자리), 원본 MAC(R3·R4 비교 기준).

**2026-11-18 사용자 실행 결과(기준값)**

| 항목 | 값 | 판단 |
|---|---|---|
| VM | `copy_20gb2`, halted | 꺼진 VM이라 복구 VM과 네트워크 충돌 걱정이 작다 |
| 디스크 | userdevice 0: `b5adc735-…` 20 GiB, 사용 약 373 MiB / userdevice 1: `b2408a84-…` 1 GiB, 사용 약 6.5 KB | R5 가능. `100000000000000`은 0번만 남기고 1번을 뺀다 |
| 백업 크기 | 사용량 기준 1 GB 미만으로 예상 → **실제 2,588,192,829바이트(2.59 GB), 약 4분 28초**. 예상이 틀렸다. 기존 스냅샷이 있어 디스크 0의 `physical-utilisation`은 스냅샷 뒤에 바뀐 부분(맨 끝 VHD)만 센다. 백업은 스냅샷 체인 전체를 합쳐 내보내므로 크기는 체인 전체 데이터(복구 VHD 5,984,395,776바이트)에 가깝다 | R6(3개 중 2개 생성)도 100GB 안 |
| 기존 스냅샷 | `926b6f3f-3f84-3f6d-75ef-2f2817f24fb9` (`copy_20gb2 스냅샷 20260930-161722`) | 시험 전부터 있던 것. 시험 뒤에도 이것 하나만 있어야 하고, R9에서 지우지 않는다 |
| SR | `ASUS NFS`, 전체 3.58 TiB, 남은 약 683 GiB | 복구 VM 3개 충분 |
| 원본 MAC | device 0: `5a:f2:8a:47:97:a1` | R3은 달라야, R4는 같아야 한다 |

스냅샷 디스크 기준값도 받아 둔다(PT-01 비교용):
```
xe vdi-list snapshot-of=b5adc735-0911-4c67-a811-b301ecaf13f2 params=uuid
xe vdi-list snapshot-of=b2408a84-c9c1-4bd0-9dbf-e546e36e38e6 params=uuid
```
기존 스냅샷이 하나이므로 보통 각각 uuid 1개가 나온다. 나온 수를 기록한다.

### R1. 준비 (PC)

```powershell
$B = "c:\dev\ncc-vbr-go\go\bin\windows-amd64"
$R = "C:\temp\vbr"
New-Item -ItemType Directory -Force "$R\BACKUP" | Out-Null
cd $R
& "$B\vbr_secret.exe" keygen "$R\agent.keyring"
$enc = Read-Host "XenServer root 암호" -AsSecureString |
    ForEach-Object { [Net.NetworkCredential]::new('', $_).Password } |
    & "$B\vbr_secret.exe" encrypt "$R\agent.keyring"
$enc.Substring(0, 4)        # ENC[ 여야 한다
# 주의(2026-11-18 실행에서 확인):
# - $OutputEncoding 이 [Text.Encoding]::UTF8 이면 파이프 입력 앞에 BOM 이 붙는다. 2026-11-18 이후 vbr_secret 은 BOM 을 뗀다.
#   그 전에 만든 실행 파일로 암호화했다면 다시 빌드하고 다시 암호화한다(증상: SESSION_AUTHENTICATION_FAILED).
# - PowerShell 변수 이름은 대소문자를 구분하지 않는다. $r 을 따로 쓰면 $R(작업 폴더)을 덮어쓴다.
@"
[VBR Agent for Xenserver]
PORT-NO = 7102
MAX Log size = 100000000
XAPI Address = https://192.168.102.53
XAPI User = root
XAPI Password = $enc
XAPI Verify SSL = N
XAPI Keyring = $R\agent.keyring
Backup Root = $R\BACKUP
Allow Client = 127.0.0.1
Async Thread = 2
"@ | Set-Content node_info.cfg
. c:\dev\ncc-vbr-go\go\test\agent\vbr-send.ps1
$vm = "42097417-b9ee-2f97-8eeb-3612efbcb25d"; $sr = "e4138437-7e20-480f-08b1-25e5b8b6027b"
```

**agent 실행: PowerShell 창을 하나 더 열어** 아래를 실행하고 그 창은 그대로 둔다(R7에서 강제 종료·재기동한다).
```powershell
cd C:\temp\vbr; & "c:\dev\ncc-vbr-go\go\bin\windows-amd64\vbr_agent.exe" -foreground
```
처음 창으로 돌아와 확인:
```powershell
Get-Content "$R\server.log" -Tail 8
```
예상: `XAPI address [https://192.168.102.53] user [root] backup root [C:\temp\vbr\BACKUP] async thread [2]`, `[0]·[1]th async thread starting.`, `VBR agent for Xenserver service start.` 방화벽 창이 뜨면 취소해도 된다(127.0.0.1만 쓴다).

이후 PC 명령은 모두 처음 창에서 실행한다. 백업·복구는 VM 크기에 따라 몇 분 이상 걸릴 수 있어 `Wait-Vbr`에 제한 시간(초)을 크게 준다.

### R2. 전체 백업 (IT-01, PT-01)

```powershell
Send-Vbr "B20261118200000001A0005BACKUP $vm vbrtest-full $R\BACKUP\full.xva"
Start-Sleep 5; Send-Vbr "B20261118200000002Q000120261118200000001"
Wait-Vbr 20261118200000001 -TimeoutSec 7200
Get-ChildItem "$R\BACKUP" | Select-Object Name, Length, LastWriteTime
Get-Content "$R\backup.log" -Tail 12
Select-String "$R\server.log" -Pattern 'login|failed|ERROR' | Select-Object -Last 5 | % Line
```
예상
- 접수 `B2026111820000000100005OK`. 5초 뒤 조회는 코드 `3`(처리 중). 이때 XenCenter에서 `vbrtest-full` 스냅샷과 내보내기 작업이 보인다
- 결과 코드 `0`, `VM BACKUP Success : VM uuid=42097417-…, Snapshot VM uuid=<스냅샷>, backup filename=C:\temp\vbr\BACKUP\full.xva`
- `full.xva`가 생긴다(압축 내보내기라 디스크 사용량보다 작다)
- `backup.log`에 순서대로 `SNAPSHOT create`(success), `SNAPSHOT VM property Change`, `VM BACKUP`(export succeeded), `SNAPSHOT destroy`(Snapshot VM remove)
- `server.log`에 `XAPI login success [https://192.168.102.53]`

[dom0] 확인(PT-01):
```
xe snapshot-list snapshot-of=42097417-b9ee-2f97-8eeb-3612efbcb25d params=uuid,name-label
xe vdi-list snapshot-of=b5adc735-0911-4c67-a811-b301ecaf13f2 params=uuid
xe vdi-list snapshot-of=b2408a84-c9c1-4bd0-9dbf-e546e36e38e6 params=uuid
```
예상: 스냅샷은 기존 `926b6f3f-…` 하나뿐이다(`vbrtest-full`이 없다). 스냅샷 디스크 수가 R0 기준값과 같다.

백업 시간을 기록한다(`backup.log`의 `SNAPSHOT create`와 `SNAPSHOT destroy` 시각 차이). 디스크 사용량이 작아 수십 초 안에 끝날 수 있다. 그러면 5초 뒤 조회가 이미 코드 `0`일 수 있고, 그것도 정상이다. R6·R7의 대기 시간은 이 시간에 맞춘다. (2026-11-18 실행: 4분 28초. 이후 백업도 4분 24~33초)

`Wait-Vbr` 결과 줄의 전문 번호는 마지막 조회를 보낸 시각으로 만든 번호다. 작업이 끝난 시각이 아니다. 시간은 `backup.log`로 본다.

### R3. 복구, MAC 보존 안 함 (IT-03, PT-02 false)

```powershell
Send-Vbr "B20261118200000003A0005RESTORE $R\BACKUP\full.xva false $sr"
Wait-Vbr 20261118200000003 -TimeoutSec 7200
```
예상: 접수 `OK`, 결과 코드 `0`, 본문은 새 VM uuid 하나(이하 `<NEW1>`). `backup.log`에 `VM IMPORT` 기록.

[dom0]:
```
xe vm-list uuid=<NEW1> params=name-label,power-state,is-a-template
xe vbd-list vm-uuid=<NEW1> type=Disk params=userdevice
xe vif-list vm-uuid=<NEW1> params=device,MAC
```
예상: `is-a-template ( RO): false`, `power-state: halted`, 디스크 수는 R0과 같다, MAC은 R0의 원본 MAC과 **다르다**.

### R4. 복구, MAC 보존 (PT-02 true)

```powershell
Send-Vbr "B20261118200000004A0005RESTORE $R\BACKUP\full.xva true $sr"
Wait-Vbr 20261118200000004 -TimeoutSec 7200
```
예상: 결과 코드 `0`과 새 VM uuid(`<NEW2>`). [dom0] `xe vif-list vm-uuid=<NEW2> params=device,MAC`의 MAC이 원본과 **같다**.

이 항목은 확인할 것이 남은 항목이다. 전환본은 XAPI import의 `restore=true`를 원본의 `xe vm-import preserve=true`와 같다고 보고 있다(agent/04). 원본 VM이 같은 풀에 있어서 오류(코드 9)가 나거나, MAC이 다르면 그 응답과 `server.log` 끝 10줄을 그대로 기록한다. 판정은 전환 수행자가 한다.

**2026-11-18 실행 결과와 대체 절차.** 원본 VM이 있는 풀에서는 코드 `9` `[ERROR]XAPI task failure: DUPLICATE_MAC_SEED: <원본 VM의 other-config:mac_seed>`로 거부되고 VM은 남지 않는다. XenServer가 같은 mac_seed의 VM이 있으면 MAC 보존 가져오기를 거부하기 때문이다. 원본의 `xe vm-import preserve=true`(force 없음)도 같은 검사를 거치므로 같은 결과로 본다(원본으로 직접 실행하지는 않음). MAC 보존은 원본 VM을 건드리지 않고 아래처럼 확인했다.
1. R3의 `<NEW1>`을 백업한다: `Send-Vbr "B20261118200000411A0005BACKUP <NEW1> vbrtest-new1 $R\BACKUP\new1.xva"`
2. [dom0] `<NEW1>`의 uuid와 이름을 확인한 뒤 `xe vm-uninstall uuid=<NEW1> force=true`
3. `Send-Vbr "B20261118200000412A0005RESTORE $R\BACKUP\new1.xva true $sr"` → `<NEW2>`
4. [dom0] `<NEW2>`의 MAC이 `<NEW1>`의 MAC과 같다(통과)

`<NEW2>`의 uuid는 1의 백업 결과에 나온 **스냅샷 uuid와 같다**. 백업 파일에는 스냅샷이 들어가고 MAC 보존 가져오기는 파일 속 uuid도 그대로 쓰기 때문이다. 그래서 같은 백업 파일을 `true`로 두 번 복구할 수 없다(두 번째는 uuid가 겹친다, 미실행). VM 이름은 백업 명령에 준 스냅샷 이름(`vbrtest-new1`)이 된다.

### R5. 부분 백업 (IT-02, 디스크 2개 이상일 때만)

디스크 위치(userdevice) 0만 받는다(15자리 중 첫 자리 `1`).
```powershell
Send-Vbr "B20261118200000005A0005PARTIAL $vm 100000000000000 vbrtest-part $R\BACKUP\part.xva"
Wait-Vbr 20261118200000005 -TimeoutSec 7200
Send-Vbr "B20261118200000006A0005RESTORE $R\BACKUP\part.xva false $sr"
Wait-Vbr 20261118200000006 -TimeoutSec 7200
Get-ChildItem "$R\BACKUP" | Select-Object Name, Length
```
예상: 백업 결과 코드 `0` `VM BACKUP Success … part.xva`, `part.xva`가 `full.xva`보다 작다. 복구 결과 코드 `0`과 `<NEW3>`. [dom0] `xe vbd-list vm-uuid=<NEW3> type=Disk params=userdevice`에 `0`만 있다. R2와 같은 방법으로 `vbrtest-part` 스냅샷과 그 VDI가 남지 않았는지 본다(부분 백업은 스냅샷의 디스크 일부를 지운 뒤 내보낸다).

NFS SR에서는 가져온 직후 `physical-utilisation`이 수십 KB로 보일 수 있다(2026-11-19 실행에서 45,056). 데이터가 들어갔는지는 `xe sr-scan uuid=$sr` 뒤 다시 보거나 `ls -ls /run/sr-mount/<sr>/<vdi>.vhd`로 본다. 실행 결과는 5,984,395,776바이트로 MAC 보존 복구 VM의 디스크 0과 같았다.

### R6. 대기·처리 중 조회와 취소 (IT-04)

작업자 2개에 백업 3개를 넣어 세 번째를 대기시킨다. 디스크 사용량 × 3의 공간이 필요하다.
```powershell
11..13 | % { Send-Vbr "B202611182000000${_}A0005BACKUP $vm vbrtest-c$_ $R\BACKUP\c$_.xva" }; Start-Sleep 5
Send-Vbr "B20261118200000020Q000120261118200000011"
Send-Vbr "B20261118200000021Q000120261118200000013"
Send-Vbr "B20261118200000022C000120261118200000013"
Send-Vbr "B20261118200000023C000120261118200000011"
Send-Vbr "B20261118200000024Q000120261118200000013"
Wait-Vbr 20261118200000011 -TimeoutSec 7200
Wait-Vbr 20261118200000012 -TimeoutSec 7200
Get-ChildItem "$R\BACKUP" -Name
```
예상(차례대로)
1. 접수 `OK` 세 줄
2. 11번 조회: 코드 `3`(처리 중)
3. 13번 조회: 코드 `2`(대기 중)
4. 13번 취소: 코드 `0`, `Request message is cancel.[20261118200000022]`
5. 11번 취소: 코드 `9`, `Request message is processing.`
6. 13번 조회: 코드 `C`
7. 11번과 12번 결과: 코드 `0` `VM BACKUP Success`
8. `c11.xva`, `c12.xva`는 있고 `c13.xva`는 없다. [dom0] 스냅샷 목록이 R0과 같다

### R7. 백업 중 agent 강제 종료와 재기동 (IT-05)

Windows에는 Linux의 `kill -TERM`과 같은 종료 신호가 없어 강제 종료로 한다. 장애로 멈춘 경우와 같다.
```powershell
Send-Vbr "B20261118200000030A0005BACKUP $vm vbrtest-kill $R\BACKUP\kill.xva"; Start-Sleep 10
Send-Vbr "B20261118200000031Q000120261118200000030"     # 코드 3 확인
Stop-Process -Name vbr_agent -Force
```
agent 창에서 다시 실행: `& "c:\dev\ncc-vbr-go\go\bin\windows-amd64\vbr_agent.exe" -foreground`. 처음 창에서:
```powershell
Select-String "$R\server.log" -Pattern 'Task cancel|Restart recovery' | Select-Object -Last 2 | % Line
Send-Vbr "B20261118200000032Q000120261118200000030"
```
예상: `[20261118200000030] Task cancel.   Please check result.`, `Restart recovery : canceled [1], requeued [0]`. 조회는 코드 `6`(재기동으로 중단)이다.

[dom0] 남은 것 확인(정리는 전환본의 범위 밖. Q-A17):
```
xe snapshot-list snapshot-of=42097417-b9ee-2f97-8eeb-3612efbcb25d params=uuid,name-label
xe task-list params=uuid,name-label,status,progress
```
예상: `vbrtest-kill` 스냅샷이 남아 있을 수 있다. 내보내기 task는 XenServer가 접속 끊김으로 정리하는 것이 보통이다. 보이는 그대로 기록한다. PC에는 덜 받은 `kill.xva.partial`이 남을 수 있다(`kill.xva`는 생기면 안 된다).

2026-11-19 실행: `vbrtest-kill` 스냅샷이 남았고(R9에서 지움), 내보내기 task는 `failure`, progress 1.000으로 남았다. task 기록은 강제 종료된 agent가 지우지 못한 것으로 자원을 차지하지 않는다. `kill.xva.partial` 168,558,592바이트.

### R8. 실제 XAPI 오류 응답

```powershell
Send-Vbr "B20261118200000040A0005BACKUP 00000000-0000-0000-0000-000000000000 vbrtest-x $R\BACKUP\x.xva"
Wait-Vbr 20261118200000040 -TimeoutSec 120
Send-Vbr "B20261118200000041A0005RESTORE $R\BACKUP\full.xva false 00000000-0000-0000-0000-000000000000"
Wait-Vbr 20261118200000041 -TimeoutSec 600
```
예상: 둘 다 본문은 `[ERROR]` 뒤에 XenServer의 오류 내용(예: `UUID_INVALID…`). 결과 전문(transaction 로그의 5·0 단계)의 코드는 `9`이고, `Wait-Vbr`의 조회 응답 코드는 `0`이다. 실패해도 조회 코드는 0이고 본문으로 실패를 알린다(원본과 같음, agent/04 K-AG-01). 정확한 문장은 실제 응답을 기록한다. `x.xva`는 생기지 않는다.

### R9. 정리

[dom0] 복구로 생긴 VM과 R7에서 남은 스냅샷을 지운다. 지우기 전에 이름과 uuid를 다시 확인한다.
```
xe vm-list uuid=<NEWn> params=uuid,name-label,power-state      # NEW1~NEW3 각각 확인
xe vm-uninstall uuid=<NEWn> force=true                          # 시험 VM 42097417-… 이 아닌지 확인 후
xe snapshot-uninstall uuid=<vbrtest-kill 스냅샷 uuid> force=true  # 기존 926b6f3f-… 는 지우지 않는다
```
PC:
```powershell
Stop-Process -Name vbr_agent
Remove-Item "$R\BACKUP\*.xva"        # 백업 파일을 더 쓸 일이 없으면
```
`server.log`, `backup.log`, `transaction-YYYYMM.log`는 결과 판정에 쓰므로 남겨 둔다.

## 5. 기록

각 번호마다 실행일, 결과(같음/다름), 다르면 실제 출력과 `server.log` 끝 20줄을 남긴다. 결과는 각 서비스의 `06-test-report.md` 실행 기록에 옮긴다.

4장 R0~R9 실행 결과(2026-11-18~19): [agent/06-test-report.md](../agent/06-test-report.md) 5.3절, 로그 사본 `docs/evidence/agent/windows-real-20261119/`.

## 6. vbr_vm (2단계, 2026-11-19 추가)

`vbr_vm`은 JSON API 서비스라 명령을 손으로 치지 않고 데모 스크립트로 시험한다([vm/06](../vm/06-test-plan-report.md) 9장). pwsh 7 이 필요하다.

### 6.1 자동 모드 (실제 풀에 접속하지 않음)

```powershell
cd c:\dev\ncc-vbr-go\go
.\build.ps1 windows
.\test\vm\demo.ps1 -Auto
```
예상: `PASS 16, FAIL 0`. 가짜 XAPI 두 개와 `vbr_vm.exe`를 임시 폴더에 띄웠다가 끝나면 모두 끈다. 마지막 줄에 로그 폴더가 나온다. 방화벽 창은 뜨지 않는다(127.0.0.1 만 쓴다).

### 6.2 실기 모드 (실제 XenServer 풀)

설정 `sysconfig.yaml`과 키링 `key.yaml`은 ncc-xen 처럼 저장소 최상위(`c:\dev\ncc-vbr-go\`)에 둔다. 두 파일은 `.gitignore`로 커밋하지 않고, 양식은 `sysconfig.example.yaml`이다. 백업 파일·작업 기록·로그는 크기가 커서 `C:\temp\vbrvm` 아래에 둔다(2026-11-19 사용자 지시로 설정·키링만 `C:\temp\vbrvm`에서 옮김).

1. 처음 한 번: 키링과 토큰을 만든다. 이미 `key.yaml`과 `sysconfig.yaml`이 있으면 건너뛰고, 토큰 평문 `$tok`만 알고 있으면 된다(잊었으면 이 절차로 토큰을 새로 만든다).
```powershell
cd c:\dev\ncc-vbr-go
$B = ".\go\bin\windows-amd64"
New-Item -ItemType Directory -Force C:\temp\vbrvm\backup | Out-Null
& "$B\vbr_secret.exe" keygen key.yaml
Copy-Item sysconfig.example.yaml sysconfig.yaml
$tok = [guid]::NewGuid().ToString('N')
$tok | & "$B\vbr_secret.exe" encrypt key.yaml sysconfig.api_auth.VBR_VM_API_TOKEN   # 나온 ENC[...] 를 sysconfig.yaml 의 VBR_VM_API_TOKEN 에
```
2. `sysconfig.yaml`에서 확인할 것: `crypto.keyring_file: C:/dev/ncc-vbr-go/key.yaml`, `backup.base_path`·`task_store.dir`·`logging.file_name`은 `C:/temp/vbrvm/…`. 항목 설명은 [vm/05](../vm/05-interface-spec.md) 10장.
3. 창 2(서비스): `cd c:\dev\ncc-vbr-go; & .\go\bin\windows-amd64\vbr_vm.exe -config C:\dev\ncc-vbr-go\sysconfig.yaml`
4. 창 1(데모): `$env:VBR_VM_API_TOKEN = $tok` 뒤 `c:\dev\ncc-vbr-go\go\test\vm\demo.ps1`. 처음 메뉴를 고르면 서비스 주소, 풀 마스터, XAPI 암호(화면에 안 보임), `key.yaml` 경로를 묻는다. 키링 기본값은 `$env:VBR_VM_KEYRING` → 현재 폴더의 `key.yaml` → 저장소 최상위 `key.yaml` 순이다. 암호는 스크립트가 ENC 값으로 바꿔 보낸다. 키링이 없거나 암호화에 실패하면 메뉴를 진행하지 않고 처음부터 다시 묻는다.
5. 메뉴 2(전체 백업) → 3(디스크 선택) → 4(복구) → 5(검증) → 6(동시) → 7(취소) → 8(오류) → 9(정리 안내) 순으로 한다. 각 메뉴가 PASS/FAIL 과 dom0 확인 명령을 출력한다. 1단계 R0~R9 결과(agent/06 5.3)와 비교한다.

주의(1단계와 같음): 시험 VM `42097417-…`과 기존 스냅샷 `926b6f3f-…`은 지우지 않는다. 복구한 VM 은 켜지 않는다. 9번은 지울 명령을 보여 주기만 하고 실행하지 않는다.

### 6.3 메뉴가 없는 시험 (I09, I10)

데모 메뉴에는 없어서 손으로 한다. 작업 조회는 처음 창에서 이렇게 한다.
```powershell
$H = @{ 'X-Internal-Token' = $env:VBR_VM_API_TOKEN }
Invoke-RestMethod -Headers $H http://127.0.0.1:8090/api/v1/jobs/<taskId> | ConvertTo-Json -Depth 6
# <taskId> 는 데모 화면 Request 의 "taskId" 값(예 export-1a2b3c4d5e6f)으로 바꾼다. 모르면 목록으로 찾는다:
(Invoke-RestMethod -Headers $H 'http://127.0.0.1:8090/api/v1/jobs?status=interrupted').items |
  Select-Object taskId, status, step, percent, @{n='code';e={$_.error.code}}, @{n='detail';e={$_.error.detail}} | Format-List
```
이 조회가 필요한 이유: I09 는 서비스를 끊으므로 데모 창이 결과를 받지 못한다. 재기동한 뒤 그 작업이 `interrupted`로 남았는지, 다시 실행되지 않는지를 직접 본다.

**I09 처리 중 강제 종료 → 재기동** (1단계 R7과 비교)
1. 메뉴 2로 백업을 보낸다. 임시 스냅샷이 생기게 `snapshot_mode`는 `always`로 한다. 화면에 나온 taskId 를 적어 둔다.
2. 진행률이 오르기 시작하면 셋째 창에서 `Stop-Process -Name vbr_vm -Force`(정리 없이 끊김). 데모 창은 응답이 끊겨 오류를 낸다. 정상이다.
3. dom0: `xe snapshot-list snapshot-of=<vm> params=uuid,name-label`, `xe task-list params=uuid,name-label,status` — 임시 스냅샷이 남아 있을 것으로 본다(강제 종료라 정리 못 함).
4. 서비스를 다시 띄우고 위 조회로 그 taskId 를 본다. 기대: `status: interrupted`, `error.code: INTERRUPTED`, 서비스 로그에 재기동 복구 경고. `C:\temp\vbrvm\backup`에 `.partial`이 남았는지 적는다.
5. (선택) 같은 절차를 2번에서 서비스 창 `Ctrl+C`(정상 종료)로 한다. 기대: `interrupted`, 임시 스냅샷·`.partial`이 정리된다(최대 30초).
6. 남은 임시 스냅샷은 uuid·이름(`NCC-BACKUP-…`)을 확인한 뒤 사용자가 `xe snapshot-uninstall`로 지운다. `926b6f3f-…`은 제외.

**I10 Halted VM 을 스냅샷 없이 내보내는 동안 VM 시작** (Q-VM-01 판단 자료)
1. 대상 VM 이 Halted 인지 dom0 `xe vm-param-get uuid=<vm> param-name=power-state`로 확인한다.
2. 메뉴 2, `snapshot_mode`는 `auto`(기본), 압축 `none`. 진행 중에 dom0 `xe vm-start uuid=<vm>`.
3. 기록할 것: 시작이 거부되는지(오류 원문, 예 `OTHER_OPERATION_IN_PROGRESS`), 허용되면 백업 결과(`succeeded`/`failed`)와 파일. 결과는 [vm/07](../vm/07-operations.md) 8장 남은 일과 4장 운영 안내에 반영한다.
4. 시작이 허용돼 VM 이 켜졌으면 시험 뒤 원래 상태(Halted)로 되돌린다(`xe vm-shutdown uuid=<vm>`). 시험 VM `42097417-…`을 이 시험에서 켰다가 끄는 것은 2026-11-19 사용자가 허용했다. 복구한 VM 은 이 시험에서도 켜지 않는다.