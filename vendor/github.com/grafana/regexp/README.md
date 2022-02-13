# Grafana Go regexp package
This repo is a fork of the upstream Go `regexp` package, with some code optimisations to make it run faster.

All the optimisations have been submitted upstream, but not yet merged.

All semantics are the same, and the optimised code passes all tests from upstream.

The `main` branch is non-optimised: switch over to [`speedup`](https://github.com/grafana/regexp/tree/speedup) branch for the improved code.

## Benchmarks:

![image](https://user-images.githubusercontent.com/8125524/152182951-856549ed-6044-4285-b799-69b31f598e32.png)

## Links to upstream changes:
* [regexp: allow patterns with no alternates to be one-pass](https://go-review.googlesource.com/c/go/+/353711)
* [regexp: speed up onepass prefix check](https://go-review.googlesource.com/c/go/+/354909)
* [regexp: handle prefix string with fold-case](https://go-review.googlesource.com/c/go/+/358756)
* [regexp: avoid copying each instruction executed](https://go-review.googlesource.com/c/go/+/355789)
* [regexp: allow prefix string anchored at beginning](https://go-review.googlesource.com/c/go/+/377294)
