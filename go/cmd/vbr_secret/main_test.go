package main

import (
	"strings"
	"testing"
)

func TestReadPassword(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "lf", input: "p@ss word\n", want: "p@ss word"},
		{name: "crlf", input: "p@ss word\r\n", want: "p@ss word"},
		{name: "no newline", input: "p@ss", want: "p@ss"},
		{name: "powershell utf8 bom", input: "\xef\xbb\xbfp@ss\r\n", want: "p@ss"},
		{name: "first line only", input: "first\nsecond\n", want: "first"},
	}
	for _, c := range cases {
		got, err := readPassword(strings.NewReader(c.input))
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v; want %q", c.name, got, err, c.want)
		}
	}
	for _, input := range []string{"", "\n", "\xef\xbb\xbf\r\n"} {
		if _, err := readPassword(strings.NewReader(input)); err == nil {
			t.Errorf("%q: empty password must be rejected", input)
		}
	}
}
