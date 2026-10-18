package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	name := "memogram"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	flags := "-X github.com/usememos/memogram/internal/version.Version=26.10.0 -X github.com/usememos/memogram/internal/version.Commit=abcdef0123456789"
	if output, err := exec.Command("go", "build", "-buildvcs=false", "-ldflags", flags, "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	// Version inspection must work without configuring or contacting Telegram/Memos.
	t.Setenv("SERVER_ADDR", "")
	t.Setenv("BOT_TOKEN", "")
	output, err := exec.Command(binary, "--version").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "memogram 26.10.0 (commit abcdef0123456789)" {
		t.Fatalf("--version: %v\n%s", err, output)
	}
}
