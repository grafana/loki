package logline

import (
	"fmt"

	"github.com/grafana/loki/pkg/push"

	v3 "github.com/grafana/loki/v3/pkg/logline/internal/v3"
	v4 "github.com/grafana/loki/v3/pkg/logline/internal/v4"
)

// ExtractFunc is the signature of an n-gram extraction function. Implementations
// must be stateless and safe for concurrent use. The structuredMetadata
// parameter carries the entry's structured metadata key-value pairs and
// labelValues carries stream label values. Versions that do not index one or
// both sources ignore them.
type ExtractFunc func(n int, line string, structuredMetadata push.LabelsAdapter, labelValues []string, ngrams [][8]byte) [][8]byte

// IsPackedTermKey reports whether an extracted key was packed into the full term
// key width rather than being a plain text n-gram.
//
// Text n-grams only ever contain the transformed alphabet: space, '.', '0'-'9'
// and 'A'-'Z'. Any byte below 0x20 (space) is therefore impossible in a text gram.
func IsPackedTermKey(key [8]byte) bool {
	return key[0] < 0x20
}

// ExtractorForVersion returns the extraction function paired with the given
// index format version. Extraction is part of the index version contract: a v3
// index is queried with the v3 extractor.
func ExtractorForVersion(version string) (ExtractFunc, error) {
	switch version {
	case "v3":
		return v3.ExtractFeatures, nil
	case "v4":
		return v4.ExtractFeatures, nil
	default:
		return nil, fmt.Errorf("no extractor registered for index version %q", version)
	}
}
