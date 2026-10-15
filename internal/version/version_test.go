package version

import (
	"runtime/debug"
	"testing"
)

func TestValidate(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })
	for _, tc := range []struct {
		version string
		valid   bool
	}{
		{"26.10.0", true}, {"26.10.1", true}, {"26.01.0", true}, {"27.12.12-rc.3", true},
		{"26.10.15-dev.abcdef0", true}, {"26.01.1-dev.abcdef0", true},
		{"", false}, {"v26.10.0", false}, {"v0.5.0", false}, {"26.10", false},
		{"26.00.0", false}, {"26.13.0", false}, {"26.1.0", false}, {"26.10.01", false},
		{"26.10.0-rc.0", false}, {"26.10.0-rc.01", false}, {"26.10.0-beta.1", false},
		{"26.10.15-dev.badsha", false}, {"26.10.15-dev.abcdef0-dirty", false},
	} {
		t.Run(tc.version, func(t *testing.T) {
			Version = tc.version
			if err := Validate(); (err == nil) != tc.valid {
				t.Fatalf("Validate(%q) = %v, want valid=%v", tc.version, err, tc.valid)
			}
		})
	}
}

func TestResolveBuild(t *testing.T) {
	settings := []debug.BuildSetting{
		{Key: "vcs.time", Value: "2026-10-01T00:30:00+08:00"},
		{Key: "vcs.revision", Value: "abcdef0123456789"},
		{Key: "vcs.modified", Value: "true"},
	}
	v, commit := resolveBuild("", "unknown", settings)
	if v != "26.09.30-dev.abcdef0" || commit != "abcdef0123456789" {
		t.Fatalf("unexpected UTC development metadata: %s, %s", v, commit)
	}
	v, commit = resolveBuild("26.10.0-rc.1", "release-commit", settings)
	if v != "26.10.0-rc.1" || commit != "release-commit" {
		t.Fatalf("release metadata was overwritten: %s, %s", v, commit)
	}
	for _, settings := range [][]debug.BuildSetting{nil, {{Key: "vcs.time", Value: "bad-date"}, {Key: "vcs.revision", Value: "abcdef0123456789"}}, {{Key: "vcs.time", Value: "2026-01-01T00:00:00Z"}}} {
		if v, _ := resolveBuild("", "unknown", settings); v != "" {
			t.Fatalf("invented version without valid VCS metadata: %q", v)
		}
	}
}
