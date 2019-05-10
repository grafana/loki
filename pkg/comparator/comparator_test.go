package comparator

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestComparatorEntryReceivedOutOfOrder(t *testing.T) {
	actual := &bytes.Buffer{}
	c := NewComparator(actual, 1*time.Hour, 1*time.Hour)

	t1 := time.Now()
	t2 := t1.Add(1 * time.Second)
	t3 := t2.Add(1 * time.Second)
	t4 := t3.Add(1 * time.Second)

	c.EntrySent(t1)
	c.EntrySent(t2)
	c.EntrySent(t3)
	c.EntrySent(t4)

	c.EntryReceived(t1)
	c.EntryReceived(t4)
	expected := fmt.Sprintf(ErrOutOfOrderEntry, t4, []time.Time{t2, t3})

	assert.Equal(t, expected, actual.String())
}

func TestComparatorEntryReceivedNotExpected(t *testing.T) {
	actual := &bytes.Buffer{}
	c := NewComparator(actual, 1*time.Hour, 1*time.Hour)

	t1 := time.Now()
	t2 := t1.Add(1 * time.Second)
	t3 := t2.Add(1 * time.Second)
	t4 := t3.Add(1 * time.Second)

	c.EntrySent(t2)
	c.EntrySent(t3)
	c.EntrySent(t4)

	c.EntryReceived(t2)
	assert.Equal(t, 2, c.Size())
	c.EntryReceived(t1)
	assert.Equal(t, 2, c.Size())
	c.EntryReceived(t3)
	assert.Equal(t, 1, c.Size())
	c.EntryReceived(t4)
	assert.Equal(t, 0, c.Size())
	expected := ""

	assert.Equal(t, expected, actual.String())
}

func TestEntryNeverReceived(t *testing.T) {

	actual := &bytes.Buffer{}
	c := NewComparator(actual, 5*time.Millisecond, 2*time.Millisecond)

	t1 := time.Now()
	t2 := t1.Add(1 * time.Millisecond)
	t3 := t2.Add(1 * time.Millisecond)
	t4 := t3.Add(1 * time.Millisecond)

	c.EntrySent(t1)
	c.EntrySent(t2)
	c.EntrySent(t3)
	c.EntrySent(t4)

	assert.Equal(t, 4, c.Size())

	c.EntryReceived(t1)
	c.EntryReceived(t2)
	c.EntryReceived(t3)

	assert.Equal(t, 1, c.Size())

	<-time.After(10 * time.Millisecond)

	expected := fmt.Sprintf(ErrEntryNotReceived, t4, 5*time.Millisecond.Seconds())

	assert.Equal(t, expected, actual.String())
	assert.Equal(t, 0, c.Size())

}
