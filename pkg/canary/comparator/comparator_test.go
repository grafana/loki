package comparator

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
)

func TestComparatorEntryReceivedOutOfOrder(t *testing.T) {
	outOfOrderEntries = &mockCounter{}
	wsMissingEntries = &mockCounter{}
	unexpectedEntries = &mockCounter{}
	duplicateEntries = &mockCounter{}

	actual := &bytes.Buffer{}
	c := NewComparator(actual, 1*time.Hour, 1*time.Hour, 15*time.Minute, 4*time.Hour, 4*time.Hour, 1*time.Minute, 0, 0, 1, make(chan time.Time), make(chan time.Time), nil, false)

	t1 := time.Now()
	t2 := t1.Add(1 * time.Second)
	t3 := t2.Add(1 * time.Second)
	t4 := t3.Add(1 * time.Second)

	c.entrySent(t1)
	c.entrySent(t2)
	c.entrySent(t3)
	c.entrySent(t4)

	c.entryReceived(t1)
	assert.Equal(t, 3, c.Size())
	c.entryReceived(t4)
	assert.Equal(t, 2, c.Size())
	c.entryReceived(t2)
	c.entryReceived(t3)
	assert.Equal(t, 0, c.Size())

	expected := fmt.Sprintf(ErrOutOfOrderEntry, t4, []time.Time{t2, t3})
	assert.Equal(t, expected, actual.String())

	assert.Equal(t, 1, outOfOrderEntries.(*mockCounter).count)
	assert.Equal(t, 0, unexpectedEntries.(*mockCounter).count)
	assert.Equal(t, 0, wsMissingEntries.(*mockCounter).count)
	assert.Equal(t, 0, duplicateEntries.(*mockCounter).count)

	// This avoids a panic on subsequent test execution,
	// seems ugly but was easy, and multiple instantiations
	// of the comparator should be an error
	prometheus.Unregister(responseLatency)
}

func TestComparatorEntryReceivedNotExpected(t *testing.T) {
	outOfOrderEntries = &mockCounter{}
	wsMissingEntries = &mockCounter{}
	unexpectedEntries = &mockCounter{}
	duplicateEntries = &mockCounter{}

	actual := &bytes.Buffer{}
	c := NewComparator(actual, 1*time.Hour, 1*time.Hour, 15*time.Minute, 4*time.Hour, 4*time.Hour, 1*time.Minute, 0, 0, 1, make(chan time.Time), make(chan time.Time), nil, false)

	t1 := time.Now()
	t2 := t1.Add(1 * time.Second)
	t3 := t2.Add(1 * time.Second)
	t4 := t3.Add(1 * time.Second)

	c.entrySent(t2)
	c.entrySent(t3)
	c.entrySent(t4)

	c.entryReceived(t2)
	assert.Equal(t, 2, c.Size())
	c.entryReceived(t1)
	assert.Equal(t, 2, c.Size())
	c.entryReceived(t3)
	assert.Equal(t, 1, c.Size())
	c.entryReceived(t4)
	assert.Equal(t, 0, c.Size())

	expected := fmt.Sprintf(ErrUnexpectedEntry, t1.UnixNano())
	assert.Equal(t, expected, actual.String())

	assert.Equal(t, 0, outOfOrderEntries.(*mockCounter).count)
	assert.Equal(t, 1, unexpectedEntries.(*mockCounter).count)
	assert.Equal(t, 0, wsMissingEntries.(*mockCounter).count)
	assert.Equal(t, 0, duplicateEntries.(*mockCounter).count)

	// This avoids a panic on subsequent test execution,
	// seems ugly but was easy, and multiple instantiations
	// of the comparator should be an error
	prometheus.Unregister(responseLatency)
}

func TestComparatorEntryReceivedDuplicate(t *testing.T) {
	outOfOrderEntries = &mockCounter{}
	wsMissingEntries = &mockCounter{}
	unexpectedEntries = &mockCounter{}
	duplicateEntries = &mockCounter{}

	actual := &bytes.Buffer{}
	c := NewComparator(actual, 1*time.Hour, 1*time.Hour, 15*time.Minute, 4*time.Hour, 4*time.Hour, 1*time.Minute, 0, 0, 1, make(chan time.Time), make(chan time.Time), nil, false)

	t1 := time.Now()
	t2 := t1.Add(1 * time.Second)
	t3 := t2.Add(1 * time.Second)
	t4 := t3.Add(1 * time.Second)

	c.entrySent(t1)
	c.entrySent(t2)
	c.entrySent(t3)
	c.entrySent(t4)

	c.entryReceived(t1)
	assert.Equal(t, 3, c.Size())
	c.entryReceived(t2)
	assert.Equal(t, 2, c.Size())
	c.entryReceived(t2)
	assert.Equal(t, 2, c.Size())
	c.entryReceived(t3)
	assert.Equal(t, 1, c.Size())
	c.entryReceived(t4)
	assert.Equal(t, 0, c.Size())

	expected := fmt.Sprintf(ErrDuplicateEntry, t2.UnixNano())
	assert.Equal(t, expected, actual.String())

	assert.Equal(t, 0, outOfOrderEntries.(*mockCounter).count)
	assert.Equal(t, 0, unexpectedEntries.(*mockCounter).count)
	assert.Equal(t, 0, wsMissingEntries.(*mockCounter).count)
	assert.Equal(t, 1, duplicateEntries.(*mockCounter).count)

	// This avoids a panic on subsequent test execution,
	// seems ugly but was easy, and multiple instantiations
	// of the comparator should be an error
	prometheus.Unregister(responseLatency)
}

func TestEntryNeverReceived(t *testing.T) {
	outOfOrderEntries = &mockCounter{}
	wsMissingEntries = &mockCounter{}
	missingEntries = &mockCounter{}
	unexpectedEntries = &mockCounter{}
	duplicateEntries = &mockCounter{}

	actual := &bytes.Buffer{}

	t1 := time.Now()
	t2 := t1.Add(1 * time.Millisecond)
	t3 := t2.Add(1 * time.Millisecond)
	t4 := t3.Add(1 * time.Millisecond)
	t5 := t4.Add(1 * time.Millisecond)

	found := []time.Time{t1, t3, t4, t5}

	mr := &mockReader{resp: found}
	maxWait := 50 * time.Millisecond
	//We set the prune interval timer to a huge value here so that it never runs, instead we call pruneEntries manually below
	c := NewComparator(actual, maxWait, 50*time.Hour, 15*time.Minute, 4*time.Hour, 4*time.Hour, 1*time.Minute, 0, 0, 1, make(chan time.Time), make(chan time.Time), mr, false)

	c.entrySent(t1)
	c.entrySent(t2)
	c.entrySent(t3)
	c.entrySent(t4)
	c.entrySent(t5)

	assert.Equal(t, 5, c.Size())

	c.entryReceived(t1)
	c.entryReceived(t3)
	c.entryReceived(t5)

	assert.Equal(t, 2, c.Size())

	//Wait a few maxWait intervals just to make sure all entries are expired
	<-time.After(2 * maxWait)

	c.pruneEntries()

	expected := fmt.Sprintf(ErrOutOfOrderEntry+ErrOutOfOrderEntry+ // Out of order because we missed entries
		ErrEntryNotReceivedWs+ErrEntryNotReceivedWs+ // Complain about missed entries
		DebugWebsocketMissingEntry+DebugWebsocketMissingEntry+ // List entries we are missing
		DebugQueryResult+DebugQueryResult+DebugQueryResult+DebugQueryResult+ // List entries we got back from Loki
		ErrEntryNotReceived, // List entry not received from Loki
		t3, []time.Time{t2},
		t5, []time.Time{t2, t4},
		t2.UnixNano(), maxWait.Seconds(),
		t4.UnixNano(), maxWait.Seconds(),
		t2.UnixNano(),
		t4.UnixNano(),
		t1.UnixNano(),
		t3.UnixNano(),
		t4.UnixNano(),
		t5.UnixNano(),
		t2.UnixNano(), maxWait.Seconds())

	assert.Equal(t, expected, actual.String())
	assert.Equal(t, 0, c.Size())

	assert.Equal(t, 2, outOfOrderEntries.(*mockCounter).count)
	assert.Equal(t, 0, unexpectedEntries.(*mockCounter).count)
	assert.Equal(t, 2, wsMissingEntries.(*mockCounter).count)
	assert.Equal(t, 1, missingEntries.(*mockCounter).count)
	assert.Equal(t, 0, duplicateEntries.(*mockCounter).count)

	// This avoids a panic on subsequent test execution,
	// seems ugly but was easy, and multiple instantiations
	// of the comparator should be an error
	prometheus.Unregister(responseLatency)

}

func TestPruneAckdEntires(t *testing.T) {
	actual := &bytes.Buffer{}
	maxWait := 30 * time.Millisecond
	//We set the prune interval timer to a huge value here so that it never runs, instead we call pruneEntries manually below
	c := NewComparator(actual, maxWait, 50*time.Hour, 15*time.Minute, 4*time.Hour, 4*time.Hour, 1*time.Minute, 0, 0, 1, make(chan time.Time), make(chan time.Time), nil, false)

	t1 := time.Now()
	t2 := t1.Add(1 * time.Millisecond)
	t3 := t2.Add(1 * time.Millisecond)
	t4 := t3.Add(100 * time.Second)

	assert.Equal(t, 0, len(c.ackdEntries))

	c.entrySent(t1)
	c.entrySent(t2)
	c.entrySent(t3)
	c.entrySent(t4)

	assert.Equal(t, 4, c.Size())
	assert.Equal(t, 0, len(c.ackdEntries))

	c.entryReceived(t1)
	c.entryReceived(t2)
	c.entryReceived(t3)
	c.entryReceived(t4)

	assert.Equal(t, 0, c.Size())
	assert.Equal(t, 4, len(c.ackdEntries))

	// Wait a couple maxWaits to make sure the first 3 timestamps get pruned from the ackdEntries,
	// the fourth should still remain because its much much newer and we only prune things older than maxWait
	<-time.After(2 * maxWait)
	c.pruneEntries()

	assert.Equal(t, 1, len(c.ackdEntries))
	assert.Equal(t, t4, *c.ackdEntries[0])

}

func TestSpotCheck(t *testing.T) {
	spotCheckMissing = &mockCounter{}
	spotCheckEntries = &mockCounter{}

	actual := &bytes.Buffer{}

	t1 := time.Unix(0, 0)
	entries := []time.Time{}
	found := []time.Time{}
	entries = append(entries, t1)
	for i := 1; i <= 20; i++ {
		t := entries[i-1].Add(1 * time.Millisecond)
		entries = append(entries, t)
		// Don't add the last entry so we get one error in spot check
		if i != 20 {
			found = append(found, t)
		}
	}

	mr := &mockReader{resp: found}
	maxWait := 50 * time.Millisecond
	spotCheck := 10 * time.Millisecond
	spotCheckMax := 10 * time.Millisecond
	//We set the prune interval timer to a huge value here so that it never runs, instead we call spotCheckEntries manually below
	c := NewComparator(actual, maxWait, 50*time.Hour, spotCheck, spotCheckMax, 4*time.Hour, 1*time.Minute, 0, 0, 1, make(chan time.Time), make(chan time.Time), mr, false)

	// Send all the entries
	for i := range entries {
		c.entrySent(entries[i])
	}

	assert.Equal(t, 3, len(c.spotCheck))

	// Run with "current time" 11ms after start which will prune the first entry which is no "before" the 10ms spot check max
	c.spotCheckEntries(time.Unix(0, 11*time.Millisecond.Nanoseconds()))

	// First entry should have been pruned
	assert.Equal(t, 2, len(c.spotCheck))

	expected := fmt.Sprintf(ErrSpotCheckEntryNotReceived, // List entry not received from Loki
		entries[20].UnixNano(), "-9ms")

	// We didn't send the last entry and our initial counter did not start at 0 so we should get back entries 1-19
	for i := 1; i < 20; i++ {
		expected = expected + fmt.Sprintf(DebugQueryResult, entries[i].UnixNano())
	}

	assert.Equal(t, expected, actual.String())

	assert.Equal(t, 2, spotCheckEntries.(*mockCounter).count)
	assert.Equal(t, 1, spotCheckMissing.(*mockCounter).count)

	prometheus.Unregister(responseLatency)
}

func TestMetricTest(t *testing.T) {
	metricTestDeviation = &mockGauge{}

	actual := &bytes.Buffer{}

	writeInterval := 500 * time.Millisecond

	mr := &mockReader{}
	maxWait := 50 * time.Millisecond
	metricTestRange := 30 * time.Second
	//We set the prune interval timer to a huge value here so that it never runs, instead we call spotCheckEntries manually below
	c := NewComparator(actual, maxWait, 50*time.Hour, 0, 0, 4*time.Hour, 10*time.Minute, metricTestRange, writeInterval, 1, make(chan time.Time), make(chan time.Time), mr, false)
	// Force the start time to a known value
	c.startTime = time.Unix(10, 0)

	// Run test at time 20s which is 10s after start
	mr.countOverTime = float64((10 * time.Second).Milliseconds()) / float64(writeInterval.Milliseconds())
	c.metricTest(time.Unix(0, 20*time.Second.Nanoseconds()))
	// We want to look back 30s but have only been running from time 10s to time 20s so the query range should be adjusted to 10s
	assert.Equal(t, "10s", mr.queryRange)
	// Should be no deviation we set countOverTime to the runtime/writeinterval which should be what metrictTest expected
	assert.Equal(t, float64(0), metricTestDeviation.(*mockGauge).val)

	// Run test at time 30s which is 20s after start
	mr.countOverTime = float64((20 * time.Second).Milliseconds()) / float64(writeInterval.Milliseconds())
	c.metricTest(time.Unix(0, 30*time.Second.Nanoseconds()))
	// We want to look back 30s but have only been running from time 10s to time 20s so the query range should be adjusted to 10s
	assert.Equal(t, "20s", mr.queryRange)
	// Gauge should be equal to the countOverTime value
	assert.Equal(t, float64(0), metricTestDeviation.(*mockGauge).val)

	// Run test 60s after start, we should now be capping the query range to 30s and expecting only 30s of counts
	mr.countOverTime = float64((30 * time.Second).Milliseconds()) / float64(writeInterval.Milliseconds())
	c.metricTest(time.Unix(0, 60*time.Second.Nanoseconds()))
	// We want to look back 30s but have only been running from time 10s to time 20s so the query range should be adjusted to 10s
	assert.Equal(t, "30s", mr.queryRange)
	// Gauge should be equal to the countOverTime value
	assert.Equal(t, float64(0), metricTestDeviation.(*mockGauge).val)

	prometheus.Unregister(responseLatency)
}

type mockCounter struct {
	cLck  sync.Mutex
	count int
}

func (m *mockCounter) Desc() *prometheus.Desc {
	panic("implement me")
}

func (m *mockCounter) Write(*io_prometheus_client.Metric) error {
	panic("implement me")
}

func (m *mockCounter) Describe(chan<- *prometheus.Desc) {
	panic("implement me")
}

func (m *mockCounter) Collect(chan<- prometheus.Metric) {
	panic("implement me")
}

func (m *mockCounter) Add(float64) {
	panic("implement me")
}

func (m *mockCounter) Inc() {
	m.cLck.Lock()
	defer m.cLck.Unlock()
	m.count++
}

type mockGauge struct {
	cLck sync.Mutex
	val  float64
}

func (m *mockGauge) Desc() *prometheus.Desc {
	panic("implement me")
}

func (m *mockGauge) Write(*io_prometheus_client.Metric) error {
	panic("implement me")
}

func (m *mockGauge) Describe(chan<- *prometheus.Desc) {
	panic("implement me")
}

func (m *mockGauge) Collect(chan<- prometheus.Metric) {
	panic("implement me")
}

func (m *mockGauge) Set(v float64) {
	m.cLck.Lock()
	m.val = v
	m.cLck.Unlock()
}

func (m *mockGauge) Inc() {
	panic("implement me")
}

func (m *mockGauge) Dec() {
	panic("implement me")
}

func (m *mockGauge) Add(float64) {
	panic("implement me")
}

func (m *mockGauge) Sub(float64) {
	panic("implement me")
}

func (m *mockGauge) SetToCurrentTime() {
	panic("implement me")
}

type mockReader struct {
	resp          []time.Time
	countOverTime float64
	queryRange    string
}

func (r *mockReader) Query(start time.Time, end time.Time) ([]time.Time, error) {
	return r.resp, nil
}

func (r *mockReader) QueryCountOverTime(queryRange string) (float64, error) {
	r.queryRange = queryRange
	return r.countOverTime, nil
}
