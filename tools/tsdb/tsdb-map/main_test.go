package main

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func TestExtractChecksum(t *testing.T) {
	x := rand.Uint32()
	s := fmt.Sprintf("a/b/c:d:e:%x", x)
	require.Equal(t, x, extractChecksumFromChunkID([]byte(s)))
}

type testCase struct {
	name     string
	matchers []*labels.Matcher
}

var cases = []testCase{
	{
		name:     "match ns",
		matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "namespace", "loki-ops")},
	},
	{
		name:     "match job regexp",
		matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "job", "loki.*/distributor")},
	},
	{
		name: "match regexp and equals",
		matchers: []*labels.Matcher{
			labels.MustNewMatcher(labels.MatchRegexp, "job", "loki.*/distributor"),
			labels.MustNewMatcher(labels.MatchEqual, "cluster", "prod-us-central-0"),
		},
	},
	{
		name: "not equals cluster",
		matchers: []*labels.Matcher{
			labels.MustNewMatcher(labels.MatchNotEqual, "cluster", "prod-us-central-0"),
		},
	},
	{
		name: "inverse regexp",
		matchers: []*labels.Matcher{
			labels.MustNewMatcher(labels.MatchNotRegexp, "cluster", "prod.*"),
		},
	},
}

// Only iterates through the low level IndexReader using `PostingsForMatchers`
// Requires LOKI_TSDB_PATH to be set or this will short-circuit
func BenchmarkQuery_PostingsForMatchers(b *testing.B) {
	for _, bm := range cases {
		indexPath := os.Getenv("LOKI_TSDB_PATH")
		if indexPath == "" {
			return
		}

		reader, err := index.NewFileReader(indexPath)
		if err != nil {
			panic(err)
		}
		b.Run(bm.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				p, _ := tsdb.PostingsForMatchers(reader, nil, bm.matchers...)

				//nolint:revive
				for p.Next() {
				}
			}
		})
	}
}

// Uses the higher level loki index interface, resolving chunk refs.
// Requires LOKI_TSDB_PATH to be set or this will short-circuit
func BenchmarkQuery_GetChunkRefs(b *testing.B) {
	for _, bm := range cases {
		indexPath := os.Getenv("LOKI_TSDB_PATH")
		if indexPath == "" {
			return
		}

		reader, err := index.NewFileReader(indexPath)
		if err != nil {
			panic(err)
		}
		idx := tsdb.NewTSDBIndex(reader)
		b.Run(bm.name, func(b *testing.B) {
			refs := tsdb.ChunkRefsPool.Get()
			for i := 0; i < b.N; i++ {
				var err error
				refs, err = idx.GetChunkRefs(context.Background(), "fake", 0, math.MaxInt64, refs, nil, bm.matchers...)
				if err != nil {
					panic(err)
				}
			}
		})
	}
}

func BenchmarkQuery_GetChunkRefsSharded(b *testing.B) {
	for _, bm := range cases {
		indexPath := os.Getenv("LOKI_TSDB_PATH")
		if indexPath == "" {
			return
		}

		reader, err := index.NewFileReader(indexPath)
		if err != nil {
			panic(err)
		}
		idx := tsdb.NewTSDBIndex(reader)
		shardFactor := 16

		b.Run(bm.name, func(b *testing.B) {
			refs := tsdb.ChunkRefsPool.Get()
			for i := 0; i < b.N; i++ {
				for j := 0; j < shardFactor; j++ {
					shard := index.ShardAnnotation{
						Shard: uint32(j),
						Of:    uint32(shardFactor),
					}
					var err error

					refs, err = idx.GetChunkRefs(context.Background(), "fake", 0, math.MaxInt64, refs, &shard, bm.matchers...)
					if err != nil {
						panic(err)
					}
				}
			}
		})
	}
}
