// 출처: ncc-xen backend/pkg/xapi/xmlrpc.go (커밋 043c6d5, 2026-11-17 복사)
// 바뀐 부분: docs/agent/08-copied-code-register.md CP-01

package xapi

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// ─────────────────────────────────────────────
// Value 타입
// ─────────────────────────────────────────────

const (
	kindNil    = "nil"
	kindString = "string"
	kindInt    = "int"
	kindBool   = "boolean"
	kindDouble = "double"
	kindArray  = "array"
	kindStruct = "struct"
)

// Value는 XAPI XML-RPC 값입니다.
type Value struct {
	kind   string
	str    string
	arr    []Value
	fields map[string]Value
}

func strVal(s string) Value { return Value{kind: kindString, str: s} }
func nilVal() Value         { return Value{kind: kindNil} }

func (v Value) Str() string { return v.str }
func (v Value) IsNil() bool { return v.kind == kindNil || v.kind == "" }

func (v Value) Field(key string) Value {
	if v.fields == nil {
		return nilVal()
	}
	r, ok := v.fields[key]
	if !ok {
		return nilVal()
	}
	return r
}

func (v Value) Arr() []Value { return v.arr }

func (v Value) ToStringSlice() []string {
	out := make([]string, 0, len(v.arr))
	for _, item := range v.arr {
		out = append(out, item.str)
	}
	return out
}

func (v Value) ToMap() map[string]interface{} {
	out := make(map[string]interface{})
	for k, child := range v.fields {
		switch child.kind {
		case kindStruct:
			out[k] = child.ToMap()
		case kindArray:
			strs := make([]string, 0, len(child.arr))
			for _, item := range child.arr {
				strs = append(strs, item.str)
			}
			out[k] = strs
		default:
			out[k] = child.str
		}
	}
	return out
}

// ─────────────────────────────────────────────
// 파서 - 토큰 스트림 기반
//
// 설계 원칙:
//   각 parse 함수는 자신의 시작 태그가 이미 소비된 상태에서 호출되고
//   자신의 끝 태그까지 소비한 후 반환합니다.
// ─────────────────────────────────────────────

type parser struct {
	d *xml.Decoder
}

func newParser(data []byte) *parser {
	// 2026-09-18: strings.NewReader(string(data))는 []byte→string 변환
	// 과정에서 data 전체를 한 번 더 복사한다(원본 []byte + 새 string, 두
	// 벌이 동시에 메모리에 존재). get_all_records/full_sync처럼 응답이
	// 큰 경우 이 이중 버퍼링만으로도 불필요하게 메모리를 두 배 점유하게
	// 되므로, 복사가 없는 bytes.NewReader(data)로 교체한다(동작은 완전히
	// 동일 — xml.Decoder는 어느 Reader든 그대로 사용).
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	return &parser{d: d}
}

// next는 의미있는 다음 토큰을 반환합니다 (빈 CharData 제거).
func (p *parser) next() (xml.Token, error) {
	for {
		t, err := p.d.Token()
		if err != nil {
			return nil, err
		}
		if cd, ok := t.(xml.CharData); ok {
			if strings.TrimSpace(string(cd)) == "" {
				continue // 공백만 있는 텍스트 노드 skip
			}
		}
		return t, nil
	}
}

// expectStart는 지정한 이름의 시작 태그를 찾을 때까지 소비하고 반환합니다.
func (p *parser) expectStart(name string) error {
	for {
		t, err := p.next()
		if err != nil {
			if err == io.EOF {
				return fmt.Errorf("<%s> missing (EOF)", name)
			}
			return err
		}
		if se, ok := t.(xml.StartElement); ok {
			if name == "" || se.Name.Local == name {
				return nil
			}
		}
	}
}

// nextStart는 다음 시작 태그를 반환합니다 (이름 무관).
func (p *parser) nextStart() (xml.StartElement, error) {
	for {
		t, err := p.next()
		if err != nil {
			return xml.StartElement{}, err
		}
		if se, ok := t.(xml.StartElement); ok {
			return se, nil
		}
	}
}

// ─────────────────────────────────────────────
// parseMethodResponse - 진입점
// ─────────────────────────────────────────────

func parseMethodResponse(data []byte) (Value, error) {
	p := newParser(data)

	if err := p.expectStart("methodResponse"); err != nil {
		return nilVal(), fmt.Errorf("methodResponse missing: %w", err)
	}

	se, err := p.nextStart()
	if err != nil {
		return nilVal(), err
	}

	switch se.Name.Local {
	case "params":
		return p.parseParams()
	case "fault":
		return nilVal(), p.parseFault()
	default:
		return nilVal(), fmt.Errorf("unexpected tag: <%s>", se.Name.Local)
	}
}

func (p *parser) parseParams() (Value, error) {
	if err := p.expectStart("param"); err != nil {
		return nilVal(), fmt.Errorf("param missing: %w", err)
	}
	if err := p.expectStart("value"); err != nil {
		return nilVal(), fmt.Errorf("value missing: %w", err)
	}
	return p.parseValue()
}

func (p *parser) parseFault() error {
	if err := p.expectStart("value"); err != nil {
		return err
	}
	v, err := p.parseValue()
	if err != nil {
		return err
	}
	fs := v.Field("faultString").Str()
	if fs == "" {
		fs = "unknown fault"
	}
	return fmt.Errorf("XMLRPC Fault: %s", fs)
}

// ─────────────────────────────────────────────
// parseValue
// 호출 전: <value> 시작 태그 소비 완료
// 반환 후: </value> 끝 태그 소비 완료
// ─────────────────────────────────────────────

func (p *parser) parseValue() (Value, error) {
	t, err := p.next()
	if err != nil {
		return nilVal(), err
	}

	switch tok := t.(type) {
	case xml.StartElement:
		// <string>, <int>, <struct>, <array> 등
		v, err := p.parseTyped(tok.Name.Local)
		if err != nil {
			return nilVal(), err
		}
		// parseTyped 는 자신의 </tag>를 소비한 상태
		// 이제 </value>를 소비
		if err := p.consumeEnd("value"); err != nil {
			return nilVal(), err
		}
		return v, nil

	case xml.CharData:
		// <value>text</value> - 타입 태그 없는 string
		s := strings.TrimSpace(string(tok))
		if err := p.consumeEnd("value"); err != nil {
			return nilVal(), err
		}
		return strVal(s), nil

	case xml.EndElement:
		// <value/> 빈 값
		return strVal(""), nil
	}

	return nilVal(), fmt.Errorf("parseValue: unexpected token %T", t)
}

// parseTyped는 타입 태그 안의 내용을 파싱합니다.
// 호출 전: <typeTag> 소비 완료
// 반환 후: </typeTag> 소비 완료
func (p *parser) parseTyped(typeName string) (Value, error) {
	switch typeName {
	case "string":
		s, err := p.readText()
		if err != nil {
			return nilVal(), err
		}
		return strVal(s), nil

	case "int", "i4", "i8":
		s, err := p.readText()
		if err != nil {
			return nilVal(), err
		}
		return Value{kind: kindInt, str: s}, nil

	case "boolean":
		s, err := p.readText()
		if err != nil {
			return nilVal(), err
		}
		return Value{kind: kindBool, str: s}, nil

	case "double":
		s, err := p.readText()
		if err != nil {
			return nilVal(), err
		}
		return Value{kind: kindDouble, str: s}, nil

	case "nil", "ex:nil":
		// <nil/> 또는 <nil></nil>
		p.consumeEnd(typeName)
		return nilVal(), nil

	case "array":
		return p.parseArray()

	case "struct":
		return p.parseStruct()

	default:
		// 알 수 없는 타입 → text로 처리
		s, err := p.readText()
		if err != nil {
			return nilVal(), err
		}
		return strVal(s), nil
	}
}

// readText는 현재 요소의 텍스트 내용을 읽고 끝 태그를 소비합니다.
// 호출 전: 시작 태그 소비 완료
// 반환 후: 끝 태그 소비 완료
func (p *parser) readText() (string, error) {
	var buf strings.Builder
	depth := 0

	for {
		t, err := p.d.Token() // 공백 포함 raw token
		if err != nil {
			return buf.String(), err
		}
		switch tok := t.(type) {
		case xml.CharData:
			buf.Write(tok)
		case xml.StartElement:
			depth++
			// 중첩 태그 내용도 수집
		case xml.EndElement:
			if depth == 0 {
				return strings.TrimSpace(buf.String()), nil
			}
			depth--
		}
	}
}

// consumeEnd는 지정한 이름의 끝 태그가 나올 때까지 소비합니다.
func (p *parser) consumeEnd(name string) error {
	for {
		t, err := p.next()
		if err != nil {
			if err == io.EOF {
				return nil // 관대하게 처리
			}
			return err
		}
		if ee, ok := t.(xml.EndElement); ok && ee.Name.Local == name {
			return nil
		}
		// 예상치 못한 StartElement는 skip
		if se, ok := t.(xml.StartElement); ok {
			_ = se
			p.d.Skip()
		}
	}
}

// ─────────────────────────────────────────────
// parseArray
// 호출 전: <array> 소비 완료
// 반환 후: </array> 소비 완료
// ─────────────────────────────────────────────

func (p *parser) parseArray() (Value, error) {
	// <data>
	if err := p.expectStart("data"); err != nil {
		return nilVal(), fmt.Errorf("array/data missing: %w", err)
	}

	var items []Value
	for {
		t, err := p.next()
		if err != nil {
			return nilVal(), err
		}
		switch tok := t.(type) {
		case xml.StartElement:
			if tok.Name.Local == "value" {
				item, err := p.parseValue()
				if err != nil {
					return nilVal(), err
				}
				items = append(items, item)
			}
		case xml.EndElement:
			if tok.Name.Local == "data" {
				// </array>
				if err := p.consumeEnd("array"); err != nil {
					return nilVal(), err
				}
				return Value{kind: kindArray, arr: items}, nil
			}
		}
	}
}

// ─────────────────────────────────────────────
// parseStruct
// 호출 전: <struct> 소비 완료
// 반환 후: </struct> 소비 완료
// ─────────────────────────────────────────────

func (p *parser) parseStruct() (Value, error) {
	fields := make(map[string]Value)

	for {
		t, err := p.next()
		if err != nil {
			return nilVal(), err
		}
		switch tok := t.(type) {
		case xml.StartElement:
			if tok.Name.Local == "member" {
				name, val, err := p.parseMember()
				if err != nil {
					return nilVal(), err
				}
				if name != "" {
					fields[name] = val
				}
			}
		case xml.EndElement:
			if tok.Name.Local == "struct" {
				return Value{kind: kindStruct, fields: fields}, nil
			}
		}
	}
}

// ─────────────────────────────────────────────
// parseMember
// 호출 전: <member> 소비 완료
// 반환 후: </member> 소비 완료
//
// XenServer는 <n> 또는 <name> 둘 다 사용합니다.
// ─────────────────────────────────────────────

func (p *parser) parseMember() (string, Value, error) {
	var name string
	var val Value
	hasVal := false

	for {
		t, err := p.next()
		if err != nil {
			return "", nilVal(), err
		}
		switch tok := t.(type) {
		case xml.StartElement:
			switch tok.Name.Local {
			case "name", "n":
				name, err = p.readText()
				if err != nil {
					return "", nilVal(), err
				}
			case "value":
				val, err = p.parseValue()
				if err != nil {
					return "", nilVal(), err
				}
				hasVal = true
			default:
				p.d.Skip()
			}
		case xml.EndElement:
			if tok.Name.Local == "member" {
				if !hasVal {
					val = nilVal()
				}
				return name, val, nil
			}
		}
	}
}

// ─────────────────────────────────────────────
// XML-RPC 요청 인코더
// ─────────────────────────────────────────────

func encodeCall(method string, args []interface{}) ([]byte, error) {
	var buf strings.Builder
	buf.WriteString(`<?xml version="1.0"?><methodCall><methodName>`)
	xml.EscapeText(&buf, []byte(method))
	buf.WriteString(`</methodName><params>`)
	for _, arg := range args {
		buf.WriteString(`<param><value>`)
		if err := writeValue(&buf, arg); err != nil {
			return nil, fmt.Errorf("parameter encoding failed (%T): %w", arg, err)
		}
		buf.WriteString(`</value></param>`)
	}
	buf.WriteString(`</params></methodCall>`)
	return []byte(buf.String()), nil
}

func writeValue(buf *strings.Builder, v interface{}) error {
	switch val := v.(type) {
	case string:
		buf.WriteString(`<string>`)
		xml.EscapeText(buf, []byte(val))
		buf.WriteString(`</string>`)
	case int:
		fmt.Fprintf(buf, `<int>%d</int>`, val)
	case int64:
		fmt.Fprintf(buf, `<int>%d</int>`, val)
	case float64:
		fmt.Fprintf(buf, `<double>%g</double>`, val)
	case bool:
		if val {
			buf.WriteString(`<boolean>1</boolean>`)
		} else {
			buf.WriteString(`<boolean>0</boolean>`)
		}
	case []string:
		buf.WriteString(`<array><data>`)
		for _, s := range val {
			buf.WriteString(`<value><string>`)
			xml.EscapeText(buf, []byte(s))
			buf.WriteString(`</string></value>`)
		}
		buf.WriteString(`</data></array>`)
	case map[string]string:
		buf.WriteString(`<struct>`)
		for k, s := range val {
			buf.WriteString(`<member><name>`)
			xml.EscapeText(buf, []byte(k))
			buf.WriteString(`</name><value><string>`)
			xml.EscapeText(buf, []byte(s))
			buf.WriteString(`</string></value></member>`)
		}
		buf.WriteString(`</struct>`)
	case map[string]interface{}:
		// VDI.create / VBD.create 등 record 전달 시 사용
		buf.WriteString(`<struct>`)
		for k, v := range val {
			buf.WriteString(`<member><name>`)
			xml.EscapeText(buf, []byte(k))
			buf.WriteString(`</name><value>`)
			if err := writeValue(buf, v); err != nil {
				return fmt.Errorf("struct field encoding failed (key=%s): %w", k, err)
			}
			buf.WriteString(`</value></member>`)
		}
		buf.WriteString(`</struct>`)
	default:
		return fmt.Errorf("unsupported type: %T", v)
	}
	return nil
}
