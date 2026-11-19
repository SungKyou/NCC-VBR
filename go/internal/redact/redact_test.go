package redact

import "testing"

func TestValueMasksNestedSecrets(t *testing.T) {
	in := map[string]interface{}{
		"file_name": "a",
		"password":  "p",
		"nested":    []interface{}{map[string]interface{}{"api_token": "t", "keep": 1}},
	}
	out := Value(in).(map[string]interface{})
	n := out["nested"].([]interface{})[0].(map[string]interface{})
	if out["password"] != Mask || n["api_token"] != Mask || n["keep"] != 1 || out["file_name"] != "a" {
		t.Fatalf("%v", out)
	}
	if in["password"] != "p" {
		t.Fatal("input must not change")
	}
}
