package thortsdbexample

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

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
			KB:      10,
		},
		{
			labels:  labels.FromStrings("cluster", "us-central", "app", "gateway"),
			path:    "dataobj/obj-001",
			section: 1, // same object, different section
			minTime: h("2026-01-15T13:00:00Z"),
			maxTime: h("2026-01-15T14:00:00Z"),
			rows:    150,
			KB:      15,
		},
		{
			labels:  labels.FromStrings("cluster", "us-central", "app", "gateway"),
			path:    "dataobj/obj-002",
			section: 0,
			minTime: h("2026-01-15T12:00:00Z"),
			maxTime: h("2026-01-15T14:15:00Z"),
			rows:    200,
			KB:      20,
		},
		{
			labels:  labels.FromStrings("cluster", "eu-west", "app", "ingester"),
			path:    "dataobj/obj-003",
			section: 0,
			minTime: h("2026-01-15T11:00:00Z"),
			maxTime: h("2026-01-15T11:30:00Z"),
			rows:    50,
			KB:      5,
		},
	}

	// --- Build ---
	builder := tsdb.NewBuilder(index.FormatV3)

	type refKey struct {
		path    string
		section int
	}
	refIndex := make(map[refKey]uint32)
	var chunkRefs []sectionRef

	for _, ref := range refs {
		key := refKey{path: ref.path, section: ref.section}
		idx, ok := refIndex[key]
		if !ok {
			idx = uint32(len(chunkRefs))
			chunkRefs = append(chunkRefs, sectionRef{Path: ref.path, SectionID: ref.section})
			refIndex[key] = idx
		}

		fp := model.Fingerprint(ref.labels.Hash())
		builder.AddSeries(ref.labels, fp, []index.ChunkMeta{
			{
				Checksum: idx,
				MinTime:  ref.minTime,
				MaxTime:  ref.maxTime,
				Entries:  uint32(ref.rows),
				KB:       uint32(ref.KB),
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
	encoded := encodeSectionRefTable(chunkRefs)
	decoded, err := decodeSectionRefTable(encoded)
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
