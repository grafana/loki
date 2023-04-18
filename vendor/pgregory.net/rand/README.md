# rand [![PkgGoDev][godev-img]][godev] [![CI][ci-img]][ci]

Fast, high quality alternative to `math/rand` and `golang.org/x/exp/rand`.

Compared to these packages, `pgregory.net/rand`:

- is API-compatible with all `*rand.Rand` methods and all top-level functions except `Seed()`,
- is significantly faster, while improving the generator quality,
- has simpler generator initialization:
  - `rand.New()` instead of `rand.New(rand.NewSource(time.Now().UnixNano()))`
  - `rand.New(1)` instead of `rand.New(rand.NewSource(1))`
- is deliberately not providing top-level `Seed()` and the `Source` interface.

## Benchmarks

All benchmarks were run on `Intel(R) Xeon(R) CPU E5-2680 v3 @ 2.50GHz`,
`linux/amd64`.

Compared to [math/rand](https://pkg.go.dev/math/rand):

```
name                     old time/op    new time/op    delta
Float64-48                  180ns ± 8%       1ns ±12%   -99.66%  (p=0.000 n=10+9)
Intn-48                     186ns ± 2%       1ns ±11%   -99.63%  (p=0.000 n=10+10)
Intn_Big-48                 200ns ± 1%       1ns ±19%   -99.28%  (p=0.000 n=8+10)
Uint64-48                   193ns ± 5%       1ns ± 3%   -99.72%  (p=0.000 n=9+9)
Rand_New                   12.7µs ± 4%     0.1µs ± 6%   -99.38%  (p=0.000 n=10+10)
Rand_ExpFloat64            9.66ns ± 3%    5.90ns ± 3%   -38.97%  (p=0.000 n=10+10)
Rand_Float32               8.90ns ± 5%    2.04ns ± 4%   -77.05%  (p=0.000 n=10+10)
Rand_Float64               7.62ns ± 4%    2.93ns ± 3%   -61.50%  (p=0.000 n=10+10)
Rand_Int                   7.74ns ± 3%    2.92ns ± 2%   -62.30%  (p=0.000 n=10+10)
Rand_Int31                 7.81ns ± 3%    1.92ns ±14%   -75.39%  (p=0.000 n=10+10)
Rand_Int31n                12.7ns ± 3%     3.0ns ± 3%   -76.66%  (p=0.000 n=9+10)
Rand_Int31n_Big            12.6ns ± 4%     3.4ns ±10%   -73.39%  (p=0.000 n=10+10)
Rand_Int63                 7.78ns ± 4%    2.96ns ± 6%   -61.97%  (p=0.000 n=10+9)
Rand_Int63n                26.4ns ± 1%     3.6ns ± 3%   -86.56%  (p=0.000 n=10+10)
Rand_Int63n_Big            26.2ns ± 7%     6.2ns ± 4%   -76.53%  (p=0.000 n=10+10)
Rand_Intn                  14.4ns ± 5%     3.5ns ± 3%   -75.72%  (p=0.000 n=10+10)
Rand_Intn_Big              28.8ns ± 2%     6.0ns ± 6%   -79.03%  (p=0.000 n=10+10)
Rand_NormFloat64           10.7ns ± 6%     6.0ns ± 3%   -44.28%  (p=0.000 n=10+10)
Rand_Perm                  1.34µs ± 1%    0.39µs ± 3%   -70.86%  (p=0.000 n=9+10)
Rand_Read                   289ns ± 5%     104ns ± 5%   -64.00%  (p=0.000 n=10+10)
Rand_Seed                  10.9µs ± 4%     0.0µs ± 6%   -99.69%  (p=0.000 n=10+10)
Rand_Shuffle                790ns ± 4%     374ns ± 5%   -52.63%  (p=0.000 n=10+10)
Rand_ShuffleOverhead        522ns ± 3%     204ns ± 4%   -60.83%  (p=0.000 n=10+10)
Rand_Uint32                7.82ns ± 3%    1.72ns ± 3%   -77.98%  (p=0.000 n=10+9)
Rand_Uint64                9.59ns ± 3%    2.83ns ± 2%   -70.47%  (p=0.000 n=10+9)

name                     old alloc/op   new alloc/op   delta
Rand_New                   5.42kB ± 0%    0.05kB ± 0%   -99.12%  (p=0.000 n=10+10)
Rand_Perm                    416B ± 0%      416B ± 0%      ~     (all equal)

name                     old allocs/op  new allocs/op  delta
Rand_New                     2.00 ± 0%      1.00 ± 0%   -50.00%  (p=0.000 n=10+10)
Rand_Perm                    1.00 ± 0%      1.00 ± 0%      ~     (all equal)

name                     old speed      new speed      delta
Rand_Read                 887MB/s ± 4%  2464MB/s ± 4%  +177.83%  (p=0.000 n=10+10)
Rand_Uint32               511MB/s ± 3%  2306MB/s ± 7%  +350.86%  (p=0.000 n=10+10)
Rand_Uint64               834MB/s ± 3%  2811MB/s ± 4%  +236.85%  (p=0.000 n=10+10)
```

<details>
<summary>Compared to <a href="https://pkg.go.dev/golang.org/x/exp/rand">golang.org/x/exp/rand</a>:</summary>

```
name                     old time/op    new time/op    delta
Float64-48                  175ns ± 8%       1ns ±12%   -99.65%  (p=0.000 n=10+9)
Intn-48                     176ns ±10%       1ns ±11%   -99.61%  (p=0.000 n=10+10)
Intn_Big-48                 174ns ± 1%       1ns ±19%   -99.18%  (p=0.000 n=9+10)
Uint64-48                   157ns ± 5%       1ns ± 3%   -99.66%  (p=0.000 n=10+9)
Rand_New                   78.8ns ± 6%    78.3ns ± 6%      ~     (p=0.853 n=10+10)
Rand_ExpFloat64            8.94ns ± 6%    5.90ns ± 3%   -34.00%  (p=0.000 n=10+10)
Rand_Float32               9.67ns ± 5%    2.04ns ± 4%   -78.89%  (p=0.000 n=10+10)
Rand_Float64               8.56ns ± 5%    2.93ns ± 3%   -65.74%  (p=0.000 n=10+10)
Rand_Int                   5.75ns ± 3%    2.92ns ± 2%   -49.25%  (p=0.000 n=9+10)
Rand_Int31                 5.72ns ± 5%    1.92ns ±14%   -66.37%  (p=0.000 n=10+10)
Rand_Int31n                17.4ns ± 7%     3.0ns ± 3%   -82.87%  (p=0.000 n=10+10)
Rand_Int31n_Big            17.3ns ± 4%     3.4ns ±10%   -80.57%  (p=0.000 n=10+10)
Rand_Int63                 5.77ns ± 4%    2.96ns ± 6%   -48.73%  (p=0.000 n=10+9)
Rand_Int63n                17.0ns ± 2%     3.6ns ± 3%   -79.13%  (p=0.000 n=9+10)
Rand_Int63n_Big            26.5ns ± 2%     6.2ns ± 4%   -76.81%  (p=0.000 n=10+10)
Rand_Intn                  17.5ns ± 5%     3.5ns ± 3%   -79.94%  (p=0.000 n=10+10)
Rand_Intn_Big              27.5ns ± 3%     6.0ns ± 6%   -78.09%  (p=0.000 n=10+10)
Rand_NormFloat64           10.0ns ± 3%     6.0ns ± 3%   -40.45%  (p=0.000 n=10+10)
Rand_Perm                  1.31µs ± 1%    0.39µs ± 3%   -70.04%  (p=0.000 n=10+10)
Rand_Read                   334ns ± 1%     104ns ± 5%   -68.88%  (p=0.000 n=8+10)
Rand_Seed                  5.36ns ± 2%   33.73ns ± 6%  +528.91%  (p=0.000 n=10+10)
Rand_Shuffle               1.22µs ± 2%    0.37µs ± 5%   -69.36%  (p=0.000 n=10+10)
Rand_ShuffleOverhead        907ns ± 2%     204ns ± 4%   -77.45%  (p=0.000 n=10+10)
Rand_Uint32                5.20ns ± 5%    1.72ns ± 3%   -66.84%  (p=0.000 n=10+9)
Rand_Uint64                5.14ns ± 5%    2.83ns ± 2%   -44.85%  (p=0.000 n=10+9)
Rand_Uint64n               17.6ns ± 3%     3.5ns ± 2%   -80.32%  (p=0.000 n=10+10)
Rand_Uint64n_Big           27.3ns ± 2%     6.0ns ± 7%   -77.97%  (p=0.000 n=10+10)
Rand_MarshalBinary         30.5ns ± 1%     3.8ns ± 4%   -87.70%  (p=0.000 n=8+10)
Rand_UnmarshalBinary       3.22ns ± 4%    3.71ns ± 3%   +15.16%  (p=0.000 n=10+10)

name                     old alloc/op   new alloc/op   delta
Rand_New                    48.0B ± 0%     48.0B ± 0%      ~     (all equal)
Rand_Perm                    416B ± 0%      416B ± 0%      ~     (all equal)
Rand_MarshalBinary          16.0B ± 0%      0.0B       -100.00%  (p=0.000 n=10+10)
Rand_UnmarshalBinary        0.00B          0.00B           ~     (all equal)

name                     old allocs/op  new allocs/op  delta
Rand_New                     2.00 ± 0%      1.00 ± 0%   -50.00%  (p=0.000 n=10+10)
Rand_Perm                    1.00 ± 0%      1.00 ± 0%      ~     (all equal)
Rand_MarshalBinary           1.00 ± 0%      0.00       -100.00%  (p=0.000 n=10+10)
Rand_UnmarshalBinary         0.00           0.00           ~     (all equal)

name                     old speed      new speed      delta
Rand_Read                 764MB/s ± 3%  2464MB/s ± 4%  +222.68%  (p=0.000 n=9+10)
Rand_Uint32               770MB/s ± 5%  2306MB/s ± 7%  +199.35%  (p=0.000 n=10+10)
Rand_Uint64              1.56GB/s ± 5%  2.81GB/s ± 4%   +80.32%  (p=0.000 n=10+10)
```
</details>

<details>
<summary>Compared to <a href="https://pkg.go.dev/github.com/valyala/fastrand">github.com/valyala/fastrand</a>:</summary>

Note that `fastrand` [does not](https://gist.github.com/flyingmutant/bf3bd489ee3c7a32f40714c11325d614)
generate good random numbers.

```
name       old time/op  new time/op  delta
Uint64-48  3.20ns ±35%  0.53ns ± 3%  -83.29%  (p=0.000 n=10+9)
Intn-48    1.83ns ±21%  0.69ns ±11%  -62.35%  (p=0.000 n=10+10)
```
</details>

## FAQ

### Why did you write this?

`math/rand` is both slow and [not up to standards](
https://gist.github.com/flyingmutant/ad5841f5e594aa8687fe47de34985e6a)
in terms of quality (but can not be changed because of Go 1 compatibility promise).
`golang.org/x/exp/rand` fixes some (but [not all](
https://gist.github.com/flyingmutant/0b380f432308beaaf09c0a038f918aa4))
quality issues, without improving the speed,
and it seems that there is no active development happening there.

### How does this thing work?

This package builds on 4 primitives: raw 64-bit generation using `sfc64` or goroutine-local
`runtime.fastrand64()`, floating-point generation using floating-point multiplication,
and integer generation in range using 32.64 or 64.128 fixed-point multiplication.

### Why is it fast?

The primitives selected are (as far as I am aware) about as fast as you can go
without sacrificing quality. On top of that, it is mainly making sure the compiler
is able to inline code, and a couple of micro-optimizations.

### Why no `Source`?

In Go (but not in C++ or Rust) it is a costly abstraction that provides no real value.
How often do you use a non-default `Source` with `math/rand`?

### Why no top-level `Seed()`?

Top-level `Seed()` would require sharing global mutex-protected state between all top-level
functions, which (unlike the goroutine-local state used) does not scale when the parallelism increases.

### Why `sfc64`?

I like it. It has withstood the test of time, with no known flaws or weaknesses despite
a lot of effort and CPU-hours spent on finding them. Also, it provides guarantees about period
length and distance between generators seeded with different seeds. And it is fast.

### Why not...

#### ...`pcg`?

A bit slow. Otherwise, [`pcg64dxsm`](https://numpy.org/devdocs/reference/random/bit_generators/pcg64dxsm.html)
is probably a fine choice.

#### ...`xoshiro`/`xoroshiro`?

Quite a bit of controversy and people finding weaknesses in variants of this design.
Did you know that `xoshiro256**`, which author describes as an "all-purpose, rock-solid generator"
that "passes all tests we are aware of", fails them in seconds if you multiply the output by 57?

#### ...`splitmix`?

With 64-bit state and 64-bit output, `splitmix64` outputs every 64-bit number exactly once
over its 2^64 period — in other words, the probability of generating the same number is 0.
A [birthday test](https://www.pcg-random.org/posts/birthday-test.html) will quickly find
this problem.

#### ...`wyrand`?

An excellent generator if you are OK with slightly lower quality. Because its output function
(unlike `splitmix`) is not a bijection, some outputs are more likely to appear than others.
You can easily observe this non-uniformity with
a [birthday test](https://gist.github.com/flyingmutant/cb69e96872023f9f580868e746d1128a).
On most modern platforms, top-level functions in this package use goroutine-local `wyrand`
generators provided by the runtime as the best compromise between quality and speed
of concurrent execution.

#### ...`romu`?

Very fast, but relatively new and untested. Also, no guarantees about the period length.

## Status

`pgregory.net/rand` is stable. In addition to API stability, deterministic pseudo-random
generation produces the same results on 32-bit and 64-bit architectures, both little-endian
and big-endian. Any observable change to these results would only occur together
with a major version bump.

## License

`pgregory.net/rand` is licensed under the [Mozilla Public License Version 2.0](./LICENSE). 

[godev-img]: https://pkg.go.dev/badge/pgregory.net/rand
[godev]: https://pkg.go.dev/pgregory.net/rand
[ci-img]: https://github.com/flyingmutant/rand/workflows/CI/badge.svg
[ci]: https://github.com/flyingmutant/rand/actions
