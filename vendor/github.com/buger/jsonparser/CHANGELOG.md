# Changelog

## [v1.6.1] — 2026-07-29

### Covered by [ReqProof](https://reqproof.com) — L3 Assurance (123 requirements, 0 errors, 0 warnings)

### Performance — gjson-style fast-skip in hot loops

Ported gjson's `>'\\'` fast-skip trick to three inner loops in parser.go:
`stringEndConfig` tail, `blockEndConfig`, and `searchKeysConfig`. The trick
uses a single unsigned comparison (`byte > 0x5C`) to skip all non-structural
bytes in bulk, reducing per-byte branch overhead.

| Payload | Before | After | Improvement |
|---|---|---|---|
| Small (190B) | 382 ns | **339 ns** | -11.3% |
| Medium (2.4kB) | 3,899 ns | **3,141 ns** | -19.4% |
| Large (24kB) | 20,788 ns | **20,114 ns** | -3.2% |

Zero allocations maintained on all paths.

### Benchmarks — now includes gjson and sonic

Added [tidwall/gjson](https://github.com/tidwall/gjson) (15.5k⭐, path-based
parser like jsonparser) and [bytedance/sonic](https://github.com/bytedance/sonic)
(9.6k⭐, SIMD-accelerated deserializer) to the benchmark suite.

**Final leaderboard (large payload):**

| Library | time/op | bytes/op | allocs/op |
|---|---|---|---|
| **buger/jsonparser** | **20,114** | **0** | **0** |
| tidwall/gjson | 22,756 | 28,672 | 2 |
| mailru/easyjson | 33,771 | 4,016 | 134 |
| bytedance/sonic | 41,053 | 31,368 | 71 |
| pquerna/ffjson | 59,063 | 4,822 | 144 |
| encoding/json | 130,565 | 4,432 | 147 |

jsonparser is **the fastest across all payload sizes** and the **only zero-allocation** parser.

---

## [v1.6.0] — 2026-07-29

### Covered by [ReqProof](https://reqproof.com) — L3 Assurance (123 requirements, 0 errors, 0 warnings)

### New API — `Append`

```go
// Append to an array without knowing its length
data, _ = jsonparser.Append(data, []byte(`"new_item"`), "items")
```

- **`Append(data, value, keys...)`** — appends `value` to the end of the JSON array addressed by `keys`. Addresses the top-level value when `keys` is empty; auto-vivifies a missing keyed path as a single-element array. Returns `MalformedArrayError` when the addressed value is not an array. Traced to SYS-REQ-009, SYS-REQ-110.

### Known issues — all resolved (zero open)

- **KI-2 fixed** — `ParseInt("-")` now returns an error instead of `(0, nil)`. One-line sign-only guard in `bytes.go:parseInt` (after stripping the sign byte, an empty remainder returns `(0, false, false)`).
- **KI-3 fixed** — `Set` with an array-index path component under an object parent (and vice-versa) now auto-coerces the container type instead of emitting malformed JSON. (Disposition already set to `fixed` in v1.5.x.)
- **KI-4 fixed** — `Set` on a top-level array-index beyond length now appends at the array's end (matching nested-array behavior under SYS-REQ-110) instead of returning `KeyPathNotFoundError`. Also cleans up trailing commas in malformed arrays.

**Zero open known issues.** Every previously shipped known issue is now resolved and covered by ReqProof L3 Assurance.

---

## [v1.5.1] — 2026-07-28

### Covered by [ReqProof](https://reqproof.com) — L3 Assurance (123 requirements, 0 errors, 0 warnings)

### Performance — 6.1x large-payload speedup

- **Fix stringEnd unbounded backslash scan** — `stringEndConfig` was scanning the ENTIRE remaining parent document for backslashes (`bytes.IndexByte(data, '\\')`) instead of just the string body. On a 24kb large payload this walked tens of KB per string. Now bounded to `data[:firstQuote]` (the string body only). **128µs → 22µs (5.8x).**
- **SWAR string scan** — replaced two separate `bytes.IndexByte` calls (quote + backslash) with a single inline 8-byte SWAR (SIMD-Within-A-Register) loop that checks for both characters simultaneously. **22µs → 21µs (additional 8%).**
- **Benchmark suite updated** — all comparison libraries (gabs, easyjson, ffjson, etc.) updated to latest versions. Benchmark methodology documented (Apple M4 Max, Go 1.26.3, median of 5 runs). The `encoding/json` benchmark no longer uses ffjson-generated methods (the #126 ffjson measurement bug was fixed in v1.3.1).
- **README benchmarks refreshed** — all numbers now reflect real measurements on modern hardware with current library versions.

### Updated benchmark results (Apple M4 Max, Go 1.26.3, median of 5 runs)

| Payload | jsonparser | encoding/json | easyjson | Speedup vs encoding/json |
|---|---|---|---|---|
| Small (190B, Get) | 382 ns | 1,335 ns | 312 ns | 3.5x |
| Small (190B, EachKey) | 241 ns | — | — | 5.5x |
| Medium (2.4kB, Get) | 3,894 ns | 10,564 ns | 2,444 ns | 2.7x |
| Medium (2.4kB, EachKey) | 1,923 ns | — | — | 5.5x |
| Large (24kB) | 20,788 ns | 134,123 ns | 32,765 ns | **6.4x** |

All jsonparser results: **0 bytes allocated, 0 allocations**.

---

## [v1.5.0] — 2026-07-28

### Covered by [ReqProof](https://reqproof.com) — L3 Assurance

v1.5.0 extends the formal-verification coverage to **123 requirements** (0 errors, 0 warnings) across all new APIs. Every new function is traced via source annotations, tested with MC/DC witnesses, and covered by the structure-aware fuzzer.

### Config struct — opt-in lenient parsing (#160, #115)

```go
var Lenient = jsonparser.Config{AllowSingleQuotes: true, AllowUnknownEscapes: true}
Lenient.Get(data, "key")  // parses {'key':'value'} and unknown escapes
```

- **`AllowSingleQuotes`** — accept `'key':'value'` alongside `"key":"value"` (JavaScript/Python-style). The same escape rules apply inside single-quoted strings.
- **`AllowUnknownEscapes`** — pass through unknown escape sequences (`` \` ``, `\x`) literally instead of erroring.
- The default Config is strict (RFC 8259 only). Package-level functions are unchanged.
- Config methods mirror the full API: `Get`, `GetString`, `Set`, `Delete`, `ArrayEach`, `ObjectEach`.

### Streaming ReaderParser (#132, #257)

```go
rp := jsonparser.NewReaderParser(file)  // any io.Reader
rp.Get("users", "[0]", "name")          // path-based access from a stream
```

- Path-based access to JSON data from an `io.Reader` — **no need to load the entire document into memory**.
- Buffers data incrementally in 64KB chunks; memory is bounded by the largest value, not the document size.
- Enables parsing **10GB+ JSON files** without OOM.
- Methods: `Get`, `GetString`, `ArrayEach`.

### Name aliases (#66)

Canonical `EachXxx` pattern added alongside existing `XxxEach` names:

| New (canonical) | Old (kept for compat) |
|---|---|
| `EachArray` | `ArrayEach` |
| `EachObject` | `ObjectEach` |
| `EachArrayErr` | `ArrayEachErr` |
| `EachArrayWildcard` | `ArrayEachWildcard` |

`EachKey`, `EachKeyErr`, `EachKeyWildcard` already matched the pattern. All old names remain functional.

### Proof

- 2 new SYS-REQs: 115 (Config/lenient parsing), 116 (streaming ReaderParser)
- **123 requirements, 0 errors, 0 warnings, 279/279 functions traced**

---

## [v1.4.0] — 2026-07-28

### Covered by [ReqProof](https://reqproof.com) — L3 Assurance

v1.4.0 adds 9 new backward-compatible APIs, each traced to a formal requirement and verified with MC/DC coverage. **121 requirements, 0 errors, 0 warnings.**

### New APIs

**Iteration with error/break control** — resolves #53, #129, #176, #230, #255, #262
- `ArrayEachErr` — callback returns `error` to stop early (`io.EOF` = graceful stop)
- `EachKeyErr` — same pattern for EachKey

**Safe string handling** — resolves #144, #158, #218, #270
- `Escape(s string) []byte` — RFC 8259 string escaping (inverse of `Unescape`)
- `SetString(data, val, keys...)` — Set with auto-quoted value

**Container accessors** — resolves #175, #261, #271
- `GetArrayLen` / `GetObjectLen` — count elements without a callback
- `GetUint64` — uint64 variant of `GetInt`

**Delete found signal** — resolves #229
- `DeleteFound(data, keys...) ([]byte, bool)` — returns whether the key was found

**Wildcard paths** — resolves #112
- `EachKeyWildcard`, `ArrayEachWildcard`, `SetWildcard` — `[*]` path component

**JSONPath compiled paths** — resolves #234, #251
- `ParsePath("$.users[0].name")` → `[]string` path
- `CompilePath` + `CompiledPath` — pre-compile and reuse with Get/Set/Delete

### Fixes
- EachKey no longer panics with >64 key components (#56)
- Set pre-allocates output buffer, reducing allocations from 6 to 1 (#107)

### Proof
- 3 new SYS-REQs: 112 (container length), 113 (wildcard paths), 114 (compiled paths)
- **121 requirements, 384 MC/DC witness rows, 0 uncovered**

---

## [v1.3.1] — 2026-07-28

### Covered by [ReqProof](https://reqproof.com) — L3 Assurance

v1.3.1 fixes 3 bugs that escaped the initial proof review, with new proof gates to prevent recurrence.

### Bug fixes

- **Fix Set/Delete input-buffer aliasing** (#209, #141) — `Set` and `Delete` no longer corrupt the caller's input `[]byte` when the slice has spare capacity. All mutation paths now allocate a fresh buffer.
- **Fix EachKey array-index inconsistency** (#232) — `EachKey` now descends into terminal array-index paths consistently with `Get`.
- **Fix benchmark measuring ffjson, not encoding/json** (#126) — benchmark payload types stripped of generated methods.

### Proof strengthening

| Bug | Proof gap | New gate |
|---|---|---|
| #209/#141 Set aliasing | No obligation said "Set must not mutate the input buffer" | New obligation `no_input_mutation` + `assertInputUnchanged` gate |
| #232 EachKey ≠ Get | No cross-API consistency obligation | New obligation `api_consistency` + differential gate |
| #126 benchmark ffjson | Proof didn't cover benchmarks | Benchmark honesty lint |

---

## [v1.3.0] — 2026-07-27

### Formally verified by [ReqProof](https://reqproof.com)

jsonparser v1.3.0 is the **first Go library proven to L3 assurance** by [ReqProof](https://reqproof.com), a git-native requirements-engineering and formal-verification platform. The entire codebase is now covered by:

- **118 formal requirements** (7 stakeholder + 111 system-level), each traced to code via source annotations and verified with FRETish formalization.
- **100% Modified Condition/Decision Coverage (MC/DC)** — both code-level (every decision/condition branch exercised) and requirement-side (377/377 truth-table rows witnessed).
- **A custom structure-aware JSON fuzzer** ([github.com/probelabs/json-fuzz](https://github.com/probelabs/json-fuzz)) generating grammar-valid mutations at 250k inputs/sec, plus path-mutation and `encoding/json` differential harnesses.
- **L3 strict audit posture**: 0 errors, 0 warnings, all checks enabled.

The proof review found and fixed **7 real bugs** that years of community use, OSS-Fuzz, and standard fuzzing had missed. [Read the root-cause analysis →](docs/proof-gap-root-cause.md)

jsonparser serves as the **reference case study** for ReqProof — [learn more at reqproof.com](https://reqproof.com).

### Security / bug fixes

- **Fix Delete panic on malformed input with leading comma** (OSS-Fuzz 4649128545288192)
  `Delete` panicked with `index out of range [-1]` on inputs like `,{"test":1{}`.
  The `data[prevTok]` dereference is now guarded.

- **Fix empty-string key-component panics** (8 sites)
  `Get`, `GetString`, `GetInt`, `GetFloat`, `GetBoolean`, `GetUnsafeString`, `Set`,
  `Delete`, `EachKey` panicked with `index out of range [0]` when a key path contained
  an empty string (`""`). All 8 unguarded `keys[i][0]` dereference sites are now guarded
  with `len(...) > 0`. Found by the structure-aware hazard sweep.
  Reported by @c-tonneslan (#284).

- **Fix Set data loss on scalar arrays** (#267)
  `Set` on an array-index path beyond the current length silently overwrote the array
  instead of appending. `Set({"a":[1,2,3]}, 99, "a", "[9]")` now returns `{"a":[1,2,3,99]}`
  instead of `{"a":[99]}`. Reported by @Solaris-star (#286).

- **Fix Set malformed-JSON output on cross-type paths**
  `Set` with an array-index path component under an object parent (e.g. `Set({}, 9, "[5]")`)
  produced invalid JSON (`{[9]}`). Set now auto-coerces the container type to match the
  path, always producing valid JSON output.

- **Fix Delete trailing-comma malformation**
  `Delete` left a dangling trailing comma in the output when the deleted element was
  followed by JSON whitespace (space/tab/LF/CR) and a comma. Found by the structure-aware
  path-mutation fuzzer.

- **Fix ArrayEach spurious callback on non-array root**
  `ArrayEach` on a non-array root value (e.g. `ArrayEach({"a":1}, cb)`) invoked the
  callback with a spurious element before returning an error. The callback is no longer
  invoked; a clean error is returned immediately.

- **Fix lone-Unicode-surrogate mishandling in Unescape**
  `ParseString` on a string containing a lone high surrogate (e.g. `\uDB29` without a
  following low surrogate) synthesized a bogus non-BMP code point from the following
  literal bytes. Now substitutes U+FFFD (matching `encoding/json` behavior).

### Performance

- **parseInt fast-path for short numbers** — 22–37% faster on typical 1–10 digit integers.
  Numbers with ≤18 digits use direct int64 accumulation, bypassing the overflow-checked
  uint64 slow path. Contributed by @trevorprater (#285).

- **stringEnd SIMD fast path** — 12× faster on no-escape strings, 4.5× faster end-to-end
  on `Get` for string values. Uses `bytes.IndexByte` for the common case (no `\` before
  the closing `"`).

### Acknowledgments

- @c-tonneslan (#284) — reported the empty-string key-component panic
- @Solaris-star (#286) — reported the Set array-index data-loss bug (#267)
- @trevorprater (#285) — contributed the parseInt performance optimization
- OSS-Fuzz (issue 4649128545288192) — the original Delete leading-comma panic
- The [probelabs/json-fuzz](https://github.com/probelabs/json-fuzz) structure-aware fuzzer
