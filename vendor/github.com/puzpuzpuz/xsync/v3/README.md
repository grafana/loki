[![GoDoc reference](https://img.shields.io/badge/godoc-reference-blue.svg)](https://pkg.go.dev/github.com/puzpuzpuz/xsync/v3)
[![GoReport](https://goreportcard.com/badge/github.com/puzpuzpuz/xsync/v3)](https://goreportcard.com/report/github.com/puzpuzpuz/xsync/v3)
[![codecov](https://codecov.io/gh/puzpuzpuz/xsync/branch/main/graph/badge.svg)](https://codecov.io/gh/puzpuzpuz/xsync)

# xsync

Concurrent data structures for Go. Aims to provide more scalable alternatives for some of the data structures from the standard `sync` package, but not only.

Covered with tests following the approach described [here](https://puzpuzpuz.dev/testing-concurrent-code-for-fun-and-profit).

## Benchmarks

Benchmark results may be found [here](BENCHMARKS.md). I'd like to thank [@felixge](https://github.com/felixge) who kindly ran the benchmarks on a beefy multicore machine.

Also, a non-scientific, unfair benchmark comparing Java's [j.u.c.ConcurrentHashMap](https://docs.oracle.com/en/java/javase/17/docs/api/java.base/java/util/concurrent/ConcurrentHashMap.html) and `xsync.MapOf` is available [here](https://puzpuzpuz.dev/concurrent-map-in-go-vs-java-yet-another-meaningless-benchmark).

## Usage

The latest xsync major version is v3, so `/v3` suffix should be used when importing the library:

```go
import (
	"github.com/puzpuzpuz/xsync/v3"
)
```

*Note for pre-v3 users*: v1 and v2 support is discontinued, so please upgrade to v3. While the API has some breaking changes, the migration should be trivial.

### Counter

A `Counter` is a striped `int64` counter inspired by the `j.u.c.a.LongAdder` class from the Java standard library.

```go
c := xsync.NewCounter()
// increment and decrement the counter
c.Inc()
c.Dec()
// read the current value
v := c.Value()
```

Works better in comparison with a single atomically updated `int64` counter in high contention scenarios.

### Map

A `Map` is like a concurrent hash table-based map. It follows the interface of `sync.Map` with a number of valuable extensions like `Compute` or `Size`.

```go
m := xsync.NewMap()
m.Store("foo", "bar")
v, ok := m.Load("foo")
s := m.Size()
```

`Map` uses a modified version of Cache-Line Hash Table (CLHT) data structure: https://github.com/LPD-EPFL/CLHT

CLHT is built around the idea of organizing the hash table in cache-line-sized buckets, so that on all modern CPUs update operations complete with minimal cache-line transfer. Also, `Get` operations are obstruction-free and involve no writes to shared memory, hence no mutexes or any other sort of locks. Due to this design, in all considered scenarios `Map` outperforms `sync.Map`.

One important difference with `sync.Map` is that only string keys are supported. That's because Golang standard library does not expose the built-in hash functions for `interface{}` values.

`MapOf[K, V]` is an implementation with parametrized key and value types. While it's still a CLHT-inspired hash map, `MapOf`'s design is quite different from `Map`. As a result, less GC pressure and fewer atomic operations on reads.

```go
m := xsync.NewMapOf[string, string]()
m.Store("foo", "bar")
v, ok := m.Load("foo")
```

Apart from CLHT, `MapOf` borrows ideas from Java's `j.u.c.ConcurrentHashMap` (immutable K/V pair structs instead of atomic snapshots) and C++'s `absl::flat_hash_map` (meta memory and SWAR-based lookups). It also has more dense memory layout when compared with `Map`. Long story short, `MapOf` should be preferred over `Map` when possible.

An important difference with `Map` is that `MapOf` supports arbitrary `comparable` key types:

```go
type Point struct {
	x int32
	y int32
}
m := NewMapOf[Point, int]()
m.Store(Point{42, 42}, 42)
v, ok := m.Load(point{42, 42})
```

Apart from `Range` method available for map iteration, there are also `ToPlainMap`/`ToPlainMapOf` utility functions to convert a `Map`/`MapOf` to a built-in Go's `map`:
```go
m := xsync.NewMapOf[int, int]()
m.Store(42, 42)
pm := xsync.ToPlainMapOf(m)
```

Both `Map` and `MapOf` use the built-in Golang's hash function which has DDOS protection. This means that each map instance gets its own seed number and the hash function uses that seed for hash code calculation. However, for smaller keys this hash function has some overhead. So, if you don't need DDOS protection, you may provide a custom hash function when creating a `MapOf`. For instance, Murmur3 finalizer does a decent job when it comes to integers:

```go
m := NewMapOfWithHasher[int, int](func(i int, _ uint64) uint64 {
	h := uint64(i)
	h = (h ^ (h >> 33)) * 0xff51afd7ed558ccd
	h = (h ^ (h >> 33)) * 0xc4ceb9fe1a85ec53
	return h ^ (h >> 33)
})
```

When benchmarking concurrent maps, make sure to configure all of the competitors with the same hash function or, at least, take hash function performance into the consideration.

### SPSCQueue

A `SPSCQueue` is a bounded single-producer single-consumer concurrent queue. This means that not more than a single goroutine must be publishing items to the queue while not more than a single goroutine must be consuming those items.

```go
q := xsync.NewSPSCQueue(1024)
// producer inserts an item into the queue
// optimistic insertion attempt; doesn't block
inserted := q.TryEnqueue("bar")
// consumer obtains an item from the queue
// optimistic obtain attempt; doesn't block
item, ok := q.TryDequeue() // interface{} pointing to a string
```

`SPSCQueueOf[I]` is an implementation with parametrized item type. It is available for Go 1.19 or later.

```go
q := xsync.NewSPSCQueueOf[string](1024)
inserted := q.TryEnqueue("foo")
item, ok := q.TryDequeue() // string
```

The queue is based on the data structure from this [article](https://rigtorp.se/ringbuffer). The idea is to reduce the CPU cache coherency traffic by keeping cached copies of read and write indexes used by producer and consumer respectively.

### MPMCQueue

A `MPMCQueue` is a bounded multi-producer multi-consumer concurrent queue.

```go
q := xsync.NewMPMCQueue(1024)
// producer optimistically inserts an item into the queue
// optimistic insertion attempt; doesn't block
inserted := q.TryEnqueue("bar")
// consumer obtains an item from the queue
// optimistic obtain attempt; doesn't block
item, ok := q.TryDequeue() // interface{} pointing to a string
```

`MPMCQueueOf[I]` is an implementation with parametrized item type. It is available for Go 1.19 or later.

```go
q := xsync.NewMPMCQueueOf[string](1024)
inserted := q.TryEnqueue("foo")
item, ok := q.TryDequeue() // string
```

The queue is based on the algorithm from the [MPMCQueue](https://github.com/rigtorp/MPMCQueue) C++ library which in its turn references D.Vyukov's [MPMC queue](https://www.1024cores.net/home/lock-free-algorithms/queues/bounded-mpmc-queue). According to the following [classification](https://www.1024cores.net/home/lock-free-algorithms/queues), the queue is array-based, fails on overflow, provides causal FIFO, has blocking producers and consumers.

The idea of the algorithm is to allow parallelism for concurrent producers and consumers by introducing the notion of tickets, i.e. values of two counters, one per producers/consumers. An atomic increment of one of those counters is the only noticeable contention point in queue operations. The rest of the operation avoids contention on writes thanks to the turn-based read/write access for each of the queue items.

In essence, `MPMCQueue` is a specialized queue for scenarios where there are multiple concurrent producers and consumers of a single queue running on a large multicore machine.

To get the optimal performance, you may want to set the queue size to be large enough, say, an order of magnitude greater than the number of producers/consumers, to allow producers and consumers to progress with their queue operations in parallel most of the time.

### RBMutex

A `RBMutex` is a reader-biased reader/writer mutual exclusion lock. The lock can be held by many readers or a single writer.

```go
mu := xsync.NewRBMutex()
// reader lock calls return a token
t := mu.RLock()
// the token must be later used to unlock the mutex
mu.RUnlock(t)
// writer locks are the same as in sync.RWMutex
mu.Lock()
mu.Unlock()
```

`RBMutex` is based on a modified version of BRAVO (Biased Locking for Reader-Writer Locks) algorithm: https://arxiv.org/pdf/1810.01553.pdf

The idea of the algorithm is to build on top of an existing reader-writer mutex and introduce a fast path for readers. On the fast path, reader lock attempts are sharded over an internal array based on the reader identity (a token in the case of Golang). This means that readers do not contend over a single atomic counter like it's done in, say, `sync.RWMutex` allowing for better scalability in terms of cores.

Hence, by the design `RBMutex` is a specialized mutex for scenarios, such as caches, where the vast majority of locks are acquired by readers and write lock acquire attempts are infrequent. In such scenarios, `RBMutex` should perform better than the `sync.RWMutex` on large multicore machines.

`RBMutex` extends `sync.RWMutex` internally and uses it as the "reader bias disabled" fallback, so the same semantics apply. The only noticeable difference is in the reader tokens returned from the `RLock`/`RUnlock` methods.

Apart from blocking methods, `RBMutex` also has methods for optimistic locking:
```go
mu := xsync.NewRBMutex()
if locked, t := mu.TryRLock(); locked {
	// critical reader section...
	mu.RUnlock(t)
}
if mu.TryLock() {
	// critical writer section...
	mu.Unlock()
}
```

## License

Licensed under MIT.
