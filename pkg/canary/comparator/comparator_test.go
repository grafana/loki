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
	c := NewComparator(actual, 1*time.Hour, 1*time.Hour, 1, make(chan time.Time), make(chan time.Time), nil, false)

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
	c := NewComparator(actual, 1*time.Hour, 1*time.Hour, 1, make(chan time.Time), make(chan time.Time), nil, false)

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
	c := NewComparator(actual, 1*time.Hour, 1*time.Hour, 1, make(chan time.Time), make(chan time.Time), nil, false)

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

	mr := &mockReader{found}
	maxWait := 50 * time.Millisecond
	//We set the prune interval timer to a huge value here so that it never runs, instead we call pruneEntries manually below
	c := NewComparator(actual, maxWait, 50*time.Hour, 1, make(chan time.Time), make(chan time.Time), mr, false)

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
	c := NewComparator(actual, maxWait, 50*time.Hour, 1, make(chan time.Time), make(chan time.Time), nil, false)

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

type mockReader struct {
	resp []time.Time
}

func (r *mockReader) Query(start time.Time, end time.Time) ([]time.Time, error) {
	return r.resp, nil
}
