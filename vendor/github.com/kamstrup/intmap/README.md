Fast hashmap with integer keys for Golang

[![GoDoc](https://godoc.org/github.com/kamstrup/intmap?status.svg)](https://godoc.org/github.com/kamstrup/intmap)
[![Go Report Card](https://goreportcard.com/badge/github.com/kamstrup/intmap)](https://goreportcard.com/report/github.com/kamstrup/intmap)

# intmap

    import "github.com/kamstrup/intmap"

Package intmap is a fast hashmap implementation for Golang, specialized for maps with integer type keys.
The values can be of any type.

It is a full port of https://github.com/brentp/intintmap to use type parameters (aka generics).

It interleaves keys and values in the same underlying array to improve locality.
This is also known as open addressing with linear probing.

It is up to 3X faster than the builtin map:
```
name                             time/op
Map64Fill-8                       201ms ± 5%
IntIntMapFill-8                   207ms ±31%
StdMapFill-8                      371ms ±11%
Map64Get10PercentHitRate-8        148µs ±40%
IntIntMapGet10PercentHitRate-8    171µs ±50%
StdMapGet10PercentHitRate-8       171µs ±33%
Map64Get100PercentHitRate-8      4.50ms ± 5%
IntIntMapGet100PercentHitRate-8  4.82ms ± 6%
StdMapGet100PercentHitRate-8     15.5ms ±32%
```

## Usage

```go
m := intmap.New[int64,int64](32768)
m.Put(int64(1234), int64(-222))
m.Put(int64(123), int64(33))

v, ok := m.Get(int64(222))
v, ok := m.Get(int64(333))

m.Del(int64(222))
m.Del(int64(333))

fmt.Println(m.Len())

m.ForEach(func(k int64, v int64) {
    fmt.Printf("key: %d, value: %d\n", k, v)
})

m.Clear() // all gone, but buffers kept
```
