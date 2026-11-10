# AGENTS.md

Guidance for people and coding agents changing this repository. It records
conventions and traps learned while working on the decoders and encoders; the
README describes the package itself.

## Checks before sending a change

- `gofmt`, `go vet ./...` and `go fix -diff ./...` (which should report
  nothing). Code written for specific platforms must be vetted for them too:
  run `go vet` with `GOOS`/`GOARCH` set to each platform it targets. `go vet`
  caches results per package and does not notice edits to files excluded by
  build tags; use a fresh `GOCACHE` to confirm a stale-looking error.
- `go test ./...` with and without `-tags noasm`, on every architecture whose
  assembly changed.
- Decoder changes: run the fuzzers CI runs (`.github/workflows/ci.yml`;
  `fuzz/README.md` describes them), also with `-tags noasm`, for longer than
  CI does.
- Compressor changes: `TestCompressGolden` checks the output of `Compressor`
  and `CompressorHC` against `testdata/compress.golden`. Regenerate it with
  `-update` only when the output changes on purpose, and report the size
  change it records. `CompressorCCompat` must produce the same bytes as the C
  library; `testdata/ccompat.golden` holds digests of lz4 1.10.0's output.
- Raising the Go version in the root `go.mod` breaks the nested `bench`
  module until you run `go mod tidy` there; CI doesn't build it.

## Tests

- Put a new test in the existing test file for the code it exercises
  (`match_copy_test.go` for the decoder's match-copy paths, `decode_test.go`,
  `robustness_test.go` and so on), and reuse its helpers. Use table tests for
  three or more similar cases.
- Extend the existing fuzzers (`func Fuzz…`) rather than adding parallel
  harnesses; if a new harness supersedes an old one, remove the old one in the
  same change.
- Test decoders on buffers that end at an inaccessible page
  (`guardedTail` in `guard_unix_test.go`), so out-of-bounds reads fault instead
  of passing silently.
- Prove which path a test or benchmark exercises (assembly or pure Go, which
  copy loop). A `panic` planted in the path is a quick check; `println`
  output is easy to miss.
- `sync.Pool` drops about a quarter of `Put`s under `-race` by design; never
  assume `Get` returns the last `Put`.
- Don't add tests whose only purpose is to catch someone deliberately undoing
  a performance pin (an alignment, say); a comment explaining it is enough.

## Performance claims

- Use real data, not only the small in-repo corpus. The Silesia corpus and
  the files at https://klauspost.com/files/compress/ (logs, JSON, CSV,
  serialized data, tar files) are good sources. Report frames from this
  package's Writer and from the C CLI (`lz4 -BD`, linked blocks) separately:
  they take different decoder paths. `bench/` compares with liblz4 on the
  same compressed blocks (see `bench/README.md`).
- Anchor `-bench` patterns at every level with `$`: `-bench 'BenchmarkFoo$'`
  also runs `BenchmarkFooBar` otherwise, which has produced fake gains and
  regressions.
- Code placement alone has moved results by 2–6% with identical hot code.
  Compare against a control build that differs from yours only in whether
  the new path is taken (for example one branch inverted), so its size and
  layout stay the same, and check addresses with `go tool nm` and
  `go tool objdump`.
- Build both variants as test binaries up front, then run them in turns: one
  short `-count 1` run of each, repeated for ten or more rounds, swapping
  which goes first every round (AB, BA, AB, …) so neither build always gets
  the warmer or colder slot. Never run all of A and then all of B. Clock
  speed, temperature, cache and page-cache state, and background load drift
  over a session; alternating spreads that drift over both builds instead of
  turning it into a difference between them. Pin to one core, use an
  otherwise idle machine, and compare the pooled results with `benchstat`.
  Individual large files can be bimodal from one process to the next, which
  is one more reason for many rounds.
- Report the number of regressed cases next to the geomean; a small win with
  no regressions is a different result from a larger one with some.
- No CPU-model-specific code paths or thresholds; gate on CPU feature flags.
  CPUs disagree, so measure a gate for arm64 on several Neoverse generations
  (N1, V1, V2, V3), not one core, and one for amd64 on both a recent Intel
  server core and a recent Zen core: wider stores, for example, helped Zen
  but slowed Sapphire Rapids once data left L1.

## Pull requests

- One commit per independent change, with fixups squashed in. Number the
  changes in the description as "(1) … (sha)" and name the test file for
  each test.
- Mention each `#NNN` once in prose; refer back in words.
- Don't open many PRs against the same area at once; reviewer time is the
  scarce resource.
- Keep Go comments short; assembly comments can explain in detail.
- Keep private or company-internal references (internal service names,
  private links, chat or agent-session URLs) out of code, commit messages and
  PR text.

## Assembly

- The assembly is hand-written: the block decoders in
  `internal/lz4block/decode_{amd64,arm64,arm}.s` and the xxHash32 kernels in
  `internal/xxh32/xxh32zero_{amd64,arm64,arm}.s`. Each has a pure-Go
  equivalent (`decodeBlockGo` for the decoder), and
  `FuzzDecodeBlockDifferential` checks the assembly decoder against it.
- Code placement moves the hot loops by several percent, even when the
  instructions don't change. The existing `PCALIGN`s each carry a comment
  with what they fixed. When you pin a loop, do the same, and remember that
  `PCALIGN $64` also raises the whole function's alignment.
- Never put a label directly on a `PCALIGN` (emit the `PCALIGN` before the
  label). A jump to it makes the amd64 assembler jump to the wrong place
  (Go ≤ 1.25, golang/go#74648) or loop forever when it has to widen a branch
  (Go ≥ 1.26, golang/go#81792).
- Build tags:
  - `noasm` selects the pure-Go code.
  - `nounsafe` and `purego` select the code without `unsafe` loads.
  - Every assembly or `unsafe` path needs a portable fallback.
  - CI runs the default and `noasm` builds, each with and without `-race`.
    Test `nounsafe` yourself when you touch `unsafe` code.
- The library doesn't use cgo. `bench/` is a separate module that uses cgo
  to compare against liblz4.
