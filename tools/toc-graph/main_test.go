package main

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// TestMain is a debugger entrypoint. Set the args below and run/debug this test.
func TestMain(_ *testing.T) {
	os.Args = []string{
		"toc-graph",
		"-bucket", "",
		"-provider", "gcs",
		"-mode", "stream-labels", // toc | streams | long-columns | merge
		"-s3-region", "eu-south-2",
		"-start", "2026-02-20T15:00:00Z",
		"-end", "2026-02-20T15:01:00Z",
		// "-index-prefix", "",
		// "-output", "toc-graph.html",
	}

	main()
}

// ObjectRef associates a label set with the object path that contains it and
// the time range covered by that object.
type ObjectRef struct {
	Labels  labels.Labels
	Path    string
	MinTime int64
	MaxTime int64
	KB      uint32
	Entries uint32
}

// BuildTSDB creates an in-memory TSDB index from the given object references.
// Each unique path is assigned an index used as the ChunkMeta Checksum field.
// The returned paths slice maps checksum values back to object paths:
// paths[chunkMeta.Checksum] == original path.
func BuildTSDB(t testing.TB, refs []ObjectRef) (*index.Reader, []string) {
	builder := tsdb.NewBuilder(index.FormatV3)

	pathIndex := make(map[string]uint32)
	var paths []string

	for _, ref := range refs {
		idx, ok := pathIndex[ref.Path]
		if !ok {
			idx = uint32(len(paths))
			paths = append(paths, ref.Path)
			pathIndex[ref.Path] = idx
		}

		fp := model.Fingerprint(ref.Labels.Hash())
		builder.AddSeries(ref.Labels, fp, []index.ChunkMeta{
			{
				Checksum: idx,
				MinTime:  ref.MinTime,
				MaxTime:  ref.MaxTime,
				KB:       uint32(ref.KB),
				Entries:  uint32(ref.Entries),
			},
		})
	}

	_, data, err := builder.BuildInMemory(
		context.Background(),
		func(from, through model.Time, checksum uint32) tsdb.Identifier {
			return tsdb.SingleTenantTSDBIdentifier{
				TS:       time.Now(),
				From:     from,
				Through:  through,
				Checksum: checksum,
			}
		},
	)
	require.NoError(t, err)

	reader, err := index.NewReader(index.RealByteSlice(data))
	require.NoError(t, err)

	return reader, paths
}

func TestTSDB(t *testing.T) {
	refs := []ObjectRef{
		{
			Labels:  labels.FromStrings("cluster", "us-central-1", "namespace", "loki-dev"),
			Path:    "tenant-a/dataobj/obj-001",
			MinTime: 1000,
			MaxTime: 2000,
			KB:      10,
			Entries: 10,
		},
		{
			Labels:  labels.FromStrings("cluster", "us-central-1", "namespace", "loki-dev"),
			Path:    "tenant-a/dataobj/obj-002",
			MinTime: 2000,
			MaxTime: 3000,
		},
		{
			Labels:  labels.FromStrings("cluster", "eu-west-1", "namespace", "loki-prod"),
			Path:    "tenant-b/dataobj/obj-003",
			MinTime: 1500,
			MaxTime: 2500,
		},
	}

	reader, paths := BuildTSDB(t, refs)
	defer reader.Close()

	require.Len(t, paths, 3)
	require.Equal(t, "tenant-a/dataobj/obj-001", paths[0])
	require.Equal(t, "tenant-a/dataobj/obj-002", paths[1])
	require.Equal(t, "tenant-b/dataobj/obj-003", paths[2])
}

// TestStreamRefRoundTrip builds a TSDB where each stream is one ChunkMeta entry,
// encodes/decodes the chunk ref lookup table (with section IDs), then queries the
// TSDB to resolve which (path, section) pairs contain matching series.
func TestStreamRefRoundTrip(t *testing.T) {
	h := func(s string) int64 {
		ts, err := time.Parse(time.RFC3339, s)
		require.NoError(t, err)
		return ts.UnixMilli()
	}

	refs := []streamRef{
		{
			labels:  labels.FromStrings("cluster", "us-central", "app", "gateway"),
			path:    "dataobj/obj-001",
			section: 0,
			minTime: h("2026-01-15T10:30:00Z"),
			maxTime: h("2026-01-15T12:45:00Z"),
			rows:    100,
		},
		{
			labels:  labels.FromStrings("cluster", "us-central", "app", "gateway"),
			path:    "dataobj/obj-001",
			section: 1, // same object, different section
			minTime: h("2026-01-15T13:00:00Z"),
			maxTime: h("2026-01-15T14:00:00Z"),
			rows:    150,
		},
		{
			labels:  labels.FromStrings("cluster", "us-central", "app", "gateway"),
			path:    "dataobj/obj-002",
			section: 0,
			minTime: h("2026-01-15T12:00:00Z"),
			maxTime: h("2026-01-15T14:15:00Z"),
			rows:    200,
		},
		{
			labels:  labels.FromStrings("cluster", "eu-west", "app", "ingester"),
			path:    "dataobj/obj-003",
			section: 0,
			minTime: h("2026-01-15T11:00:00Z"),
			maxTime: h("2026-01-15T11:30:00Z"),
			rows:    50,
		},
	}

	// --- Build ---
	builder := tsdb.NewBuilder(index.FormatV3)

	type refKey struct {
		path    string
		section int
	}
	refIndex := make(map[refKey]uint32)
	var chunkRefs []chunkRef

	for _, ref := range refs {
		key := refKey{path: ref.path, section: ref.section}
		idx, ok := refIndex[key]
		if !ok {
			idx = uint32(len(chunkRefs))
			chunkRefs = append(chunkRefs, chunkRef{Path: ref.path, SectionID: ref.section})
			refIndex[key] = idx
		}

		fp := model.Fingerprint(ref.labels.Hash())
		builder.AddSeries(ref.labels, fp, []index.ChunkMeta{
			{
				Checksum: idx,
				MinTime:  ref.minTime,
				MaxTime:  ref.maxTime,
				Entries:  uint32(ref.rows),
			},
		})
	}

	_, tsdbData, err := builder.BuildInMemory(
		context.Background(),
		func(from, through model.Time, checksum uint32) tsdb.Identifier {
			return tsdb.SingleTenantTSDBIdentifier{
				TS: time.Now(), From: from, Through: through, Checksum: checksum,
			}
		},
	)
	require.NoError(t, err)

	// --- Encode + Decode the chunk ref lookup table ---
	encoded := encodeChunkRefTable(chunkRefs)
	decoded, err := decodeChunkRefTable(encoded)
	require.NoError(t, err)
	require.Equal(t, chunkRefs, decoded)

	// --- Query: find all (path, section) pairs for {app="gateway"} ---
	reader, err := index.NewReader(index.RealByteSlice(tsdbData))
	require.NoError(t, err)
	defer reader.Close()

	postings, err := reader.Postings("app", nil, "gateway")
	require.NoError(t, err)

	type resolvedChunk struct {
		path      string
		sectionID int
		minTime   time.Time
		maxTime   time.Time
	}
	var results []resolvedChunk

	for postings.Next() {
		var lbls labels.Labels
		var chks []index.ChunkMeta
		_, err := reader.Series(postings.At(), 0, math.MaxInt64, &lbls, &chks)
		require.NoError(t, err)

		t.Logf("Series: %s", lbls)
		for _, chk := range chks {
			ref := decoded[chk.Checksum]
			results = append(results, resolvedChunk{
				path:      ref.Path,
				sectionID: ref.SectionID,
				minTime:   time.UnixMilli(chk.MinTime).UTC(),
				maxTime:   time.UnixMilli(chk.MaxTime).UTC(),
			})
			t.Logf("  -> %s section=%d  (minT=%s, maxT=%s)",
				ref.Path, ref.SectionID,
				time.UnixMilli(chk.MinTime).UTC().Format(time.RFC3339),
				time.UnixMilli(chk.MaxTime).UTC().Format(time.RFC3339),
			)
		}
	}
	require.NoError(t, postings.Err())

	// {app="gateway"} appears in 3 chunks, sorted by (MinTime, MaxTime, Checksum):
	//   obj-001 sec 0 (10:30–12:45), obj-002 sec 0 (12:00–14:15), obj-001 sec 1 (13:00–14:00)
	require.Len(t, results, 3)

	require.Equal(t, "dataobj/obj-001", results[0].path)
	require.Equal(t, 0, results[0].sectionID)
	require.Equal(t, h("2026-01-15T10:30:00Z"), results[0].minTime.UnixMilli())
	require.Equal(t, h("2026-01-15T12:45:00Z"), results[0].maxTime.UnixMilli())

	require.Equal(t, "dataobj/obj-002", results[1].path)
	require.Equal(t, 0, results[1].sectionID)
	require.Equal(t, h("2026-01-15T12:00:00Z"), results[1].minTime.UnixMilli())
	require.Equal(t, h("2026-01-15T14:15:00Z"), results[1].maxTime.UnixMilli())

	require.Equal(t, "dataobj/obj-001", results[2].path)
	require.Equal(t, 1, results[2].sectionID)
	require.Equal(t, h("2026-01-15T13:00:00Z"), results[2].minTime.UnixMilli())
	require.Equal(t, h("2026-01-15T14:00:00Z"), results[2].maxTime.UnixMilli())
}
