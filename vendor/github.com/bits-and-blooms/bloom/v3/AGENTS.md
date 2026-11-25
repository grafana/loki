# Instructions for AI agents

This file is for AI coding agents (Claude Code, Codex, Copilot, Cursor and
similar tools) and for the people who use them to work on
`github.com/bits-and-blooms/bloom/v3`.

## Human review policy

Every issue and pull request in this project is read and assessed by a human
being. Please respect their time.

- **Do not generate answers automatically.** Do not post AI-written comments,
  replies, reviews or triage on issues or pull requests. If you use an agent to
  help you, you are responsible for what you submit: read it, understand it, and
  write the discussion yourself.
- **Every issue must be well motivated.** Explain the concrete problem: what you
  did, what you expected, what happened, and the Go version and platform. Include
  a minimal reproducer. Do not open issues about speculative, cosmetic or
  stylistic concerns, and do not file bulk reports produced by a tool.
- **Optimizations and bug fixes are welcome**, but they must be of high quality.
  A human maintainer will assess them, so they must be easy to review:
  - A bug fix comes with a regression test that fails before the fix and passes
    after it.
  - An optimization comes with benchmark results (before and after, ideally with
    `benchstat`) on realistic inputs, and states the hardware and Go version.
    A change that makes the code harder to read needs a measurable gain.
  - Keep each pull request small and focused on one change. Do not mix in
    reformatting, renames or unrelated cleanups.
- Do not open pull requests that only reword comments or documentation, add
  generated boilerplate, or "modernize" code without a clear benefit.

If you are an agent acting on someone's behalf: do not open issues, pull
requests or comments on GitHub unless the person explicitly asks you to, and
show them the exact text first.

## Project overview

Package `bloom` implements Bloom filters. It is a small, widely used library,
so stability and backward compatibility matter more than new features.

- `bloom.go`: the `BloomFilter` type, parameter estimation
  (`EstimateParameters`, `EstimateFalsePositiveRate`) and serialization.
- `murmur.go`: the MurmurHash3 128-bit hashing used to derive the bit
  locations. Its output determines which bits are set, so it must not change.
- The filter is backed by `github.com/bits-and-blooms/bitset`. Bugs in the
  bitset itself belong in that repository.

## Constraints

- **Go 1.16 compatibility.** `go.mod` declares `go 1.16`. Do not use generics,
  `min`/`max` builtins, `clear`, range-over-int, `slices`, `maps` or other newer
  language and library features.
- **32-bit platforms.** `uint` is 32 bits on 386 and ARM. Watch for overflow in
  size and index arithmetic, and test with `GOARCH=386`.
- **Hash and location stability.** Filters are persisted and shared between
  machines. Changing the hashing, the location computation or the byte order
  makes existing filters return wrong answers. Such changes are not accepted
  within the `v3` module.
- **Serialization format.** `WriteTo`/`ReadFrom`,
  `MarshalBinary`/`UnmarshalBinary`, `GobEncode`/`GobDecode` and the JSON
  encoding must stay compatible with data written by earlier releases.
- **Public API.** Do not remove or change the signature or semantics of an
  exported identifier. New exported functions need a clear use case and doc
  comments.
- **Dependencies.** Do not add new dependencies.
- Keep the code idiomatic and consistent with the surrounding style; run
  `gofmt`.

## Testing

Run these before proposing a change:

```bash
go vet ./...
go test ./...
GOARCH=386 go test ./...            # on Linux or Windows
GOOS=linux GOARCH=386 go vet ./...  # on macOS, which cannot run 386 binaries
```

For performance work, run the benchmarks
(`go test -run=^$ -bench=. -count=10`) before and after the change.
