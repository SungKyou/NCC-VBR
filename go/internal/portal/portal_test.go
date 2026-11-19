package portal

import (
	"net"
	"strconv"
	"testing"
	"time"
)

func sample() Event {
	return Event{
		Type:     'N',
		HostName: "VBR CIFS",
		HostUUID: "NONE",
		Time:     time.Date(2026, 11, 17, 9, 5, 3, 0, time.Local),
		Code:     "CIFSFIN00",
		Message:  "20261117090503001 BACKUP: /src => /tgt Finished.",
	}
}

func TestEncodeLayout(t *testing.T) {
	message, err := Encode(sample())
	if err != nil {
		t.Fatal(err)
	}
	text := string(message)

	length, err := strconv.Atoi(text[:7])
	if err != nil || length != len(text)-7 {
		t.Fatalf("length field = %q, body = %d", text[:7], len(text)-7)
	}
	if header := text[7:30]; header != "B20261117090503001"+"0"+"000"+"1" {
		t.Errorf("header = %q", header)
	}
	want := `{"event_info":[{"event_type":"N","host_name":"VBR CIFS","host_uuid":"NONE",` +
		`"event_time":"2026-11-17 09:05:03","event_code":"CIFSFIN00",` +
		`"event_msg":"20261117090503001 BACKUP: /src => /tgt Finished."}]}`
	if text[30:] != want {
		t.Errorf("json = %s\nwant = %s", text[30:], want)
	}
}

func TestSendAndReceive(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	received := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buffer := make([]byte, 4096)
		n, _ := conn.Read(buffer)
		received <- string(buffer[:n])
		_, _ = conn.Write([]byte("OK"))
	}()

	address := listener.Addr().(*net.TCPAddr)
	reply, err := Client{Host: "127.0.0.1", Port: address.Port}.Send(sample())
	if err != nil {
		t.Fatal(err)
	}
	if reply != "OK" {
		t.Errorf("reply = %q", reply)
	}
	expected, _ := Encode(sample())
	if got := <-received; got != string(expected) {
		t.Errorf("server received %q", got)
	}
}
