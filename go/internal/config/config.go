// Package config 는 node_info.cfg(INI 유사 형식)를 읽는다.
//
// 원본 bs_common.c 의 BS_GetProfileString 과 같은 규칙을 따른다.
//   - "[섹션명]" 줄부터 다음 "[" 줄 전까지가 한 섹션이다.
//   - 빈 줄과 '#' 으로 시작하는 줄은 무시한다.
//   - 첫 번째 '=' 를 기준으로 키와 값을 나누고 양쪽 공백(공백, 탭, CR, LF)을 제거한다.
//   - 섹션명과 키는 대소문자를 구분한다. 같은 섹션·키가 여러 번 나오면 처음 것을 쓴다.
//
// 원본과 다른 점: 한 줄 255자 제한이 없다(원본은 초과분이 잘려 값이 유실된다).
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// DefaultFile 은 원본 CONFIG_FILE 과 같은 실행 디렉터리 기준 경로다.
const DefaultFile = "./node_info.cfg"

const blanks = " \t\r\n"

// File 은 읽어 들인 설정 파일 전체다.
type File struct {
	path     string
	sections map[string]map[string]string
}

// Load 는 설정 파일을 읽는다.
func Load(path string) (*File, error) {
	fp, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("config file open error [%s]: %w", path, err)
	}
	defer fp.Close()

	file := &File{path: path, sections: map[string]map[string]string{}}
	scanner := bufio.NewScanner(fp)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)

	var current map[string]string
	for scanner.Scan() {
		line := strings.Trim(scanner.Text(), blanks)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			current = file.openSection(line)
			continue
		}
		if current == nil {
			continue
		}
		key, value := splitKeyValue(line)
		if _, exists := current[key]; !exists {
			current[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("config file read error [%s]: %w", path, err)
	}
	return file, nil
}

// openSection 은 "[이름]" 줄에서 섹션을 연다. 같은 이름이 다시 나오면 무시할 빈 맵을 돌려준다.
func (f *File) openSection(line string) map[string]string {
	end := strings.Index(line, "]")
	if end < 0 {
		return nil
	}
	name := line[1:end]
	if _, exists := f.sections[name]; exists {
		return map[string]string{}
	}
	section := map[string]string{}
	f.sections[name] = section
	return section
}

func splitKeyValue(line string) (string, string) {
	key, value, found := strings.Cut(line, "=")
	if !found {
		return strings.Trim(line, blanks), ""
	}
	return strings.Trim(key, blanks), strings.Trim(value, blanks)
}

// Path 는 읽은 파일 경로다.
func (f *File) Path() string {
	return f.path
}

// Section 은 한 섹션에 대한 조회 창구를 돌려준다. 섹션이 없어도 nil 이 아니다.
func (f *File) Section(name string) Section {
	return Section{name: name, values: f.sections[name]}
}

// Section 은 한 섹션의 키·값 조회를 제공한다.
type Section struct {
	name   string
	values map[string]string
}

// Name 은 섹션명이다.
func (s Section) Name() string {
	return s.name
}

// String 은 키의 값을 돌려준다. 키가 없으면 found 가 false 다.
func (s Section) String(key string) (value string, found bool) {
	value, found = s.values[key]
	return value, found
}

// Int64 는 정수 값을 돌려준다. 키가 없거나 숫자가 아니면 found 가 false 다.
func (s Section) Int64(key string) (value int64, found bool) {
	text, found := s.values[key]
	if !found {
		return 0, false
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// Fields 는 공백으로 구분한 목록 값을 돌려준다(예: "EXCLUSION DIR").
func (s Section) Fields(key string) []string {
	return strings.Fields(s.values[key])
}
