package scripts

import (
	"os"
	"regexp"
	"testing"
)

// The Go version is pinned in go.mod, the Docker build image and the README.
// CI's setup-go reads go.mod, so the others must follow it.
func TestGoVersionMatchesModule(t *testing.T) {
	find := func(path, pattern string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		match := regexp.MustCompile(pattern).FindSubmatch(data)
		if match == nil {
			t.Fatalf("%s: no match for %s", path, pattern)
		}
		return string(match[1])
	}
	want := find("../go.mod", `(?m)^go (\S+)$`)
	for path, pattern := range map[string]string{
		"../Dockerfile": `(?m)^FROM golang:(\S+)-alpine `,
		"../README.md":  `Requires Go (\S+) or newer\.`,
	} {
		if got := find(path, pattern); got != want {
			t.Errorf("%s uses Go %s, but go.mod requires %s", path, got, want)
		}
	}
}
