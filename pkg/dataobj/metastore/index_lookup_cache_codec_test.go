package metastore

import (
	"reflect"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestIndexLookupCacheCodec(t *testing.T) {
	t.Run("restores the selector matches with every field set", func(t *testing.T) {
		matches := &indexSelectorMatches{sections: []sectionSelectorMatch{
			{
				dataObjectPath:    "obj-1",
				logsSectionIndex:  2,
				streamIDs:         []int64{1, 2, 40},
				matcherLabelNames: []string{"app", "env"},
				timeTerms: []matcherTimeTerm{
					{minUnixNanos: 10, maxUnixNanos: 20, keepsAllStreams: true},
					{minUnixNanos: 15, maxUnixNanos: 30, keepsAllStreams: false, keptStreamIDs: []int64{2, 40}},
					{minUnixNanos: 5, maxUnixNanos: 6, keepsAllStreams: false, keptStreamIDs: nil},
				},
			},
			{
				dataObjectPath:    "obj-2",
				logsSectionIndex:  0,
				streamIDs:         []int64{7},
				matcherLabelNames: []string{"app"},
				timeTerms:         []matcherTimeTerm{{minUnixNanos: 1, maxUnixNanos: 1, keepsAllStreams: true}},
			},
		}}

		value, err := encodeSelectorMatches("key", matches)
		require.NoError(t, err)
		decoded, err := decodeSelectorMatches("key", value)
		require.NoError(t, err)

		require.Equal(t, matches, decoded)
	})

	t.Run("restores the predicate admission with every field set", func(t *testing.T) {
		predicate := labels.MustNewMatcher(labels.MatchEqual, "detected_level", "error")
		admission := &predicateAdmission{
			predicateName:       "detected_level",
			predicateValue:      "error",
			bloomHitSections:    sectionKeySet{{ObjectPath: "obj-1", SectionIdx: 0}: {}, {ObjectPath: "obj-2", SectionIdx: 3}: {}},
			streamLabelSections: sectionKeySet{{ObjectPath: "obj-1", SectionIdx: 2}: {}},
		}

		value, err := encodePredicateAdmission("key", admission)
		require.NoError(t, err)
		decoded, err := decodePredicateAdmission("key", value, predicate)
		require.NoError(t, err)

		require.Equal(t, admission, decoded)
	})

	t.Run("covers every field of the structs that it stores", func(t *testing.T) {
		const advice = "a new field needs an encoder, a decoder and a new indexLookupCacheVersion"
		require.Equal(t, 1, reflect.TypeFor[indexSelectorMatches]().NumField(), "indexSelectorMatches: "+advice)
		require.Equal(t, 5, reflect.TypeFor[sectionSelectorMatch]().NumField(), "sectionSelectorMatch: "+advice)
		require.Equal(t, 4, reflect.TypeFor[matcherTimeTerm]().NumField(), "matcherTimeTerm: "+advice)
		require.Equal(t, 4, reflect.TypeFor[predicateAdmission]().NumField(), "predicateAdmission: "+advice)
	})
}
