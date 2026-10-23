# glob.[go](https://golang.org)

[![GoDoc][godoc-image]][godoc-url] [![CI][ci-image]][ci-url]

> Go Globbing Library.

## Install

```shell
    go get github.com/gobwas/glob
```

## Example

```go

package main

import "github.com/gobwas/glob"

func main() {
    var g *glob.Pattern

    // create simple glob
    g = glob.MustCompile("*.github.com")
    g.Match("api.github.com") // true

    // quote meta characters and then create simple glob
    g = glob.MustCompile(glob.QuoteMeta("*.github.com"))
    g.Match("*.github.com") // true

    // create new glob with set of delimiters as ["."]
    g = glob.MustCompile("api.*.com", '.')
    g.Match("api.github.com") // true
    g.Match("api.gi.hub.com") // false

    // create new glob with set of delimiters as ["."]
    // but now with super wildcard
    g = glob.MustCompile("api.**.com", '.')
    g.Match("api.github.com") // true
    g.Match("api.gi.hub.com") // true

    // create glob with single symbol wildcard
    g = glob.MustCompile("?at")
    g.Match("cat") // true
    g.Match("fat") // true
    g.Match("at") // false

    // create glob with single symbol wildcard and delimiters ['f']
    g = glob.MustCompile("?at", 'f')
    g.Match("cat") // true
    g.Match("fat") // false
    g.Match("at") // false

    // create glob with character-list matchers
    g = glob.MustCompile("[abc]at")
    g.Match("cat") // true
    g.Match("bat") // true
    g.Match("fat") // false
    g.Match("at") // false

    // create glob with character-list matchers
    g = glob.MustCompile("[!abc]at")
    g.Match("cat") // false
    g.Match("bat") // false
    g.Match("fat") // true
    g.Match("at") // false

    // create glob with character-range matchers
    g = glob.MustCompile("[a-c]at")
    g.Match("cat") // true
    g.Match("bat") // true
    g.Match("fat") // false
    g.Match("at") // false

    // create glob with character-range matchers
    g = glob.MustCompile("[!a-c]at")
    g.Match("cat") // false
    g.Match("bat") // false
    g.Match("fat") // true
    g.Match("at") // false

    // create glob with pattern-alternatives list
    g = glob.MustCompile("{cat,bat,[fr]at}")
    g.Match("cat") // true
    g.Match("bat") // true
    g.Match("fat") // true
    g.Match("rat") // true
    g.Match("at") // false
    g.Match("zat") // false
}

```

`Compile` reports malformed patterns with a `*glob.SyntaxError` carrying the
byte offset and the reason:

```go
_, err := glob.Compile("{a,b")
// err: glob: syntax error at 4: unclosed `{`
```

A compiled `Pattern` captures what it was compiled from, so it can be passed
around instead of the raw arguments and inspected when needed (`String()` makes
it a `fmt.Stringer`, like `regexp.Regexp`):

```go
g := glob.MustCompile("*.github.com", '.')
g.String()     // "*.github.com"
g.Separators() // []rune{'.'}
```

## Syntax

Syntax is inspired by [standard wildcards](http://tldp.org/LDP/GNU-Linux-Tools-Summary/html/x11655.htm),
with one addition: `**` (the "super-asterisk"), which matches any sequence
of characters *including* the separators, where `*` stops at them. Note that
it is just that -- a `*` that crosses separators -- and not the `**/`
"globstar" of shells and file globbers: `**/x` requires the literal `/`, so
it does not match `x`; use `{**/,}x` for that. The same applies to a
`**` between separators, e.g. `a/**/b` does not match `a/b`.

```
pattern:
    { term }

term:
    `*`         matches any sequence of non-separator characters
    `**`        matches any sequence of characters
    `?`         matches any single non-separator character
    `[` [ `!` ] class `]`
                character class; `!` negates it
    `{` pattern-list `}`
                pattern alternatives
    c           matches character c (c != `*`, `**`, `?`, `\`, `[`, `{`, `}`)
    `\` c       matches character c

class:
    lo `-` hi   matches character c for lo <= c <= hi
    { c }       matches any of the listed characters (c != `\`, `]`;
                `\` c matches c, `-` is literal here); must be non-empty

pattern-list:
    pattern { `,` pattern }
                comma-separated (without spaces) patterns
```

### Escaping

The backslash is the escape character: `\*` is a literal asterisk, and a
backslash itself is `\\`. Mind the Go string literals: `"foo\\bar"` is the
pattern `foo\bar`, which is the literal `foobar`, not `foo\bar`. To match a
backslash (e.g. in the Windows paths) write `"foo\\\\bar"` or `` `foo\\bar` ``,
or use `QuoteMeta` on the literal part.

### Separators

The separators are not part of the pattern syntax -- they are configured
once, at compilation time, as the extra arguments of `Compile`:

```go
g := glob.MustCompile("api.*.com", '.', '/')
```

They only limit the wildcards: `*` and `?` never match a separator, while
`**` matches across them; the literals and the character classes are not
affected. With no separators given, `*` and `**` are equivalent. A compiled
`*glob.Pattern` keeps its separators for all matches -- to match the same
pattern with different separators, compile it again.

## Performance

This library is created for compile-once patterns. This means, that
compilation could take time, but strings matching is done faster, than in
case when always parsing template.

If you will not use compiled `*glob.Pattern` object, and do
`g := glob.MustCompile(pattern); g.Match(...)` every time, then your code
will be much more slower.

`Match` performs zero allocations and is safe for concurrent use. Common
pattern shapes (literals, prefixes, suffixes, substrings) are recognized at
compile time and matched with plain string comparisons; the backtracking
engine behind the rest is differentially fuzzed against the `regexp` package
(see `FuzzMatchRegexp`).

Run `go test -bench=.` from source root to see the benchmarks (the numbers
below are from an Apple M4):

Pattern | Fixture | Match | Speed (ns/op)
--------|---------|-------|--------------
`[a-z][!a-x]*cat*[h][!b]*eyes*` | `my cat has very bright eyes` | `true` | 141
`[a-z][!a-x]*cat*[h][!b]*eyes*` | `my dog has very bright eyes` | `false` | 46
`https://*.google.*` | `https://account.google.com` | `true` | 16
`https://*.google.*` | `https://google.com` | `false` | 13
`{https://*.google.*,*yandex.*,*yahoo.*,*mail.ru}` | `http://yahoo.com` | `true` | 61
`{https://*.google.*,*yandex.*,*yahoo.*,*mail.ru}` | `http://google.com` | `false` | 70
`{https://*gobwas.com,http://exclude.gobwas.com}` | `https://safe.gobwas.com` | `true` | 24
`{https://*gobwas.com,http://exclude.gobwas.com}` | `http://safe.gobwas.com` | `false` | 32
`google.com` | `google.com` | `true` | 5.0
`google.com` | `gobwas.com` | `false` | 3.9
`abc*` | `abcdef` | `true` | 4.1
`abc*` | `af` | `false` | 3.0
`*def` | `abcdef` | `true` | 4.1
`*def` | `af` | `false` | 2.9
`ab*ef` | `abcdef` | `true` | 6.0
`ab*ef` | `af` | `false` | 3.0

The same things with the `regexp` package -- not to pick on it (it is a
general-purpose engine with much stronger guarantees), but as a reference
for how the glob-shaped specialization pays off per pattern. The regular
expressions are the exact equivalents: anchored, and with the `s` flag
where there is a `*`, since a `*` matches a newline like any other
character (see `BenchmarkCompareGlobAndRegexp`):

Pattern | Fixture | Match | Speed (ns/op) | glob is
--------|---------|-------|---------------|--------
`(?s)^[a-z][^a-x].*cat.*[h][^b].*eyes.*$` | `my cat has very bright eyes` | `true` | 505 | 3.6x faster
`(?s)^[a-z][^a-x].*cat.*[h][^b].*eyes.*$` | `my dog has very bright eyes` | `false` | 221 | 4.9x faster
`(?s)^https://.*\.google\..*$` | `https://account.google.com` | `true` | 251 | 16x faster
`(?s)^https://.*\.google\..*$` | `https://google.com` | `false` | 128 | 9.6x faster
`(?s)^(https://.*\.google\..*\|.*yandex\..*\|.*yahoo\..*\|.*mail\.ru)$` | `http://yahoo.com` | `true` | 396 | 6.5x faster
`(?s)^(https://.*\.google\..*\|.*yandex\..*\|.*yahoo\..*\|.*mail\.ru)$` | `http://google.com` | `false` | 558 | 8.0x faster
`(?s)^(https://.*gobwas\.com\|http://exclude\.gobwas\.com)$` | `https://safe.gobwas.com` | `true` | 210 | 8.8x faster
`(?s)^(https://.*gobwas\.com\|http://exclude\.gobwas\.com)$` | `http://safe.gobwas.com` | `false` | 46 | 1.4x faster
`^google\.com$` | `google.com` | `true` | 25 | 5.0x faster
`^google\.com$` | `gobwas.com` | `false` | 17 | 4.3x faster
`(?s)^abc.*$` | `abcdef` | `true` | 43 | 10x faster
`(?s)^abc.*$` | `af` | `false` | 1.5 | 2.0x slower
`(?s)^.*def$` | `abcdef` | `true` | 73 | 18x faster
`(?s)^.*def$` | `af` | `false` | 1.5 | 1.9x slower
`(?s)^ab.*ef$` | `abcdef` | `true` | 77 | 13x faster
`(?s)^ab.*ef$` | `af` | `false` | 1.5 | 2.0x slower

(The three `slower` rows are the tiny-mismatch cases. Both engines reject
them with the same literal check; `regexp` just reaches it through less
call overhead. In absolute terms it is 1.5ns vs 3ns -- negligible either
way.)

[godoc-image]: https://pkg.go.dev/badge/github.com/gobwas/glob.svg
[godoc-url]: https://pkg.go.dev/github.com/gobwas/glob
[ci-image]: https://github.com/gobwas/glob/actions/workflows/ci.yml/badge.svg?branch=master
[ci-url]: https://github.com/gobwas/glob/actions/workflows/ci.yml
