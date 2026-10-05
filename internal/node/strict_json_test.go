package node

import "testing"

func TestDecodeStrictJSONRejectsInvalidUTF8AndUnpairedSurrogates(t *testing.T) {
	invalidUTF8 := append([]byte(`{"path":"`), 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"}`)...)
	for name, encoded := range map[string][]byte{
		"invalid UTF-8":            invalidUTF8,
		"unpaired high surrogate":  []byte(`{"path":"/tmp/node-\uD800"}`),
		"unpaired low surrogate":   []byte(`{"path":"/tmp/node-\uDC00"}`),
		"high followed by non-low": []byte(`{"path":"/tmp/node-\uD800x"}`),
	} {
		t.Run(name, func(t *testing.T) {
			var decoded map[string]string
			if err := decodeStrictJSON(encoded, &decoded); err == nil {
				t.Fatal("malformed Unicode JSON was accepted")
			}
		})
	}
}

func TestDecodeStrictJSONAcceptsValidUnicodeAndPairedSurrogates(t *testing.T) {
	cases := map[string]struct {
		encoded  []byte
		expected string
	}{
		"raw valid Unicode":         {[]byte(`{"path":"/tmp/café-😀"}`), "/tmp/café-😀"},
		"paired surrogate escape":   {[]byte(`{"path":"/tmp/node-\uD83D\uDE00"}`), "/tmp/node-😀"},
		"explicit replacement rune": {[]byte(`{"path":"/tmp/node-\uFFFD"}`), "/tmp/node-�"},
		"escaped backslash literal": {[]byte(`{"path":"/tmp/\\uD800"}`), `/tmp/\uD800`},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			var decoded map[string]string
			if err := decodeStrictJSON(testCase.encoded, &decoded); err != nil {
				t.Fatalf("valid Unicode JSON was rejected: %v", err)
			}
			if decoded["path"] != testCase.expected {
				t.Fatalf("decoded path=%q, want %q", decoded["path"], testCase.expected)
			}
		})
	}
}
