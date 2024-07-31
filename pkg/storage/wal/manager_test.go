package wal

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coder/quartz"
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
	}, NewManagerMetrics(nil))
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

func TestManager_AppendNoEntries(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         30 * time.Second,
		MaxSegments:    1,
		MaxSegmentSize: 1024, // 1KB
	}, NewManagerMetrics(nil))
	require.NoError(t, err)

	// Append no entries.
	lbs := labels.Labels{{Name: "a", Value: "b"}}
	res, err := m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   []*logproto.Entry{},
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	// The data hasn't been flushed, so reading from Done() should block.
	select {
	case <-res.Done():
		t.Fatal("unexpected closed Done()")
	default:
	}

	// The segment that was just appended to has neither reached the maximum
	// age nor maximum size to be flushed.
	require.Equal(t, 1, m.available.Len())
	require.Equal(t, 0, m.pending.Len())
}

func TestManager_AppendFailed(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         30 * time.Second,
		MaxSegments:    1,
		MaxSegmentSize: 1024, // 1KB
	}, NewManagerMetrics(nil))
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

func TestManager_AppendFailedWALClosed(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         30 * time.Second,
		MaxSegments:    10,
		MaxSegmentSize: 1024, // 1KB
	}, NewManagerMetrics(nil))
	require.NoError(t, err)

	// Append some data.
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

	// Close the WAL.
	m.Close()

	// Should not be able to append more data as the WAL is closed.
	res, err = m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   entries,
	})
	require.Nil(t, res)
	require.ErrorIs(t, err, ErrClosed)
}

func TestManager_AppendFailedWALFull(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         30 * time.Second,
		MaxSegments:    10,
		MaxSegmentSize: 1024, // 1KB
	}, NewManagerMetrics(nil))
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

func TestManager_AppendMaxAgeExceeded(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         100 * time.Millisecond,
		MaxSegments:    1,
		MaxSegmentSize: 8 * 1024 * 1024, // 8MB
	}, NewManagerMetrics(nil))
	require.NoError(t, err)

	// Create a mock clock.
	clock := quartz.NewMock(t)
	m.clock = clock

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
	clock.Advance(100 * time.Millisecond)
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

func TestManager_AppendMaxSizeExceeded(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         30 * time.Second,
		MaxSegments:    1,
		MaxSegmentSize: 1024, // 1KB
	}, NewManagerMetrics(nil))
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

func TestManager_NextPending(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         30 * time.Second,
		MaxSegments:    1,
		MaxSegmentSize: 1024, // 1KB
	}, NewManagerMetrics(nil))
	require.NoError(t, err)

	// There should be no segments waiting to be flushed as no data has been
	// written.
	s, err := m.NextPending()
	require.NoError(t, err)
	require.Nil(t, s)

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
	s, err = m.NextPending()
	require.NoError(t, err)
	require.NotNil(t, s)

	// There should be no more segments waiting to be flushed.
	s, err = m.NextPending()
	require.NoError(t, err)
	require.Nil(t, s)
}

func TestManager_NextPendingAge(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         100 * time.Millisecond,
		MaxSegments:    1,
		MaxSegmentSize: 1024, // 1KB
	}, NewManagerMetrics(nil))
	require.NoError(t, err)

	// Create a mock clock.
	clock := quartz.NewMock(t)
	m.clock = clock

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

	// Wait 100ms. The segment that was just appended to should have reached
	// the maximum age.
	clock.Advance(100 * time.Millisecond)
	s, err := m.NextPending()
	require.NoError(t, err)
	require.NotNil(t, s)
	require.Equal(t, 100*time.Millisecond, s.Writer.Age(clock.Now()))
	m.Put(s)

	// Append 1KB of data using two separate append requests, 1ms apart.
	entries = []*logproto.Entry{{Timestamp: time.Now(), Line: strings.Repeat("c", 512)}}
	res, err = m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   entries,
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	// Wait 1ms and then append the rest of the data.
	clock.Advance(time.Millisecond)
	entries = []*logproto.Entry{{Timestamp: time.Now(), Line: strings.Repeat("c", 512)}}
	res, err = m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   entries,
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	// The segment that was just appended to should have reached the maximum
	// size.
	s, err = m.NextPending()
	require.NoError(t, err)
	require.NotNil(t, s)
	require.Equal(t, time.Millisecond, s.Writer.Age(clock.Now()))
}

func TestManager_NextPendingMaxAgeExceeded(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         100 * time.Millisecond,
		MaxSegments:    1,
		MaxSegmentSize: 1024, // 1KB
	}, NewManagerMetrics(nil))
	require.NoError(t, err)

	// Create a mock clock.
	clock := quartz.NewMock(t)
	m.clock = clock

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
	s, err := m.NextPending()
	require.NoError(t, err)
	require.Nil(t, s)
	require.Equal(t, 1, m.available.Len())
	require.Equal(t, 0, m.pending.Len())

	// Wait 100ms. The segment that was just appended to should have reached
	// the maximum age.
	clock.Advance(100 * time.Millisecond)
	s, err = m.NextPending()
	require.NoError(t, err)
	require.NotNil(t, s)
	require.Equal(t, 0, m.available.Len())
	require.Equal(t, 0, m.pending.Len())
}

func TestManager_NextPendingWALClosed(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         30 * time.Second,
		MaxSegments:    1,
		MaxSegmentSize: 1024, // 1KB
	}, NewManagerMetrics(nil))
	require.NoError(t, err)

	// Append some data.
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

	// There should be no segments waiting to be flushed as neither the maximum
	// age nor maximum size has been exceeded.
	s, err := m.NextPending()
	require.NoError(t, err)
	require.Nil(t, s)

	// Close the WAL.
	m.Close()

	// There should be one segment waiting to be flushed.
	s, err = m.NextPending()
	require.NoError(t, err)
	require.NotNil(t, s)

	// There are no more segments waiting to be flushed, and since the WAL is
	// closed, successive calls should return ErrClosed.
	for i := 0; i < 10; i++ {
		s, err = m.NextPending()
		require.ErrorIs(t, err, ErrClosed)
		require.Nil(t, s)
	}
}

func TestManager_Put(t *testing.T) {
	m, err := NewManager(Config{
		MaxAge:         30 * time.Second,
		MaxSegments:    1,
		MaxSegmentSize: 1024, // 1KB
	}, NewManagerMetrics(nil))
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
	s, err := m.NextPending()
	require.NoError(t, err)
	require.NotNil(t, s)
	require.Equal(t, 0, m.available.Len())
	require.Equal(t, 0, m.pending.Len())

	// The segment should contain 1KB of data.
	require.Equal(t, int64(1024), s.Writer.InputSize())

	// Putting it back should add it to the available list.
	m.Put(s)
	require.Equal(t, 1, m.available.Len())
	require.Equal(t, 0, m.pending.Len())

	// The segment should be reset.
	require.Equal(t, int64(0), s.Writer.InputSize())
}

func TestManager_Metrics(t *testing.T) {
	r := prometheus.NewRegistry()
	m, err := NewManager(Config{
		MaxSegments:    1,
		MaxSegmentSize: 1024, // 1KB
	}, NewManagerMetrics(r))
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
	s, err := m.NextPending()
	require.NoError(t, err)
	require.NotNil(t, s)
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
	m.Put(s)
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
