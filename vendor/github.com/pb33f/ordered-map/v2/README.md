# Ordered Maps

This repo was forked from [wk8/go-ordered-map](https://github.com/wk8/go-ordered-map) because of this: https://github.com/pb33f/libopenapi/issues/446

The [easyjson](https://github.com/mailru/easyjson) library which the wk8/go-ordered-map project depends on is now considered a **security risk** to pb33f.

So we forked it and removed the dependency on easyjson.

---

Same as regular maps, but also remembers the order in which keys were inserted, akin to [Python's `collections.OrderedDict`s](https://docs.python.org/3.7/library/collections.html#ordereddict-objects).

It offers the following features:
* optimal runtime performance (all operations are constant time)
* optimal memory usage (only one copy of values, no unnecessary memory allocation)
* allows iterating from newest or oldest keys indifferently, without memory copy, allowing to `break` the iteration, and in time linear to the number of keys iterated over rather than the total length of the ordered map
* supports any generic types for both keys and values. If you're running go < 1.18, you can use [version 1](https://github.com/wk8/go-ordered-map/tree/v1) that takes and returns generic `interface{}`s instead of using generics
* idiomatic API, akin to that of [`container/list`](https://golang.org/pkg/container/list)
* support for JSON and YAML marshalling

## Documentation

[The full documentation is available on pkg.go.dev](https://pkg.go.dev/github.com/pb33f/ordered-map).

## Installation
```bash
go get -u github.com/pb33f/ordered-map
```

Or use your favorite golang vendoring tool!

## Supported go versions

Go >= 1.23 is required to use version >= 2.2.0 of this library, as it uses generics and iterators.

if you're running go < 1.23, you can use [version 2.1.8](https://github.com/wk8/go-ordered-map/tree/v2.1.8) instead.

If you're running go < 1.18, you can use [version 1](https://github.com/wk8/go-ordered-map/tree/v1) instead.

## Example / usage

```go
package main

import (
	"fmt"
	"strings"

	"github.com/pb33f/ordered-map"
)

func main() {
	om := orderedmap.New[string, string]()

	om.Set("foo", "bar")
	om.Set("bar", "baz")
	om.Set("coucou", "toi")

	fmt.Println(om.Get("foo"))          // => "bar", true
	fmt.Println(om.Get("i dont exist")) // => "", false

	// iterating pairs from oldest to newest:
	for pair := om.Oldest(); pair != nil; pair = pair.Next() {
		fmt.Printf("%s => %s\n", pair.Key, pair.Value)
	} // prints:
	// foo => bar
	// bar => baz
	// coucou => toi

	// iterating over the 2 newest pairs:
	i := 0
	for pair := om.Newest(); pair != nil; pair = pair.Prev() {
		fmt.Printf("%s => %s\n", pair.Key, pair.Value)
		i++
		if i >= 2 {
			break
		}
	} // prints:
	// coucou => toi
	// bar => baz
	
	// removing all pairs which do not have an "o" in their key
	om.Filter(func(key, value string) bool { return strings.Contains(key, "o") })
	
	// new iteration syntax
	for key, value := range om.FromOldest() {
		fmt.Printf("%s => %s\n", key, value)
	}// prints:
	// foo => bar
	// coucou => toi
}
```

An `OrderedMap`'s keys must implement `comparable`, and its values can be anything, for example:

```go
type myStruct struct {
	payload string
}

func main() {
	om := orderedmap.New[int, *myStruct]()

	om.Set(12, &myStruct{"foo"})
	om.Set(1, &myStruct{"bar"})

	value, present := om.Get(12)
	if !present {
		panic("should be there!")
	}
	fmt.Println(value.payload) // => foo

	for pair := om.Oldest(); pair != nil; pair = pair.Next() {
		fmt.Printf("%d => %s\n", pair.Key, pair.Value.payload)
	} // prints:
	// 12 => foo
	// 1 => bar
}
```

Also worth noting that you can provision ordered maps with a capacity hint, as you would do by passing an optional hint to `make(map[K]V, capacity`):
```go
om := orderedmap.New[int, *myStruct](28)
```

You can also pass in some initial data to store in the map:
```go
om := orderedmap.New[int, string](orderedmap.WithInitialData[int, string](
	orderedmap.Pair[int, string]{
		Key:   12,
		Value: "foo",
	},
	orderedmap.Pair[int, string]{
		Key:   28,
		Value: "bar",
	},
))
```

`OrderedMap`s also support JSON serialization/deserialization, and preserves order:

```go
// serialization
data, err := json.Marshal(om)
...

// deserialization
om := orderedmap.New[string, string]() // or orderedmap.New[int, any](), or any type you expect
err := json.Unmarshal(data, &om)
...
```

Similarly, it also supports YAML serialization/deserialization using the yaml.v3 package, which also preserves order:

```go
// serialization
data, err := yaml.Marshal(om)
...

// deserialization
om := orderedmap.New[string, string]() // or orderedmap.New[int, any](), or any type you expect
err := yaml.Unmarshal(data, &om)
...
```

## Iterator support (go >= 1.23)

The `FromOldest`, `FromNewest`, `KeysFromOldest`, `KeysFromNewest`, `ValuesFromOldest` and `ValuesFromNewest` methods return iterators over the map's pairs, starting from the oldest or newest pair, respectively.

For example:

```go
om := orderedmap.New[int, string]()
om.Set(1, "foo")
om.Set(2, "bar")
om.Set(3, "baz")

for k, v := range om.FromOldest() {
	fmt.Printf("%d => %s\n", k, v)
}

// prints:
// 1 => foo
// 2 => bar
// 3 => baz

for k := range om.KeysNewest() {
	fmt.Printf("%d\n", k)
}

// prints:
// 3
// 2
// 1
```

`From` is a convenience function that creates a new `OrderedMap` from an iterator over key-value pairs.

```go
om := orderedmap.New[int, string]()
om.Set(1, "foo")
om.Set(2, "bar")
om.Set(3, "baz")

om2 := orderedmap.From(om.FromOldest())

for k, v := range om2.FromOldest() {
	fmt.Printf("%d => %s\n", k, v)
}

// prints:
// 1 => foo
// 2 => bar
// 3 => baz
```

## Alternatives

There are several other ordered map golang implementations out there, but I believe that at the time of writing none of them offer the same functionality as this library; more specifically:
* [iancoleman/orderedmap](https://github.com/iancoleman/orderedmap) only accepts `string` keys, its `Delete` operations are linear
* [cevaris/ordered_map](https://github.com/cevaris/ordered_map) uses a channel for iterations, and leaks goroutines if the iteration is interrupted before fully traversing the map
* [mantyr/iterator](https://github.com/mantyr/iterator) also uses a channel for iterations, and its `Delete` operations are linear
* [samdolan/go-ordered-map](https://github.com/samdolan/go-ordered-map) adds unnecessary locking (users should add their own locking instead if they need it), its `Delete` and `Get` operations are linear, iterations trigger a linear memory allocation
