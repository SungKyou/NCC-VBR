# vbr-send.ps1 : agent 에 전문 하나를 보내고 응답을 출력한다.
#   . .\vbr-send.ps1            (함수 불러오기)
#   Send-Vbr "B20261118100000001A0005BACKUP ..."
function Send-Vbr {
    param([string]$Body, [string]$HostName = "127.0.0.1", [int]$Port = 7102, [int]$TimeoutSec = 15)
    $client = New-Object System.Net.Sockets.TcpClient
    try {
        $client.Connect($HostName, $Port)
        $stream = $client.GetStream()
        $stream.ReadTimeout = $TimeoutSec * 1000
        $bytes = [Text.Encoding]::ASCII.GetBytes($Body)
        $head = [Text.Encoding]::ASCII.GetBytes(("{0:D7}" -f $bytes.Length))
        $stream.Write($head, 0, $head.Length)
        $stream.Write($bytes, 0, $bytes.Length)
        $len = [int][Text.Encoding]::ASCII.GetString((Read-Exact $stream 7))
        [Text.Encoding]::ASCII.GetString((Read-Exact $stream $len))
    } catch {
        "ERROR " + $_.Exception.Message
    } finally {
        $client.Close()
    }
}

function Read-Exact($stream, [int]$count) {
    $buf = New-Object byte[] $count
    $off = 0
    while ($off -lt $count) {
        $n = $stream.Read($buf, $off, $count - $off)
        if ($n -le 0) { throw "connection closed after $off of $count bytes" }
        $off += $n
    }
    return ,$buf
}

# Wait-Vbr : 결과 조회(Q)를 코드가 1·2·3(대기·처리 중)이 아닐 때까지 반복한다.
function Wait-Vbr {
    param([string]$Seq, [int]$TimeoutSec = 60, [int]$Port = 7102)
    $deadline = (Get-Date).AddSeconds($TimeoutSec)
    $n = 0
    while ($true) {
        $n++
        $q = "B" + (Get-Date -Format "yyyyMMddHHmmss") + ("{0:D3}" -f ($n % 1000)) + "Q0001" + $Seq
        $r = Send-Vbr $q -Port $Port
        if ($r.Length -gt 18 -and "123".IndexOf($r[18]) -lt 0) { return $r }
        if ((Get-Date) -gt $deadline) { return "TIMEOUT " + $r }
        Start-Sleep -Milliseconds 300
    }
}
