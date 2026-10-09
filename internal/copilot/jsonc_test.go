package copilot

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestStripJSONC(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain JSON unchanged", `{"a":1,"b":[true,null]}`, `{"a":1,"b":[true,null]}`},
		{"line comment", "{\n// note\n\"a\":1\n}", `{"a":1}`},
		{"line comment at EOF", "{\"a\":1}// trailing", `{"a":1}`},
		{"block comment", `{/* x */"a":/* y */1}`, `{"a":1}`},
		{"multi-line block comment", "{\"a\":1 /* one\ntwo */}", `{"a":1}`},
		{"trailing comma in object", `{"a":1,}`, `{"a":1}`},
		{"trailing comma in array", `[1,2,]`, `[1,2]`},
		{"trailing comma before comment", "{\"a\":1, // c\n}", `{"a":1}`},
		{"comment markers inside string", `{"a":"// not /* a */ comment"}`, `{"a":"// not /* a */ comment"}`},
		{"comma and bracket inside string", `{"a":",}"}`, `{"a":",}"}`},
		{"escaped quote inside string", `{"a":"x\"//y"}`, `{"a":"x\"//y"}`},
		{"escaped backslash ends string", `{"a":"x\\"// c` + "\n}", `{"a":"x\\"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripJSONC([]byte(tt.in))
			if !jsonEqual(t, got, []byte(tt.want)) {
				t.Fatalf("stripJSONC(%q) = %q, want JSON equal to %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestStripJSONCUnterminatedBlockCommentIsInvalid(t *testing.T) {
	if json.Valid(stripJSONC([]byte(`{"a":1 /* never closed`))) {
		t.Fatal("unterminated block comment produced valid JSON")
	}
}

// Valid JSON has no comments or trailing commas, so it must pass through unchanged.
func FuzzStripJSONC(f *testing.F) {
	for _, seed := range []string{
		`{"a":1}`, `[1,2,3]`, `{"a":"//"}`, `{"a":"/*","b":"*/"}`, `"\\"`, `{"a":[{},[]]}`,
		"{\n// c\n\"a\":1,}", `{"a":"x\"y"}`, `1E700`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		out := stripJSONC(data)
		if !json.Valid(data) {
			return
		}
		if !bytes.Equal(out, data) {
			t.Fatalf("stripJSONC changed valid JSON %q to %q", data, out)
		}
	})
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		t.Fatalf("invalid expected JSON %q: %v", b, err)
	}
	return reflect.DeepEqual(va, vb)
}
