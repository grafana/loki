package build

import (
	"runtime"

	"github.com/prometheus/common/version"
)

// These variables are set from the Makefile.
var (
	Version   string
	Revision  string
	Branch    string
	BuildUser string
	BuildDate string
	GoVersion string
)

func init() {
	// Loki uses the metrics from prometheus/common/version. Here we set the values
	// that are used in those metrics.
	version.Version = Version
	version.Revision = Revision
	version.Branch = Branch
	version.BuildUser = BuildUser
	version.BuildDate = BuildDate
	GoVersion = runtime.Version()
}

func GetVersion() VersionInfo {
	return VersionInfo{
		Version:   Version,
		Revision:  Revision,
		Branch:    Branch,
		BuildUser: BuildUser,
		BuildDate: BuildDate,
		GoVersion: GoVersion,
	}
}

type VersionInfo struct {
	Version   string `json:"version"`
	Revision  string `json:"revision"`
	Branch    string `json:"branch"`
	BuildUser string `json:"buildUser"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
}
