package metastore

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// buildToCObject builds an in-memory ToC object holding tenant's index pointers, as the ToC writer does.
func buildToCObject(t *testing.T, tenant string, pointers []indexpointers.IndexPointer) (*dataobj.Object, func()) {
	t.Helper()
	b, err := indexobj.NewBuilder(tocBuilderCfg, nil)
	require.NoError(t, err)
	for _, p := range pointers {
		require.NoError(t, b.AppendIndexPointer(tenant, p))
	}
	obj, closer, err := b.Flush()
	require.NoError(t, err)
	return obj, func() { _ = closer.Close() }
}

// TestForEachIndexPointer_SingleTenantFullRangeMatchesAllTenants checks that with one tenant and a query
// spanning every pointer's time range, the time-filtered forEachIndexPointer yields exactly what the
// unfiltered forEachIndexPointerAllTenants yields for that tenant.
func TestForEachIndexPointer_SingleTenantFullRangeMatchesAllTenants(t *testing.T) {
	const tenant = "tenant-a"
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	pointers := []indexpointers.IndexPointer{
		{Path: "obj-1", StartTs: base, EndTs: base.Add(time.Hour)},
		{Path: "obj-2", StartTs: base.Add(2 * time.Hour), EndTs: base.Add(3 * time.Hour)},
		{Path: "obj-3", StartTs: base.Add(4 * time.Hour), EndTs: base.Add(5 * time.Hour)},
	}
	obj, closer := buildToCObject(t, tenant, pointers)
	defer closer()

	ctx := user.InjectOrgID(context.Background(), tenant)

	// Span the whole data: earliest start to latest end.
	sStart := scalar.NewTimestampScalar(arrow.Timestamp(pointers[0].StartTs.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
	sEnd := scalar.NewTimestampScalar(arrow.Timestamp(pointers[len(pointers)-1].EndTs.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)

	var filtered []indexpointers.IndexPointer
	require.NoError(t, forEachIndexPointer(ctx, obj, sStart, sEnd, func(p indexpointers.IndexPointer) {
		filtered = append(filtered, p)
	}))

	var all []indexpointers.IndexPointer
	require.NoError(t, forEachIndexPointerAllTenants(ctx, obj, func(gotTenant string, p indexpointers.IndexPointer) {
		require.Equal(t, tenant, gotTenant)
		all = append(all, p)
	}))

	require.Len(t, filtered, len(pointers))
	require.Equal(t, filtered, all)
}

func TestBuildLabelPredicate_MatchNotRegexp(t *testing.T) {
	// This test verifies that MatchNotRegexp correctly excludes values that match the regex
	// and includes values that don't match the regex.
	//
	// For slug!~"^k6.*":
	// - "k6testinstant1" MATCHES the regex ^k6.*, so it should be EXCLUDED (Keep returns false)
	// - "gitsync" does NOT match the regex ^k6.*, so it should be INCLUDED (Keep returns true)

	tests := []struct {
		name        string
		regex       string
		labelValue  string
		shouldMatch bool // true if the value should be KEPT (i.e., does NOT match the regex)
	}{
		{
			name:        "excludes value matching regex",
			regex:       "^k6.*",
			labelValue:  "k6testinstant1",
			shouldMatch: false, // k6testinstant1 matches ^k6.*, so it should be excluded
		},
		{
			name:        "includes value not matching regex",
			regex:       "^k6.*",
			labelValue:  "gitsync",
			shouldMatch: true, // gitsync doesn't match ^k6.*, so it should be included
		},
		{
			name:        "includes value not matching regex - different pattern",
			regex:       "^k6.*",
			labelValue:  "dev",
			shouldMatch: true, // dev doesn't match ^k6.*, so it should be included
		},
		{
			name:        "excludes another value matching regex",
			regex:       "^k6.*",
			labelValue:  "k6testslow5",
			shouldMatch: false, // k6testslow5 matches ^k6.*, so it should be excluded
		},
		{
			name:        "excludes exact match",
			regex:       "^k6$",
			labelValue:  "k6",
			shouldMatch: false, // k6 matches ^k6$, so it should be excluded
		},
		{
			name:        "includes partial non-match",
			regex:       "^k6$",
			labelValue:  "k6test",
			shouldMatch: true, // k6test doesn't match ^k6$, so it should be included
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			matcher := labels.MustNewMatcher(labels.MatchNotRegexp, "slug", tc.regex)

			// Create a mock column for the label
			col := &streams.Column{}

			columns := map[string]*streams.Column{
				"slug": col,
			}

			predicate := buildLabelPredicate(matcher, columns)

			// The predicate should be a FuncPredicate
			funcPred, ok := predicate.(streams.FuncPredicate)
			require.True(t, ok, "expected FuncPredicate, got %T", predicate)

			// Create a scalar value for the label
			value := scalar.NewStringScalar(tc.labelValue)

			// Test the Keep function
			result := funcPred.Keep(col, value)
			require.Equal(t, tc.shouldMatch, result,
				"for regex %q and value %q: expected Keep to return %v, got %v",
				tc.regex, tc.labelValue, tc.shouldMatch, result)
		})
	}
}

func TestBuildLabelPredicate_MatchNotRegexp_NilColumn(t *testing.T) {
	// When the column doesn't exist (nil), the value is treated as empty string.
	// For MatchNotRegexp, if the empty string does NOT match the regex, we should return TruePredicate.
	// If the empty string DOES match the regex, we should return FalsePredicate.

	tests := []struct {
		name       string
		regex      string
		expectTrue bool // true if we expect TruePredicate (empty string doesn't match regex)
	}{
		{
			name:       "nil column with regex that doesn't match empty string",
			regex:      "^k6.*",
			expectTrue: true, // empty string doesn't match ^k6.*, so TruePredicate
		},
		{
			name:       "nil column with regex that matches empty string",
			regex:      "^$",
			expectTrue: false, // empty string matches ^$, so FalsePredicate
		},
		{
			name:       "nil column with regex that matches any string including empty",
			regex:      ".*",
			expectTrue: false, // empty string matches .*, so FalsePredicate
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			matcher := labels.MustNewMatcher(labels.MatchNotRegexp, "slug", tc.regex)

			// No column exists for this label
			columns := map[string]*streams.Column{}

			predicate := buildLabelPredicate(matcher, columns)

			if tc.expectTrue {
				_, ok := predicate.(streams.TruePredicate)
				require.True(t, ok, "expected TruePredicate, got %T", predicate)
			} else {
				_, ok := predicate.(streams.FalsePredicate)
				require.True(t, ok, "expected FalsePredicate, got %T", predicate)
			}
		})
	}
}

func TestBuildLabelPredicate_MatchRegexp(t *testing.T) {
	// Sanity check: verify that MatchRegexp (positive regex) works correctly too
	tests := []struct {
		name        string
		regex       string
		labelValue  string
		shouldMatch bool // true if the value should be KEPT (i.e., DOES match the regex)
	}{
		{
			name:        "includes value matching regex",
			regex:       "^k6.*",
			labelValue:  "k6testinstant1",
			shouldMatch: true, // k6testinstant1 matches ^k6.*, so it should be included
		},
		{
			name:        "excludes value not matching regex",
			regex:       "^k6.*",
			labelValue:  "gitsync",
			shouldMatch: false, // gitsync doesn't match ^k6.*, so it should be excluded
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			matcher := labels.MustNewMatcher(labels.MatchRegexp, "slug", tc.regex)

			col := &streams.Column{}
			columns := map[string]*streams.Column{
				"slug": col,
			}

			predicate := buildLabelPredicate(matcher, columns)

			funcPred, ok := predicate.(streams.FuncPredicate)
			require.True(t, ok, "expected FuncPredicate, got %T", predicate)

			value := scalar.NewStringScalar(tc.labelValue)
			result := funcPred.Keep(col, value)
			require.Equal(t, tc.shouldMatch, result,
				"for regex %q and value %q: expected Keep to return %v, got %v",
				tc.regex, tc.labelValue, tc.shouldMatch, result)
		})
	}
}
