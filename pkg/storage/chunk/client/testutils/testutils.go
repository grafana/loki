package testutils

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	chunkclient "github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
)

const (
	userID = "userID"
)

// Fixture type for per-backend testing.
type Fixture interface {
	Name() string
	Clients() (index.Client, chunkclient.Client, index.TableClient, config.SchemaConfig, io.Closer, error)
}

// CloserFunc is to io.Closer as http.HandlerFunc is to http.Handler.
type CloserFunc func() error

// Close implements io.Closer.
func (f CloserFunc) Close() error {
	return f()
}

// DefaultSchemaConfig returns default schema for use in test fixtures
func DefaultSchemaConfig(kind string) config.SchemaConfig {
	return SchemaConfig(kind, "v9", model.Now().Add(-time.Hour*2))
}

// Setup a fixture with initial tables
func Setup(fixture Fixture, tableName string) (index.Client, chunkclient.Client, io.Closer, error) {
	var tbmConfig index.TableManagerConfig
	flagext.DefaultValues(&tbmConfig)
	indexClient, chunkClient, tableClient, schemaConfig, closer, err := fixture.Clients()
	if err != nil {
		return nil, nil, nil, err
	}

	tableManager, err := index.NewTableManager(tbmConfig, schemaConfig, 12*time.Hour, tableClient, nil, nil, nil, log.NewNopLogger())
	if err != nil {
		return nil, nil, nil, err
	}

	err = tableManager.SyncTables(context.Background())
	if err != nil {
		return nil, nil, nil, err
	}

	err = tableClient.CreateTable(context.Background(), config.TableDesc{
		Name: tableName,
	})

	return indexClient, chunkClient, closer, err
}

// CreateChunks creates some chunks for testing
func CreateChunks(scfg config.SchemaConfig, startIndex, batchSize int, from model.Time, through model.Time) ([]string, []chunk.Chunk, error) {
	keys := []string{}
	chunks := []chunk.Chunk{}
	for j := 0; j < batchSize; j++ {
		chunk := DummyChunkFor(from, through, labels.Labels{
			{Name: model.MetricNameLabel, Value: "foo"},
			{Name: "index", Value: strconv.Itoa(startIndex*batchSize + j)},
		})
		chunks = append(chunks, chunk)
		keys = append(keys, scfg.ExternalKey(chunk.ChunkRef))
	}
	return keys, chunks, nil
}

func DummyChunkFor(from, through model.Time, metric labels.Labels) chunk.Chunk {
	cs := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, compression.GZIP, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, 256*1024, 0)

	for ts := from; ts <= through; ts = ts.Add(15 * time.Second) {
		_, err := cs.Append(&logproto.Entry{Timestamp: ts.Time(), Line: fmt.Sprintf("line ts=%d", ts)})
		if err != nil {
			panic(err)
		}
	}

	chunk := chunk.NewChunk(
		userID,
		client.Fingerprint(metric),
		metric,
		chunkenc.NewFacade(cs, 0, 0),
		from,
		through,
	)
	// Force checksum calculation.
	err := chunk.Encode()
	if err != nil {
		panic(err)
	}
	return chunk
}

func SchemaConfig(store, schema string, from model.Time) config.SchemaConfig {
	s := config.SchemaConfig{
		Configs: []config.PeriodConfig{{
			IndexType: store,
			Schema:    schema,
			From:      config.DayTime{Time: from},
			ChunkTables: config.PeriodicTableConfig{
				Prefix: "cortex",
				Period: 7 * 24 * time.Hour,
			},
			IndexTables: config.IndexPeriodicTableConfig{
				PeriodicTableConfig: config.PeriodicTableConfig{
					Prefix: "cortex_chunks",
					Period: 7 * 24 * time.Hour,
				}},
		}},
	}
	if err := s.Validate(); err != nil {
		panic(err)
	}
	return s
}
