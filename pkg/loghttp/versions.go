package loghttp

import (
	"strings"
)

type Version int

const (
	VersionLegacy = iota
	VersionV1
)

//Version returns the http/json version for a given path.
func GetVersion(uri string) Version {
	if strings.HasPrefix(strings.ToLower(uri), "/loki/api/v1") {
		return VersionV1
	}

	return VersionLegacy
}
