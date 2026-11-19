package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, content string) *File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "node_info.cfg")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func TestSectionAndKeyRules(t *testing.T) {
	file := load(t, strings.Join([]string{
		"# comment",
		"[VBR Manager]",
		"PORT-NO \t\t= 10002",
		"MAX Thread cnt\t\t= 2",
		"#WEB server(IP)\t\t= 192.168.1.113",
		"Empty =",
		"PORT-NO = 99999",
		"",
		"[Other]",
		"PORT-NO = 20001\r",
	}, "\n"))

	section := file.Section("VBR Manager")
	if value, _ := section.String("PORT-NO"); value != "10002" {
		t.Errorf("first value must win, got %q", value)
	}
	if count, found := section.Int64("MAX Thread cnt"); !found || count != 2 {
		t.Errorf("MAX Thread cnt = %d, %v", count, found)
	}
	if _, found := section.String("WEB server(IP)"); found {
		t.Error("commented key must not be found")
	}
	if value, found := section.String("Empty"); !found || value != "" {
		t.Errorf("empty value = %q, %v", value, found)
	}
	if _, found := section.String("port-no"); found {
		t.Error("key is case sensitive")
	}
	if value, _ := file.Section("Other").String("PORT-NO"); value != "20001" {
		t.Errorf("CR must be trimmed, got %q", value)
	}
	if _, found := file.Section("Missing").String("PORT-NO"); found {
		t.Error("missing section must not be found")
	}
}

func TestLongLineIsNotTruncated(t *testing.T) {
	dirs := make([]string, 200)
	for i := range dirs {
		dirs[i] = "/nas/very/long/directory/name/" + strings.Repeat("x", 20)
	}
	file := load(t, "[VBR Manager]\nEXCLUSION DIR\t=\t"+strings.Join(dirs, " ")+"\n")
	if got := len(file.Section("VBR Manager").Fields("EXCLUSION DIR")); got != 200 {
		t.Errorf("fields = %d, want 200", got)
	}
}

// 개발 환경의 실제 설정 파일을 읽는다.
func TestRealDevelopmentFile(t *testing.T) {
	file, err := Load(filepath.Join("..", "..", "..", "node_info.cfg"))
	if err != nil {
		t.Skip("node_info.cfg not found:", err)
	}
	section := file.Section("VBR Manager")
	if got := len(section.Fields("EXCLUSION DIR")); got != 28 {
		t.Errorf("EXCLUSION DIR count = %d, want 28", got)
	}
	if got := section.Fields("2ND SEARCH DIR"); len(got) != 2 || got[0] != "/tmp/1/detail" {
		t.Errorf("2ND SEARCH DIR = %v", got)
	}
	if size, _ := section.Int64("MAX Log size"); size != 100000000 {
		t.Errorf("MAX Log size = %d", size)
	}
}
