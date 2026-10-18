package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// requireTools skips a test when the release scripts cannot run, such as on
// Windows without bash.
func requireTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"bash", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is required to test the release scripts", tool)
		}
	}
}

func releaseCommand(args ...string) *exec.Cmd {
	return exec.Command("bash", append([]string{"release_version.sh"}, args...)...)
}

func TestReleaseTags(t *testing.T) {
	requireTools(t)
	for _, tag := range []string{"26.10.0", "26.10.1", "26.01.0", "27.12.0", "26.10.0-rc.1", "26.10.2-rc.12"} {
		t.Run(tag, func(t *testing.T) {
			output, err := releaseCommand("version", tag).CombinedOutput()
			if err != nil {
				t.Fatalf("version: %v\n%s", err, output)
			}
			parts := strings.Split(tag, ".")
			want := "tag=" + tag + "\nversion=" + tag + "\nseries=" + parts[0] + "." + parts[1] + "\nis_prerelease="
			want += map[bool]string{true: "true", false: "false"}[strings.Contains(tag, "-rc.")] + "\n"
			if string(output) != want {
				t.Fatalf("got %q, want %q", output, want)
			}
		})
	}
	for _, tag := range []string{"", "v0.5.0", "v26.10.0", "26.10", "26.1.0", "26.00.0", "26.13.0", "26.10.01", "26.10.0-rc.0", "26.10.0-rc.01", "26.10.1-dev.abcdef0", "26.10.0-beta.1", "26.10.0\n"} {
		if output, err := releaseCommand("version", tag).CombinedOutput(); err == nil {
			t.Errorf("accepted invalid release tag %q: %s", tag, output)
		}
	}
}

func git(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2026-01-01T00:30:00+08:00", "GIT_COMMITTER_DATE=2026-01-01T00:30:00+08:00")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func testRepository(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.email", "memogram-test@example.com")
	git(t, repo, "config", "user.name", "Memogram Test")
	git(t, repo, "config", "commit.gpgsign", "false")
	git(t, repo, "config", "tag.gpgsign", "false")
	git(t, repo, "commit", "--allow-empty", "-m", "initial")
	return repo
}

func TestDevelopmentVersionMatchesBinary(t *testing.T) {
	requireTools(t)
	repo := testRepository(t)
	output, err := releaseCommand("development-version", repo).CombinedOutput()
	if err != nil {
		t.Fatalf("development-version: %v\n%s", err, output)
	}
	version := strings.TrimSpace(string(output))
	if want := "25.12.31-dev." + git(t, repo, "rev-parse", "--short=7", "HEAD"); version != want {
		t.Fatalf("got %q, want UTC version %q", version, want)
	}
	// Compile the Go fallback with the same synthetic VCS metadata, then compare
	// its result to the script. This catches drift between the two implementations.
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module memogram-version-test\n\ngo 1.27.2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(root, "internal/version/version.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, "version"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "version/version.go"), source, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\nimport (\"fmt\"; \"memogram-version-test/version\")\nfunc main() { if err := version.Validate(); err != nil { panic(err) }; fmt.Println(version.Version) }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", "-buildvcs=true", ".")
	cmd.Dir = repo
	output, err = cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != version {
		t.Fatalf("script/Go metadata mismatch: want %q, got %s, err %v", version, output, err)
	}
}

func TestPreviousStableRelease(t *testing.T) {
	requireTools(t)
	repo := testRepository(t)
	git(t, repo, "tag", "v0.5.0")
	git(t, repo, "commit", "--allow-empty", "-m", "first CalVer release")
	assertPrevious := func(want string) {
		t.Helper()
		output, err := releaseCommand("previous-tag", repo).CombinedOutput()
		if err != nil || strings.TrimSpace(string(output)) != want {
			t.Fatalf("previous-tag: got %q, %v, want %q", output, err, want)
		}
	}
	assertPrevious("v0.5.0")
	git(t, repo, "tag", "26.09.0")
	git(t, repo, "commit", "--allow-empty", "-m", "candidate")
	git(t, repo, "tag", "26.10.0-rc.1")
	git(t, repo, "commit", "--allow-empty", "-m", "new release")
	git(t, repo, "tag", "26.10.0")
	// A valid, numerically newer release outside HEAD's ancestry must not win.
	orphan := git(t, repo, "commit-tree", "HEAD^{tree}", "-m", "unrelated release")
	git(t, repo, "tag", "99.12.0", orphan)
	assertPrevious("26.09.0")
	git(t, repo, "commit", "--allow-empty", "-m", "next release")
	assertPrevious("26.10.0")
}
