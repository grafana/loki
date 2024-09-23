package ingester

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
	"github.com/grafana/loki/v3/pkg/ingester-rf1/objstore"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type mockCommitter struct {
	committed int64
}

func newMockCommitter() *mockCommitter {
	return &mockCommitter{
		committed: -1,
	}
}

func (m *mockCommitter) Commit(_ context.Context, offset int64) error {
	m.committed = offset
	return nil
}

func TestConsumer_PeriodicFlush(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	storage, err := objstore.NewTestStorage(t)
	require.NoError(t, err)

	metastore := NewTestMetastore()
	reg := prometheus.NewRegistry()

	flushInterval := 100 * time.Millisecond
	maxFlushSize := int64(1000)

	committer := &mockCommitter{}
	consumerFactory := NewConsumerFactory(metastore, storage, flushInterval, maxFlushSize, log.NewLogfmtLogger(os.Stdout), reg)
	consumer, err := consumerFactory(committer)
	require.NoError(t, err)

	recordsChan := make(chan []record)
	_ = consumer.Start(ctx, recordsChan)

	stream := logproto.Stream{
		Labels: `{__name__="test_metric", label="value1"}`,
		Entries: []logproto.Entry{
			{Timestamp: time.Unix(0, 1000), Line: "10.5"},
		},
	}

	encodedRecords, err := kafka.Encode(0, "tenant1", stream, 10<<20)
	require.NoError(t, err)

	records := []record{{
		tenantID: "tenant1",
		content:  encodedRecords[0].Value,
		offset:   0,
	}}

	recordsChan <- records

	require.Eventually(t, func() bool {
		blocks, err := metastore.ListBlocksForQuery(ctx, &metastorepb.ListBlocksForQueryRequest{
			TenantId:  "tenant1",
			StartTime: 0,
			EndTime:   100000,
		})
		require.NoError(t, err)
		return len(blocks.Blocks) == 1
	}, 5*time.Second, 100*time.Millisecond)

	// Verify committed offset
	require.Equal(t, int64(0), committer.committed)
}

func TestConsumer_ShutdownFlush(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	storage, err := objstore.NewTestStorage(t)
	require.NoError(t, err)

	metastore := NewTestMetastore()
	reg := prometheus.NewRegistry()

	flushInterval := 1 * time.Hour
	maxFlushSize := int64(1000)

	committer := &mockCommitter{}
	consumerFactory := NewConsumerFactory(metastore, storage, flushInterval, maxFlushSize, log.NewLogfmtLogger(os.Stdout), reg)
	consumer, err := consumerFactory(committer)
	require.NoError(t, err)

	recordsChan := make(chan []record)
	wait := consumer.Start(ctx, recordsChan)

	stream := logproto.Stream{
		Labels: `{__name__="test_metric", label="value1"}`,
		Entries: []logproto.Entry{
			{Timestamp: time.Unix(0, 1000), Line: "10.5"},
		},
	}

	encodedRecords, err := kafka.Encode(0, "tenant1", stream, 10<<20)
	require.NoError(t, err)

	records := []record{{
		tenantID: "tenant1",
		content:  encodedRecords[0].Value,
		offset:   0,
	}}

	recordsChan <- records

	cancel()
	wait()

	blocks, err := metastore.ListBlocksForQuery(ctx, &metastorepb.ListBlocksForQueryRequest{
		TenantId:  "tenant1",
		StartTime: 0,
		EndTime:   100000,
	})
	require.NoError(t, err)
	require.Equal(t, 1, len(blocks.Blocks))

	// Verify committed offset
	require.Equal(t, int64(0), committer.committed)
}

func TestConsumer_MaxFlushSize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	storage, err := objstore.NewTestStorage(t)
	require.NoError(t, err)

	metastore := NewTestMetastore()
	reg := prometheus.NewRegistry()

	flushInterval := 1 * time.Hour
	maxFlushSize := int64(10)

	committer := &mockCommitter{}
	consumerFactory := NewConsumerFactory(metastore, storage, flushInterval, maxFlushSize, log.NewLogfmtLogger(os.Stdout), reg)
	consumer, err := consumerFactory(committer)
	require.NoError(t, err)

	recordsChan := make(chan []record)
	_ = consumer.Start(ctx, recordsChan)

	stream := logproto.Stream{
		Labels: `{__name__="test_metric", label="value1"}`,
		Entries: []logproto.Entry{
			{Timestamp: time.Unix(0, 1000), Line: strings.Repeat("a", 100)},
		},
	}

	encodedRecords, err := kafka.Encode(0, "tenant1", stream, 10<<20)
	require.NoError(t, err)

	records := []record{{
		tenantID: "tenant1",
		content:  encodedRecords[0].Value,
		offset:   0,
	}}

	recordsChan <- records

	require.Eventually(t, func() bool {
		blocks, err := metastore.ListBlocksForQuery(ctx, &metastorepb.ListBlocksForQueryRequest{
			TenantId:  "tenant1",
			StartTime: 0,
			EndTime:   100000,
		})
		require.NoError(t, err)
		return len(blocks.Blocks) == 1
	}, 5*time.Second, 100*time.Millisecond)

	require.Equal(t, int64(0), committer.committed)
}
