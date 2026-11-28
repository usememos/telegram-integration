package scripts

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// packageRelease runs the release build the way the Release workflow does and
// returns the names of the archives it wrote to dir.
func packageRelease(t *testing.T, dir string, env ...string) ([]string, error) {
	t.Helper()
	cmd := exec.Command("sh", "build.sh", "release", dir)
	cmd.Env = append(os.Environ(), env...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, errors.New(string(output))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names, nil
}

// requireCheckout skips a test that needs the repository's Git metadata, which
// a source archive does not have.
func requireCheckout(t *testing.T) string {
	t.Helper()
	requireTools(t)
	commit, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Skip("the release build needs a Git checkout")
	}
	return strings.TrimSpace(string(commit))
}

// The native archive holds one binary that reports the release tag and commit.
func TestReleaseArchiveReportsItsVersion(t *testing.T) {
	commit := requireCheckout(t)
	if runtime.GOOS == "windows" || runtime.GOARCH == "arm" {
		t.Skip("native Windows and 32-bit ARM archives are named differently; TestReleaseArchiveNames covers them")
	}
	for _, tag := range []string{"26.10.0", "26.10.0-rc.1"} {
		t.Run(tag, func(t *testing.T) {
			dir := t.TempDir()
			// Clear cross-compilation settings inherited from the caller.
			names, err := packageRelease(t, dir, "MEMOGRAM_VERSION="+tag, "GOOS=", "GOARCH=", "GOARM=")
			if err != nil {
				t.Fatal(err)
			}
			want := "memogram_" + tag + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
			if len(names) != 1 || names[0] != want {
				t.Fatalf("got %v, want %s", names, want)
			}

			file, err := os.Open(filepath.Join(dir, want))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = file.Close() }()
			compressed, err := gzip.NewReader(file)
			if err != nil {
				t.Fatal(err)
			}
			archive := tar.NewReader(compressed)
			header, err := archive.Next()
			if err != nil || header.Name != "memogram" {
				t.Fatalf("first entry %+v, %v", header, err)
			}
			binary := filepath.Join(dir, "memogram")
			out, err := os.OpenFile(binary, os.O_CREATE|os.O_WRONLY, 0700)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.Copy(out, archive); err != nil {
				t.Fatal(err)
			}
			if err := out.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := archive.Next(); err != io.EOF {
				t.Fatalf("archive holds more than the binary: %v", err)
			}

			output, err := exec.Command(binary, "--version").CombinedOutput()
			if want := "memogram " + tag + " (commit " + commit + ")"; err != nil || strings.TrimSpace(string(output)) != want {
				t.Fatalf("got %q, %v, want %q", output, err, want)
			}
		})
	}
}

// Archives are named after their target, and Windows gets a .zip.
func TestReleaseArchiveNames(t *testing.T) {
	requireCheckout(t)
	t.Run("linux arm", func(t *testing.T) {
		names, err := packageRelease(t, t.TempDir(), "MEMOGRAM_VERSION=26.10.0", "GOOS=linux", "GOARCH=arm", "GOARM=7")
		if err != nil || len(names) != 1 || names[0] != "memogram_26.10.0_linux_armv7.tar.gz" {
			t.Fatalf("got %v, %v", names, err)
		}
	})
	t.Run("windows", func(t *testing.T) {
		if _, err := exec.LookPath("zip"); err != nil {
			t.Skip("zip is required to package Windows archives")
		}
		dir := t.TempDir()
		names, err := packageRelease(t, dir, "MEMOGRAM_VERSION=26.10.0", "GOOS=windows", "GOARCH=arm64", "GOARM=")
		if err != nil || len(names) != 1 || names[0] != "memogram_26.10.0_windows_arm64.zip" {
			t.Fatalf("got %v, %v", names, err)
		}
		archive, err := zip.OpenReader(filepath.Join(dir, names[0]))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = archive.Close() }()
		if len(archive.File) != 1 || archive.File[0].Name != "memogram.exe" {
			t.Fatalf("unexpected archive contents: %d files", len(archive.File))
		}
	})
}

// Only release tags can be packaged under an explicit version.
func TestReleaseBuildRejectsInvalidVersion(t *testing.T) {
	requireCheckout(t)
	for _, version := range []string{"v0.5.0", "26.10", "26.10.1-dev.abcdef0"} {
		dir := t.TempDir()
		if names, err := packageRelease(t, dir, "MEMOGRAM_VERSION="+version); err == nil || len(names) != 0 {
			t.Errorf("packaged invalid version %q: %v", version, names)
		}
	}
}
