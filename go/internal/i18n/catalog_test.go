package i18n

import (
	"strings"
	"testing"
)

// T-VM-U74: ko, en, ja 가 같은 키를 갖고, 형식 지시자 수가 같다. 모르는 locale 은 바꾸지 않는다.
func TestCatalogParity(t *testing.T) {
	ko := catalogs[KO]
	for _, loc := range []string{EN, JA} {
		m := catalogs[loc]
		if len(m) != len(ko) {
			t.Errorf("%s has %d keys, ko has %d", loc, len(m), len(ko))
		}
		for k, v := range ko {
			o, ok := m[k]
			if !ok {
				t.Errorf("%s missing %s", loc, k)
				continue
			}
			if strings.Count(o, "%") != strings.Count(v, "%") {
				t.Errorf("%s %s: format verbs differ", loc, k)
			}
		}
	}
	if SetLocale("fr") || CurrentLocale() != KO {
		t.Fatal("unknown locale must be ignored")
	}
	SetLocale(EN)
	defer SetLocale(KO)
	if T("vm.export.running", 42) != "Exporting VM (42%)" || T("no.such.key") != "no.such.key" {
		t.Fatal(T("vm.export.running", 42))
	}
}
