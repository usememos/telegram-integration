package memogram

import "testing"

func TestNormalizeServerAddr(t *testing.T) {
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
			got, err := normalizeServerAddr(tc.input)
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
