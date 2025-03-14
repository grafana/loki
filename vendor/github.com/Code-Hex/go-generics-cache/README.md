# go-generics-cache

[![.github/workflows/test.yml](https://github.com/Code-Hex/go-generics-cache/actions/workflows/test.yml/badge.svg)](https://github.com/Code-Hex/go-generics-cache/actions/workflows/test.yml) [![codecov](https://codecov.io/gh/Code-Hex/go-generics-cache/branch/main/graph/badge.svg?token=Wm7UEwgiZu)](https://codecov.io/gh/Code-Hex/go-generics-cache) [![Go Reference](https://pkg.go.dev/badge/github.com/Code-Hex/go-generics-cache.svg)](https://pkg.go.dev/github.com/Code-Hex/go-generics-cache)

go-generics-cache is an in-memory key:value store/cache that is suitable for applications running on a single machine. This in-memory cache uses [Go Generics](https://go.dev/blog/generics-proposal) which is introduced in 1.18.

- a thread-safe
- implemented with [Go Generics](https://go.dev/blog/generics-proposal)
- TTL supported (with expiration times)
- Simple cache is like `map[string]interface{}`
  - See [examples](https://github.com/Code-Hex/go-generics-cache/blob/main/policy/simple/example_test.go)
- Cache replacement policies
  - **Least recently used (LRU)**
    - Discards the least recently used items first.
    - See [examples](https://github.com/Code-Hex/go-generics-cache/blob/main/policy/lru/example_test.go)
  - **Least-frequently used (LFU)**
    - Counts how often an item is needed. Those that are used least often are discarded first.
    - [An O(1) algorithm for implementing the LFU cache eviction scheme](http://dhruvbird.com/lfu.pdf)
    - See [examples](https://github.com/Code-Hex/go-generics-cache/blob/main/policy/lfu/example_test.go)
  - **First in first out (FIFO)**
    - Using this algorithm the cache behaves in the same way as a [FIFO queue](https://en.wikipedia.org/wiki/FIFO_(computing_and_electronics)).
    - The cache evicts the blocks in the order they were added, without any regard to how often or how many times they were accessed before.
	- See [examples](https://github.com/Code-Hex/go-generics-cache/blob/main/policy/fifo/example_test.go)
  - **Most recently used (MRU)**
    - In contrast to Least Recently Used (LRU), MRU discards the most recently used items first.
	- See [examples](https://github.com/Code-Hex/go-generics-cache/blob/main/policy/mru/example_test.go)
  - **Clock**
    - Clock is a more efficient version of FIFO than Second-chance cache algorithm.
	- See [examples](https://github.com/Code-Hex/go-generics-cache/blob/main/policy/clock/example_test.go)

## Requirements

Go 1.18 or later.

## Install

    $ go get github.com/Code-Hex/go-generics-cache

## Usage

See also [examples](https://github.com/Code-Hex/go-generics-cache/blob/main/example_test.go) or [go playground](https://go.dev/play/p/kDs-6wpRAcX)

```go
package main

import (
	"context"
	"fmt"
	"time"

	cache "github.com/Code-Hex/go-generics-cache"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// use simple cache algorithm without options.
	c := cache.NewContext[string, int](ctx)
	c.Set("a", 1)
	gota, aok := c.Get("a")
	gotb, bok := c.Get("b")
	fmt.Println(gota, aok) // 1 true
	fmt.Println(gotb, bok) // 0 false

	// Create a cache for Number constraint. key as string, value as int.
	nc := cache.NewNumber[string, int]()
	nc.Set("age", 26, cache.WithExpiration(time.Hour))

	incremented := nc.Increment("age", 1)
	fmt.Println(incremented) // 27

	decremented := nc.Decrement("age", 1)
	fmt.Println(decremented) // 26
}
```

## Articles

- English: [Some tips and bothers for Go 1.18 Generics](https://dev.to/codehex/some-tips-and-bothers-for-go-118-generics-lc7)
- Japanese: [Go 1.18 の Generics を使ったキャッシュライブラリを作った時に見つけた tips と微妙な点](https://zenn.dev/codehex/articles/3e6935ee6d853e)
