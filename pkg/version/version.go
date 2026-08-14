// Package version holds build-time metadata injected via -ldflags.
package version

import "fmt"

// These are overridden at build time, e.g.:
//
//	go build -ldflags "-X github.com/domehahn/housekeeping/pkg/version.Version=1.2.3 \
//	  -X github.com/domehahn/housekeeping/pkg/version.Commit=$(git rev-parse HEAD) \
//	  -X github.com/domehahn/housekeeping/pkg/version.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
	GoVersion = "unknown"
)

// Info is a plain struct for machine-readable version output.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
}

func Get() Info {
	return Info{Version: Version, Commit: Commit, BuildDate: BuildDate, GoVersion: GoVersion}
}

func (i Info) String() string {
	return fmt.Sprintf("scm-cleaner %s (commit %s, built %s, %s)", i.Version, i.Commit, i.BuildDate, i.GoVersion)
}
