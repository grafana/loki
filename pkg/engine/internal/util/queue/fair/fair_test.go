package fair_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/util/queue/fair"
)

func Example() {
	// task is a basic representation of a task to run.
	type task struct{}

	var q fair.Queue[task]

	// Define scopes for us to add tasks to.
	_ = q.RegisterScope(fair.Scope{"tenant-a", "user-a"})
	_ = q.RegisterScope(fair.Scope{"tenant-a", "user-b"})
	_ = q.RegisterScope(fair.Scope{"tenant-b", "user-c"})
	_ = q.RegisterScope(fair.Scope{"tenant-c", "user-d"})

	// Push some tasks into the queue.
	_ = q.Push(fair.Scope{"tenant-a", "user-a"}, task{}) // task-1
	_ = q.Push(fair.Scope{"tenant-a", "user-a"}, task{}) // task-2
	_ = q.Push(fair.Scope{"tenant-a", "user-b"}, task{}) // task-3
	_ = q.Push(fair.Scope{"tenant-b", "user-c"}, task{}) // task-4
	_ = q.Push(fair.Scope{"tenant-b", "user-c"}, task{}) // task-5
	_ = q.Push(fair.Scope{"tenant-c", "user-d"}, task{}) // task-6

	// Fairness is achieved by lowering the priority along the entire path of a
	// task that was just executed.
	//
	// That means, for example, a task for tenant-a/user-a lowers the priority
	// of all tasks for both tenant-a and tenant-a/user-a. However, this does
	// not directly touch the priority of tenant-a/user-b; so the next time a
	// tenant-a task is selected, it will prioritize tenant-a/user-b over
	// tenant-a/user-a.
	for {
		_, scope := q.Pop()
		if scope == nil {
			break
		}
		fmt.Println(scope)
		_ = q.AdjustScope(scope, 1) // Maintain fairness
	}

	// Output:
	// tenant-a/user-a
	// tenant-b/user-c
	// tenant-c/user-d
	// tenant-a/user-b
	// tenant-b/user-c
	// tenant-a/user-a
}

func TestQueue_RegisterScope(t *testing.T) {
	t.Run("can register a new scope", func(t *testing.T) {
		var q fair.Queue[string]
		require.NoError(t, q.RegisterScope(fair.Scope{"queue-a"}))
	})

	t.Run("can register a nested scope", func(t *testing.T) {
		var q fair.Queue[string]
		require.NoError(t, q.RegisterScope(fair.Scope{"queue-a"}))
		require.NoError(t, q.RegisterScope(fair.Scope{"queue-a", "nested"}))
	})

	t.Run("cannot register an existing scope", func(t *testing.T) {
		var q fair.Queue[string]

		require.NoError(t, q.RegisterScope(fair.Scope{"queue-a", "nested"}))
		require.ErrorIs(t, q.RegisterScope(fair.Scope{"queue-a", "nested"}), fair.ErrScopeExists)
	})
}

func TestQueue_UnregisterScope(t *testing.T) {
	t.Run("cannot unregister a non-existent scope", func(t *testing.T) {
		var q fair.Queue[string]

		require.ErrorIs(t, q.UnregisterScope(fair.Scope{"queue-a"}), fair.ErrNotFound)
	})

	t.Run("can unregister a registered scope", func(t *testing.T) {
		var q fair.Queue[string]

		require.NoError(t, q.RegisterScope(fair.Scope{"queue-a"}))
		require.NoError(t, q.UnregisterScope(fair.Scope{"queue-a"}))
		require.ErrorIs(t, q.UnregisterScope(fair.Scope{"queue-a"}), fair.ErrNotFound)
	})

	t.Run("should not unregister parents with empty registered children", func(t *testing.T) {
		var q fair.Queue[string]

		var (
			scopeA = fair.Scope{"tenant-a", "queue-a"}
			scopeB = fair.Scope{"tenant-a", "queue-b"}
		)

		require.NoError(t, q.RegisterScope(scopeA))
		require.NoError(t, q.RegisterScope(scopeB))

		require.NoError(t, q.Push(scopeA, "hello"))
		require.NoError(t, q.Push(scopeB, "world"))

		for {
			_, s := q.Pop()
			if s == nil {
				break
			}
		}

		// Unregister scope a.
		//
		// This previously caused a bug where unregistering scope a would also
		// cause the empty scope b to be incorrectly removed.
		require.NoError(t, q.UnregisterScope(scopeA))

		// Push a new value to scope B.
		require.NoError(t, q.Push(scopeB, "!"))

		actualValue, actualScope := q.Pop()
		assert.Equal(t, "!", actualValue)
		assert.Equal(t, scopeB, actualScope)
	})
}

func TestQueue_Push(t *testing.T) {
	t.Run("cannot push values in unregistered scope", func(t *testing.T) {
		var q fair.Queue[string]

		require.ErrorIs(t, q.Push(fair.Scope{"tenant-a"}, "John"), fair.ErrNotFound)
	})

	t.Run("can push new values", func(t *testing.T) {
		var q fair.Queue[string]
		require.NoError(t, q.RegisterScope(fair.Scope{"tenant-a"}))
		require.NoError(t, q.RegisterScope(fair.Scope{"tenant-b"}))

		require.NoError(t, q.Push(fair.Scope{"tenant-a"}, "John"))
		require.NoError(t, q.Push(fair.Scope{"tenant-b"}, "John"))
	})

	t.Run("can push duplicate values", func(t *testing.T) {
		var q fair.Queue[string]
		require.NoError(t, q.RegisterScope(fair.Scope{"tenant-a"}))

		require.NoError(t, q.Push(fair.Scope{"tenant-a"}, "John"))
		require.NoError(t, q.Push(fair.Scope{"tenant-a"}, "John"))
	})

	t.Run("can enqueue values in scopes with children scopes", func(t *testing.T) {
		var q fair.Queue[string]
		require.NoError(t, q.RegisterScope(fair.Scope{"tenant-a"}))
		require.NoError(t, q.RegisterScope(fair.Scope{"tenant-a", "user-a"}))

		require.NoError(t, q.Push(fair.Scope{"tenant-a"}, "John"))
		require.NoError(t, q.Push(fair.Scope{"tenant-a", "user-a"}, "John"))
	})
}

func TestQueue_Peek(t *testing.T) {
	t.Run("returns nil for empty queue", func(t *testing.T) {
		var q fair.Queue[string]

		_, s := q.Peek()
		require.Nil(t, s)
	})

	t.Run("returns an added element", func(t *testing.T) {
		var q fair.Queue[string]
		require.NoError(t, q.RegisterScope(fair.Scope{"tenant-a", "key-a"}))
		require.NoError(t, q.Push(fair.Scope{"tenant-a", "key-a"}, "John"))

		v, s := q.Peek()
		require.Equal(t, "John", v)
		require.Equal(t, fair.Scope{"tenant-a", "key-a"}, s)
	})

	t.Run("returns the first added element", func(t *testing.T) {
		var q fair.Queue[string]
		require.NoError(t, q.RegisterScope(fair.Scope{"tenant-a", "subpath", "key-a"}))
		require.NoError(t, q.RegisterScope(fair.Scope{"tenant-a", "key-a"}))
		require.NoError(t, q.RegisterScope(fair.Scope{"key-a"}))

		require.NoError(t, q.Push(fair.Scope{"tenant-a", "subpath", "key-a"}, "Peter"))
		require.NoError(t, q.Push(fair.Scope{"tenant-a", "key-a"}, "Paul"))
		require.NoError(t, q.Push(fair.Scope{"key-a"}, "Mary"))

		v, s := q.Peek()
		require.Equal(t, "Peter", v)
		require.Equal(t, fair.Scope{"tenant-a", "subpath", "key-a"}, s)
	})
}

func TestQueue_Pop(t *testing.T) {
	t.Run("returns nil for empty queue", func(t *testing.T) {
		var q fair.Queue[string]

		_, s := q.Pop()
		require.Nil(t, s)
	})

	t.Run("returns elements in push order", func(t *testing.T) {
		type element struct {
			value string
			scope fair.Scope
		}

		elements := []element{
			{"John", fair.Scope{"tenant-a", "key-a"}},
			{"Peter", fair.Scope{"tenant-a", "subpath", "key-a"}},
			{"Paul", fair.Scope{"tenant-a", "key-b"}},
			{"Mary", fair.Scope{"key-a"}},
		}

		var q fair.Queue[string]

		for _, e := range elements {
			_ = q.RegisterScope(e.scope)
			require.NoError(t, q.Push(e.scope, e.value))
		}

		for _, expect := range elements {
			actualValue, actualPath := q.Pop()

			require.Equal(t, expect.value, actualValue)
			require.Equal(t, expect.scope, actualPath)
		}

		// One more pop and the queue should be empty.
		_, s := q.Pop()
		require.Nil(t, s)
	})
}

func TestQueue_Adjust(t *testing.T) {
	t.Run("fails on unregistered scope", func(t *testing.T) {
		var q fair.Queue[string]

		require.ErrorIs(t, q.AdjustScope(fair.Scope{"tenant-a", "key-a"}, 1), fair.ErrNotFound)
	})

	t.Run("succeeds on registered scope", func(t *testing.T) {
		var q fair.Queue[string]
		require.NoError(t, q.RegisterScope(fair.Scope{"tenant-a", "key-a"}))
		require.NoError(t, q.AdjustScope(fair.Scope{"tenant-a", "key-a"}, 1))
	})

	t.Run("can lower priority of scope", func(t *testing.T) {
		var q fair.Queue[string]
		require.NoError(t, q.RegisterScope(fair.Scope{"tenant-a", "key-a"}))
		require.NoError(t, q.RegisterScope(fair.Scope{"tenant-a", "key-b"}))

		require.NoError(t, q.Push(fair.Scope{"tenant-a", "key-a"}, "Peter"))
		require.NoError(t, q.Push(fair.Scope{"tenant-a", "key-b"}, "Paul"))

		// Positive cost gives a key a lower priority.
		require.NoError(t, q.AdjustScope(fair.Scope{"tenant-a", "key-a"}, 1))

		v, s := q.Pop()
		require.Equal(t, "Paul", v)
		require.Equal(t, fair.Scope{"tenant-a", "key-b"}, s)
	})

	t.Run("can bump priority of existing element", func(t *testing.T) {
		var q fair.Queue[string]
		require.NoError(t, q.RegisterScope(fair.Scope{"tenant-a", "key-a"}))
		require.NoError(t, q.RegisterScope(fair.Scope{"tenant-a", "key-b"}))

		require.NoError(t, q.Push(fair.Scope{"tenant-a", "key-a"}, "Peter"))
		require.NoError(t, q.Push(fair.Scope{"tenant-a", "key-b"}, "Paul"))

		// Negative cost gives a key a higher priority.
		require.NoError(t, q.AdjustScope(fair.Scope{"tenant-a", "key-b"}, -1))

		v, s := q.Pop()
		require.Equal(t, "Paul", v)
		require.Equal(t, fair.Scope{"tenant-a", "key-b"}, s)
	})
}

func TestQueue_Len(t *testing.T) {
	t.Run("starts at zero", func(t *testing.T) {
		var q fair.Queue[string]
		require.Equal(t, 0, q.Len())
	})

	t.Run("changes on push/pop", func(t *testing.T) {
		var q fair.Queue[string]

		require.NoError(t, q.RegisterScope(fair.Scope{"tenant-a", "key-a"}))
		require.NoError(t, q.Push(fair.Scope{"tenant-a", "key-a"}, "foo"))

		require.Equal(t, 1, q.Len())
		q.Pop()
		require.Equal(t, 0, q.Len())
	})

	t.Run("unchanged by empty pop", func(t *testing.T) {
		var q fair.Queue[string]

		_, scope := q.Pop()
		require.Nil(t, scope, "pop was not empty")
		require.Equal(t, 0, q.Len())
	})

	t.Run("decremented by reachable items on UnregisterScope", func(t *testing.T) {
		var q fair.Queue[int]

		var (
			scopeA = fair.Scope{"tenant-a", "queue-a"}
			scopeB = fair.Scope{"tenant-a", "queue-b"}
		)

		require.NoError(t, q.RegisterScope(scopeA))
		require.NoError(t, q.RegisterScope(scopeB))

		for i := range 10 {
			require.NoError(t, q.Push(scopeA, i))
			require.NoError(t, q.Push(scopeB, i))
		}
		require.Equal(t, 20, q.Len())

		// Unregister the parent scope, which unregisters all children.
		require.NoError(t, q.UnregisterScope(fair.Scope{"tenant-a"}))
		require.Equal(t, 0, q.Len())
	})
}

func TestQueue_EdgeCases(t *testing.T) {
	// Test an edge case where an empty scope retained its original rank when
	// being revived, causing it to jump the line incorrectly.
	t.Run("revived scopes do not retain previous low rank", func(t *testing.T) {
		var q fair.Queue[string]

		var (
			scopeA = fair.Scope{"tenant-a", "queue-a"}
			scopeB = fair.Scope{"tenant-a", "queue-b"}
			scopeC = fair.Scope{"tenant-a", "queue-c"}
		)

		require.NoError(t, q.RegisterScope(scopeA))
		require.NoError(t, q.RegisterScope(scopeB))
		require.NoError(t, q.RegisterScope(scopeC))

		// Give scopes initial ranks so that scopeA > scopeB > scopeC in
		// priority.
		require.NoError(t, q.AdjustScope(scopeA, 100))
		require.NoError(t, q.AdjustScope(scopeB, 150))
		require.NoError(t, q.AdjustScope(scopeC, 200))

		// Push values to scopeB/scopeC, marking them as active.
		require.NoError(t, q.Push(scopeB, "hello"))
		require.NoError(t, q.Push(scopeC, "!"))

		// Finally, push a value to scopeA to mark it as active.
		//
		// This should force its rank to be adjusted based on the minimum
		// sibling rank (scopeB/150), and not retain its original rank of 100.
		require.NoError(t, q.Push(scopeA, "world"))

		var actual []string
		for {
			v, s := q.Pop()
			if s == nil {
				break
			}
			actual = append(actual, v)
		}

		require.Equal(t, []string{"hello", "world", "!"}, actual)
	})

	// Similarly to above, a scope with a very high rank should not become
	// higher priority when revived.
	t.Run("revived scopes retain previous high rank", func(t *testing.T) {
		var q fair.Queue[string]

		var (
			scopeA = fair.Scope{"tenant-a", "queue-a"}
			scopeB = fair.Scope{"tenant-a", "queue-b"}
			scopeC = fair.Scope{"tenant-a", "queue-c"}
		)

		require.NoError(t, q.RegisterScope(scopeA))
		require.NoError(t, q.RegisterScope(scopeB))
		require.NoError(t, q.RegisterScope(scopeC))

		// Give scopes initial ranks so that scopeA > scopeB > scopeC in
		// priority.
		require.NoError(t, q.AdjustScope(scopeA, 200))
		require.NoError(t, q.AdjustScope(scopeB, 100))
		require.NoError(t, q.AdjustScope(scopeC, 150))

		// Push values to scopeB/scopeC, marking them as active.
		require.NoError(t, q.Push(scopeB, "hello"))
		require.NoError(t, q.Push(scopeC, "world"))

		// Finally, push a value to scopeA to mark it as active.
		//
		// This should retain its high rank, rather than being reset to the
		// minimum sibling.
		require.NoError(t, q.Push(scopeA, "!"))

		var actual []string
		for {
			v, s := q.Pop()
			if s == nil {
				break
			}
			actual = append(actual, v)
		}

		require.Equal(t, []string{"hello", "world", "!"}, actual)
	})
}
