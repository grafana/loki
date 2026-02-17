package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
)

type Status string

const (
	StatusCurrent      Status = "CURRENT"
	StatusSupported    Status = "SUPPORTED"
	StatusDeprecated   Status = "DEPRECATED"
	StatusExperimental Status = "EXPERIMENTAL"
	StatusUnknown      Status = ""
)

// SupportedVersion stores a normalized form of the API version data. It handles APIs that
// support microversions as well as those that do not.
type SupportedVersion struct {
	// Major is the major version number of the API
	Major int
	// Minor is the minor version number of the API
	Minor int
	// Status is the status of the API
	Status Status
	SupportedMicroversions
}

// SupportedMicroversions stores a normalized form of the maximum and minimum API microversions
// supported by a given service.
type SupportedMicroversions struct {
	// MaxMajor is the major version number of the maximum supported API microversion
	MaxMajor int
	// MaxMinor is the minor version number of the maximum supported API microversion
	MaxMinor int
	// MinMajor is the major version number of the minimum supported API microversion
	MinMajor int
	// MinMinor is the minor version number of the minimum supported API microversion
	MinMinor int
}

type version struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Version    string `json:"version,omitempty"`
	MaxVersion string `json:"max_version,omitempty"`
	MinVersion string `json:"min_version"`
}

type response struct {
	Versions []version `json:"-"`
}

func (r *response) UnmarshalJSON(in []byte) error {
	// intermediateResponse is an intermediate struct that allows us to offload the difference
	// between a single version document and a multi-version document to the json parser and
	// only focus on differences in the latter
	type intermediateResponse struct {
		ID       string           `json:"id"`
		Version  *version         `json:"version"`
		Versions *json.RawMessage `json:"versions"`
	}

	data := intermediateResponse{}
	if err := json.Unmarshal(in, &data); err != nil {
		return err
	}

	// case 1: we have a single enveloped version object
	//
	// this is the approach used by Manila for single version responses
	if data.Version != nil {
		r.Versions = []version{*data.Version}
		return nil
	}

	// case 2: we have an singly enveloped array of version objects
	//
	// this is the approach used by nova, cinder and glance, among others, for multi-version
	// responses
	if data.Versions != nil {
		var versionArr []version
		if err := json.Unmarshal(*data.Versions, &versionArr); err == nil {
			r.Versions = versionArr
			return nil
		}
	}

	// case 3: we have an doubly enveloped array of version objects
	//
	// this is the approach used by keystone and barbican, among others, for multi-version
	// responses
	if data.Versions != nil {
		type values struct {
			Values []version `json:"values"`
		}

		var valuesObj values
		if err := json.Unmarshal(*data.Versions, &valuesObj); err == nil {
			r.Versions = valuesObj.Values
			return nil
		}
	}

	// case 4: we have a single unenveloped version object
	//
	// this is the approach used by most other services for single version responses
	if data.ID != "" {
		r.Versions = []version{{ID: data.ID}}
		return nil
	}

	return fmt.Errorf("failed to unmarshal versions document: %s", in)
}

func extractVersion(endpointURL string) (int, int, error) {
	u, err := url.Parse(endpointURL)
	if err != nil {
		return 0, 0, err
	}

	parts := strings.Split(strings.TrimRight(u.Path, "/"), "/")
	if len(parts) == 0 {
		return 0, 0, fmt.Errorf("expected path with version, got: %s", u.Path)
	}

	// first, check the nth path element for a version string
	if majorVersion, minorVersion, err := ParseVersion(parts[len(parts)-1]); err == nil {
		return majorVersion, minorVersion, nil
	}

	// if there are no more parts, quit
	if len(parts) == 1 {
		// we don't return the error message directly since it might be misleading: at this point
		// we might have a *malformed* version identifier rather than *no* version identifier
		return 0, 0, fmt.Errorf("failed to infer version from path: %s", u.Path)
	}

	// the guidelines say we should use the currently scoped project_id from the token, but we
	// don't necessarily have a token yet so we speculatively look at the (n-1)th path element
	// (but only that) just as keystoneauth does
	//
	// https://github.com/openstack/keystoneauth/blob/master/keystoneauth1/discover.py#L1534-L1545
	if majorVersion, minorVersion, err := ParseVersion(parts[len(parts)-1]); err == nil {
		return majorVersion, minorVersion, err
	}

	// once again, we don't return the error message directly
	return 0, 0, fmt.Errorf("failed to infer version from path: %s", u.Path)
}

// GetServiceVersions returns the versions supported by the ServiceClient Endpoint.
// If the endpoint resolves to an unversioned discovery API, this should return one or more supported versions.
// If the endpoint resolves to a versioned discovery API, this should return exactly one supported version.
func GetServiceVersions(ctx context.Context, client *gophercloud.ProviderClient, endpointURL string, discoverVersions bool) ([]SupportedVersion, error) {
	var supportedVersions []SupportedVersion
	var endpointVersion *SupportedVersion

	if majorVersion, minorVersion, err := extractVersion(endpointURL); err == nil {
		endpointVersion = &SupportedVersion{Major: majorVersion, Minor: minorVersion}
		if !discoverVersions {
			return append(supportedVersions, *endpointVersion), nil
		}
	}

	var resp response
	_, err := client.Request(ctx, "GET", endpointURL, &gophercloud.RequestOpts{
		JSONResponse: &resp,
		OkCodes:      []int{200, 300},
	})
	if err != nil {
		// we weren't able to find a discovery document but we have version information from the URL
		if endpointVersion != nil {
			return append(supportedVersions, *endpointVersion), nil
		}
		return supportedVersions, err
	}

	versions := resp.Versions

	for _, version := range versions {
		majorVersion, minorVersion, err := ParseVersion(version.ID)
		if err != nil {
			return supportedVersions, err
		}

		status, err := ParseStatus(version.Status)
		if err != nil {
			return supportedVersions, err
		}

		supportedVersion := SupportedVersion{
			Major:  majorVersion,
			Minor:  minorVersion,
			Status: status,
		}

		// Only normalize the microversions if there are microversions to normalize
		if (version.Version != "" || version.MaxVersion != "") && version.MinVersion != "" {
			supportedVersion.MinMajor, supportedVersion.MinMinor, err = ParseMicroversion(version.MinVersion)
			if err != nil {
				return supportedVersions, err
			}

			maxVersion := version.Version
			if maxVersion == "" {
				maxVersion = version.MaxVersion
			}
			supportedVersion.MaxMajor, supportedVersion.MaxMinor, err = ParseMicroversion(maxVersion)
			if err != nil {
				return supportedVersions, err
			}
		}

		supportedVersions = append(supportedVersions, supportedVersion)
	}

	sort.Slice(supportedVersions, func(i, j int) bool {
		return supportedVersions[i].Major > supportedVersions[j].Major || (supportedVersions[i].Major == supportedVersions[j].Major &&
			supportedVersions[i].Minor > supportedVersions[j].Minor)
	})

	return supportedVersions, nil
}

// GetSupportedMicroversions returns the minimum and maximum microversion that is supported by the ServiceClient Endpoint.
func GetSupportedMicroversions(ctx context.Context, client *gophercloud.ServiceClient) (SupportedMicroversions, error) {
	var supportedMicroversions SupportedMicroversions

	supportedVersions, err := GetServiceVersions(ctx, client.ProviderClient, client.Endpoint, true)
	if err != nil {
		return supportedMicroversions, err
	}

	// If there are multiple versions then we were handed an unversioned endpoint. These don't
	// provide microversion information, so we need to fail. Likewise, if there are no versions
	// then something has gone wrong and we also need to fail.
	if len(supportedVersions) > 1 {
		return supportedMicroversions, fmt.Errorf("unversioned endpoint with multiple alternatives not supported")
	} else if len(supportedVersions) == 0 {
		return supportedMicroversions, fmt.Errorf("microversions not supported by endpoint")
	}

	supportedMicroversions = supportedVersions[0].SupportedMicroversions

	if supportedMicroversions.MaxMajor == 0 &&
		supportedMicroversions.MaxMinor == 0 &&
		supportedMicroversions.MinMajor == 0 &&
		supportedMicroversions.MinMinor == 0 {
		return supportedMicroversions, fmt.Errorf("microversions not supported by endpoint")
	}

	return supportedMicroversions, err
}

// RequireMicroversion checks that the required microversion is supported and
// returns a ServiceClient with the microversion set.
func RequireMicroversion(ctx context.Context, client gophercloud.ServiceClient, required string) (gophercloud.ServiceClient, error) {
	supportedMicroversions, err := GetSupportedMicroversions(ctx, &client)
	if err != nil {
		return client, fmt.Errorf("unable to determine supported microversions: %w", err)
	}
	supported, err := supportedMicroversions.IsSupported(required)
	if err != nil {
		return client, err
	}
	if !supported {
		return client, fmt.Errorf("microversion %s not supported. Supported versions: %v", required, supportedMicroversions)
	}
	client.Microversion = required
	return client, nil
}

// IsSupported checks if a microversion falls in the supported interval.
// It returns true if the version is within the interval and false otherwise.
func (supported SupportedMicroversions) IsSupported(version string) (bool, error) {
	// Parse the version X.Y into X and Y integers that are easier to compare.
	vMajor, vMinor, err := ParseMicroversion(version)
	if err != nil {
		return false, err
	}

	// Check that the major version number is supported.
	if (vMajor < supported.MinMajor) || (vMajor > supported.MaxMajor) {
		return false, nil
	}

	// Check that the minor version number is supported
	if (vMinor <= supported.MaxMinor) && (vMinor >= supported.MinMinor) {
		return true, nil
	}

	return false, nil
}

// ParseVersion parsed the version strings v{MAJOR} and v{MAJOR}.{MINOR} into separate integers
// major and minor.
// For example, "v2.1" becomes 2 and 1, "v3" becomes 3 and 0, and "1" becomes 1 and 0.
func ParseVersion(version string) (major, minor int, err error) {
	if version == "" {
		return 0, 0, fmt.Errorf("empty version provided")
	}

	// We use the regex indicated by the version discovery guidelines.
	//
	// https://specs.openstack.org/openstack/api-sig/guidelines/consuming-catalog/version-discovery.html#inferring-version
	//
	// However, we diverge slightly since not all services include the 'v' prefix (glares at zaqar)
	versionRe := regexp.MustCompile(`^v?(?P<major>[0-9]+)(\.(?P<minor>[0-9]+))?$`)

	match := versionRe.FindStringSubmatch(version)
	if len(match) == 0 {
		return 0, 0, fmt.Errorf("invalid format: %q", version)
	}

	major, err = strconv.Atoi(match[versionRe.SubexpIndex("major")])
	if err != nil {
		return 0, 0, err
	}

	minor = 0
	if match[versionRe.SubexpIndex("minor")] != "" {
		minor, err = strconv.Atoi(match[versionRe.SubexpIndex("minor")])
		if err != nil {
			return 0, 0, err
		}
	}

	return major, minor, nil
}

// ParseMicroversion parses the version major.minor into separate integers major and minor.
// For example, "2.53" becomes 2 and 53.
func ParseMicroversion(version string) (major int, minor int, err error) {
	parts := strings.Split(version, ".")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid microversion format: %q", version)
	}
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return major, minor, nil
}

func ParseStatus(status string) (Status, error) {
	switch strings.ToUpper(status) {
	case "CURRENT", "STABLE": // keystone uses STABLE instead of CURRENT
		return StatusCurrent, nil
	case "SUPPORTED":
		return StatusSupported, nil
	case "DEPRECATED":
		return StatusDeprecated, nil
	case "":
		return StatusUnknown, nil
	default:
		return StatusUnknown, fmt.Errorf("invalid status: %q", status)
	}
}
