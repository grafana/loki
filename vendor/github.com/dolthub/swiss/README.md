# SwissMap

SwissMap is a hash table adapated from the "SwissTable" family of hash tables from [Abseil](https://abseil.io/blog/20180927-swisstables). It uses [AES](https://github.com/dolthub/maphash) instructions for fast-hashing and performs key lookups in parallel using [SSE](https://en.wikipedia.org/wiki/Streaming_SIMD_Extensions) instructions. Because of these optimizations, SwissMap is faster and more memory efficient than Golang's built-in `map`. If you'd like to learn more about its design and implementation, check out this [blog post](https://www.dolthub.com/blog/2023-03-28-swiss-map/) announcing its release.


## Example

SwissMap exposes the same interface as the built-in `map`. Give it a try using this [Go playground](https://go.dev/play/p/JPDC5WhYN7g).

```go
package main

import "github.com/dolthub/swiss"

func main() {
	m := swiss.NewMap[string, int](42)

	m.Put("foo", 1)
	m.Put("bar", 2)

	m.Iter(func(k string, v int) (stop bool) {
		println("iter", k, v)
		return false // continue
	})

	if x, ok := m.Get("foo"); ok {
		println(x)
	}
	if m.Has("bar") {
		x, _ := m.Get("bar")
		println(x)
	}

	m.Put("foo", -1)
	m.Delete("bar")

	if x, ok := m.Get("foo"); ok {
		println(x)
	}
	if m.Has("bar") {
		x, _ := m.Get("bar")
		println(x)
	}

	m.Clear()

	// Output:
	// iter foo 1
	// iter bar 2
	// 1
	// 2
	// -1
}
```
