package build

import (
	"runtime"
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
