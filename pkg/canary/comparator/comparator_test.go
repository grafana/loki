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
	c := NewComparator(actual, 1*time.Hour, 1*time.Hour, 1*time.Hour, 15*time.Minute, 4*time.Hour, 4*time.Hour, 0, 1*time.Minute, 0, 1*time.Hour, 3*time.Hour, 30*time.Minute, 0, 1, make(chan time.Time), make(chan time.Time), nil, false)

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
	c := NewComparator(actual, 1*time.Hour, 1*time.Hour, 1*time.Hour, 15*time.Minute, 4*time.Hour, 4*time.Hour, 0, 1*time.Minute, 0, 1*time.Hour, 3*time.Hour, 30*time.Minute, 0, 1, make(chan time.Time), make(chan time.Time), nil, false)

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
	c := NewComparator(actual, 1*time.Hour, 1*time.Hour, 1*time.Hour, 15*time.Minute, 4*time.Hour, 4*time.Hour, 0, 1*time.Minute, 0, 1*time.Hour, 3*time.Hour, 30*time.Minute, 0, 1, make(chan time.Time), make(chan time.Time), nil, false)

	t1 := time.Unix(0, 0)
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

	t1 := time.Unix(10, 0)
	t2 := time.Unix(20, 0)
	t3 := time.Unix(30, 0)
	t4 := time.Unix(40, 0)
	t5 := time.Unix(50, 0)

	found := []time.Time{t1, t3, t4, t5}

	mr := &mockReader{resp: found}
	wait := 60 * time.Second
	maxWait := 300 * time.Second
	//We set the prune interval timer to a huge value here so that it never runs, instead we call pruneEntries manually below
	c := NewComparator(actual, wait, maxWait, 50*time.Hour, 15*time.Minute, 4*time.Hour, 4*time.Hour, 0, 1*time.Minute, 0, 1*time.Hour, 3*time.Hour, 30*time.Minute, 0, 1, make(chan time.Time), make(chan time.Time), mr, false)

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

	//Set the time to 120s, this would be more than one wait (60s) and enough to go looking for the missing entries
	c1Time := time.Unix(120, 0)
	c.pruneEntries(c1Time)
	assert.Equal(t, 1, len(c.missingEntries)) // One of the entries was found so only one should be missing

	//Now set the time to 2x maxWait which should guarnatee we stopped looking for the other missing entry
	c2Time := t1.Add(2 * maxWait)
	c.pruneEntries(c2Time)

	expected := fmt.Sprintf(ErrOutOfOrderEntry+ErrOutOfOrderEntry+ // 1 Out of order because we missed entries
		ErrEntryNotReceivedWs+ErrEntryNotReceivedWs+ // 2 Log that entries weren't received over websocket
		DebugWebsocketMissingEntry+DebugWebsocketMissingEntry+ // 3 List entries we are missing
		DebugQueryResult+DebugQueryResult+DebugQueryResult+DebugQueryResult+ // 4 List entries we got back from Loki
		DebugEntryFound+ // 5 We log when t4 was found on followup query
		DebugWebsocketMissingEntry+ // 6 Log missing entries on second run of pruneEntries
		DebugQueryResult+DebugQueryResult+DebugQueryResult+DebugQueryResult+ // 7 Because we call pruneEntries twice we get the confirmation query results back twice
		ErrEntryNotReceived, // 8 List entry we never received and is missing completely

		t3, []time.Time{t2}, t5, []time.Time{t2, t4}, // 1 Out of order entry params
		t2.UnixNano(), wait.Seconds(), t4.UnixNano(), wait.Seconds(), // 2 Entry not received over websocket params
		t2.UnixNano(), t4.UnixNano(), // 3 Missing entries
		t1.UnixNano(), t3.UnixNano(), t4.UnixNano(), t5.UnixNano(), // 4 Confirmation query results first run
		t4.UnixNano(), c1Time.Sub(t4).Seconds(), // 5 t4 Entry found in follow up query
		t2.UnixNano(),                                              // 6 Missing Entry
		t1.UnixNano(), t3.UnixNano(), t4.UnixNano(), t5.UnixNano(), // 7 Confirmation query results second run
		t2.UnixNano(), maxWait.Seconds()) // 8 Entry never found

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

// Ensure that if confirmMissing calls pile up and run concurrently, this doesn't cause a panic
func TestConcurrentConfirmMissing(t *testing.T) {
	found := []time.Time{
		time.Unix(0, 0),
		time.UnixMilli(1),
		time.UnixMilli(2),
	}
	mr := &mockReader{resp: found}

	output := &bytes.Buffer{}

	wait := 30 * time.Millisecond
	maxWait := 30 * time.Millisecond

	c := NewComparator(output, wait, maxWait, 50*time.Hour, 15*time.Minute, 4*time.Hour, 4*time.Hour, 0, 1*time.Minute, 0, 1*time.Hour, 3*time.Hour, 30*time.Minute, 0, 1, make(chan time.Time), make(chan time.Time), mr, false)

	for _, t := range found {
		tCopy := t
		c.missingEntries = append(c.missingEntries, &tCopy)
	}

	wg := sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NotPanics(t, func() { c.confirmMissing(time.UnixMilli(3)) })
		}()
	}
	assert.Eventually(
		t,
		func() bool {
			wg.Wait()
			return true
		},
		time.Second,
		10*time.Millisecond,
	)
}

func TestPruneAckdEntires(t *testing.T) {
	actual := &bytes.Buffer{}
	wait := 30 * time.Millisecond
	maxWait := 30 * time.Millisecond
	//We set the prune interval timer to a huge value here so that it never runs, instead we call pruneEntries manually below
	c := NewComparator(actual, wait, maxWait, 50*time.Hour, 15*time.Minute, 4*time.Hour, 4*time.Hour, 0, 1*time.Minute, 0, 1*time.Hour, 3*time.Hour, 30*time.Minute, 0, 1, make(chan time.Time), make(chan time.Time), nil, false)

	t1 := time.Unix(0, 0)
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
	// the fourth should still remain because its much much newer and we only prune things older than wait
	c.pruneEntries(t1.Add(2 * maxWait))

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
	spotCheck := 10 * time.Millisecond
	spotCheckMax := 20 * time.Millisecond
	//We set the prune interval timer to a huge value here so that it never runs, instead we call spotCheckEntries manually below
	c := NewComparator(actual, 1*time.Hour, 1*time.Hour, 50*time.Hour, spotCheck, spotCheckMax, 4*time.Hour, 3*time.Millisecond, 1*time.Minute, 0, 1*time.Hour, 3*time.Hour, 30*time.Minute, 0, 1, make(chan time.Time), make(chan time.Time), mr, false)

	// Send all the entries
	for i := range entries {
		c.entrySent(entries[i])
	}

	// Should include the following entries
	assert.Equal(t, 3, len(c.spotCheck))
	assert.Equal(t, time.Unix(0, 0), *c.spotCheck[0])
	assert.Equal(t, time.Unix(0, 10*time.Millisecond.Nanoseconds()), *c.spotCheck[1])
	assert.Equal(t, time.Unix(0, 20*time.Millisecond.Nanoseconds()), *c.spotCheck[2])

	// Run with "current time" 1ms after start which is less than spotCheckWait so nothing should be checked
	c.spotCheckEntries(time.Unix(0, 2*time.Millisecond.Nanoseconds()))
	assert.Equal(t, 3, len(c.spotCheck))
	assert.Equal(t, 0, spotCheckEntries.(*mockCounter).count)

	// Run with "current time" at 25ms, the first entry should be pruned, the second entry should be found, and the last entry should come back as missing
	c.spotCheckEntries(time.Unix(0, 25*time.Millisecond.Nanoseconds()))

	// First entry should have been pruned, second and third entries have not expired yet
	assert.Equal(t, 2, len(c.spotCheck))

	expected := fmt.Sprintf(ErrSpotCheckEntryNotReceived, // List entry not received from Loki
		entries[20].UnixNano(), "5ms")

	// We didn't send the last entry and our initial counter did not start at 0 so we should get back entries 1-19
	for i := 1; i < 20; i++ {
		expected = expected + fmt.Sprintf(DebugQueryResult, entries[i].UnixNano())
	}

	assert.Equal(t, expected, actual.String())

	assert.Equal(t, 2, spotCheckEntries.(*mockCounter).count)
	assert.Equal(t, 1, spotCheckMissing.(*mockCounter).count)

	prometheus.Unregister(responseLatency)
}

func TestCacheTest(t *testing.T) {
	actual := &bytes.Buffer{}
	mr := &mockReader{}
	now := time.Now()
	cacheTestInterval := 500 * time.Millisecond
	cacheTestRange := 30 * time.Second
	cacheTestNow := 2 * time.Second

	c := NewComparator(actual, 1*time.Hour, 1*time.Hour, 50*time.Hour, 0, 0, 4*time.Hour, 0, 10*time.Minute, 0, cacheTestInterval, cacheTestRange, cacheTestNow, 1*time.Hour, 1, make(chan time.Time), make(chan time.Time), mr, false)
	// Force the start time to a known value
	c.startTime = time.Unix(10, 0)

	queryResultsDiff = &mockCounter{}
	mr.countOverTime = 2.3
	mr.noCacheCountOvertime = mr.countOverTime // same value for both with and without cache
	c.cacheTest(now)
	assert.Equal(t, 0, queryResultsDiff.(*mockCounter).count)

	queryResultsDiff = &mockCounter{} // reset counter
	mr.countOverTime = 2.3            // value not important
	mr.noCacheCountOvertime = 2.5     // different than `countOverTime` value.
	c.cacheTest(now)
	assert.Equal(t, 1, queryResultsDiff.(*mockCounter).count)

	queryResultsDiff = &mockCounter{}    // reset counter
	mr.countOverTime = 2.3               // value not important
	mr.noCacheCountOvertime = 2.30000005 // different than `countOverTime` value but within tolerance
	c.cacheTest(now)
	assert.Equal(t, 0, queryResultsDiff.(*mockCounter).count)

	// This avoids a panic on subsequent test execution,
	// seems ugly but was easy, and multiple instantiations
	// of the comparator should be an error
	prometheus.Unregister(responseLatency)
}

func TestMetricTest(t *testing.T) {
	metricTestActual = &mockGauge{}
	metricTestExpected = &mockGauge{}

	actual := &bytes.Buffer{}

	writeInterval := 500 * time.Millisecond

	mr := &mockReader{}
	metricTestRange := 30 * time.Second
	//We set the prune interval timer to a huge value here so that it never runs, instead we call spotCheckEntries manually below
	c := NewComparator(actual, 1*time.Hour, 1*time.Hour, 50*time.Hour, 0, 0, 4*time.Hour, 0, 10*time.Minute, metricTestRange, 1*time.Hour, 3*time.Hour, 30*time.Minute, writeInterval, 1, make(chan time.Time), make(chan time.Time), mr, false)
	// Force the start time to a known value
	c.startTime = time.Unix(10, 0)

	// Run test at time 20s which is 10s after start
	mr.countOverTime = float64((10 * time.Second).Milliseconds()) / float64(writeInterval.Milliseconds())
	c.metricTest(time.Unix(0, 20*time.Second.Nanoseconds()))
	// We want to look back 30s but have only been running from time 10s to time 20s so the query range should be adjusted to 10s
	assert.Equal(t, "10s", mr.queryRange)
	// Should be no deviation we set countOverTime to the runtime/writeinterval which should be what metrictTest expected
	assert.Equal(t, float64(20), metricTestExpected.(*mockGauge).val)
	assert.Equal(t, float64(20), metricTestActual.(*mockGauge).val)

	// Run test at time 30s which is 20s after start
	mr.countOverTime = float64((20 * time.Second).Milliseconds()) / float64(writeInterval.Milliseconds())
	c.metricTest(time.Unix(0, 30*time.Second.Nanoseconds()))
	// We want to look back 30s but have only been running from time 10s to time 20s so the query range should be adjusted to 10s
	assert.Equal(t, "20s", mr.queryRange)
	// Gauge should be equal to the countOverTime value
	assert.Equal(t, float64(40), metricTestExpected.(*mockGauge).val)
	assert.Equal(t, float64(40), metricTestActual.(*mockGauge).val)

	// Run test 60s after start, we should now be capping the query range to 30s and expecting only 30s of counts
	mr.countOverTime = float64((30 * time.Second).Milliseconds()) / float64(writeInterval.Milliseconds())
	c.metricTest(time.Unix(0, 60*time.Second.Nanoseconds()))
	// We want to look back 30s but have only been running from time 10s to time 20s so the query range should be adjusted to 10s
	assert.Equal(t, "30s", mr.queryRange)
	// Gauge should be equal to the countOverTime value
	assert.Equal(t, float64(60), metricTestExpected.(*mockGauge).val)
	assert.Equal(t, float64(60), metricTestActual.(*mockGauge).val)

	prometheus.Unregister(responseLatency)
}

func Test_pruneList(t *testing.T) {
	t1 := time.Unix(0, 0)
	t2 := time.Unix(1, 0)
	t3 := time.Unix(2, 0)
	t4 := time.Unix(3, 0)
	t5 := time.Unix(4, 0)

	list := []*time.Time{&t1, &t2, &t3, &t4, &t5}

	outList := pruneList(list, func(_ int, ts *time.Time) bool {
		// Sorry t2, nobody likes you
		return ts.Equal(t2)
	}, func(i int, ts *time.Time) {
		assert.Equal(t, 1, i)
		assert.Equal(t, t2, *ts)
	})

	assert.Equal(t, []*time.Time{&t1, &t3, &t4, &t5}, outList)
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

type mockCounterVec struct {
	mockCounter
	labels []string
}

func (m *mockCounterVec) WithLabelValues(lvs ...string) prometheus.Counter {
	m.labels = lvs
	return &m.mockCounter
}

func (m *mockCounterVec) Desc() *prometheus.Desc {
	panic("implement me")
}

func (m *mockCounterVec) Write(*io_prometheus_client.Metric) error {
	panic("implement me")
}

func (m *mockCounterVec) Describe(chan<- *prometheus.Desc) {
	panic("implement me")
}

func (m *mockCounterVec) Collect(chan<- prometheus.Metric) {
	panic("implement me")
}

func (m *mockCounterVec) Add(float64) {
	panic("implement me")
}

func (m *mockCounterVec) Inc() {
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

	// return this value if called without cache.
	noCacheCountOvertime float64
}

func (r *mockReader) Query(_ time.Time, _ time.Time) ([]time.Time, error) {
	return r.resp, nil
}

func (r *mockReader) QueryCountOverTime(queryRange string, _ time.Time, cache bool) (float64, error) {
	r.queryRange = queryRange
	res := r.countOverTime
	if !cache {
		res = r.noCacheCountOvertime
	}
	return res, nil
}
