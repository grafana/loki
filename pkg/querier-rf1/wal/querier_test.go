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
	"github.com/grafana/loki/v3/pkg/storage/wal/chunks"
)

// MockStorage is a simple in-memory storage for testing
type MockStorage struct {
	data map[string][]byte
}

func NewMockStorage() *MockStorage {
	return &MockStorage{data: make(map[string][]byte)}
}

func (m *MockStorage) GetObjectRange(_ context.Context, objectKey string, off, length int64) (io.ReadCloser, error) {
	data, ok := m.data[objectKey]
	if !ok {
		return nil, fmt.Errorf("object not found: %s", objectKey)
	}
	return io.NopCloser(bytes.NewReader(data[off : off+length])), nil
}

func (m *MockStorage) PutObject(objectKey string, data []byte) {
	m.data[objectKey] = data
}

// MockMetastore is a simple in-memory metastore for testing
type MockMetastore struct {
	blocks map[string][]*metastorepb.BlockMeta
}

func NewMockMetastore() *MockMetastore {
	return &MockMetastore{blocks: make(map[string][]*metastorepb.BlockMeta)}
}

func (m *MockMetastore) ListBlocksForQuery(_ context.Context, req *metastorepb.ListBlocksForQueryRequest, _ ...grpc.CallOption) (*metastorepb.ListBlocksForQueryResponse, error) {
	blocks := m.blocks[req.TenantId]
	var result []*metastorepb.BlockMeta
	for _, block := range blocks {
		if block.MinTime <= req.EndTime && block.MaxTime >= req.StartTime {
			result = append(result, block)
		}
	}
	return &metastorepb.ListBlocksForQueryResponse{Blocks: result}, nil
}

func (m *MockMetastore) AddBlock(tenantID string, block *metastorepb.BlockMeta) {
	m.blocks[tenantID] = append(m.blocks[tenantID], block)
}

func TestQuerier_SelectLogs(t *testing.T) {
	storage := NewMockStorage()
	metastore := NewMockMetastore()

	querier, err := New(metastore, storage)
	require.NoError(t, err)

	tenantID := "test-tenant"
	ctx := user.InjectOrgID(context.Background(), tenantID)

	// Create expanded test data
	testData := []struct {
		labels  labels.Labels
		entries []*logproto.Entry
	}{
		{
			labels:  labels.FromStrings("app", "test1", "env", "prod"),
			entries: generateEntries(1000, 1050),
		},
		{
			labels:  labels.FromStrings("app", "test2", "env", "staging"),
			entries: generateEntries(1025, 1075),
		},
		{
			labels:  labels.FromStrings("app", "test3", "env", "dev", "version", "v1"),
			entries: generateEntries(1050, 1100),
		},
		{
			labels:  labels.FromStrings("app", "test4", "env", "prod", "version", "v2"),
			entries: generateEntries(1075, 1125),
		},
	}

	// Setup test data
	setupTestData(t, storage, metastore, tenantID, testData)

	// Test cases
	testCases := []struct {
		name          string
		query         string
		expectedCount int
		expectedFirst logproto.Entry
		expectedLast  logproto.Entry
	}{
		{
			name:          "Query all logs",
			query:         `{app=~"test.*"}`,
			expectedCount: 204,
			expectedFirst: logproto.Entry{
				Timestamp: time.Unix(1000, 0),
				Line:      "line1000",
				Parsed: []logproto.LabelAdapter{
					{Name: "app", Value: "test1"},
					{Name: "env", Value: "prod"},
				},
			},
			expectedLast: logproto.Entry{
				Timestamp: time.Unix(1125, 0),
				Line:      "line1125",
				Parsed: []logproto.LabelAdapter{
					{Name: "app", Value: "test4"},
					{Name: "env", Value: "prod"},
					{Name: "version", Value: "v2"},
				},
			},
		},
		{
			name:          "Query specific app",
			query:         `{app="test1"}`,
			expectedCount: 51,
			expectedFirst: logproto.Entry{
				Timestamp: time.Unix(1000, 0),
				Line:      "line1000",
				Parsed: []logproto.LabelAdapter{
					{Name: "app", Value: "test1"},
					{Name: "env", Value: "prod"},
				},
			},
			expectedLast: logproto.Entry{
				Timestamp: time.Unix(1050, 0),
				Line:      "line1050",
				Parsed: []logproto.LabelAdapter{
					{Name: "app", Value: "test1"},
					{Name: "env", Value: "prod"},
				},
			},
		},
		{
			name:          "Query with multiple label equality",
			query:         `{app="test4", env="prod"}`,
			expectedCount: 51,
			expectedFirst: logproto.Entry{
				Timestamp: time.Unix(1075, 0),
				Line:      "line1075",
				Parsed: []logproto.LabelAdapter{
					{Name: "app", Value: "test4"},
					{Name: "env", Value: "prod"},
					{Name: "version", Value: "v2"},
				},
			},
			expectedLast: logproto.Entry{
				Timestamp: time.Unix(1125, 0),
				Line:      "line1125",
				Parsed: []logproto.LabelAdapter{
					{Name: "app", Value: "test4"},
					{Name: "env", Value: "prod"},
					{Name: "version", Value: "v2"},
				},
			},
		},
		{
			name:          "Query with negative regex",
			query:         `{app=~"test.*", env!~"stag.*|dev"}`,
			expectedCount: 102,
			expectedFirst: logproto.Entry{
				Timestamp: time.Unix(1000, 0),
				Line:      "line1000",
				Parsed: []logproto.LabelAdapter{
					{Name: "app", Value: "test1"},
					{Name: "env", Value: "prod"},
				},
			},
			expectedLast: logproto.Entry{
				Timestamp: time.Unix(1125, 0),
				Line:      "line1125",
				Parsed: []logproto.LabelAdapter{
					{Name: "app", Value: "test4"},
					{Name: "env", Value: "prod"},
					{Name: "version", Value: "v2"},
				},
			},
		},
		{
			name:          "Query with label presence",
			query:         `{app=~"test.*", version=""}`,
			expectedCount: 102,
			expectedFirst: logproto.Entry{
				Timestamp: time.Unix(1000, 0),
				Line:      "line1000",
				Parsed: []logproto.LabelAdapter{
					{Name: "app", Value: "test1"},
					{Name: "env", Value: "prod"},
				},
			},
			expectedLast: logproto.Entry{
				Timestamp: time.Unix(1075, 0),
				Line:      "line1075",
				Parsed: []logproto.LabelAdapter{
					{Name: "app", Value: "test2"},
					{Name: "env", Value: "staging"},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := syntax.ParseExpr(tc.query)
			require.NoError(t, err)

			req := logql.SelectLogParams{
				QueryRequest: &logproto.QueryRequest{
					Selector:  tc.query,
					Start:     time.Unix(1000, 0),
					End:       time.Unix(1126, 0),
					Limit:     10000,
					Direction: logproto.FORWARD,
					Plan: &plan.QueryPlan{
						AST: expr,
					},
				},
			}

			iter, err := querier.SelectLogs(ctx, req)
			require.NoError(t, err)

			results := collectPushEntries(t, iter)

			assert.Len(t, results, tc.expectedCount, "Unexpected number of log entries")
			if len(results) > 0 {
				assert.Equal(t, tc.expectedFirst, results[0], "First log entry mismatch")
				assert.Equal(t, tc.expectedLast, results[len(results)-1], "Last log entry mismatch")
			}
		})
	}
}

// SampleWithLabels is a new struct to hold both the sample and its labels
type SampleWithLabels struct {
	Sample logproto.Sample
	Labels labels.Labels
}

func TestQuerier_SelectSamples(t *testing.T) {
	storage := NewMockStorage()
	metastore := NewMockMetastore()

	querier, err := New(metastore, storage)
	require.NoError(t, err)

	tenantID := "test-tenant"
	ctx := user.InjectOrgID(context.Background(), tenantID)

	// Create test data
	testData := []struct {
		labels  labels.Labels
		samples []logproto.Sample
	}{
		{
			labels:  labels.FromStrings("app", "test1", "env", "prod"),
			samples: generateSamples(1000, 1050, 1),
		},
		{
			labels:  labels.FromStrings("app", "test2", "env", "staging"),
			samples: generateSamples(1025, 1075, 2),
		},
		{
			labels:  labels.FromStrings("app", "test3", "env", "dev", "version", "v1"),
			samples: generateSamples(1050, 1100, 3),
		},
		{
			labels:  labels.FromStrings("app", "test4", "env", "prod", "version", "v2"),
			samples: generateSamples(1075, 1125, 4),
		},
	}

	// Setup test data
	setupTestSampleData(t, storage, metastore, tenantID, testData)

	// Test cases
	testCases := []struct {
		name          string
		query         string
		expectedCount int
		expectedFirst SampleWithLabels
		expectedLast  SampleWithLabels
	}{
		{
			name:          "Query all samples",
			query:         `sum_over_time({app=~"test.*"} | label_format v="{{__line__}}" | unwrap v[1s])`,
			expectedCount: 204,
			expectedFirst: SampleWithLabels{
				Sample: logproto.Sample{
					Timestamp: time.Unix(1000, 0).UnixNano(),
					Value:     1,
				},
				Labels: labels.FromStrings("app", "test1", "env", "prod"),
			},
			expectedLast: SampleWithLabels{
				Sample: logproto.Sample{
					Timestamp: time.Unix(1125, 0).UnixNano(),
					Value:     4,
				},
				Labels: labels.FromStrings("app", "test4", "env", "prod", "version", "v2"),
			},
		},
		{
			name:          "Query specific app",
			query:         `sum_over_time({app="test1"}| label_format v="{{__line__}}" | unwrap v[1s])`,
			expectedCount: 51,
			expectedFirst: SampleWithLabels{
				Sample: logproto.Sample{
					Timestamp: time.Unix(1000, 0).UnixNano(),
					Value:     1,
				},
				Labels: labels.FromStrings("app", "test1", "env", "prod"),
			},
			expectedLast: SampleWithLabels{
				Sample: logproto.Sample{
					Timestamp: time.Unix(1050, 0).UnixNano(),
					Value:     1,
				},
				Labels: labels.FromStrings("app", "test1", "env", "prod"),
			},
		},
		{
			name:          "Query with multiple label equality",
			query:         `sum_over_time({app="test4", env="prod"}| label_format v="{{__line__}}" | unwrap v[1s])`,
			expectedCount: 51,
			expectedFirst: SampleWithLabels{
				Sample: logproto.Sample{
					Timestamp: time.Unix(1075, 0).UnixNano(),
					Value:     4,
				},
				Labels: labels.FromStrings("app", "test4", "env", "prod", "version", "v2"),
			},
			expectedLast: SampleWithLabels{
				Sample: logproto.Sample{
					Timestamp: time.Unix(1125, 0).UnixNano(),
					Value:     4,
				},
				Labels: labels.FromStrings("app", "test4", "env", "prod", "version", "v2"),
			},
		},
		{
			name:          "Query with negative regex",
			query:         `sum_over_time({app=~"test.*", env!~"stag.*|dev"}| label_format v="{{__line__}}" | unwrap v[1s])`,
			expectedCount: 102,
			expectedFirst: SampleWithLabels{
				Sample: logproto.Sample{
					Timestamp: time.Unix(1000, 0).UnixNano(),
					Value:     1,
				},
				Labels: labels.FromStrings("app", "test1", "env", "prod"),
			},
			expectedLast: SampleWithLabels{
				Sample: logproto.Sample{
					Timestamp: time.Unix(1125, 0).UnixNano(),
					Value:     4,
				},
				Labels: labels.FromStrings("app", "test4", "env", "prod", "version", "v2"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := syntax.ParseExpr(tc.query)
			require.NoError(t, err)

			req := logql.SelectSampleParams{
				SampleQueryRequest: &logproto.SampleQueryRequest{
					Selector: tc.query,
					Start:    time.Unix(1000, 0),
					End:      time.Unix(1126, 0),
					Plan: &plan.QueryPlan{
						AST: expr,
					},
				},
			}

			iter, err := querier.SelectSamples(ctx, req)
			require.NoError(t, err)

			results := collectSamplesWithLabels(t, iter)

			assert.Len(t, results, tc.expectedCount, "Unexpected number of samples")
			if len(results) > 0 {
				assert.Equal(t, tc.expectedFirst.Sample, results[0].Sample, "First sample mismatch")
				assert.Equal(t, tc.expectedFirst.Labels, results[0].Labels, "First sample labels mismatch")
				assert.Equal(t, tc.expectedLast.Sample, results[len(results)-1].Sample, "Last sample mismatch")
				assert.Equal(t, tc.expectedLast.Labels, results[len(results)-1].Labels, "Last sample labels mismatch")
			}
		})
	}
}

func TestQuerier_matchingChunks(t *testing.T) {
	storage := NewMockStorage()
	metastore := NewMockMetastore()

	querier, err := New(metastore, storage)
	require.NoError(t, err)

	tenantID := "test-tenant"
	ctx := user.InjectOrgID(context.Background(), tenantID)

	// Create test data
	testData := []struct {
		labels  labels.Labels
		entries []*logproto.Entry
	}{
		{
			labels:  labels.FromStrings("app", "app1", "env", "prod"),
			entries: generateEntries(1000, 1050),
		},
		{
			labels:  labels.FromStrings("app", "app2", "env", "staging"),
			entries: generateEntries(1025, 1075),
		},
		{
			labels:  labels.FromStrings("app", "app3", "env", "dev"),
			entries: generateEntries(1050, 1100),
		},
	}

	// Setup test data
	setupTestData(t, storage, metastore, tenantID, testData)

	// Test cases
	testCases := []struct {
		name           string
		matchers       []*labels.Matcher
		start          int64
		end            int64
		expectedChunks []ChunkData
	}{
		{
			name: "Equality matcher",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "app1"),
			},
			start: time.Unix(1000, 0).UnixNano(),
			end:   time.Unix(1100, 0).UnixNano(),
			expectedChunks: []ChunkData{
				{
					labels: labels.FromStrings("app", "app1", "env", "prod"),
					meta:   &chunks.Meta{MinTime: time.Unix(1000, 0).UnixNano(), MaxTime: time.Unix(1050, 0).UnixNano()},
				},
			},
		},
		{
			name: "Negative matcher",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchNotEqual, "app", "app1"),
			},
			start: time.Unix(1000, 0).UnixNano(),
			end:   time.Unix(1100, 0).UnixNano(),
			expectedChunks: []ChunkData{
				{
					labels: labels.FromStrings("app", "app2", "env", "staging"),
					meta:   &chunks.Meta{MinTime: time.Unix(1025, 0).UnixNano(), MaxTime: time.Unix(1075, 0).UnixNano()},
				},
				{
					labels: labels.FromStrings("app", "app3", "env", "dev"),
					meta:   &chunks.Meta{MinTime: time.Unix(1050, 0).UnixNano(), MaxTime: time.Unix(1100, 0).UnixNano()},
				},
			},
		},
		{
			name: "Regex matcher",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "app", "app[12]"),
			},
			start: time.Unix(1000, 0).UnixNano(),
			end:   time.Unix(1100, 0).UnixNano(),
			expectedChunks: []ChunkData{
				{
					labels: labels.FromStrings("app", "app1", "env", "prod"),
					meta:   &chunks.Meta{MinTime: time.Unix(1000, 0).UnixNano(), MaxTime: time.Unix(1050, 0).UnixNano()},
				},
				{
					labels: labels.FromStrings("app", "app2", "env", "staging"),
					meta:   &chunks.Meta{MinTime: time.Unix(1025, 0).UnixNano(), MaxTime: time.Unix(1075, 0).UnixNano()},
				},
			},
		},
		{
			name: "Not regex matcher",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchNotRegexp, "app", "app[12]"),
			},
			start: time.Unix(1000, 0).UnixNano(),
			end:   time.Unix(1100, 0).UnixNano(),
			expectedChunks: []ChunkData{
				{
					labels: labels.FromStrings("app", "app3", "env", "dev"),
					meta:   &chunks.Meta{MinTime: time.Unix(1050, 0).UnixNano(), MaxTime: time.Unix(1100, 0).UnixNano()},
				},
			},
		},
		{
			name: "Multiple matchers",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "app", "app.*"),
				labels.MustNewMatcher(labels.MatchNotEqual, "env", "prod"),
			},
			start: time.Unix(1000, 0).UnixNano(),
			end:   time.Unix(1100, 0).UnixNano(),
			expectedChunks: []ChunkData{
				{
					labels: labels.FromStrings("app", "app2", "env", "staging"),
					meta:   &chunks.Meta{MinTime: time.Unix(1025, 0).UnixNano(), MaxTime: time.Unix(1075, 0).UnixNano()},
				},
				{
					labels: labels.FromStrings("app", "app3", "env", "dev"),
					meta:   &chunks.Meta{MinTime: time.Unix(1050, 0).UnixNano(), MaxTime: time.Unix(1100, 0).UnixNano()},
				},
			},
		},
		{
			name: "Time range filter",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "app", "app.*"),
			},
			start: time.Unix(1080, 0).UnixNano(),
			end:   time.Unix(1100, 0).UnixNano(),
			expectedChunks: []ChunkData{
				{
					labels: labels.FromStrings("app", "app3", "env", "dev"),
					meta:   &chunks.Meta{MinTime: time.Unix(1050, 0).UnixNano(), MaxTime: time.Unix(1100, 0).UnixNano()},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			chunks, err := querier.matchingChunks(ctx, tenantID, tc.start, tc.end, tc.matchers...)
			require.NoError(t, err)

			sort.Slice(tc.expectedChunks, func(i, j int) bool {
				return tc.expectedChunks[i].labels.String() < tc.expectedChunks[j].labels.String()
			})
			sort.Slice(chunks, func(i, j int) bool {
				return chunks[i].labels.String() < chunks[j].labels.String()
			})
			assert.Equal(t, len(tc.expectedChunks), len(chunks), "Unexpected number of matching chunks")

			// Verify that all returned chunks match the expected chunks
			for i, expectedChunk := range tc.expectedChunks {
				if i < len(chunks) {
					assert.Equal(t, expectedChunk.labels, chunks[i].labels, "Labels mismatch for chunk %d", i)
					assert.Equal(t, expectedChunk.meta.MinTime, chunks[i].meta.MinTime, "MinTime mismatch for chunk %d", i)
					assert.Equal(t, expectedChunk.meta.MaxTime, chunks[i].meta.MaxTime, "MaxTime mismatch for chunk %d", i)
				}
			}

			// Additional checks for time range and matchers
			for _, chunk := range chunks {
				for _, matcher := range tc.matchers {
					assert.True(t, matcher.Matches(chunk.labels.Get(matcher.Name)),
						"Chunk labels %v do not match criteria %v", chunk.labels, matcher)
				}
			}
		})
	}
}

func setupTestData(t *testing.T, storage *MockStorage, metastore *MockMetastore, tenantID string, testData []struct {
	labels  labels.Labels
	entries []*logproto.Entry
},
) {
	total := 0
	for i, data := range testData {
		segmentID := fmt.Sprintf("segment%d", i)
		writer, err := wal.NewWalSegmentWriter()
		require.NoError(t, err)
		total += len(data.entries)
		writer.Append(tenantID, data.labels.String(), data.labels, data.entries, time.Now())

		var buf bytes.Buffer
		_, err = writer.WriteTo(&buf)
		require.NoError(t, err)

		segmentData := buf.Bytes()
		storage.PutObject(wal.Dir+segmentID, segmentData)

		blockMeta := writer.Meta(segmentID)
		metastore.AddBlock(tenantID, blockMeta)
	}
	t.Log("Total entries in storage:", total)
}

func collectPushEntries(t *testing.T, iter iter.EntryIterator) []logproto.Entry {
	var results []logproto.Entry
	for iter.Next() {
		entry := iter.At()
		lbs := iter.Labels()
		parsed, err := syntax.ParseLabels(lbs)
		require.NoError(t, err)
		results = append(results, logproto.Entry{
			Timestamp: entry.Timestamp,
			Line:      entry.Line,
			Parsed:    logproto.FromLabelsToLabelAdapters(parsed),
		})
	}
	require.NoError(t, iter.Close())
	return results
}

func collectSamplesWithLabels(t *testing.T, iter iter.SampleIterator) []SampleWithLabels {
	var results []SampleWithLabels
	for iter.Next() {
		sample := iter.At()
		labelString := iter.Labels()
		parsedLabels, err := syntax.ParseLabels(labelString)
		require.NoError(t, err)
		results = append(results, SampleWithLabels{
			Sample: sample,
			Labels: parsedLabels,
		})
	}
	require.NoError(t, iter.Close())
	return results
}

func generateSamples(start, end int64, value float64) []logproto.Sample {
	var samples []logproto.Sample
	for i := start; i <= end; i++ {
		samples = append(samples, logproto.Sample{
			Timestamp: time.Unix(i, 0).UnixNano(),
			Value:     value,
		})
	}
	return samples
}

func setupTestSampleData(t *testing.T, storage *MockStorage, metastore *MockMetastore, tenantID string, testData []struct {
	labels  labels.Labels
	samples []logproto.Sample
},
) {
	total := 0
	for i, data := range testData {
		segmentID := fmt.Sprintf("segment%d", i)
		writer, err := wal.NewWalSegmentWriter()
		require.NoError(t, err)
		total += len(data.samples)

		// Convert samples to entries for the WAL writer
		entries := make([]*logproto.Entry, len(data.samples))
		for i, sample := range data.samples {
			entries[i] = &logproto.Entry{
				Timestamp: time.Unix(0, sample.Timestamp),
				Line:      fmt.Sprintf("%f", sample.Value),
			}
		}

		writer.Append(tenantID, data.labels.String(), data.labels, entries, time.Now())

		var buf bytes.Buffer
		_, err = writer.WriteTo(&buf)
		require.NoError(t, err)

		segmentData := buf.Bytes()
		storage.PutObject(wal.Dir+segmentID, segmentData)

		blockMeta := writer.Meta(segmentID)
		metastore.AddBlock(tenantID, blockMeta)
	}
	t.Log("Total samples in storage:", total)
}
