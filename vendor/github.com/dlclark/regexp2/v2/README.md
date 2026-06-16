# regexp2 - full featured regular expressions for Go
Regexp2 is a feature-rich RegExp engine for Go.  It doesn't have constant time guarantees like the built-in `regexp` package, but it allows backtracking and is compatible with Perl5 and .NET.  You'll likely be better off with the RE2 engine from the `regexp` package and should only use this if you need to write very complex patterns or require compatibility with .NET.

## Basis of the engine
The engine is ported from the .NET framework's System.Text.RegularExpressions.Regex engine.  That engine was open sourced in 2015 under the MIT license.  There are some fundamental differences between .NET strings and Go strings that required a bit of borrowing from the Go framework regex engine as well.  I cleaned up a couple of the dirtier bits during the port (regexcharclass.cs was terrible), but the parse tree, code emmitted, and therefore patterns matched should be identical.

## New Code Generation
For extra performance use `regexp2` with [`regexp2cg`](https://github.com/dlclark/regexp2cg). It is a code generation utility for `regexp2` and you can likely improve your regexp runtime performance by 3-10x in hot code paths. As always you should benchmark your specifics to confirm the results. Give it a try!

## Installing
This is a go-gettable library, so install is easy:

    go get github.com/dlclark/regexp2/v2@latest

## Changes in v2
Version 2 includes changes that may affect compatibility with existing v1 users:

* The module path is now `github.com/dlclark/regexp2/v2`, so imports need to use the `/v2` suffix.
* The minimum supported Go version is now Go 1.26.
* Changes to support https://github.com/dlclark/regexp2cg are merged in to support generated regex engines.
* `Regexp.Split` is now available for splitting strings with regexp matches.
* The new `compat` sub-package provides a [`regexp` compatibility adapter](#regexp-compatibility-adapter) with the same `Find*` and `Match*` method signatures as `regexp.Regexp`, plus a `compat.Matcher` interface that is implemented by both `*regexp.Regexp` and the adapter.
* The parser, optimizer, and runner internals have changed significantly to support generated regexes and additional matching optimizations.
* `Compile` and `MustCompile` now use variadic compile options for regex behavior and memory/performance tuning. See [Compile options](#compile-options) for more details.
* Moved `regexp2.Debug` and `regexp2.Compile` to new `regexp2.OptionDebug()` and `regexp2.OptionIsCodeGen()` compile options.
* Some types and constants in the `syntax` package have been exported or changed to support code generation.
* Conceptually changed the goal of the `regexp2.ECMAScript` option to be closer to the ECMAScript standard rather than C#'s ECMAScript behavior.
* Renamed the fields `Capture.Index` and `Capture.Length` to `Capture.RuneIndex` and `Capture.RuneLength` to be more clear that we're dealing with rune offsets.
* Added `Capture.ByteRange()` to return the byte offset index and length of the captured text. This requires some additional processing to be done behind the scenes the first time it's called for a given capture to convert the native rune offsets to byte offsets.

## Usage
Usage is similar to the Go `regexp` package.  Just like in `regexp`, you start by converting a regex into a state machine via the `Compile` or `MustCompile` methods.  They ultimately do the same thing, but `MustCompile` will panic if the regex is invalid.  You can then use the provided `Regexp` struct to find matches repeatedly.  A `Regexp` struct is safe to use across goroutines.

```go
re := regexp2.MustCompile(`Your pattern`)
if isMatch, _ := re.MatchString(`Something to match`); isMatch {
    //do something
}
```

The only error that the `*Match*` methods *should* return is a Timeout if you set the `re.MatchTimeout` field.  Any other error is a bug in the `regexp2` package.  If you need more details about capture groups in a match then use the `FindStringMatch` method, like so:

```go
if m, _ := re.FindStringMatch(`Something to match`); m != nil {
    // the whole match is always group 0
    fmt.Printf("Group 0: %v\n", m.String())

    // you can get all the groups too
    gps := m.Groups()

    // a group can be captured multiple times, so each cap is separately addressable
    fmt.Printf("Group 1, first capture", gps[1].Captures[0].String())
    fmt.Printf("Group 1, second capture", gps[1].Captures[1].String())
}
```

Group 0 is embedded in the Match.  Group 0 is an automatically-assigned group that encompasses the whole pattern.  This means that `m.String()` is the same as `m.Group.String()` and `m.Groups()[0].String()`

The __last__ capture is embedded in each group, so `g.String()` will return the same thing as `g.Capture.String()` and  `g.Captures[len(g.Captures)-1].String()`.

If you want to find multiple matches from a single input string you should use the `FindNextMatch` method.  For example, to implement a function similar to `regexp.FindAllString`:

```go
func regexp2FindAllString(re *regexp2.Regexp, s string) []string {
	var matches []string
	m, _ := re.FindStringMatch(s)
	for m != nil {
		matches = append(matches, m.String())
		m, _ = re.FindNextMatch(m)
	}
	return matches
}
```

`FindNextMatch` is optmized so that it re-uses the underlying string/rune slice.

The internals of `regexp2` always operate on `[]rune` so `RuneIndex` and `RuneLength` data in a `Match` always reference a position in `rune`s rather than `byte`s (even if the input was given as a string). `ByteRange()` provides UTF-8 byte offsets, matching the original string input for string APIs. It's advisable to use the provided `String()` methods when you do not need explicit offsets. `ByteRange()` lazily caches byte offsets on the shared match text, so the first call on captures from the same match is not safe to run concurrently with other `ByteRange()` calls on that match.

## `regexp` compatibility adapter

The `github.com/dlclark/regexp2/v2/compat` package provides an adapter for callers that want the same `Find*` and `Match*` method signatures as the standard library's `regexp.Regexp`, while still using the `regexp2` engine.

```go
import (
	"github.com/dlclark/regexp2/v2"
	"github.com/dlclark/regexp2/v2/compat"
)

re := compat.MustCompile(`Your pattern`, regexp2.RE2)
if re.MatchString(`Something to match`) {
	// do something
}

matches := re.FindAllString(`abc axbc`, -1)
_ = matches
```

You can also wrap an existing compiled regexp:

```go
base := regexp2.MustCompile(`Your pattern`)
re := compat.Wrap(base)
```

The adapter includes the standard-library matching surface: `Match`, `MatchString`, `MatchReader`, and all `Find(All)?(String)?(Submatch)?(Index)?` methods. Index-returning methods use UTF-8 byte offsets like `regexp`, not regexp2's rune offsets.

The package also defines `compat.Matcher`, a common interface implemented by both `*regexp.Regexp` and `*compat.Regexp`. Use it when code should accept either the standard library engine or a regexp2-backed adapter:

```go
func findWords(re compat.Matcher, input string) []string {
	return re.FindAllString(input, -1)
}
```

Because those standard-library method signatures do not return errors, the adapter panics if the wrapped regexp2 matcher returns an error such as a match timeout. Use the main `regexp2` APIs directly when you need to handle timeouts as errors.

## Compile options

`Compile` and `MustCompile` take variadic compile options. Most users can omit them and get default regex behavior plus bounded shared pools for rune buffers and replacement output buffers, plus per-regexp caches for parsed replacement patterns and ASCII character class bitmaps.

Regex option constants can be passed directly, individually or as a bitmask:

```go
re := regexp2.MustCompile(`Your pattern`, regexp2.IgnoreCase, regexp2.Singleline)
re = regexp2.MustCompile(`Your pattern`, regexp2.IgnoreCase|regexp2.Singleline)
```

Performance tuning options override the default cache settings:

```go
re := regexp2.MustCompile(`Your pattern`,
	regexp2.IgnoreCase,
	regexp2.OptionMaxCachedRuneBufferLength(64*1024),
	regexp2.OptionMaxCachedReplacerDataEntries(8),
)
```

Compile-only options configure behavior that is not settable from the pattern:

```go
re := regexp2.MustCompile(`(?<first>This) (is)`, regexp2.OptionMaintainCaptureOrder())
```

The defaults are intentionally bounded:

| Option | Default | Used by | Working-set growth | Tradeoffs |
| --- | ---: | --- | --- | --- |
| `OptionMaintainCaptureOrder()` | false | Parser capture-slot assignment for mixed named and unnamed captures. | None at match time. This changes compile-time capture numbering only. | Keeps named and unnamed captures in pattern order instead of appending named captures after unnamed captures. This can change numeric backreference meaning, so it is caller-controlled rather than an inline regex option. |
| `OptionDebug()` | false | Compile dumps and runner tracing. | Debug output volume only. | Useful for diagnostics, but it can produce noisy output and slower traced matching. |
| `OptionIsCodeGen()` | false | Compile-time find-optimization analysis for [`regexp2cg`](https://github.com/dlclark/regexp2cg). | Per compiled regexp, during `Compile` or `MustCompile`. | Enables more expensive analysis intended for generated engines. Do not use it for normal interpreter execution; the interpreter defaults intentionally avoid this extra compile-time cost. |
| `OptionMaxCachedRuneBufferLength(n)` | 256K runes | String APIs that run through pooled runners, such as `MatchString` and replacement-pattern `Replace`, when converting input strings to the engine's internal `[]rune` representation. | Process-wide shared `sync.Pool` retention by size class. This does not grow per compiled regexp or per input string; the practical working set follows recent and concurrent use across all regexps and can be dropped by GC. | Raising this lets calls use larger pooled rune buffers and can reduce allocations for repeated matches against large strings. Lowering it prevents larger buffers from being borrowed or returned, so large inputs allocate directly. |
| `OptionMaxCachedReplaceBufferLength(n)` | 256 KB | Replacement-pattern `Replace` calls that build output through a shared byte buffer. | Process-wide shared `sync.Pool` retention by size class after replacement-pattern `Replace` runs. It does not grow from evaluator-based `ReplaceFunc` output and is shared across compiled regexps. | Raising this lets larger replacement outputs use pooled buffers and can reduce allocations. Lowering it prevents larger output buffers from being retained, so large replacements allocate directly. |
| `OptionMaxCachedReplacerDataEntries(n)` | `16` | `Replace` with replacement pattern strings, after the replacement pattern is parsed into reusable replacement data. | Per compiled regexp. The cache grows as distinct cacheable replacement strings are used with `Replace`, up to this entry count. | Raising this helps when a single compiled regexp is used with many recurring replacement patterns. It increases per-regexp cache memory and lock-protected cache bookkeeping. Setting it to `0` disables this cache. |
| `OptionMaxCachedReplacerDataBytes(n)` | 4 KB | The parsed replacement-pattern cache. Replacement strings longer than this are parsed for the call but not retained. | Per compiled regexp, combined with `OptionMaxCachedReplacerDataEntries`. Only replacement strings whose source text is at or below this size can add parsed data to the cache. | Raising this helps if large replacement patterns are reused. It can retain more memory per cached replacement. Lowering it avoids keeping unusual large replacement patterns around. |
| `OptionDisableCharClassASCIIBitmap()` | false | Compile-time preparation of character classes and first-character prefix sets. By default, character classes with ASCII membership get a small bitmap used by `CharIn`. | Per compiled regexp, during `Compile` or `MustCompile`. Each eligible character class can hold one small bitmap; this does not scale with match concurrency or input size. | Leaving this false speeds up ASCII-heavy character class checks at the cost of a small amount of per-char-class memory and compile-time work. Setting to true can reduce memory for large numbers of compiled char classes in regexps, but ASCII character class matching may be slower. |

For pooled buffer cache options, set `n` to `0` to disable pooling, or `-1` to allow all built-in size classes. The rune buffer classes are 1K, 4K, 16K, 64K, and 256K runes. The replacement byte buffer classes are 4 KB, 16 KB, 64 KB, 256 KB, and 1 MB. By default the 1 MB pool is unused. For replacement data byte-size cache options, `-1` means unbounded. For entry-count cache options, set `n` to `0` to disable the cache.

## Compare `regexp` and `regexp2`
| Category | regexp | regexp2 |
| --- | --- | --- |
| Catastrophic backtracking possible | no, constant execution time guarantees | yes, if your pattern is at risk you can use the `re.MatchTimeout` field |
| Python-style capture groups `(?P<name>re)` | yes | no (yes in RE2 compat mode) |
| .NET-style capture groups `(?<name>re)` or `(?'name're)` | yes | yes |
| comments `(?#comment)` | no | yes |
| branch numbering reset `(?\|a\|b)` | no | no |
| possessive match `(?>re)` | no | yes |
| positive lookahead `(?=re)` | no | yes |
| negative lookahead `(?!re)` | no | yes |
| positive lookbehind `(?<=re)` | no | yes |
| negative lookbehind `(?<!re)` | no | yes |
| back reference `\1` | no | yes |
| named back reference `\k'name'` | no | yes |
| Python-style named back reference `(?P=name)` | no | no (yes in RE2 compat mode) |
| named ascii character class `[[:foo:]]`| yes | no (yes in RE2 compat mode) |
| conditionals `(?(expr)yes\|no)` | no | yes |

## RE2 compatibility mode
The default behavior of `regexp2` is to match the .NET regexp engine, however the `RE2` option is provided to change the parsing to increase compatibility with RE2.  Using the `RE2` option when compiling a regexp will not take away any features, but will change the following behaviors:
* add support for named ascii character classes (e.g. `[[:foo:]]`)
* add support for python-style capture groups (e.g. `(?P<name>re)`)
* add support for python-style named backreferences (e.g. `(?P=name)`)
* change singleline behavior for `$` to only match end of string (like RE2) (see [#24](https://github.com/dlclark/regexp2/issues/24))
* change the character classes `\d` `\s` and `\w` to match the same characters as RE2. NOTE: if you also use the `ECMAScript` option then this will change the `\s` character class to match ECMAScript instead of RE2.  ECMAScript allows more whitespace characters in `\s` than RE2 (but still fewer than the the default behavior).
* allow character escape sequences to have defaults. For example, by default `\_` isn't a known character escape and will fail to compile, but in RE2 mode it will match the literal character `_`
 
```go
re := regexp2.MustCompile(`Your RE2-compatible pattern`, regexp2.RE2)
if isMatch, _ := re.MatchString(`Something to match`); isMatch {
    //do something
}
```

This feature is a work in progress and I'm open to ideas for more things to put here (maybe more relaxed character escaping rules?).

## Catastrophic Backtracking and Timeouts

`regexp2` supports features that can lead to catastrophic backtracking.
`Regexp.MatchTimeout` can be set to to limit the impact of such behavior; the
match will fail with an error after approximately MatchTimeout. No timeout
checks are done by default.

Timeout checking is not free. The current timeout checking implementation starts
a background worker that updates a clock value approximately once every 100
milliseconds. The matching code compares this value against the precomputed
deadline for the match. The performance impact is as follows.

1.  A match with a timeout runs almost as fast as a match without a timeout.
2.  If any live matches have a timeout, there will be a background CPU load
    (`~0.15%` currently on a modern machine). This load will remain constant
    regardless of the number of matches done including matches done in parallel.
3.  If no live matches are using a timeout, the background load will remain
    until the longest deadline (match timeout + the time when the match started)
    is reached. E.g., if you set a timeout of one minute the load will persist
    for approximately a minute even if the match finishes quickly.

See [PR #58](https://github.com/dlclark/regexp2/pull/58) for more details and 
alternatives considered.

## Goroutine leak error
If you're using a library during unit tests (e.g. https://github.com/uber-go/goleak) that validates all goroutines are exited then you'll likely get an error if you or any of your dependencies use regex's with a MatchTimeout. 
To remedy the problem you'll need to tell the unit test to wait until the backgroup timeout goroutine is exited.

```go
func TestSomething(t *testing.T) {
    defer goleak.VerifyNone(t)
    defer regexp2.StopTimeoutClock()

    // ... test
}

//or

func TestMain(m *testing.M) {
    // setup
    // ...

    // run 
    m.Run()

    //tear down
    regexp2.StopTimeoutClock()
    goleak.VerifyNone(t)
}
```

This will add ~100ms runtime to each test (or TestMain). If that's too much time you can set the clock cycle rate of the timeout goroutine in an init function in a test file. `regexp2.SetTimeoutCheckPeriod` isn't threadsafe so it must be setup before starting any regex's with Timeouts.

```go
func init() {
	//speed up testing by making the timeout clock 1ms
	regexp2.SetTimeoutCheckPeriod(time.Millisecond)
}
```

## ECMAScript compatibility mode
In this mode the engine attempts to match the [regex engine](https://tc39.es/ecma262/multipage/text-processing.html#sec-regexp-regular-expression-objects) described in the ECMAScript specification as closely as reasonably possible within regexp2's API and implementation.

This flag should not be treated as compatibility with C#'s `RegexOptions.ECMAScript`. regexp2's ECMAScript behavior prioritizes ECMAScript specification behavior over matching the C# regex engine's interpretation of that option.

Additionally a Unicode mode is provided which allows parsing of `\u{CodePoint}` syntax only when both `ECMAScript` and `Unicode` are provided.

## Potential bugs
I've run a battery of tests against regexp2 from various sources and found the debug output matches the .NET engine, but .NET and Go handle strings very differently.  I've attempted to handle these differences, but most of my testing deals with basic ASCII with a little bit of multi-byte Unicode.  There's a chance that there are bugs in the string handling related to character sets with supplementary Unicode chars.  Right-to-Left support is coded, but not well tested either.

## Find a bug?
I'm open to new issues and pull requests with tests if you find something odd!
