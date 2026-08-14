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

// TermKeyLengthForVersion returns the width, in bytes, of a term key in the
// on-disk term dictionary for the given index version.
//
// This is a property of the format, not of the extraction length. The builder
// always stores the first TermKeyLengthForVersion bytes of the [8]byte an
// extractor produces (see the writer's copy(key[:], term[:NgramLength])), and
// IndexReader.FindTerm zero-pads whatever it is given back to that width. Query
// terms must therefore be sliced to this width and not to ngram_length: those
// two happen to be equal for v3 at the production setting, but an extractor is
// free to use the full key, as v4 does for packed numeric grams.
func TermKeyLengthForVersion(version string) (int, error) {
	switch version {
	case "v3", "v4": // v4 shares the v3 on-disk format
		return v3.NgramLength, nil
	default:
		return 0, fmt.Errorf("no term key length registered for index version %q", version)
	}
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
