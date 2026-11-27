// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otlp // import "go.opentelemetry.io/collector/pdata/internal/otlp"

import "fmt"

// ProfilesDevelopmentVersion is the version of the Profiles schema compiled into pdata.
// Update it when adopting an incompatible schema change from opentelemetry-proto.
const ProfilesDevelopmentVersion = "1"

// ProfilesDevelopmentVersionHeader is the OTLP Profiles development version metadata key.
const ProfilesDevelopmentVersionHeader = "otlp-profiles-development-version"

// ValidateProfilesDevelopmentVersion checks all values of the request metadata.
// Missing metadata identifies version 1, including after pdata adopts a later version.
func ValidateProfilesDevelopmentVersion(values []string) error {
	if len(values) > 1 {
		return fmt.Errorf("%s must contain exactly one value", ProfilesDevelopmentVersionHeader)
	}
	version := "1"
	if len(values) == 1 {
		version = values[0]
	}
	// An exact match also rejects malformed values, including empty strings,
	// leading zeros, and comma-separated values combined by an HTTP intermediary.
	if version != ProfilesDevelopmentVersion {
		return fmt.Errorf("invalid or unsupported %s: supported version is %s", ProfilesDevelopmentVersionHeader, ProfilesDevelopmentVersion)
	}
	return nil
}
