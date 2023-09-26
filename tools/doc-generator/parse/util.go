// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/cortexproject/cortex/blob/master/tools/doc-generator/util.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Cortex Authors.

package parse

import (
	"math"
	"strings"
)

func FindFlagsPrefix(flags []string) []string {
	if len(flags) == 0 {
		return flags
	}

	// Split the input flags input tokens separated by "."
	// because the want to find the prefix where segments
	// are dot-separated.
	var tokens [][]string
	for _, flag := range flags {
		tokens = append(tokens, strings.Split(flag, "."))
	}

	// Find the shortest tokens.
	minLength := math.MaxInt32
	for _, t := range tokens {
		if len(t) < minLength {
			minLength = len(t)
		}
	}

	// We iterate backward to find common suffixes. Each time
	// a common suffix is found, we remove it from the tokens.
outer:
	for i := 0; i < minLength; i++ {
		lastToken := tokens[0][len(tokens[0])-1]

		// Interrupt if the last token is different across the flags.
		for _, t := range tokens {
			if t[len(t)-1] != lastToken {
				break outer
			}
		}

		// The suffix token is equal across all flags, so we
		// remove it from all of them and re-iterate.
		for i, t := range tokens {
			tokens[i] = t[:len(t)-1]
		}
	}

	// The remaining tokens are the different flags prefix, which we can
	// now merge with the ".".
	var prefixes []string
	for _, t := range tokens {
		prefixes = append(prefixes, strings.Join(t, "."))
	}

	return prefixes
}
