# Changelog

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
