package metastore

import (
	"bytes"
	"cmp"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestIndexRuns(t *testing.T) {
	const (
		tocA = "tocs/a/tenant/toc.toc"
		tocB = "tocs/b/tenant/toc.toc"
	)
	paths := func(indexes []indexRun) [][]string {
		var out [][]string
		for _, index := range indexes {
			var group []string
			for _, entry := range index.entries {
				group = append(group, entry.Path)
			}
			out = append(out, group)
		}
		return out
	}

	t.Run("makes each entry of no run a run of its own", func(t *testing.T) {
		got := indexRuns([]IndexEntry{
			{Path: "b", TableOfContentsPath: tocA},
			{Path: "a", TableOfContentsPath: tocA},
		})
		require.Equal(t, [][]string{{"b"}, {"a"}}, paths(got))
	})

	t.Run("groups the entries of one run into one index run in the order of the entries", func(t *testing.T) {
		got := indexRuns([]IndexEntry{
			{Path: "run-1/b", TableOfContentsPath: tocA, RunID: 1},
			{Path: "single", TableOfContentsPath: tocA},
			{Path: "run-1/a", TableOfContentsPath: tocA, RunID: 1},
			{Path: "run-2/a", TableOfContentsPath: tocA, RunID: 2},
		})
		require.Equal(t, [][]string{{"run-1/b", "run-1/a"}, {"single"}, {"run-2/a"}}, paths(got))
	})

	t.Run("keeps runs of different ToCs apart when they share a run ID", func(t *testing.T) {
		got := indexRuns([]IndexEntry{
			{Path: "a-run", TableOfContentsPath: tocA, RunID: 1},
			{Path: "b-run", TableOfContentsPath: tocB, RunID: 1},
		})
		require.Equal(t, [][]string{{"a-run"}, {"b-run"}}, paths(got))
	})
}

func TestIndexRun_WithPostingsFor(t *testing.T) {
	keyRange := func(t *testing.T, minKey, maxKey postings.Key) postings.KeyRange {
		t.Helper()
		r, err := postings.NewKeyRange(minKey, maxKey)
		require.NoError(t, err)
		return r
	}
	label := func(name, value string) postings.Key {
		return postings.Key{Kind: postings.KindLabel, ColumnName: name, LabelValue: value}
	}
	bloom := func(name string) postings.Key { return postings.Key{Kind: postings.KindBloom, ColumnName: name} }

	index := indexRun{entries: []IndexEntry{
		{Path: "blooms", PostingKeys: keyRange(t, bloom("trace_id"), bloom("trace_id"))},
		{Path: "app", PostingKeys: keyRange(t, label("app", "api"), label("app", "web"))},
		{Path: "env", PostingKeys: keyRange(t, label("env", "dev"), label("env", "prod"))},
	}}
	keptPaths := func(t *testing.T, matchers, predicates []*labels.Matcher) []string {
		t.Helper()
		kept, ok := index.withPostingsFor(matchers, predicates)
		if !ok {
			return nil
		}
		var out []string
		for _, entry := range kept.entries {
			out = append(out, entry.Path)
		}
		return out
	}
	matcher := func(typ labels.MatchType, name, value string) *labels.Matcher {
		return labels.MustNewMatcher(typ, name, value)
	}

	t.Run("keeps the index objects that hold a label of a stream matcher, whatever the matcher type", func(t *testing.T) {
		for _, typ := range []labels.MatchType{labels.MatchEqual, labels.MatchNotEqual, labels.MatchRegexp, labels.MatchNotRegexp} {
			require.Equal(t, []string{"app"}, keptPaths(t, []*labels.Matcher{matcher(typ, "app", "zzz")}, nil), typ.String())
		}
	})

	t.Run("keeps the index objects of every matcher name", func(t *testing.T) {
		got := keptPaths(t, []*labels.Matcher{matcher(labels.MatchEqual, "app", "api"), matcher(labels.MatchNotEqual, "env", "dev")}, nil)
		require.Equal(t, []string{"app", "env"}, got)
	})

	t.Run("keeps the bloom and label index objects of an equality predicate", func(t *testing.T) {
		got := keptPaths(t, []*labels.Matcher{matcher(labels.MatchEqual, "env", "prod")}, []*labels.Matcher{matcher(labels.MatchEqual, "trace_id", "x")})
		require.Equal(t, []string{"blooms", "env"}, got)

		got = keptPaths(t, []*labels.Matcher{matcher(labels.MatchEqual, "env", "prod")}, []*labels.Matcher{matcher(labels.MatchEqual, "app", "api")})
		require.Equal(t, []string{"app", "env"}, got)
	})

	t.Run("keeps no index object for a predicate that is not an equality", func(t *testing.T) {
		got := keptPaths(t, []*labels.Matcher{matcher(labels.MatchEqual, "env", "prod")}, []*labels.Matcher{matcher(labels.MatchNotEqual, "trace_id", "x")})
		require.Equal(t, []string{"env"}, got)
	})

	t.Run("returns false when no index object holds a label of the request", func(t *testing.T) {
		_, ok := index.withPostingsFor([]*labels.Matcher{matcher(labels.MatchEqual, "namespace", "x")}, nil)
		require.False(t, ok)
	})

	t.Run("keeps an index object with unknown posting key bounds", func(t *testing.T) {
		unknown := indexRun{entries: []IndexEntry{{Path: "legacy"}}}
		kept, ok := unknown.withPostingsFor([]*labels.Matcher{matcher(labels.MatchEqual, "namespace", "x")}, nil)
		require.True(t, ok)
		require.Equal(t, unknown, kept)
	})
}

// runTestTime lies well inside one ToC window, so every test entry belongs to that window.
var runTestTime = tocTestWindow.Add(time.Hour)

// runFile is one index object of a test run.
type runFile struct {
	observations []postings.LabelObservation

	// shardBuckets is the shard layout that the ToC records for the index object.
	shardBuckets indexpointers.ShardBuckets

	// missing leaves the index object out of the bucket, so a read of it fails.
	missing bool

	// unknownKeys makes the ToC record no posting key bounds for the index object.
	unknownKeys bool
}

// newRunMetastore returns a metastore whose ToC lists files as the index objects of one run. The ToC
// records the posting key bounds of each index object, unless unknownKeys is set.
func newRunMetastore(t *testing.T, files ...runFile) *ObjectMetastore {
	t.Helper()
	bucket := objstore.NewInMemBucket()

	pointers := make([]indexpointers.IndexPointer, len(files))
	for i, file := range files {
		path := fmt.Sprintf("indexes/run/%02d", i)
		pointers[i] = indexpointers.IndexPointer{
			Path:         path,
			StartTs:      runTestTime,
			EndTs:        runTestTime.Add(time.Minute),
			RunID:        1,
			ShardBuckets: file.shardBuckets,
		}
		if !file.unknownKeys {
			pointers[i].PostingKeys = postingKeysOf(t, file.observations)
		}
		if file.missing {
			continue
		}

		obj, closer := buildIndexObject(t, tenantID, file.observations)
		t.Cleanup(closer)
		reader, err := obj.Reader(t.Context())
		require.NoError(t, err)
		require.NoError(t, bucket.Upload(t.Context(), IndexStoragePrefix+"/"+path, reader))
		require.NoError(t, reader.Close())
	}

	tocPath := IndexStoragePrefix + "/" + TableOfContentsPath(tenantID, tocTestWindow)
	require.NoError(t, bucket.Upload(t.Context(), tocPath, bytes.NewReader(newTOCBytes(t, tenantID, pointers...))))
	return NewObjectMetastore(bucket, Config{}, log.NewNopLogger(), prometheus.NewRegistry())
}

// newSingleIndexMetastore returns a metastore whose ToC lists one index object of no run, which
// holds observations.
func newSingleIndexMetastore(t *testing.T, observations []postings.LabelObservation) *ObjectMetastore {
	t.Helper()
	bucket := objstore.NewInMemBucket()

	obj, closer := buildIndexObject(t, tenantID, observations)
	t.Cleanup(closer)
	reader, err := obj.Reader(t.Context())
	require.NoError(t, err)
	require.NoError(t, bucket.Upload(t.Context(), IndexStoragePrefix+"/indexes/single", reader))
	require.NoError(t, reader.Close())

	tocPath := IndexStoragePrefix + "/" + TableOfContentsPath(tenantID, tocTestWindow)
	pointer := indexpointers.IndexPointer{Path: "indexes/single", StartTs: runTestTime, EndTs: runTestTime.Add(time.Minute)}
	require.NoError(t, bucket.Upload(t.Context(), tocPath, bytes.NewReader(newTOCBytes(t, tenantID, pointer))))
	return NewObjectMetastore(bucket, Config{}, log.NewNopLogger(), prometheus.NewRegistry())
}

// postingKeysOf returns the range of the posting keys of observations.
func postingKeysOf(t *testing.T, observations []postings.LabelObservation) postings.KeyRange {
	t.Helper()
	keys := make([]postings.Key, len(observations))
	for i, o := range observations {
		keys[i] = postings.Key{Kind: postings.KindLabel, ColumnName: o.ColumnName, LabelValue: o.LabelValue}
	}
	r, err := postings.NewKeyRange(
		slices.MinFunc(keys, postings.Key.Compare),
		slices.MaxFunc(keys, postings.Key.Compare),
	)
	require.NoError(t, err)
	return r
}

// streamObservations returns the label observations of one stream of section 0 of obj-a.
func streamObservations(streamID int64, lbls ...string) []postings.LabelObservation {
	var out []postings.LabelObservation
	for i := 0; i < len(lbls); i += 2 {
		out = append(out, postings.LabelObservation{
			ObjectPath:   "obj-a",
			SectionIndex: 0,
			ColumnName:   lbls[i],
			LabelValue:   lbls[i+1],
			StreamID:     streamID,
			Time:         postings.At(runTestTime),
		})
	}
	return out
}

// streamsOf returns the stream IDs of every section, keyed by object path and section index.
func streamsOf(sections DataobjSectionDescriptors) map[string][]int64 {
	out := make(map[string][]int64, len(sections))
	for _, section := range sections {
		ids := slices.Clone(section.StreamIDs)
		slices.Sort(ids)
		out[fmt.Sprintf("%s/%d", section.ObjectPath, section.SectionIdx)] = ids
	}
	return out
}

func TestObjectMetastore_Sections_Runs(t *testing.T) {
	ctx := user.InjectOrgID(t.Context(), tenantID)
	request := func(matchers ...*labels.Matcher) SectionsRequest {
		return SectionsRequest{Start: runTestTime.Add(-time.Minute), End: runTestTime.Add(2 * time.Minute), Matchers: matchers}
	}
	appFile := runFile{observations: slices.Concat(
		streamObservations(1, "app", "api"),
		streamObservations(2, "app", "api"),
		streamObservations(3, "app", "web"),
	)}
	envFile := runFile{observations: slices.Concat(
		streamObservations(1, "env", "prod"),
		streamObservations(2, "env", "dev"),
		streamObservations(3, "env", "prod"),
	)}

	t.Run("matches a stream whose labels sit in different index objects of the run", func(t *testing.T) {
		m := newRunMetastore(t, appFile, envFile)

		resp, err := m.Sections(ctx, request(
			labels.MustNewMatcher(labels.MatchEqual, "app", "api"),
			labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
		))
		require.NoError(t, err)
		require.Equal(t, map[string][]int64{"obj-a/0": {1}}, streamsOf(resp.Sections))
	})

	t.Run("applies a filter matcher whose label sits in another index object of the run", func(t *testing.T) {
		m := newRunMetastore(t, appFile, envFile)

		resp, err := m.Sections(ctx, request(
			labels.MustNewMatcher(labels.MatchEqual, "app", "api"),
			labels.MustNewMatcher(labels.MatchNotEqual, "env", "dev"),
		))
		require.NoError(t, err)
		require.Equal(t, map[string][]int64{"obj-a/0": {1}}, streamsOf(resp.Sections))
	})

	t.Run("does not read an index object whose posting keys hold no label of the request", func(t *testing.T) {
		missingEnv := envFile
		missingEnv.missing = true
		m := newRunMetastore(t, appFile, missingEnv)

		resp, err := m.Sections(ctx, request(labels.MustNewMatcher(labels.MatchEqual, "app", "web")))
		require.NoError(t, err)
		require.Equal(t, map[string][]int64{"obj-a/0": {3}}, streamsOf(resp.Sections))

		_, err = m.Sections(ctx, request(labels.MustNewMatcher(labels.MatchEqual, "env", "prod")))
		require.Error(t, err, "a request for env must read the missing index object")
	})

	t.Run("reads an index object with unknown posting key bounds for every request", func(t *testing.T) {
		missingEnv := envFile
		missingEnv.missing = true
		missingEnv.unknownKeys = true
		m := newRunMetastore(t, appFile, missingEnv)

		_, err := m.Sections(ctx, request(labels.MustNewMatcher(labels.MatchEqual, "app", "web")))
		require.Error(t, err)
	})

	t.Run("returns no sections when no index object holds a label of the request", func(t *testing.T) {
		m := newRunMetastore(t, appFile, envFile)

		resp, err := m.Sections(ctx, request(labels.MustNewMatcher(labels.MatchEqual, "namespace", "x")))
		require.NoError(t, err)
		require.Empty(t, resp.Sections)
	})

	t.Run("fails when the index objects of the run record different shard layouts", func(t *testing.T) {
		eight, sixteen := appFile, envFile
		eight.shardBuckets = indexpointers.NewShardBuckets(streams.MustShardLayoutFromBuckets(8))
		sixteen.shardBuckets = indexpointers.NewShardBuckets(streams.MustShardLayoutFromBuckets(16))
		m := newRunMetastore(t, eight, sixteen)

		req := request(
			labels.MustNewMatcher(labels.MatchEqual, "app", "api"),
			labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
		)
		req.StreamHashRange = &streams.HashRange{From: 1 << 63, To: math.MaxUint64}
		_, err := m.Sections(ctx, req)
		require.ErrorIs(t, err, errRunShardLayouts)
	})
}

// TestObjectMetastore_Sections_RunEqualsSingleIndex checks the core invariant of runs: splitting
// the postings of an index into a run, by posting key, does not change which sections and streams
// a request resolves, with or without pruning.
func TestObjectMetastore_Sections_RunEqualsSingleIndex(t *testing.T) {
	ctx := user.InjectOrgID(t.Context(), tenantID)
	rng := rand.New(rand.NewPCG(1, 2))

	// Each stream has a random subset of the labels, so some streams lack a label.
	labelValues := map[string][]string{
		"app":    {"api", "db", "web"},
		"env":    {"dev", "prod"},
		"region": {"eu", "us"},
		"tier":   {"a", "b", "c"},
	}
	names := []string{"app", "env", "region", "tier"}
	newObservations := func() []postings.LabelObservation {
		var out []postings.LabelObservation
		for streamID := int64(1); streamID <= 12; streamID++ {
			for _, name := range names {
				if rng.IntN(4) == 0 {
					continue
				}
				values := labelValues[name]
				out = append(out, streamObservations(streamID, name, values[rng.IntN(len(values))])...)
			}
		}
		return out
	}

	// splitByKey cuts observations into files of whole posting keys, in key order.
	splitByKey := func(observations []postings.LabelObservation, files int) [][]postings.LabelObservation {
		keyOf := func(o postings.LabelObservation) postings.Key {
			return postings.Key{Kind: postings.KindLabel, ColumnName: o.ColumnName, LabelValue: o.LabelValue}
		}
		sorted := slices.Clone(observations)
		slices.SortStableFunc(sorted, func(a, b postings.LabelObservation) int { return keyOf(a).Compare(keyOf(b)) })

		var keys []postings.Key
		for _, o := range sorted {
			if len(keys) == 0 || keys[len(keys)-1] != keyOf(o) {
				keys = append(keys, keyOf(o))
			}
		}
		files = min(files, len(keys))
		cuts := rng.Perm(len(keys) - 1)[:files-1]
		for i := range cuts {
			cuts[i]++
		}
		slices.Sort(cuts)

		out := make([][]postings.LabelObservation, files)
		file := 0
		for _, o := range sorted {
			for file < len(cuts) && keyOf(o).Compare(keys[cuts[file]]) >= 0 {
				file++
			}
			out[file] = append(out[file], o)
		}
		return out
	}

	matcherTypes := []labels.MatchType{labels.MatchEqual, labels.MatchNotEqual, labels.MatchRegexp, labels.MatchNotRegexp}
	newMatcher := func() *labels.Matcher {
		name := append(slices.Clone(names), "missing")[rng.IntN(len(names)+1)]
		value := ""
		if values := labelValues[name]; len(values) > 0 && rng.IntN(5) > 0 {
			value = values[rng.IntN(len(values))]
		}
		typ := matcherTypes[rng.IntN(len(matcherTypes))]
		if typ == labels.MatchRegexp || typ == labels.MatchNotRegexp {
			value = []string{value, value + "|zzz", ".*", ".+", "a.*"}[rng.IntN(5)]
		}
		return labels.MustNewMatcher(typ, name, value)
	}

	for iteration := range 60 {
		observations := newObservations()
		single := newSingleIndexMetastore(t, observations)

		parts := splitByKey(observations, 1+rng.IntN(4))
		files := make([]runFile, len(parts))
		for i, part := range parts {
			files[i] = runFile{observations: part}
		}
		run := newRunMetastore(t, files...)

		for range 10 {
			// A request needs a matcher that does not match the empty value.
			matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, names[rng.IntN(len(names))], "")}
			for matchers[0].Matches("") {
				matchers[0] = newMatcher()
			}
			for range rng.IntN(3) {
				matchers = append(matchers, newMatcher())
			}
			req := SectionsRequest{Start: runTestTime.Add(-time.Minute), End: runTestTime.Add(2 * time.Minute), Matchers: matchers}

			want, wantErr := single.Sections(ctx, req)
			got, gotErr := run.Sections(ctx, req)

			msg := fmt.Sprintf("iteration %d, %d files, matchers %s", iteration, len(files), matchersToString(matchers))
			if wantErr != nil {
				require.Error(t, gotErr, msg)
				continue
			}
			require.NoError(t, gotErr, msg)
			require.Equal(t, normalizeSections(want.Sections), normalizeSections(got.Sections), msg)
		}
	}
}

// normalizeSections returns copies of sections in key order, with sorted stream IDs.
func normalizeSections(sections DataobjSectionDescriptors) []DataobjSectionDescriptor {
	out := make([]DataobjSectionDescriptor, len(sections))
	for i, section := range sections {
		out[i] = *section
		out[i].StreamIDs = slices.Clone(section.StreamIDs)
		slices.Sort(out[i].StreamIDs)
		out[i].AmbiguousPredicates = slices.Clone(section.AmbiguousPredicates)
		slices.Sort(out[i].AmbiguousPredicates)
		out[i].Start, out[i].End = out[i].Start.UTC(), out[i].End.UTC()
	}
	slices.SortFunc(out, func(a, b DataobjSectionDescriptor) int {
		return cmp.Or(cmp.Compare(a.ObjectPath, b.ObjectPath), cmp.Compare(a.SectionIdx, b.SectionIdx))
	})
	return out
}
