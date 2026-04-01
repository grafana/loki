// Copyright 2023-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package strict

import "strings"

// isHeaderIgnored checks if a header name should be ignored in strict validation.
// Uses the effective ignored headers list from options (defaults, replaced, or merged).
// Set-Cookie is direction-aware: ignored in responses but reported in requests.
func (v *Validator) isHeaderIgnored(name string, direction Direction) bool {
	lower := strings.ToLower(name)

	// Set-Cookie is expected in responses but unexpected in requests
	if lower == "set-cookie" {
		return direction == DirectionResponse
	}

	// Check effective ignored list
	for _, h := range v.getEffectiveIgnoredHeaders() {
		if strings.ToLower(h) == lower {
			return true
		}
	}
	return false
}

// getEffectiveIgnoredHeaders returns the list of headers to ignore based on
// configuration. Uses the ValidationOptions method for consistency.
func (v *Validator) getEffectiveIgnoredHeaders() []string {
	if v.options == nil {
		return nil
	}
	return v.options.GetEffectiveStrictIgnoredHeaders()
}
