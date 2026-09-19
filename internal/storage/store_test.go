package storage

import "testing"

func TestSanitize(t *testing.T) {
	cases := map[string]bool{
		"reports/a.svg":     true,
		"nested/dir/f.txt":  true,
		"../secret":         false,
		"a/../../secret":    false,
		"/etc/passwd":       false,
		`C:\Windows\win.ini`: false,
		"":                  false,
	}
	for input, ok := range cases {
		_, err := sanitize(input)
		if ok && err != nil {
			t.Fatalf("expected %q to be allowed, got %v", input, err)
		}
		if !ok && err == nil {
			t.Fatalf("expected %q to be rejected", input)
		}
	}
}
