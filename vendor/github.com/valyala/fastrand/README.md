[![Build Status](https://travis-ci.org/valyala/fastrand.svg)](https://travis-ci.org/valyala/fastrand)
[![GoDoc](https://godoc.org/github.com/valyala/fastrand?status.svg)](http://godoc.org/github.com/valyala/fastrand)
[![Go Report](https://goreportcard.com/badge/github.com/valyala/fastrand)](https://goreportcard.com/report/github.com/valyala/fastrand)


# fastrand

Fast pseudorandom number generator.


# Features

- Optimized for speed.
- Performance scales on multiple CPUs.

# How does it work?

It abuses [sync.Pool](https://golang.org/pkg/sync/#Pool) for maintaining
"per-CPU" pseudorandom number generators.

TODO: firgure out how to use real per-CPU pseudorandom number generators.


# Benchmark results


```
$ GOMAXPROCS=1 go test -bench=. github.com/valyala/fastrand
goos: linux
goarch: amd64
pkg: github.com/valyala/fastrand
BenchmarkUint32n                   	50000000	        29.7 ns/op
BenchmarkRNGUint32n                	200000000	         6.50 ns/op
BenchmarkRNGUint32nWithLock        	100000000	        21.5 ns/op
BenchmarkMathRandInt31n            	50000000	        31.8 ns/op
BenchmarkMathRandRNGInt31n         	100000000	        17.9 ns/op
BenchmarkMathRandRNGInt31nWithLock 	50000000	        30.2 ns/op
PASS
ok  	github.com/valyala/fastrand	10.634s
```

```
$ GOMAXPROCS=2 go test -bench=. github.com/valyala/fastrand
goos: linux
goarch: amd64
pkg: github.com/valyala/fastrand
BenchmarkUint32n-2                     	100000000	        17.6 ns/op
BenchmarkRNGUint32n-2                  	500000000	         3.36 ns/op
BenchmarkRNGUint32nWithLock-2          	50000000	        32.0 ns/op
BenchmarkMathRandInt31n-2              	20000000	        51.2 ns/op
BenchmarkMathRandRNGInt31n-2           	100000000	        11.0 ns/op
BenchmarkMathRandRNGInt31nWithLock-2   	20000000	        91.0 ns/op
PASS
ok  	github.com/valyala/fastrand	9.543s
```

```
$ GOMAXPROCS=4 go test -bench=. github.com/valyala/fastrand
goos: linux
goarch: amd64
pkg: github.com/valyala/fastrand
BenchmarkUint32n-4                     	100000000	        14.2 ns/op
BenchmarkRNGUint32n-4                  	500000000	         3.30 ns/op
BenchmarkRNGUint32nWithLock-4          	20000000	        88.7 ns/op
BenchmarkMathRandInt31n-4              	10000000	       145 ns/op
BenchmarkMathRandRNGInt31n-4           	200000000	         8.35 ns/op
BenchmarkMathRandRNGInt31nWithLock-4   	20000000	       102 ns/op
PASS
ok  	github.com/valyala/fastrand	11.534s
```

As you can see, [fastrand.Uint32n](https://godoc.org/github.com/valyala/fastrand#Uint32n)
scales on multiple CPUs, while [rand.Int31n](https://golang.org/pkg/math/rand/#Int31n)
doesn't scale. Their performance is comparable on `GOMAXPROCS=1`,
but `fastrand.Uint32n` runs 3x faster than `rand.Int31n` on `GOMAXPROCS=2`
and 10x faster than `rand.Int31n` on `GOMAXPROCS=4`.
