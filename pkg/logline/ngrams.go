package logline

import (
	"fmt"

	"github.com/grafana/loki/pkg/push"

	v3 "github.com/grafana/loki/v3/pkg/logline/internal/v3"
	v4 "github.com/grafana/loki/v3/pkg/logline/internal/v4"
	v5 "github.com/grafana/loki/v3/pkg/logline/internal/v5"
)

// ExtractFunc is the signature of an n-gram extraction function. Implementations
// must be stateless and safe for concurrent use. The structuredMetadata
// parameter carries the entry's structured metadata key-value pairs and
// labelValues carries stream label values. Versions that do not index one or
// both sources ignore them.
type ExtractFunc func(n int, line string, structuredMetadata push.LabelsAdapter, labelValues []string, ngrams [][8]byte) [][8]byte

// TermFormatFunc renders an extracted key as the term string to look up in the
// index. Implementations must be stateless and safe for concurrent use.
// ngramLength must be in [1, 8].
//
// A key is 8 bytes wide but a version decides how many of them carry the term.
type TermFormatFunc func(key [8]byte, ngramLength int) string

// ngramsByVersion pairs each index version's extraction and formatting
// functions. They are registered together because: a version whose
// extractor emits a new kind of key needs the formatter that renders it.
var ngramsByVersion = map[string]struct {
	extract ExtractFunc
	format  TermFormatFunc
}{
	"v3": {extract: v3.ExtractFeatures, format: v3.FormatTerm},
	"v4": {extract: v4.ExtractFeatures, format: v4.FormatTerm},
	// v5 is in development. Its extractor is v4's, forked unchanged.
	"v5": {extract: v5.ExtractFeatures, format: v5.FormatTerm},
}

// ExtractorForVersion returns the extraction function paired with the given
// index format version. Extraction is part of the index version contract: a v3
// index is queried with the v3 extractor.
func ExtractorForVersion(version string) (ExtractFunc, error) {
	fns, ok := ngramsByVersion[version]
	if !ok {
		return nil, fmt.Errorf("no extractor registered for index version %q", version)
	}
	return fns.extract, nil
}

// FormatterForVersion returns the term formatter paired with the given index
// format version. Keys extracted with a version's extractor must be rendered
// with that same version's formatter.
func FormatterForVersion(version string) (TermFormatFunc, error) {
	fns, ok := ngramsByVersion[version]
	if !ok {
		return nil, fmt.Errorf("no term formatter registered for index version %q", version)
	}
	return fns.format, nil
}
