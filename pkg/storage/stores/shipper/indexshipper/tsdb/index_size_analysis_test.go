package tsdb

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// genSeries builds nSeries realistic Loki streams with chunksPer chunks each.
// Label cardinality is modeled on a k8s logging deployment: a few low-card
// dimensions (cluster/namespace/level/stream) and one high-card dimension (pod).
func genSeries(nSeries, chunksPer int) []LoadableSeries {
	out := make([]LoadableSeries, 0, nSeries)
	base := model.Time(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli())
	for i := range nSeries {
		lb := labels.NewScratchBuilder(8)
		lb.Add("cluster", fmt.Sprintf("cluster-%d", i%3))
		lb.Add("namespace", fmt.Sprintf("ns-%d", i%40))
		lb.Add("service_name", fmt.Sprintf("svc-%d", i%200))
		lb.Add("container", fmt.Sprintf("container-%d", i%4))
		lb.Add("level", []string{"info", "warn", "error", "debug"}[i%4])
		lb.Add("stream", []string{"stdout", "stderr"}[i%2])
		// high-cardinality dimension: pod, ~unique per series
		lb.Add("pod", fmt.Sprintf("svc-%d-%x-%05d", i%200, i*2654435761&0xffffff, i))
		lb.Sort()
		ls := lb.Labels()

		chks := make(index.ChunkMetas, 0, chunksPer)
		t := base
		for c := range chunksPer {
			mint := t
			maxt := t + model.Time(2*time.Hour.Milliseconds())
			chks = append(chks, index.ChunkMeta{
				Checksum: uint32(i*31 + c),
				MinTime:  int64(mint),
				MaxTime:  int64(maxt),
				KB:       uint32(1024 + c*7),
				Entries:  uint32(5000 + c*13),
			})
			t = maxt
		}
		out = append(out, LoadableSeries{Labels: ls, Chunks: chks})
	}
	return out
}

type sect struct {
	name  string
	bytes uint64
}

func TestIndexSectionSizes(t *testing.T) {
	const tocLen = 76 // 7 section offsets + From + Through (9*8) + CRC32 (4)

	type shape struct {
		series    int
		chunksPer int
	}
	shapes := []shape{
		{100_000, 12},
		{500_000, 12},
		{100_000, 60},
	}

	for _, sh := range shapes {
		t.Run(fmt.Sprintf("series=%d_chunks=%d", sh.series, sh.chunksPer), func(t *testing.T) {
			b := NewBuilder(index.FormatV3)
			for _, s := range genSeries(sh.series, sh.chunksPer) {
				b.AddSeries(s.Labels, model.Fingerprint(labels.StableHash(s.Labels)), s.Chunks)
			}

			_, data, err := b.BuildInMemory(context.Background(), func(from, through model.Time, checksum uint32) Identifier {
				return SingleTenantTSDBIdentifier{TS: time.Unix(0, 0), From: from, Through: through, Checksum: checksum}
			})
			require.NoError(t, err)

			toc, err := index.NewTOCFromByteSlice(index.RealByteSlice(data))
			require.NoError(t, err)

			total := uint64(len(data))
			sectEnd := total - tocLen // sections end where the fixed-size TOC begins

			// The TOC struct field order is NOT the on-disk order, and sections
			// can be padded. Recover each section's size by sorting the offsets
			// and diffing consecutive ones — robust to any write order.
			type off struct {
				name string
				at   uint64
			}
			offs := []off{
				{"Symbols", toc.Symbols},
				{"Series", toc.Series},
				{"LabelIndices", toc.LabelIndices},
				{"LabelIndicesTable", toc.LabelIndicesTable},
				{"Postings", toc.Postings},
				{"PostingsTable", toc.PostingsTable},
				{"FingerprintOffsets", toc.FingerprintOffsets},
			}
			sort.Slice(offs, func(i, j int) bool { return offs[i].at < offs[j].at })

			sects := []sect{{"Header", offs[0].at}} // bytes before the first section
			for i, o := range offs {
				end := sectEnd
				if i+1 < len(offs) {
					end = offs[i+1].at
				}
				sects = append(sects, sect{o.name, end - o.at})
			}
			sects = append(sects, sect{"TOC", tocLen})

			fmt.Printf("\n=== series=%d chunks/series=%d  total=%.2f MB  (%.1f bytes/series) ===\n",
				sh.series, sh.chunksPer, float64(total)/(1<<20), float64(total)/float64(sh.series))
			fmt.Printf("%-20s %14s %8s %14s\n", "section", "bytes", "pct", "bytes/series")
			for _, s := range sects {
				fmt.Printf("%-20s %14d %7.2f%% %14.1f\n",
					s.name, s.bytes, 100*float64(s.bytes)/float64(total), float64(s.bytes)/float64(sh.series))
			}
		})
	}
}
