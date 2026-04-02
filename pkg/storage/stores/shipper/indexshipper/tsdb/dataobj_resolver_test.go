package tsdb

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/sectionref"
)

func TestGetDataobjSections(t *testing.T) {
	dir := t.TempDir()

	table := sectionref.NewSectionRefTable(nil)
	idx0 := table.Add(sectionref.SectionRef{Path: "objects/ab/obj001", SectionID: 0, SeriesID: 1})
	idx1 := table.Add(sectionref.SectionRef{Path: "objects/ab/obj001", SectionID: 1, SeriesID: 1})
	idx2 := table.Add(sectionref.SectionRef{Path: "objects/cd/obj002", SectionID: 0, SeriesID: 5})
	idx3 := table.Add(sectionref.SectionRef{Path: "objects/ab/obj001", SectionID: 0, SeriesID: 3})

	cases := []LoadableSeries{
		{
			Labels: mustParseLabels(`{app="gateway", cluster="us"}`),
			Chunks: index.ChunkMetas{
				{Checksum: idx0, MinTime: 1000, MaxTime: 2000, KB: 10, Entries: 100},
				{Checksum: idx1, MinTime: 3000, MaxTime: 4000, KB: 15, Entries: 150},
			},
		},
		{
			Labels: mustParseLabels(`{app="gateway", cluster="eu"}`),
			Chunks: index.ChunkMetas{
				{Checksum: idx2, MinTime: 1500, MaxTime: 2500, KB: 20, Entries: 200},
			},
		},
		{
			Labels: mustParseLabels(`{app="ingester", cluster="us"}`),
			Chunks: index.ChunkMetas{
				{Checksum: idx3, MinTime: 1200, MaxTime: 1800, KB: 5, Entries: 50},
			},
		},
	}

	tsdbFile := BuildIndex(t, dir, cases)

	tsdbPath := tsdbFile.Path()
	sectionsPath := tsdbPath + ".sections"
	lookupData, err := table.Encode()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(sectionsPath, lookupData, 0o644))

	id, err := identifierFromPath(tsdbPath)
	require.NoError(t, err)
	file, err := NewShippableTSDBFile(id)
	require.NoError(t, err)
	defer file.Close()

	resolver, ok := file.Index.(index.DataobjResolver)
	require.True(t, ok)

	t.Run("all sections for app=gateway", func(t *testing.T) {
		refs, err := resolver.GetDataobjSections(
			context.Background(), "fake", 0, 5000, nil,
			labels.MustNewMatcher(labels.MatchEqual, "app", "gateway"),
		)
		require.NoError(t, err)
		require.Len(t, refs, 3)

		paths := make(map[string]bool)
		for _, ref := range refs {
			paths[ref.Path] = true
		}
		require.True(t, paths["objects/ab/obj001"])
		require.True(t, paths["objects/cd/obj002"])
	})

	t.Run("time-bounded query", func(t *testing.T) {
		refs, err := resolver.GetDataobjSections(
			context.Background(), "fake", 900, 1100, nil,
			labels.MustNewMatcher(labels.MatchEqual, "app", "gateway"),
		)
		require.NoError(t, err)
		require.Len(t, refs, 1)
		require.Equal(t, "objects/ab/obj001", refs[0].Path)
		require.Equal(t, 0, refs[0].SectionID)
	})

	t.Run("groups StreamIDs by section across series", func(t *testing.T) {
		refs, err := resolver.GetDataobjSections(
			context.Background(), "fake", 0, 5000, nil,
			labels.MustNewMatcher(labels.MatchRegexp, "app", ".+"),
		)
		require.NoError(t, err)

		bySection := make(map[string]index.DataobjSectionRef)
		for _, ref := range refs {
			k := ref.Path + "/" + strings.Repeat("0", ref.SectionID)
			bySection[k] = ref
		}

		sec0 := bySection["objects/ab/obj001/"]
		require.Equal(t, 0, sec0.SectionID)
		streamIDs := sec0.StreamIDs
		sort.Slice(streamIDs, func(i, j int) bool { return streamIDs[i] < streamIDs[j] })
		require.Equal(t, []int64{1, 3}, streamIDs,
			"section 0 should have SeriesIDs from both gateway/us (1) and ingester/us (3)")

		require.Equal(t, model.Time(1000), sec0.MinTime)
		require.Equal(t, model.Time(2000), sec0.MaxTime)
	})

	t.Run("dedup across series sharing same section", func(t *testing.T) {
		refs, err := resolver.GetDataobjSections(
			context.Background(), "fake", 0, 5000, nil,
			labels.MustNewMatcher(labels.MatchRegexp, "app", ".+"),
		)
		require.NoError(t, err)

		type key struct {
			Path      string
			SectionID int
		}
		seen := make(map[key]int)
		for _, ref := range refs {
			seen[key{Path: ref.Path, SectionID: ref.SectionID}]++
		}
		require.Equal(t, 1, seen[key{Path: "objects/ab/obj001", SectionID: 0}],
			"section (obj001, 0) should appear exactly once despite being in two series")
	})
}

func TestNewShippableTSDBFile_LoadsLookupCompanion(t *testing.T) {
	dir := t.TempDir()

	table := sectionref.NewSectionRefTable(nil)
	table.Add(sectionref.SectionRef{Path: "objects/ab/test", SectionID: 0, SeriesID: 1})

	cases := []LoadableSeries{
		{
			Labels: mustParseLabels(`{foo="bar"}`),
			Chunks: index.ChunkMetas{
				{Checksum: 0, MinTime: 0, MaxTime: 1, KB: 1, Entries: 1},
			},
		},
	}

	b := NewBuilder(index.FormatV3)
	for _, s := range cases {
		b.AddSeries(s.Labels, model.Fingerprint(labels.StableHash(s.Labels)), s.Chunks)
	}
	dst, err := b.Build(context.Background(), dir, func(from, through model.Time, checksum uint32) Identifier {
		return NewPrefixedIdentifier(SingleTenantTSDBIdentifier{
			TS: time.Now(), From: from, Through: through, Checksum: checksum,
		}, dir, dir)
	})
	require.NoError(t, err)

	t.Run("without lookup file", func(t *testing.T) {
		file, err := NewShippableTSDBFile(dst)
		require.NoError(t, err)
		defer file.Close()

		tsdbIdx := file.Index.(*TSDBIndex)
		require.Nil(t, tsdbIdx.sectionRefTable)
	})

	t.Run("with sections file", func(t *testing.T) {
		sectionsPath := dst.Path() + ".sections"
		lookupData, err := table.Encode()
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(sectionsPath, lookupData, 0o644))
		defer os.Remove(sectionsPath)

		file, err := NewShippableTSDBFile(dst)
		require.NoError(t, err)
		defer file.Close()

		tsdbIdx := file.Index.(*TSDBIndex)
		require.NotNil(t, tsdbIdx.sectionRefTable)
		require.Equal(t, 1, tsdbIdx.sectionRefTable.Len())
		ref, ok := tsdbIdx.sectionRefTable.Lookup(0)
		require.True(t, ok)
		require.Equal(t, "objects/ab/test", ref.Path)
	})
}
