package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
)

var (
	storeV4 = getLocalStoreWithFormat("v13", "/tmp/benchmarkv4", cm)
	storeV5 = getLocalStoreWithFormat("v14", "/tmp/benchmarkv5", cm)
)

func benchmarkSelectSamples(b *testing.B, version string) {
	for _, q := range []string{
		`count_over_time({foo="bar"}[5m])`,
		`rate({foo="bar"}[5m])`,
		`bytes_rate({foo="bar"}[5m])`,
		`bytes_over_time({foo="bar"}[5m])`,
	} {
		b.Run(fmt.Sprintf("ChunkFormat_%s_SelectSamples", version), func(b *testing.B) {
			b.ReportAllocs()
			sampleCount := 0
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				statsCtx, ctx := stats.NewContext(user.InjectOrgID(context.Background(), "fake"))

				params := logql.SelectSampleParams{
					SampleQueryRequest: newSampleQuery(q, time.Unix(0, start.UnixNano()), time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano()), nil, nil),
					EvaluatorMode:      logql.ModeMetricsOnly,
				}
				st := storeFor(version)
				it, err := st.SelectSamples(ctx, params)
				if err != nil {
					b.Fatal(err)
				}

				for it.Next() {
					_ = it.At()
					sampleCount++
				}
				if err := it.Close(); err != nil {
					b.Fatal(err)
				}

				// Report stats for each iteration
				r := statsCtx.Result(time.Since(start.Time()), 0, 0)
				b.ReportMetric(float64(r.TotalChunksRef()), "chunks")
				b.ReportMetric(float64(r.TotalDecompressedBytes()), "bytes_decompressed")
				b.ReportMetric(float64(r.TotalDecompressedLines()), "lines_decompressed")
				b.ReportMetric(float64(r.TotalDecompressedStructuredMetadataBytes()), "structured_metadata_bytes")
				b.ReportMetric(float64(sampleCount)/float64(b.N), "samples/op")
				//b.ReportMetric(float64(r.TotalBytesProcessed()), "bytes_processed")
			}
		})
	}
}

func Benchmark_ChunkFormats_SelectSamples_V4(b *testing.B) {
	benchmarkSelectSamples(b, "v4")
}

func Benchmark_ChunkFormats_SelectSamples_V5(b *testing.B) {
	benchmarkSelectSamples(b, "v5")
}

func benchmarkWithStructuredMetadata(b *testing.B, version string) {
	b.Run(fmt.Sprintf("ChunkFormat_%s_SelectSamples_StructuredMetadata", version), func(b *testing.B) {
		b.ReportAllocs()
		st := storeFor(version)

		b.ResetTimer()
		sampleCount := 0
		for i := 0; i < b.N; i++ {
			statsCtx, ctx := stats.NewContext(user.InjectOrgID(context.Background(), "fake"))

			q := `sum by (detected_level) (count_over_time({foo="bar"} [1m]))`
			params := logql.SelectSampleParams{
				SampleQueryRequest: newSampleQuery(q, time.Unix(0, start.UnixNano()), time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano()), nil, nil),
				EvaluatorMode:      logql.ModeMetricsOnly,
			}

			it, err := st.SelectSamples(ctx, params)
			if err != nil {
				b.Fatal(err)
			}

			//var sample logproto.Sample
			for it.Next() {
				_ = it.At()
				sampleCount++
			}
			if err := it.Close(); err != nil {
				b.Fatal(err)
			}

			// Report detailed stats including structured metadata handling
			r := statsCtx.Result(time.Since(start.Time()), 0, 0)

			b.ReportMetric(float64(r.TotalChunksRef()), "chunks")
			b.ReportMetric(float64(r.TotalDecompressedBytes()), "bytes_decompressed")
			b.ReportMetric(float64(r.TotalDecompressedLines()), "lines_decompressed")
			b.ReportMetric(float64(r.TotalDecompressedStructuredMetadataBytes()), "structured_metadata_bytes")
			b.ReportMetric(float64(sampleCount)/float64(b.N), "samples/op")
		}
	})
}

// Benchmark for sample queries with structured metadata
func Benchmark_ChunkFormats_SelectSamples_WithStructuredMetadata_V4(b *testing.B) {
	benchmarkWithStructuredMetadata(b, "v4")
}

func Benchmark_ChunkFormats_SelectSamples_WithStructuredMetadata_V5(b *testing.B) {
	benchmarkWithStructuredMetadata(b, "v5")
}

func getLocalStoreWithFormat(schema string, path string, cm ClientMetrics) Store {
	limits, err := validation.NewOverrides(validation.Limits{
		MaxQueryLength: model.Duration(6000 * time.Hour),
	}, nil)
	if err != nil {
		panic(err)
	}

	storeConfig := Config{
		BoltDBConfig:      local.BoltDBConfig{Directory: filepath.Join(path, "index")},
		FSConfig:          local.FSConfig{Directory: filepath.Join(path, "chunks")},
		MaxChunkBatchSize: 10,
	}

	schemaConfig := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       config.DayTime{Time: start},
				IndexType:  "boltdb",
				ObjectType: types.StorageTypeFileSystem,
				Schema:     schema,
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 168,
					}},
				RowShards: 16,
			},
		},
	}

	store, err := NewStore(storeConfig, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, log.NewNopLogger(), constants.Loki)
	if err != nil {
		panic(err)
	}
	return store
}

func storeFor(version string) Store {
	switch version {
	case "v4":
		return storeV4
	case "v5":
		return storeV5
	default:
		return nil
	}

}
