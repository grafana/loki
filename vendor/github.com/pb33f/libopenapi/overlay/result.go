// Copyright 2022-2025 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package overlay

// Result represents the result of applying an overlay to a target document.
type Result struct {
	// Bytes is the raw YAML/JSON bytes of the modified document.
	Bytes []byte

	// Warnings contains non-fatal issues encountered during application.
	Warnings []*Warning
}
