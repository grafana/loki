package metastore

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestScanPointers_NoSelectorReturnsEOF(t *testing.T) {
	t.Parallel()

	sStart := scalar.NewTimestampScalar(arrow.Timestamp(now.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
	sEnd := scalar.NewTimestampScalar(arrow.Timestamp(now.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
	s := newScanPointers(nil, sStart, sEnd, nil, nil)

	rec, err := s.Read(context.Background())
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, rec)
}

func TestScanPointers_MissingOrgIDReturnsError(t *testing.T) {
	t.Parallel()

	builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	_, err = builder.AppendStream(tenantID, streams.Stream{
		ID:               1,
		Labels:           labels.New(labels.Label{Name: "app", Value: "foo"}),
		MinTimestamp:     now.Add(-3 * time.Hour),
		MaxTimestamp:     now.Add(-2 * time.Hour),
		UncompressedSize: 5,
	})
	require.NoError(t, err)
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-3*time.Hour), 5))
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-2*time.Hour), 0))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}
	sStart := scalar.NewTimestampScalar(arrow.Timestamp(start.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
	sEnd := scalar.NewTimestampScalar(arrow.Timestamp(end.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)

	s := newScanPointers(obj, sStart, sEnd, matchers, nil)

	rec, err := s.Read(context.Background())
	require.Error(t, err)
	require.Nil(t, rec)
}

func TestScanPointers_FiltersByStreamMatcherAndTime(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	_, err = builder.AppendStream(tenantID, streams.Stream{
		ID:               1,
		Labels:           labels.New(labels.Label{Name: "app", Value: "foo"}),
		MinTimestamp:     now.Add(-3 * time.Hour),
		MaxTimestamp:     now.Add(-2 * time.Hour),
		UncompressedSize: 5,
	})
	require.NoError(t, err)
	_, err = builder.AppendStream(tenantID, streams.Stream{
		ID:               2,
		Labels:           labels.New(labels.Label{Name: "app", Value: "bar"}),
		MinTimestamp:     now.Add(-3 * time.Hour),
		MaxTimestamp:     now.Add(-2 * time.Hour),
		UncompressedSize: 5,
	})
	require.NoError(t, err)

	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-3*time.Hour), 5))
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 2, 2, now.Add(-3*time.Hour), 5))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}
	sStart := scalar.NewTimestampScalar(arrow.Timestamp(start.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
	sEnd := scalar.NewTimestampScalar(arrow.Timestamp(end.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)

	s := newScanPointers(obj, sStart, sEnd, matchers, nil)
	t.Cleanup(s.Close)

	rec, err := s.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, int64(1), rec.NumRows())

	var streamIDCol *array.Int64
	for i, f := range rec.Schema().Fields() {
		if f.Name == "stream_id.int64" {
			streamIDCol = rec.Column(i).(*array.Int64)
			break
		}
	}
	require.NotNil(t, streamIDCol)
	require.Equal(t, int64(1), streamIDCol.Value(0))
}
