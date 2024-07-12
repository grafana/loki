package wal

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestManager_Append(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         30 * time.Second,
		MaxSegments:    1,
		MaxSegmentSize: 1024, // 1KB
	}, NewMetrics(nil))
	require.NoError(t, err)

	// Append some data.
	lbs := labels.Labels{{Name: "a", Value: "b"}}
	entries := []*logproto.Entry{{Timestamp: time.Now(), Line: strings.Repeat("c", 1024)}}
	res, err := m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   entries,
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	// The data hasn't been flushed, so reading from Done() should block.
	select {
	case <-res.Done():
		t.Fatal("unexpected closed Done()")
	default:
	}

	// Pretend to flush the data.
	res.SetDone(nil)

	// Should be able to read from Done() as it is closed.
	select {
	case <-res.Done():
	default:
		t.Fatal("expected closed Done()")
	}
	require.NoError(t, res.Err())
}

func TestManager_AppendFailed(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         30 * time.Second,
		MaxSegments:    1,
		MaxSegmentSize: 1024, // 1KB
	}, NewMetrics(nil))
	require.NoError(t, err)

	// Append some data.
	lbs := labels.Labels{{Name: "a", Value: "b"}}
	entries := []*logproto.Entry{{Timestamp: time.Now(), Line: strings.Repeat("c", 1024)}}
	res, err := m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   entries,
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	// Pretend that the flush failed.
	res.SetDone(errors.New("failed to flush"))

	// Should be able to read from the Done() as it is closed and assert
	// that the error is the expected error.
	select {
	case <-res.Done():
	default:
		t.Fatal("expected closed Done()")
	}
	require.EqualError(t, res.Err(), "failed to flush")
}

func TestManager_AppendMaxAge(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         100 * time.Millisecond,
		MaxSegments:    1,
		MaxSegmentSize: 8 * 1024 * 1024, // 8MB
	}, NewMetrics(nil))
	require.NoError(t, err)

	// Append 1B of data.
	lbs := labels.Labels{{Name: "a", Value: "b"}}
	entries := []*logproto.Entry{{Timestamp: time.Now(), Line: "c"}}
	res, err := m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   entries,
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	// The segment that was just appended to has neither reached the maximum
	// age nor maximum size to be flushed.
	require.Equal(t, 1, m.available.Len())
	require.Equal(t, 0, m.pending.Len())

	// Wait 100ms and append some more data.
	time.Sleep(100 * time.Millisecond)
	entries = []*logproto.Entry{{Timestamp: time.Now(), Line: "c"}}
	res, err = m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   entries,
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	// The segment has reached the maximum age and should have been moved to
	// pending list to be flushed.
	require.Equal(t, 0, m.available.Len())
	require.Equal(t, 1, m.pending.Len())
}

func TestManager_AppendMaxSize(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         30 * time.Second,
		MaxSegments:    1,
		MaxSegmentSize: 1024, // 1KB
	}, NewMetrics(nil))
	require.NoError(t, err)

	// Append 512B of data.
	lbs := labels.Labels{{Name: "a", Value: "b"}}
	entries := []*logproto.Entry{{Timestamp: time.Now(), Line: strings.Repeat("c", 512)}}
	res, err := m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   entries,
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	// The segment that was just appended to has neither reached the maximum
	// age nor maximum size to be flushed.
	require.Equal(t, 1, m.available.Len())
	require.Equal(t, 0, m.pending.Len())

	// Append another 512B of data.
	entries = []*logproto.Entry{{Timestamp: time.Now(), Line: strings.Repeat("c", 512)}}
	res, err = m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   entries,
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	// The segment has reached the maximum size and should have been moved to
	// pending list to be flushed.
	require.Equal(t, 0, m.available.Len())
	require.Equal(t, 1, m.pending.Len())
}

func TestManager_AppendWALFull(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         30 * time.Second,
		MaxSegments:    10,
		MaxSegmentSize: 1024, // 1KB
	}, NewMetrics(nil))
	require.NoError(t, err)

	// Should be able to write 100KB of data, 10KB per segment.
	lbs := labels.Labels{{Name: "a", Value: "b"}}
	for i := 0; i < 10; i++ {
		entries := []*logproto.Entry{{Timestamp: time.Now(), Line: strings.Repeat("c", 1024)}}
		res, err := m.Append(AppendRequest{
			TenantID:  "1",
			Labels:    lbs,
			LabelsStr: lbs.String(),
			Entries:   entries,
		})
		require.NoError(t, err)
		require.NotNil(t, res)
	}

	// However, appending more data should fail as all segments are full and
	// waiting to be flushed.
	entries := []*logproto.Entry{{Timestamp: time.Now(), Line: strings.Repeat("c", 1024)}}
	res, err := m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   entries,
	})
	require.ErrorIs(t, err, ErrFull)
	require.Nil(t, res)
}

func TestManager_NextPending(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         30 * time.Second,
		MaxSegments:    1,
		MaxSegmentSize: 1024, // 1KB
	}, NewMetrics(nil))
	require.NoError(t, err)

	// There should be no segments waiting to be flushed as no data has been
	// written.
	it := m.NextPending()
	require.Nil(t, it)

	// Append 1KB of data.
	lbs := labels.Labels{{Name: "a", Value: "b"}}
	entries := []*logproto.Entry{{Timestamp: time.Now(), Line: strings.Repeat("c", 1024)}}
	_, err = m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   entries,
	})
	require.NoError(t, err)

	// There should be a segment waiting to be flushed.
	it = m.NextPending()
	require.NotNil(t, it)

	// There should be no more segments waiting to be flushed.
	it = m.NextPending()
	require.Nil(t, it)
}

func TestManager_NexPendingMaxAge(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         100 * time.Millisecond,
		MaxSegments:    1,
		MaxSegmentSize: 1024, // 1KB
	}, NewMetrics(nil))
	require.NoError(t, err)

	// Append 1B of data.
	lbs := labels.Labels{{Name: "a", Value: "b"}}
	entries := []*logproto.Entry{{Timestamp: time.Now(), Line: "c"}}
	res, err := m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   entries,
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	// The segment that was just appended to has neither reached the maximum
	// age nor maximum size to be flushed.
	it := m.NextPending()
	require.Nil(t, it)
	require.Equal(t, 1, m.available.Len())
	require.Equal(t, 0, m.pending.Len())

	// Wait 100ms. The segment that was just appended to should have reached
	// the maximum age.
	time.Sleep(100 * time.Millisecond)
	it = m.NextPending()
	require.NotNil(t, it)
	require.Equal(t, 0, m.available.Len())
	require.Equal(t, 0, m.pending.Len())
}

func TestManager_Put(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         30 * time.Second,
		MaxSegments:    1,
		MaxSegmentSize: 1024, // 1KB
	}, NewMetrics(nil))
	require.NoError(t, err)

	// There should be 1 available and 0 pending segments.
	require.Equal(t, 1, m.available.Len())
	require.Equal(t, 0, m.pending.Len())

	// Append 1KB of data.
	lbs := labels.Labels{{Name: "a", Value: "b"}}
	entries := []*logproto.Entry{{Timestamp: time.Now(), Line: strings.Repeat("c", 1024)}}
	_, err = m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   entries,
	})
	require.NoError(t, err)

	// The segment is full, so there should now be 0 available segments and 1
	// pending segment.
	require.Equal(t, 0, m.available.Len())
	require.Equal(t, 1, m.pending.Len())

	// Getting the pending segment should remove it from the list.
	it := m.NextPending()
	require.NotNil(t, it)
	require.Equal(t, 0, m.available.Len())
	require.Equal(t, 0, m.pending.Len())

	// The segment should contain 1KB of data.
	require.Equal(t, int64(1024), it.Writer.InputSize())

	// Putting it back should add it to the available list.
	m.Put(it)
	require.Equal(t, 1, m.available.Len())
	require.Equal(t, 0, m.pending.Len())

	// The segment should be reset.
	require.Equal(t, int64(0), it.Writer.InputSize())
}

func TestManager_Metrics(t *testing.T) {
	r := prometheus.NewRegistry()
	m, err := NewManager(Config{
		MaxSegments:    1,
		MaxSegmentSize: 1024, // 1KB
	}, NewMetrics(r))
	require.NoError(t, err)

	metricNames := []string{
		"wal_segments_available",
		"wal_segments_flushing",
		"wal_segments_pending",
	}
	expected := `
# HELP wal_segments_available The number of WAL segments accepting writes.
# TYPE wal_segments_available gauge
wal_segments_available 1
# HELP wal_segments_flushing The number of WAL segments being flushed.
# TYPE wal_segments_flushing gauge
wal_segments_flushing 0
# HELP wal_segments_pending The number of WAL segments waiting to be flushed.
# TYPE wal_segments_pending gauge
wal_segments_pending 0
`
	require.NoError(t, testutil.CollectAndCompare(r, strings.NewReader(expected), metricNames...))

	// Appending 1KB of data.
	lbs := labels.Labels{{Name: "a", Value: "b"}}
	entries := []*logproto.Entry{{Timestamp: time.Now(), Line: strings.Repeat("c", 1024)}}
	_, err = m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   entries,
	})
	require.NoError(t, err)

	// This should move the segment from the available to the pending list.
	expected = `
# HELP wal_segments_available The number of WAL segments accepting writes.
# TYPE wal_segments_available gauge
wal_segments_available 0
# HELP wal_segments_flushing The number of WAL segments being flushed.
# TYPE wal_segments_flushing gauge
wal_segments_flushing 0
# HELP wal_segments_pending The number of WAL segments waiting to be flushed.
# TYPE wal_segments_pending gauge
wal_segments_pending 1
`
	require.NoError(t, testutil.CollectAndCompare(r, strings.NewReader(expected), metricNames...))

	// Get the segment from the pending list.
	it := m.NextPending()
	require.NotNil(t, it)
	expected = `
# HELP wal_segments_available The number of WAL segments accepting writes.
# TYPE wal_segments_available gauge
wal_segments_available 0
# HELP wal_segments_flushing The number of WAL segments being flushed.
# TYPE wal_segments_flushing gauge
wal_segments_flushing 1
# HELP wal_segments_pending The number of WAL segments waiting to be flushed.
# TYPE wal_segments_pending gauge
wal_segments_pending 0
`
	require.NoError(t, testutil.CollectAndCompare(r, strings.NewReader(expected), metricNames...))

	// Reset the segment and put it back in the available list.
	m.Put(it)
	expected = `
# HELP wal_segments_available The number of WAL segments accepting writes.
# TYPE wal_segments_available gauge
wal_segments_available 1
# HELP wal_segments_flushing The number of WAL segments being flushed.
# TYPE wal_segments_flushing gauge
wal_segments_flushing 0
# HELP wal_segments_pending The number of WAL segments waiting to be flushed.
# TYPE wal_segments_pending gauge
wal_segments_pending 0
`
	require.NoError(t, testutil.CollectAndCompare(r, strings.NewReader(expected), metricNames...))

}
