package streamcount

import (
	"context"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

type mockClient struct{ fetches kgo.Fetches }

func (m *mockClient) PollFetches(context.Context) kgo.Fetches { return m.fetches }

func record(t *testing.T, tenant, policy string, hash uint64, ts time.Time) *kgo.Record {
	b, err := (&proto.StreamMetadataRecord{
		Tenant:   tenant,
		Metadata: &proto.StreamMetadata{StreamHash: hash, IngestionPolicy: policy},
	}).Marshal()
	require.NoError(t, err)
	return &kgo.Record{Value: b, Timestamp: ts}
}

func TestService_CountsUniqueStreamsByTenant(t *testing.T) {
	reg := prometheus.NewRegistry()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	yesterday := now.Add(-13 * time.Hour)
	recs := []*kgo.Record{
		record(t, "a", "", 1, now), record(t, "a", "", 1, now), record(t, "a", "", 2, now),
		record(t, "a", "p", 1, now),
		record(t, "b", "", 1, now), record(t, "b", "", 2, yesterday),
		{Value: []byte("garbage"), Timestamp: now},
	}
	c := &mockClient{fetches: kgo.Fetches{{Topics: []kgo.FetchTopic{{
		Topic:      "t",
		Partitions: []kgo.FetchPartition{{Records: recs}},
	}}}}}
	clock := quartz.NewMock(t)
	clock.Set(now)
	s := newService(Config{}, c, log.NewNopLogger(), reg, clock)
	require.NoError(t, s.poll(context.Background()))

	require.Equal(t, 2, s.Count("a", ""))
	require.Equal(t, 1, s.Count("a", "p"))
	require.Equal(t, 1, s.Count("b", ""))
	require.Equal(t, 0, s.Count("c", ""))
	require.Equal(t, 1.0, testutil.ToFloat64(s.recordsInvalid))
	require.Equal(t, 3, testutil.CollectAndCount(s, "loki_24h_streams"))

	require.Equal(t, 1.0, testutil.ToFloat64(s.recordsOld))

	// The counts reset when the UTC day changes.
	clock.Set(time.Date(2026, 10, 15, 0, 0, 1, 0, time.UTC))
	require.Equal(t, 0, s.Count("a", ""))
	require.Equal(t, 0, testutil.CollectAndCount(s, "loki_24h_streams"))

	// Records from the previous day are now ignored.
	require.NoError(t, s.poll(context.Background()))
	require.Equal(t, 0, s.Count("a", ""))
}
