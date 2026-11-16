package tsdb

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// zipfDistribution provides random numbers between 1 and n in a zipf distribution.
type zipfDistribution struct {
	random  *rand.Rand
	weights []float64
}

func newZipfDistribution(n int) *zipfDistribution {
	weights := make([]float64, n)
	sum := 0.0
	for i := range weights {
		sum += 1 / math.Pow(float64(i+1), 0.8)
		weights[i] = sum
	}
	return &zipfDistribution{
		random:  rand.New(rand.NewSource(0)),
		weights: weights,
	}
}

func (z *zipfDistribution) sample() int {
	return sort.SearchFloat64s(z.weights, z.random.Float64()*z.weights[len(z.weights)-1])
}

// buildIndex writes an index where each series has the labels:
//   - cluster: cluster-00 -> cluster-39, zipf distributed.
//   - namespace: namespace-a000 -> namespace-h599, zipf distributed.
//   - pod: pod-000000 -> pod-249999, unique per series.
func buildIndex(b *testing.B) *TSDBIndex {
	b.Helper()
	const (
		numClusters   = 40
		numNamespaces = 600
		numSeries     = 250000
	)

	clusters := newZipfDistribution(numClusters)
	namespaces := newZipfDistribution(numNamespaces)

	builder := NewBuilder(index.FormatV3)
	chunks := []index.ChunkMeta{{MinTime: 0, MaxTime: 1000, Checksum: 1, KB: 1, Entries: 1}}
	for i := range numSeries {
		ns := namespaces.sample()
		ls := labels.FromStrings(
			"cluster", fmt.Sprintf("cluster-%02d", clusters.sample()),
			"namespace", fmt.Sprintf("namespace-%c%03d", 'a'+ns%8, ns),
			"pod", fmt.Sprintf("pod-%06d", i),
		)
		builder.AddSeries(ls, model.Fingerprint(labels.StableHash(ls)), chunks)
	}

	dir := b.TempDir()
	id, err := builder.Build(context.Background(), dir, func(from, through model.Time, checksum uint32) Identifier {
		return NewPrefixedIdentifier(SingleTenantTSDBIdentifier{
			TS:       time.Now(),
			From:     from,
			Through:  through,
			Checksum: checksum,
		}, dir, dir)
	})
	require.NoError(b, err)

	idx, _, err := NewTSDBIndexFromFile(id.Path(), index.MmapOptions{})
	require.NoError(b, err)
	b.Cleanup(func() { _ = idx.Close() })
	return idx
}

// BenchmarkTSDBIndex_forPostings aims to measure forPostings
// against somewhat realistic indexes and matchers.
func BenchmarkTSDBIndex_forPostings(b *testing.B) {
	const shardFactor = 256

	idx := buildIndex(b)

	cases := []struct {
		name     string
		matchers []*labels.Matcher
	}{
		{
			name: "broad_by_cluster",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "cluster", "cluster-.*"), // Matches all clusters
			},
		},
		{
			name: "broad_by_cluster_and_namespace",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "cluster", "cluster-.*"),          // Matches all clusters
				labels.MustNewMatcher(labels.MatchRegexp, "namespace", "namespace-(a|b).*"), // Matches 1/4 namespaces
			},
		},
		{
			name: "broad_by_cluster_and_namespace_not_equal",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "cluster", "cluster-.*"), // Matches all clusters
				labels.MustNewMatcher(labels.MatchNotEqual, "namespace", ""),       // Matches all namespaces
			},
		},
		{
			name: "broad_by_cluster_narrow_by_namespace",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "cluster", "cluster-.*"),       // Matches all clusters
				labels.MustNewMatcher(labels.MatchRegexp, "namespace", "namespace-a000"), // Matches a single namespace
			},
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				n := 0
				for i := range shardFactor {
					shard := index.ShardAnnotation{Shard: uint32(i), Of: shardFactor}
					err := idx.forPostings(context.Background(), shard, 0, 0, tc.matchers, func(p index.Postings) error {
						for p.Next() {
							n++
						}
						return p.Err()
					})
					if err != nil {
						b.Fatal(err)
					}
				}
				if n == 0 {
					b.Fatal("no series matched")
				}
			}
		})
	}
}
