// Copyright 2025 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// Provenance-includes-location: https://github.com/prometheus/prometheus/blob/93e991ef7ed19cc997a9360c8016cac3767b8057/storage/remote/otlptranslator/prometheus/normalize_label.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Copyright The Prometheus Authors
// Provenance-includes-location: https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/95e8f8fdc2a9dc87230406c9a3cf02be4fd68bea/pkg/translator/prometheus/normalize_label.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Copyright The OpenTelemetry Authors.

package otlptranslator

import (
	"fmt"
	"strings"
	"unicode"
)

// LabelNamer is a helper struct to build label names.
// It translates OpenTelemetry Protocol (OTLP) attribute names to Prometheus-compliant label names.
//
// Example usage:
//
//	namer := LabelNamer{UTF8Allowed: false}
//	result := namer.Build("http.method") // "http_method"
type LabelNamer struct {
	UTF8Allowed bool
}

// Build normalizes the specified label to follow Prometheus label names standard.
//
// Translation rules:
//   - Replaces invalid characters with underscores
//   - Prefixes labels with invalid start characters (numbers or `_`) with "key"
//   - Preserves double underscore labels (reserved names)
//   - If UTF8Allowed is true, returns label as-is
//
// Examples:
//
//	namer := LabelNamer{UTF8Allowed: false}
//	namer.Build("http.method")     // "http_method"
//	namer.Build("123invalid")      // "key_123invalid"
//	namer.Build("__reserved__")    // "__reserved__" (preserved)
func (ln *LabelNamer) Build(label string) (normalizedName string, err error) {
	defer func() {
		if len(normalizedName) == 0 {
			err = fmt.Errorf("normalization for label name %q resulted in empty name", label)
			return
		}

		if ln.UTF8Allowed || normalizedName == label {
			return
		}

		// Check that the resulting normalized name contains at least one non-underscore character
		for _, c := range normalizedName {
			if c != '_' {
				return
			}
		}
		err = fmt.Errorf("normalization for label name %q resulted in invalid name %q", label, normalizedName)
		normalizedName = ""
	}()

	// Trivial case.
	if len(label) == 0 || ln.UTF8Allowed {
		normalizedName = label
		return
	}

	normalizedName = sanitizeLabelName(label)

	// If label starts with a number, prepend with "key_".
	if unicode.IsDigit(rune(normalizedName[0])) {
		normalizedName = "key_" + normalizedName
	} else if strings.HasPrefix(normalizedName, "_") && !strings.HasPrefix(normalizedName, "__") {
		normalizedName = "key" + normalizedName
	}

	return
}
