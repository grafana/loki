package postings

import (
	"bytes"
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bits-and-blooms/bloom/v3"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/xcap"
)

// Key identifies a bloom-match result by its (object path, section index) tuple. Defined locally
// to avoid an import cycle with metastore, which converts it to metastore.SectionKey.
type Key struct {
	ObjectPath   string
	SectionIndex int64
}

func MatchSections(ctx context.Context, batches []arrow.RecordBatch, matchers []*labels.Matcher) (map[Key]struct{}, error) {
	// Filter to MatchEqual matchers only; other types are handled on separate caller paths.
	equalMatchers := make([]*labels.Matcher, 0, len(matchers))
	for _, m := range matchers {
		if m != nil && m.Type == labels.MatchEqual {
			equalMatchers = append(equalMatchers, m)
		}
	}
	if len(equalMatchers) == 0 {
		return map[Key]struct{}{}, nil
	}

	ctx, span := xcap.StartSpan(ctx, tracer, "postings.MatchSections")
	defer span.End()
	// Accumulate bloom-deserialize failures locally and record once at the
	// end — keeps the inner per-row loop free of region lookups while still
	// surfacing corruption via xcap.
	var bloomDeserializeFailures int64
	defer func() {
		if bloomDeserializeFailures > 0 {
			xcap.RegionFromContext(ctx).Record(
				xcap.StatPostingsBloomDeserializeFailures.Observe(bloomDeserializeFailures),
			)
		}
	}()

	predicateIndexesByName := make(map[string][]int, len(equalMatchers))
	for i, m := range equalMatchers {
		predicateIndexesByName[m.Name] = append(predicateIndexesByName[m.Name], i)
	}

	sectionMatches := make(map[Key]map[int]struct{})

	for _, rec := range batches {
		if rec == nil || rec.NumRows() == 0 {
			continue
		}

		// Column-position contract: ReadBloomRows projects exactly
		// [object_path, section_index, column_name, bloom_filter] in that
		// order. Type-assertions are guarded so a projection drift surfaces
		// as a typed error rather than a panic.
		pathCol, ok := rec.Column(0).(*array.String)
		if !ok {
			return nil, fmt.Errorf("ReadBloomRows projection violated: column 0 wrong type %T", rec.Column(0))
		}
		sectionCol, ok := rec.Column(1).(*array.Int64)
		if !ok {
			return nil, fmt.Errorf("ReadBloomRows projection violated: column 1 wrong type %T", rec.Column(1))
		}
		columnNameCol, ok := rec.Column(2).(*array.String)
		if !ok {
			return nil, fmt.Errorf("ReadBloomRows projection violated: column 2 wrong type %T", rec.Column(2))
		}
		bloomCol, ok := rec.Column(3).(*array.Binary)
		if !ok {
			return nil, fmt.Errorf("ReadBloomRows projection violated: column 3 wrong type %T", rec.Column(3))
		}

		for i := 0; i < int(rec.NumRows()); i++ {
			if pathCol.IsNull(i) || sectionCol.IsNull(i) || columnNameCol.IsNull(i) || bloomCol.IsNull(i) {
				continue
			}

			predicateIndexes := predicateIndexesByName[columnNameCol.Value(i)]
			if len(predicateIndexes) == 0 {
				continue
			}

			sectionKey := Key{
				ObjectPath:   pathCol.Value(i),
				SectionIndex: sectionCol.Value(i),
			}

			for _, predicateIndex := range predicateIndexes {
				mayContain, deserializeFailed := bloomFilterMayContain(
					bloomCol.Value(i),
					equalMatchers[predicateIndex].Value,
				)
				if deserializeFailed {
					bloomDeserializeFailures++
				}
				if !mayContain {
					continue
				}
				matchedPredicates := sectionMatches[sectionKey]
				if matchedPredicates == nil {
					matchedPredicates = make(map[int]struct{}, len(equalMatchers))
					sectionMatches[sectionKey] = matchedPredicates
				}
				matchedPredicates[predicateIndex] = struct{}{}
			}
		}
	}

	// a Key is kept only if every Equal matcher matched at
	// least one bloom row for that Key.
	matchedSectionKeys := make(map[Key]struct{})
	for sectionKey, matchedPredicates := range sectionMatches {
		if len(matchedPredicates) == len(equalMatchers) {
			matchedSectionKeys[sectionKey] = struct{}{}
		}
	}
	return matchedSectionKeys, nil
}

// bloomFilterMayContain reports whether value may be present in the bloom filter
func bloomFilterMayContain(bloomBytes []byte, value string) (mayContain, deserializeFailed bool) {
	defer func() {
		if r := recover(); r != nil {
			// Corrupted payload that panics on ReadFrom must yield "may contain", not propagate.
			mayContain = true
			deserializeFailed = true
		}
	}()

	var bf bloom.BloomFilter
	if _, err := bf.ReadFrom(bytes.NewReader(bloomBytes)); err != nil {
		return true, true
	}
	return bf.TestString(value), false
}
