package ingester

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/ingester-rf1/objstore"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestConsumer(t *testing.T) {
	ctx := context.Background()
	storage, err := objstore.NewTestStorage(t)
	require.NoError(t, err)

	metastore := NewTestMetastore()
	reg := prometheus.NewRegistry()

	c, err := newConsumer(metastore, storage, log.NewNopLogger(), reg)
	require.NoError(t, err)

	t.Run("Consume and Flush", func(t *testing.T) {
		// Create a Kafka encoder
		encoder := kafka.NewEncoder()

		// Prepare test data
		streams := []logproto.Stream{
			{
				Labels: `{__name__="test_metric", label="value1"}`,
				Entries: []logproto.Entry{
					{Timestamp: time.Unix(1234567890, 0), Line: "10.5"},
				},
			},
			{
				Labels: `{__name__="test_metric", label="value2"}`,
				Entries: []logproto.Entry{
					{Timestamp: time.Unix(1234567891, 0), Line: "20.5"},
				},
			},
		}

		var records []record
		for i, stream := range streams {
			// Encode the stream
			encodedRecords, err := encoder.Encode(int32(i), fmt.Sprintf("tenant%d", i+1), stream)
			require.NoError(t, err)

			// Convert encoded records to our test record format
			for _, encodedRecord := range encodedRecords {
				records = append(records, record{
					tenantID: string(encodedRecord.Key),
					content:  encodedRecord.Value,
				})
			}
		}

		// Consume records
		err = c.Consume(ctx, 0, records)
		require.NoError(t, err)

		// Flush data
		err = c.Flush(ctx)
		require.NoError(t, err)

		// Verify metastore update
		require.Len(t, metastore.blocks, 2)
		require.Len(t, metastore.blocks["tenant1"], 1)
		require.Len(t, metastore.blocks["tenant2"], 1)
	})
}
