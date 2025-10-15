// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package trustboundary

import (
	"context"
	"fmt"
	"regexp"
)

const (
	workloadAllowedLocationsEndpoint  = "https://iamcredentials.%s/v1/projects/%s/locations/global/workloadIdentityPools/%s/allowedLocations"
	workforceAllowedLocationsEndpoint = "https://iamcredentials.%s/v1/locations/global/workforcePools/%s/allowedLocations"
)

var (
	workforceAudiencePattern = regexp.MustCompile(`//iam\.([^/]+)/locations/global/workforcePools/([^/]+)`)
	workloadAudiencePattern  = regexp.MustCompile(`//iam\.([^/]+)/projects/([^/]+)/locations/global/workloadIdentityPools/([^/]+)`)
)

// NewExternalAccountConfigProvider creates a new ConfigProvider for external accounts.
func NewExternalAccountConfigProvider(audience, inputUniverseDomain string) (ConfigProvider, error) {
	var audienceDomain, projectNumber, poolID string
	var isWorkload bool

	matches := workloadAudiencePattern.FindStringSubmatch(audience)
	if len(matches) == 4 { // Expecting full match, domain, projectNumber, poolID
		audienceDomain = matches[1]
		projectNumber = matches[2]
		poolID = matches[3]
		isWorkload = true
	} else {
		matches = workforceAudiencePattern.FindStringSubmatch(audience)
		if len(matches) == 3 { // Expecting full match, domain, poolID
			audienceDomain = matches[1]
			poolID = matches[2]
			isWorkload = false
		} else {
			return nil, fmt.Errorf("trustboundary: unknown audience format: %q", audience)
		}
	}

	effectiveUniverseDomain := inputUniverseDomain
	if effectiveUniverseDomain == "" {
		effectiveUniverseDomain = audienceDomain
	} else if audienceDomain != "" && effectiveUniverseDomain != audienceDomain {
		return nil, fmt.Errorf("trustboundary: provided universe domain (%q) does not match domain in audience (%q)", inputUniverseDomain, audienceDomain)
	}

	if isWorkload {
		return &workloadIdentityPoolConfigProvider{
			projectNumber:  projectNumber,
			poolID:         poolID,
			universeDomain: effectiveUniverseDomain,
		}, nil
	}
	return &workforcePoolConfigProvider{
		poolID:         poolID,
		universeDomain: effectiveUniverseDomain,
	}, nil
}

type workforcePoolConfigProvider struct {
	poolID         string
	universeDomain string
}

func (p *workforcePoolConfigProvider) GetTrustBoundaryEndpoint(ctx context.Context) (string, error) {
	return fmt.Sprintf(workforceAllowedLocationsEndpoint, p.universeDomain, p.poolID), nil
}

func (p *workforcePoolConfigProvider) GetUniverseDomain(ctx context.Context) (string, error) {
	return p.universeDomain, nil
}

type workloadIdentityPoolConfigProvider struct {
	projectNumber  string
	poolID         string
	universeDomain string
}

func (p *workloadIdentityPoolConfigProvider) GetTrustBoundaryEndpoint(ctx context.Context) (string, error) {
	return fmt.Sprintf(workloadAllowedLocationsEndpoint, p.universeDomain, p.projectNumber, p.poolID), nil
}

func (p *workloadIdentityPoolConfigProvider) GetUniverseDomain(ctx context.Context) (string, error) {
	return p.universeDomain, nil
}
