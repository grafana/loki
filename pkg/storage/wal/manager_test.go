package wal

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestManager_Append(t *testing.T) {
	m, err := NewManager(Config{
		MaxSegments:    1,
		MaxSegmentSize: 1024, // 1KB
	}, NewMetrics(nil))
	require.NoError(t, err)

	// Append some data.
	lbs := labels.Labels{{
		Name:  "foo",
		Value: "bar",
	}}
	entries := []*logproto.Entry{{
		Timestamp: time.Now(),
		Line:      strings.Repeat("a", 1024),
	}}
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

	// Flush the data and broadcast that the flush is successful.
	it, err := m.NextPending()
	require.NoError(t, err)
	require.NotNil(t, it)
	it.Result.SetDone(nil)

	// Should be able to read from the Done() as it is closed.
	select {
	case <-res.Done():
	default:
		t.Fatal("expected closed Done()")
	}
	require.NoError(t, res.Err())

	// Return the segment to be written to again.
	require.NoError(t, m.Put(it))

	// Append some more data.
	entries = []*logproto.Entry{{
		Timestamp: time.Now(),
		Line:      strings.Repeat("b", 1024),
	}}
	res, err = m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   entries,
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	// Flush the data, but this time broadcast an error that the flush failed.
	it, err = m.NextPending()
	require.NoError(t, err)
	require.NotNil(t, it)
	it.Result.SetDone(errors.New("failed to flush"))

	// Should be able to read from the Done() as it is closed.
	select {
	case <-res.Done():
	default:
		t.Fatal("expected closed Done()")
	}
	require.EqualError(t, res.Err(), "failed to flush")
}

// This test asserts that Append operations return ErrFull if all segments
// are full and waiting to be flushed.
func TestManager_Append_ErrFull(t *testing.T) {
	m, err := NewManager(Config{
		MaxSegments:    10,
		MaxSegmentSize: 1024, // 1KB
	}, NewMetrics(nil))
	require.NoError(t, err)

	// Should be able to write to all 10 segments of 1KB each.
	lbs := labels.Labels{{
		Name:  "foo",
		Value: "bar",
	}}
	for i := 0; i < 10; i++ {
		entries := []*logproto.Entry{{
			Timestamp: time.Now(),
			Line:      strings.Repeat("a", 1024),
		}}
		res, err := m.Append(AppendRequest{
			TenantID:  "1",
			Labels:    lbs,
			LabelsStr: lbs.String(),
			Entries:   entries,
		})
		require.NoError(t, err)
		require.NotNil(t, res)
	}

	// Append more data should fail as all segments are full and waiting to be
	// flushed.
	entries := []*logproto.Entry{{
		Timestamp: time.Now(),
		Line:      strings.Repeat("b", 1024),
	}}
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
		MaxAge:         DefaultMaxAge,
		MaxSegments:    1,
		MaxSegmentSize: 1024, // 1KB
	}, NewMetrics(nil))
	require.NoError(t, err)

	// There should be no items as no data has been written.
	it, err := m.NextPending()
	require.NoError(t, err)
	require.Nil(t, it)

	// Append 512B of data. There should still be no items to as the segment is
	// not full (1KB).
	lbs := labels.Labels{{
		Name:  "foo",
		Value: "bar",
	}}
	entries := []*logproto.Entry{{
		Timestamp: time.Now(),
		Line:      strings.Repeat("b", 512),
	}}
	_, err = m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   entries,
	})
	require.NoError(t, err)
	it, err = m.NextPending()
	require.NoError(t, err)
	require.Nil(t, it)

	// Write another 512B of data. There should be an item waiting to be flushed.
	entries = []*logproto.Entry{{
		Timestamp: time.Now(),
		Line:      strings.Repeat("b", 512),
	}}
	_, err = m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   entries,
	})
	require.NoError(t, err)
	it, err = m.NextPending()
	require.NoError(t, err)
	require.NotNil(t, it)

	// Should not get the same item more than once.
	it, err = m.NextPending()
	require.NoError(t, err)
	require.Nil(t, it)
}

func TestManager_Put(t *testing.T) {
	m, err := NewManager(Config{
		MaxSegments:    10,
		MaxSegmentSize: 1024, // 1KB
	}, NewMetrics(nil))
	require.NoError(t, err)

	// There should be 10 available segments, and 0 pending.
	require.Equal(t, 10, m.available.Len())
	require.Equal(t, 0, m.pending.Len())

	// Append 1KB of data.
	lbs := labels.Labels{{
		Name:  "foo",
		Value: "bar",
	}}
	entries := []*logproto.Entry{{
		Timestamp: time.Now(),
		Line:      strings.Repeat("b", 1024),
	}}
	_, err = m.Append(AppendRequest{
		TenantID:  "1",
		Labels:    lbs,
		LabelsStr: lbs.String(),
		Entries:   entries,
	})
	require.NoError(t, err)

	// 1 segment is full, so there should now be 9 available segments,
	// and 1 pending segment.
	require.Equal(t, 9, m.available.Len())
	require.Equal(t, 1, m.pending.Len())

	// Getting the pending segment should remove it from the list.
	it, err := m.NextPending()
	require.NoError(t, err)
	require.NotNil(t, it)
	require.Equal(t, 9, m.available.Len())
	require.Equal(t, 0, m.pending.Len())

	// The segment should contain 1KB of data.
	require.Equal(t, int64(1024), it.Writer.InputSize())

	// Putting it back should add it to the available list.
	require.NoError(t, m.Put(it))
	require.Equal(t, 10, m.available.Len())
	require.Equal(t, 0, m.pending.Len())

	// The segment should be reset.
	require.Equal(t, int64(0), it.Writer.InputSize())
}
