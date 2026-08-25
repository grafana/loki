package consumer

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

func TestMetastoreEvents_Emit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			testCtx         = t.Context()
			mockKafka       = &mockKafka{}
			metastoreEvents = newMetastoreEvents(1, 10, mockKafka)
		)
		earlistRecordTime := time.Now()
		// Sleep 1 second so we can check the metastore event contains different
		// timestamps assigned to the correct fields. This is not a real sleep
		// as the test is run inside a synctest.
		time.Sleep(time.Second)
		require.NotEqual(t, time.Now(), earlistRecordTime)
		require.NoError(t, metastoreEvents.Emit(testCtx, "path1", earlistRecordTime))
		// A message should have been produced to the Kafka topic.
		require.Len(t, mockKafka.produced, 1)
		rec := mockKafka.produced[0]
		var event metastore.ObjectWrittenEvent
		require.NoError(t, event.Unmarshal(rec.Value))
		require.Equal(t, "path1", event.ObjectPath)
		require.Equal(t, earlistRecordTime.Format(time.RFC3339), event.EarliestRecordTime)
		require.Equal(t, time.Now().Format(time.RFC3339), event.WriteTime)
	})
}
