package utils

import (
	"net/url"
	"regexp"
	"strings"
)

func parseEndpoint(endpoint string, includeVersion bool) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}

	u.RawQuery, u.Fragment = "", ""

	path := u.Path
	versionRe := regexp.MustCompile("v[0-9.]+/?")

	if version := versionRe.FindString(path); version != "" {
		versionIndex := strings.Index(path, version)
		if includeVersion {
			versionIndex += len(version)
		}
		u.Path = path[:versionIndex]
	}

	return u.String(), nil
}

// BaseEndpoint will return a URL without the /vX.Y
// portion of the URL.
func BaseEndpoint(endpoint string) (string, error) {
	return parseEndpoint(endpoint, false)
}

// BaseVersionedEndpoint will return a URL with the /vX.Y portion of the URL,
// if present, but without a project ID or similar
func BaseVersionedEndpoint(endpoint string) (string, error) {
	return parseEndpoint(endpoint, true)
}
