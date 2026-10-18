package memos

import "testing"

func TestMemoUID(t *testing.T) {
	for name, want := range map[string]string{
		"memos/abc":       "abc",
		"memos/a-b_C1":    "a-b_C1",
		"memos/":          "",
		"memos/abc/def":   "",
		"users/abc":       "",
		"abc":             "",
		"":                "",
		"prefix/memos/ab": "",
	} {
		got, err := MemoUID(name)
		if want == "" {
			if err == nil {
				t.Errorf("MemoUID(%q) = %q, want error", name, got)
			}
		} else if err != nil || got != want {
			t.Errorf("MemoUID(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"dns:localhost:5230", "http://localhost:5230"},
		{"localhost:5230", "http://localhost:5230"},
		{" https://memos.example.com/notes/// ", "https://memos.example.com/notes"},
		{"", ""},
		{"ftp://memos.example.com", ""},
		{"https://", ""},
		{"https://user:pass@memos.example.com", ""},
		{"https://memos.example.com?token=secret", ""},
		{"https://memos.example.com#fragment", ""},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := NormalizeBaseURL(tc.input)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("expected invalid address, got %q", got)
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}
