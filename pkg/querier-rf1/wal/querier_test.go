package wal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/grafana/dskit/user"
	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/storage/wal"
)

// InMemoryStorage implements BlockStorage interface
type InMemoryStorage struct {
	data map[string][]byte
}

func NewInMemoryStorage() *InMemoryStorage {
	return &InMemoryStorage{
		data: make(map[string][]byte),
	}
}

func (s *InMemoryStorage) GetObjectRange(ctx context.Context, objectKey string, off, length int64) (io.ReadCloser, error) {
	data, ok := s.data[objectKey]
	if !ok {
		return nil, fmt.Errorf("object not found: %s", objectKey)
	}
	if off < 0 || length < 0 || off+length > int64(len(data)) {
		return nil, fmt.Errorf("invalid range: off=%d, length=%d, dataLen=%d", off, length, len(data))
	}
	return io.NopCloser(bytes.NewReader(data[off : off+length])), nil
}

func (s *InMemoryStorage) PutObject(objectKey string, data []byte) {
	s.data[objectKey] = data
}

// InMemoryMetastore implements Metastore interface
type InMemoryMetastore struct {
	blocks map[string][]*metastorepb.BlockMeta
}

func NewInMemoryMetastore() *InMemoryMetastore {
	return &InMemoryMetastore{
		blocks: make(map[string][]*metastorepb.BlockMeta),
	}
}

func (m *InMemoryMetastore) ListBlocksForQuery(ctx context.Context, in *metastorepb.ListBlocksForQueryRequest, opts ...grpc.CallOption) (*metastorepb.ListBlocksForQueryResponse, error) {
	blocks := m.blocks[in.TenantId]
	var result []*metastorepb.BlockMeta
	for _, block := range blocks {
		if block.MinTime <= in.EndTime && block.MaxTime >= in.StartTime {
			result = append(result, block)
		}
	}
	return &metastorepb.ListBlocksForQueryResponse{Blocks: result}, nil
}

func (m *InMemoryMetastore) AddBlock(tenantID string, block *metastorepb.BlockMeta) {
	m.blocks[tenantID] = append(m.blocks[tenantID], block)
}

func TestQuerier_Integration(t *testing.T) {
	storage := NewInMemoryStorage()
	metastore := NewInMemoryMetastore()

	querier, err := New(metastore, storage)
	require.NoError(t, err)

	tenantID := "test-tenant"
	ctx := user.InjectOrgID(context.Background(), tenantID)

	testData := generateTestData()
	setupTestData(t, storage, metastore, tenantID, testData)

	t.Run("SelectLogs", func(t *testing.T) {
		testSelectLogs(t, ctx, querier)
	})

	t.Run("SelectLogs_FilterByApp", func(t *testing.T) {
		testSelectLogsFilterByApp(t, ctx, querier)
	})

	t.Run("SelectSamples", func(t *testing.T) {
		testSelectSamples(t, ctx, querier)
	})
}

func generateTestData() []struct {
	labels  labels.Labels
	entries []*logproto.Entry
} {
	return []struct {
		labels  labels.Labels
		entries []*logproto.Entry
	}{
		{
			labels:  labels.FromStrings("app", "test1", "env", "prod"),
			entries: generateEntries(1000, 1200),
		},
		{
			labels:  labels.FromStrings("app", "test2", "env", "dev"),
			entries: generateEntries(1100, 1300),
		},
		{
			labels:  labels.FromStrings("app", "test3", "env", "staging"),
			entries: generateEntries(1200, 1400),
		},
	}
}

func setupTestData(t *testing.T, storage *InMemoryStorage, metastore *InMemoryMetastore, tenantID string, testData []struct {
	labels  labels.Labels
	entries []*logproto.Entry
},
) {
	for i, data := range testData {
		chunkSize := 50 // This will create multiple chunks per set of test data
		for j := 0; j < len(data.entries); j += chunkSize {
			end := j + chunkSize
			if end > len(data.entries) {
				end = len(data.entries)
			}

			segmentID := fmt.Sprintf("segment%d_%d", i, j/chunkSize)
			writer, err := wal.NewWalSegmentWriter()
			require.NoError(t, err)

			writer.Append(tenantID, data.labels.String(), data.labels, data.entries[j:end], time.Now())

			var buf bytes.Buffer
			_, err = writer.WriteTo(&buf)
			require.NoError(t, err)

			segmentData := buf.Bytes()
			storage.PutObject(wal.Dir+segmentID, segmentData)

			blockMeta := writer.Meta(segmentID)
			metastore.AddBlock(tenantID, blockMeta)
		}
	}
}

func testSelectLogs(t *testing.T, ctx context.Context, querier *Querier) {
	query := `{app=~"test.*"}`
	expr, err := syntax.ParseExpr(query)
	require.NoError(t, err)
	req := logql.SelectLogParams{
		QueryRequest: &logproto.QueryRequest{
			Selector:  query,
			Start:     time.Unix(1000, 0),
			End:       time.Unix(1400, 0),
			Limit:     1000,
			Direction: logproto.FORWARD,
			Plan: &plan.QueryPlan{
				AST: expr,
			},
		},
	}

	iter, err := querier.SelectLogs(ctx, req)
	require.NoError(t, err)

	results := collectLogEntries(t, iter)

	assert.Len(t, results, 601, "Expected 601 log entries")
	validateLogEntries(t, results)
}

func testSelectLogsFilterByApp(t *testing.T, ctx context.Context, querier *Querier) {
	query := `{app="test2"}`
	expr, err := syntax.ParseExpr(query)
	require.NoError(t, err)
	req := logql.SelectLogParams{
		QueryRequest: &logproto.QueryRequest{
			Selector:  query,
			Start:     time.Unix(1000, 0),
			End:       time.Unix(1400, 0),
			Limit:     1000,
			Direction: logproto.FORWARD,
			Plan: &plan.QueryPlan{
				AST: expr,
			},
		},
	}

	iter, err := querier.SelectLogs(ctx, req)
	require.NoError(t, err)

	results := collectLogEntries(t, iter)

	assert.Len(t, results, 201, "Expected 201 log entries for test2")
	for _, entry := range results {
		assert.Contains(t, entry.Line, "line", "Log line should contain 'line'")
		assert.True(t, entry.Timestamp.Unix() >= 1100 && entry.Timestamp.Unix() <= 1300, "Timestamp should be between 1100 and 1300")
	}
}

func testSelectSamples(t *testing.T, ctx context.Context, querier *Querier) {
	query := `rate({app=~"test.*"}[5m])`
	expr, err := syntax.ParseExpr(query)
	require.NoError(t, err)

	req := logql.SelectSampleParams{
		SampleQueryRequest: &logproto.SampleQueryRequest{
			Selector: query,
			Start:    time.Unix(1000, 0),
			End:      time.Unix(1400, 0),
			Plan: &plan.QueryPlan{
				AST: expr,
			},
		},
	}

	iter, err := querier.SelectSamples(ctx, req)
	require.NoError(t, err)

	results := collectSamples(t, iter)

	assert.NotEmpty(t, results, "Should have sample results")
	for _, sample := range results {
		assert.True(t, sample.Value > 0 && sample.Value <= 0.2, "Rate should be between 0 and 0.2 logs/second")
	}
}

func collectLogEntries(t *testing.T, iter iter.EntryIterator) []logproto.Entry {
	var results []logproto.Entry
	for iter.Next() {
		entry := iter.At()
		results = append(results, logproto.Entry{
			Timestamp: entry.Timestamp,
			Line:      entry.Line,
		})
	}
	require.NoError(t, iter.Close())
	return results
}

func collectSamples(t *testing.T, iter iter.SampleIterator) []logproto.Sample {
	var results []logproto.Sample
	for iter.Next() {
		results = append(results, iter.At())
	}
	require.NoError(t, iter.Close())
	return results
}

func validateLogEntries(t *testing.T, entries []logproto.Entry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.Before(entries[j].Timestamp)
	})

	var prevTimestamp time.Time
	for _, entry := range entries {
		assert.True(t, entry.Timestamp.After(prevTimestamp) || entry.Timestamp.Equal(prevTimestamp),
			"Timestamps should be in non-decreasing order")
		prevTimestamp = entry.Timestamp

		assert.Contains(t, entry.Line, "line", "Log line should contain 'line'")
		assert.True(t, entry.Timestamp.Unix() >= 1000 && entry.Timestamp.Unix() <= 1400, "Timestamp should be between 1000 and 1400")
	}
}
