package queue

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLeafQueue(t *testing.T) {
	t.Run("add sub queues recursively", func(t *testing.T) {
		pathA := QueuePath([]string{"l0", "l1", "l3"})
		pathB := QueuePath([]string{"l0", "l2", "l3"})

		q := newLeafQueue(1, "root")
		require.NotNil(t, q)
		require.Equal(t, "root", q.Name())
		require.Equal(t, 0, q.Len())
		require.Equal(t, 0, q.mapping.Len())

		q.add(pathA)
		require.Equal(t, 1, q.mapping.Len())

		q.add(pathB)
		require.Equal(t, 1, q.mapping.Len())
	})

	t.Run("enqueue/dequeue to/from subqueues", func(t *testing.T) {
		/**
		root: [0]
		  a: [1]
			b: [2]
			  b0: [20]
				b1: [21]
			c: [3]
			  c0: [30]
				  c00: [300]
					c01: [301]
				c1: [31]
				  c10: [310]
					c11: [311]
		**/
		paths := []QueuePath{
			QueuePath([]string{"a"}),
			QueuePath([]string{"b", "b0"}),
			QueuePath([]string{"b", "b1"}),
			QueuePath([]string{"c", "c0", "c00"}),
			QueuePath([]string{"c", "c0", "c01"}),
			QueuePath([]string{"c", "c1", "c10"}),
			QueuePath([]string{"c", "c1", "c11"}),
		}

		q := newLeafQueue(10, "root")
		require.NotNil(t, q)
		for _, p := range paths {
			q.add(p)
		}

		require.Equal(t, 3, q.mapping.Len())

		// no items in any queues
		require.Equal(t, 0, q.Len())

		type dummyRequest struct {
			id int
		}

		r := func(id int) *dummyRequest {
			return &dummyRequest{id}
		}

		q.Chan() <- r(0)
		require.Equal(t, 1, q.Len())

		q.mapping.GetByKey("a").Chan() <- r(1)
		require.Equal(t, 2, q.Len())

		q.mapping.GetByKey("b").Chan() <- r(2)
		q.mapping.GetByKey("b").mapping.GetByKey("b0").Chan() <- r(20)
		q.mapping.GetByKey("b").mapping.GetByKey("b1").Chan() <- r(21)
		require.Equal(t, 5, q.Len())

		q.mapping.GetByKey("c").Chan() <- r(3)
		q.mapping.GetByKey("c").mapping.GetByKey("c0").Chan() <- r(30)
		q.mapping.GetByKey("c").mapping.GetByKey("c0").mapping.GetByKey("c00").Chan() <- r(300)
		q.mapping.GetByKey("c").mapping.GetByKey("c0").mapping.GetByKey("c01").Chan() <- r(301)
		q.mapping.GetByKey("c").mapping.GetByKey("c1").Chan() <- r(31)
		q.mapping.GetByKey("c").mapping.GetByKey("c1").mapping.GetByKey("c10").Chan() <- r(310)
		q.mapping.GetByKey("c").mapping.GetByKey("c1").mapping.GetByKey("c11").Chan() <- r(311)
		require.Equal(t, 12, q.Len())
		t.Log(q)

		items := make([]int, 0, q.Len())

		for q.Len() > 0 {
			r := q.Dequeue()
			if r == nil {
				continue
			}
			items = append(items, r.(*dummyRequest).id)
		}
		require.Len(t, items, 12)
		require.Equal(t, []int{0, 1, 2, 3, 20, 30, 21, 31, 300, 310, 301, 311}, items)
	})

	t.Run("dequeue ensure round-robin", func(t *testing.T) {
		/**
		root:
		  a: [100, 101, 102]
			b: [200]
			c: [300, 301]
		**/
		paths := []QueuePath{
			QueuePath([]string{"a"}),
			QueuePath([]string{"b"}),
			QueuePath([]string{"c"}),
		}

		q := newLeafQueue(10, "root")
		require.NotNil(t, q)
		for _, p := range paths {
			q.add(p)
		}

		require.Equal(t, 3, q.mapping.Len())

		// no items in any queues
		require.Equal(t, 0, q.Len())

		type dummyRequest struct {
			id int
		}

		r := func(id int) *dummyRequest {
			return &dummyRequest{id}
		}

		q.mapping.GetByKey("a").Chan() <- r(100)
		q.mapping.GetByKey("a").Chan() <- r(101)
		q.mapping.GetByKey("a").Chan() <- r(102)
		q.mapping.GetByKey("b").Chan() <- r(200)
		q.mapping.GetByKey("c").Chan() <- r(300)
		q.mapping.GetByKey("c").Chan() <- r(301)

		t.Log(q)

		items := make([]int, 0, q.Len())

		for q.Len() > 0 {
			r := q.Dequeue()
			if r == nil {
				continue
			}
			items = append(items, r.(*dummyRequest).id)
		}
		require.Len(t, items, 6)
		require.Equal(t, []int{100, 200, 300, 101, 301, 102}, items)
	})
}
