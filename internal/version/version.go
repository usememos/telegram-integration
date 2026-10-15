package version

import (
	"fmt"
	"regexp"
	"runtime/debug"
	"time"
)

// Version and Commit are injected by release builds or derived from Go's VCS metadata.
var Version string
var Commit = "unknown"

var buildVersion = regexp.MustCompile(`^[1-9][0-9]\.(0[1-9]|1[0-2])\.(0|[1-9][0-9]*)(-rc\.[1-9][0-9]*|-dev\.[0-9a-f]{7})?$`)

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		Version, Commit = resolveBuild(Version, Commit, info.Settings)
	}
}

func resolveBuild(version, commit string, settings []debug.BuildSetting) (string, string) {
	var date, revision string
	for _, setting := range settings {
		switch setting.Key {
		case "vcs.time":
			if timestamp, err := time.Parse(time.RFC3339, setting.Value); err == nil {
				date = timestamp.UTC().Format("06.01.2")
			}
		case "vcs.revision":
			revision = setting.Value
			if commit == "" || commit == "unknown" {
				commit = revision
			}
		}
	}
	if version == "" && date != "" && len(revision) >= 7 {
		version = date + "-dev." + revision[:7]
	}
	return version, commit
}

func Validate() error {
	if !buildVersion.MatchString(Version) {
		return fmt.Errorf("invalid build version %q: build with scripts/build.sh or inject internal/version.Version as YY.MM.N[-rc.K] or YY.MM.D-dev.<sha7>", Version)
	}
	return nil
}
