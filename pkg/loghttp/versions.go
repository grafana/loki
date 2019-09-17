package loghttp

import (
	"strings"
)

// Version holds a loghttp version
type Version int

const (
	VersionLegacy = iota
	VersionV1
)

//GetVersion returns the loghttp version for a given path.
func GetVersion(uri string) Version {
	if strings.HasPrefix(strings.ToLower(uri), "/loki/api/v1") {
		return VersionV1
	}

	return VersionLegacy
}
