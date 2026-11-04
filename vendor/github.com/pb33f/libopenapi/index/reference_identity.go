// Copyright 2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/pb33f/libopenapi/utils"
)

// CanonicalReferenceIdentity normalizes the document portion of a resolved
// reference while preserving its JSON Pointer fragment. It is intended for
// identity comparisons, not for rewriting authored reference values.
func CanonicalReferenceIdentity(ref string) string {
	base, fragment := SplitRefFragment(ref)
	if base == "" {
		return fragment
	}
	if parsed, err := url.Parse(base); err == nil && parsed.IsAbs() && parsed.Host != "" {
		parsed.Path = path.Clean(parsed.Path)
		return parsed.String() + fragment
	}
	base = utils.ReplaceWindowsDriveWithLinuxPath(base)
	base = filepath.ToSlash(filepath.Clean(base))
	base = strings.TrimSuffix(base, "/.")
	return base + fragment
}
