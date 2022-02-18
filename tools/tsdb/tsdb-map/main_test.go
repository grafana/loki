package main

import (
	"fmt"
	"math/rand"
	"os"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/tsdb"
	"github.com/grafana/loki/pkg/storage/tsdb/index"
)

func TestExtractChecksum(t *testing.T) {
	x := rand.Uint32()
	s := fmt.Sprintf("a/b/c:d:e:%x", x)
	require.Equal(t, x, extractChecksumFromChunkID([]byte(s)))
}

// Requires LOKI_TSDB_PATH to be set or this will short-circuit
func BenchmarkQuery(b *testing.B) {
	for _, bm := range []struct {
		name     string
		matchers []*labels.Matcher
	}{
		{
			name:     "match ns",
			matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "namespace", "loki-ops")},
		},
		{
			name:     "match ns regexp",
			matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "namespace", "loki-ops")},
		},
	} {
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
				p, _ := tsdb.PostingsForMatchers(reader, bm.matchers...)

				for p.Next() {
				}
			}
		})
	}
}
