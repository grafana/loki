package index

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"
)

// TestBoundedPostingsOffsetTableReads exercises the per-name bounded reads of the
// postings offset table: the postingsEnd map plus the region-bounded Range calls
// in Postings and LabelValues. The old code sliced the whole offset table; these
// paths slice only one label name's region, so a boundary-math error would
// silently truncate the decoded entries and drop postings/values rather than
// error loudly. The cases below target the boundaries the bounding introduced:
//
//   - a single-value name (the offset table holds one sampled entry for it, so it
//     is bounded solely by postingsEnd[name]);
//   - the lexicographically-last name (bounded by the table payloadLen, since there
//     is no following name whose first entry marks the end);
//   - names with more than symbolFactor (32) values, so the sparse offset table has
//     several sampled blocks, queried at the first/middle/last/absent positions —
//     the absent and boundary cases push the Postings scan up to the e[i+2] bound.
//
// A regression in the bounding (too-small postingsEnd, wrong payloadStart, or an
// insufficient e[i+2] bound) makes the bounded Range too short, so the decbuf trips
// ErrInvalidSize (surfaced as an error here) or returns fewer entries than written
// (caught by the value-set / count assertions).
func TestBoundedPostingsOffsetTableReads(t *testing.T) {
	dir := t.TempDir()
	fn := filepath.Join(dir, IndexFilename)

	iw, err := NewWriter(context.Background(), FormatV3, fn)
	require.NoError(t, err)

	// > symbolFactor (32) so "a" and "z_last" each span multiple sampled blocks.
	const manyValues = 50

	// Ground truth: label name -> the set of values written for it. Names sort as
	// a < foo < m_single < z_last, so "z_last" exercises the payloadLen (last-name)
	// bound and "m_single"/"foo" exercise the single-entry bound.
	want := map[string][]string{
		"m_single": {"only"},
		"foo":      {"bar"},
	}
	for i := range manyValues {
		v := fmt.Sprintf("%03d", i)
		want["a"] = append(want["a"], v)
		want["z_last"] = append(want["z_last"], v)
	}

	// One series per index i carries a="00i", z_last="00i", plus the shared
	// single-value labels. So "a" and "z_last" get manyValues distinct values each,
	// while "m_single" and "foo" get exactly one.
	var series []labels.Labels
	for i := range manyValues {
		v := fmt.Sprintf("%03d", i)
		series = append(series, labels.FromStrings(
			"a", v,
			"z_last", v,
			"m_single", "only",
			"foo", "bar",
		))
	}

	// Add all symbols (names + values) in sorted order.
	symset := map[string]struct{}{}
	for name, vals := range want {
		symset[name] = struct{}{}
		for _, v := range vals {
			symset[v] = struct{}{}
		}
	}
	syms := make([]string, 0, len(symset))
	for s := range symset {
		syms = append(syms, s)
	}
	sort.Strings(syms)
	for _, s := range syms {
		require.NoError(t, iw.AddSymbol(s))
	}

	// Series must be added in fingerprint order.
	sort.Slice(series, func(i, j int) bool {
		return labels.StableHash(series[i]) < labels.StableHash(series[j])
	})
	for i, s := range series {
		require.NoError(t, iw.AddSeries(storage.SeriesRef(i), s, model.Fingerprint(labels.StableHash(s))))
	}
	_, err = iw.Close(false)
	require.NoError(t, err)

	ir, err := NewFileReader(fn)
	require.NoError(t, err)
	defer func() { require.NoError(t, ir.Close()) }()

	// --- LabelValues: the bounded read must return the complete value set. ---
	for name, vals := range want {
		got, err := ir.LabelValues(name)
		require.NoError(t, err)
		sort.Strings(got)
		exp := append([]string(nil), vals...)
		sort.Strings(exp)
		require.Equal(t, exp, got, "LabelValues(%q) truncated by the bounded read", name)
	}

	// An absent name must yield no values, not a panic / out-of-bounds Range.
	got, err := ir.LabelValues("does_not_exist")
	require.NoError(t, err)
	require.Empty(t, got)

	// postingValues resolves Postings(name=value) to the value each matched series
	// actually carries for `name`, via the bounded Postings + Series paths. A wrong
	// posting list shows up as a wrong count or a mismatched value.
	postingValues := func(name, value string) []string {
		p, err := ir.Postings(name, nil, value)
		require.NoError(t, err)
		var out []string
		var lbls labels.Labels
		var metas []ChunkMeta
		for p.Next() {
			_, err := ir.Series(p.At(), 0, math.MaxInt64, &lbls, &metas)
			require.NoError(t, err)
			out = append(out, lbls.Get(name))
		}
		require.NoError(t, p.Err())
		return out
	}

	last := fmt.Sprintf("%03d", manyValues-1)
	mid := fmt.Sprintf("%03d", manyValues/2) // crosses a sampled-block boundary
	cases := []struct {
		name, value string
		count       int
	}{
		{"a", "000", 1},                  // first value of a multi-block name
		{"a", mid, 1},                    // middle value
		{"a", last, 1},                   // last value
		{"a", "999", 0},                  // absent, after all values (scans to the bound)
		{"z_last", "000", 1},             // last name, first value (payloadLen bound)
		{"z_last", last, 1},              // last name, last value
		{"z_last", "999", 0},             // last name, absent value
		{"m_single", "only", manyValues}, // single-entry name, shared by every series
		{"m_single", "nope", 0},          // single-entry name, absent value
		{"foo", "bar", manyValues},       // single-entry name
	}
	for _, c := range cases {
		out := postingValues(c.name, c.value)
		require.Len(t, out, c.count, "Postings(%q=%q) returned the wrong series count", c.name, c.value)
		for _, v := range out {
			require.Equal(t, c.value, v, "Postings(%q=%q) returned a series with the wrong value", c.name, c.value)
		}
	}
}
