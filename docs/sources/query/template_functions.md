---
title: Template functions
menuTItle:  
description: Describes functions that are supported by the Go text template.
aliases: 
- ../logql/template_functions/
weight: 30
---

# Template functions

The [text template](https://golang.org/pkg/text/template) format used in `| line_format` and `| label_format` support the usage of functions.

{{% admonition type="note" %}}
In the examples below we use backticks to quote the template strings. This is because some template strings contain double quotes, and using backticks lets us avoid escaping the double quotes.
If you are using a different quoting style, you may need to escape the double quotes.
{{% /admonition %}}

All labels are added as variables in the template engine. They can be referenced using they label name prefixed by a `.`(e.g `.label_name`). For example the following template will output the value of the path label:

```template
`{{ .path }}`
```

Additionally you can also access the log line using the [`__line__`](#__line__) function and the timestamp using the [`__timestamp__`](#__timestamp__) function.

You can take advantage of [pipeline](https://golang.org/pkg/text/template/#hdr-Pipelines) to join together multiple functions.
In a chained pipeline, the result of each command is passed as the last argument of the following command.

Example:

```template
`{{ .path | replace " " "_" | trunc 5 | upper }}`
```

For function that returns a `bool` such as `contains`, `eq`, `hasPrefix` and `hasSuffix`, you can apply `AND` / `OR` and nested `if` logic.

Example:

```template
`{{ if and (contains "he" "hello") (contains "llo" "hello") }} yes {{end}}`
`{{ if or (contains "he" "hello") (contains("llo" "hello") }} yes {{end}}`
`{{ if contains "ErrTimeout" .err }} timeout {{else if contains "he" "hello"}} yes {{else}} no {{end}}`
```

## __line__

This function returns the current log line.

Signature: `line() string`

Examples:

```template
`{{ __line__ | lower }}`
`{{ __line__ }}`
```

## __timestamp__

This function returns the current log lines timestamp.

Signature: `timestamp() time.Time`

```template
`{{ __timestamp__ }}`
`{{ __timestamp__ | date "2006-01-02T15:04:05.00Z-07:00" }}`
`{{ __timestamp__ | unixEpoch }}`
```

See the blog: [Parsing and formatting date/time in Go](https://www.pauladamsmith.com/blog/2011/05/go_time.html) for more information.

## regexReplaceAll and regexReplaceAllLiteral

`regexReplaceAll` returns a copy of the input string, replacing matches of the Regexp with the replacement string replacement. Inside string replacement, $ signs are interpreted as in Expand, so for instance $1 represents the text of the first sub-match. See the golang [Regexp.replaceAll documentation](https://golang.org/pkg/regexp/#Regexp.ReplaceAll) for more examples.

Example:

```template
`{{ regexReplaceAll "(a*)bc" .some_label "${1}a" }}`
```

`regexReplaceAllLiteral` function returns a copy of the input string and replaces matches of the Regexp with the replacement string replacement. The replacement string is substituted directly, without using Expand.

Example:

```template
`{{ regexReplaceAllLiteral "(ts=)" .timestamp "timestamp=" }}`
```

## lower

Use this function to convert to lower case.

Signature: `lower(string) string`

Examples:

```template
`{{ .request_method | lower }}`
`{{ lower  "HELLO"}}`
```

The last example will return `hello`.

## upper

Use this function to convert to upper case.

Signature: `upper(string) string`

Examples:

```template
`{ .request_method | upper }}`
`{{ upper  "hello"}}`
```

This results in `HELLO`.

## title

Convert to title case.

Signature: `title(string) string`

Examples:

```template
`{{.request_method | title}}`
`{{ title "hello world"}}`
```

The last example will return `Hello World`.

## trunc

Truncate a string and add no suffix.

Signature: `trunc(count int,value string) string`

Examples:

```template
`{{ .path | trunc 2 }}`
`{{ trunc 5 "hello world"}}`   // output: hello
`{{ trunc -5 "hello world"}}`  // output: world
```

## substr

Get a substring from a string.

Signature: `substr(start int,end int,value string) string`

If start is < 0, this calls value[:end].
If start is >= 0 and end < 0 or end bigger than s length, this calls value[start:]
Otherwise, this calls value[start, end].

Examples:

```template
`{{ .path | substr 2 5 }}`
`{{ substr 0 5 "hello world"}}`  // output: hello
`{{ substr 6 11 "hello world"}}` // output: world
```

## replace

This function performs simple string replacement.

Signature: `replace(old string, new string, src string) string`

It takes three arguments:

- `old` string to replace
- `new` string to replace with
- `src` source string

Examples:

```template
`{{ .cluster | replace "-cluster" "" }}`
`{{ replace "hello" "world" "hello world" }}`
```

The last example will return `world world`.

## trim

The trim function removes space from either side of a string.

Signature: `trim(string) string`

Examples:

```template
`{{ .ip | trim }}`
`{{ trim "   hello    " }}` // output: hello
```

## trimAll

Use this function to remove given characters from the front or back of a string.

Signature: `trimAll(chars string,src string) string`

Examples:

```template
`{{ .path | trimAll "/" }}`
`{{ trimAll "$" "$5.00" }}` // output: 5.00
```

## trimSuffix

Use this function to trim just the suffix from a string.

Signature: `trimSuffix(suffix string, src string) string`

Examples:

```template
`{{  .path | trimSuffix "/" }}`
`{{ trimSuffix "-" "hello-" }}` // output: hello
```

## trimPrefix

Use this function to trim just the prefix from a string.

Signature: `trimPrefix(prefix string, src string) string`

Examples:

```template
`{{  .path | trimPrefix "/" }}`
`{{ trimPrefix "-" "-hello" }}` // output: hello
```

## alignLeft

Use this function to format a string to a fixed with, aligning the content to the left.

Signature: `alignLeft(count int, src string) string`

Examples:

```template
`{{ alignLeft 5 "hello world"}}` // output: "hello"
`{{ alignLeft 5 "hi"}}`          // output: "hi   "
```

## alignRight

Use this function to format a string to a fixed with, aligning the content to the right.

Signature: `alignRight(count int, src string) string`

Examples:

```template
`{{ alignRight 5 "hello world"}}` // output: "world"
`{{ alignRight 5 "hi"}}`          // output: "   hi"
```

## indent

The indent function indents every line in a given string to the specified indent width. This is useful when aligning multi-line strings.

Signature: `indent(spaces int,src string) string`

Example:

```template
`{{ indent 4 .query }}`
```

This indents each line contained in the `.query` by four (4) spaces.

## nindent

The nindent function is the same as the indent function, but prepends a new line to the beginning of the string.

Signature: `nindent(spaces int,src string) string`

Example:

```template
`{{ nindent 4 .query }}`
```

This will indent every line of text by 4 space characters and add a new line to the beginning.

## repeat

Use this function to repeat a string multiple times.

Signature: `repeat(c int,value string) string`

Example:

```template
`{{ repeat 3 "hello" }}` // output: hellohellohello
```

## contains

Use this function to test to see if one string is contained inside of another.

Signature: `contains(s string, src string,) bool`

Examples:

```template
`{{ if contains "ErrTimeout" .err }} timeout {{end}}`
`{{ if contains "he" "hello" }} yes {{end}}`
```

## eq

Use this function to test to see if one string has exact matching inside of another.

Signature: `eq(s string, src string) bool`

Examples:

```template
`{{ if eq "ErrTimeout" .err }} timeout {{end}}`
`{{ if eq "hello" "hello" }} yes {{end}}`
```

## hasPrefix and hasSuffix

The `hasPrefix` and `hasSuffix` functions test whether a string has a given prefix or suffix.

Signatures:

- `hasPrefix(prefix string, src string) bool`
- `hasSuffix(suffix string, src string) bool`

Examples:

```template
`{{ if hasSuffix .err "Timeout" }} timeout {{end}}`
`{{ if hasPrefix "he" "hello" }} yes {{end}}`
```

## add

Sum numbers. Supports multiple numbers

Signature: `func(i ...interface{}) int64`

Example:

```template
`{{ add 3 2 5 }}` // output: 10
```

## sub

Subtract numbers.

Signature: `func(a, b interface{}) int64`

Example:

```template
`{{ sub 5 2 }}` // output: 3
```

## mul

Multiply numbers. Supports multiple numbers.

Signature: `func(a interface{}, v ...interface{}) int64`

Example:

```template
`{{ mul 5 2 3}}` // output: 30
```

## div

Integer divide numbers.

Signature: `func(a, b interface{}) int64`

Example:

```template
`{{ div 10 2}}` // output: 5
```

## addf

Sum numbers. Supports multiple numbers.

Signature: `func(i ...interface{}) float64`

Example:

```template
`{{ addf 3.5 2 5 }}` // output: 10.5
```

## subf

Subtract numbers. Supports multiple numbers.

Signature: `func(a interface{}, v ...interface{}) float64`

Example:

```template
`{{ subf  5.5 2 1.5 }}` // output: 2
```

## mulf

Multiply numbers. Supports multiple numbers

Signature: `func(a interface{}, v ...interface{}) float64`

Example:

```template
`{{ mulf 5.5 2 2.5 }}` // output: 27.5
```

## divf

Divide numbers. Supports multiple numbers.

Signature: `func(a interface{}, v ...interface{}) float64`

Example:

```template
`{{ divf 10 2 4}}` // output: 1.25
```

## mod

Modulo wit mod.

Signature: `func(a, b interface{}) int64`

Example:

```template
`{{ mod 10 3}}` // output: 1
```

## max

Return the largest of a series of integers:

Signature: `max(a interface{}, i ...interface{}) int64`

Example:

```template
`{{ max 1 2 3 }}` //output 3
```

## min

Return the smallest of a series of integers.

Signature: `min(a interface{}, i ...interface{}) int64`

Example:

```template
`{{ min 1 2 3 }}`//output 1
```

## maxf

Return the largest of a series of floats:

Signature: `maxf(a interface{}, i ...interface{}) float64`

Example:

```template
`{{ maxf 1 2.5 3 }}` //output 3
```

## minf

Return the smallest of a series of floats.

Signature: `minf(a interface{}, i ...interface{}) float64`

Example:

```template
`{{ minf 1 2.5 3 }}` //output 1.5
```

## ceil

Returns the greatest float value greater than or equal to input value

Signature: `ceil(a interface{}) float64`

Example:

```template
`{{ ceil 123.001 }}` //output 124.0
```

## floor

Returns the greatest float value less than or equal to input value

Signature: `floor(a interface{}) float64`

Example:

```template
`{{ floor 123.9999 }}` //output 123.0
```

## round

Returns a float value with the remainder rounded to the given number of digits after the decimal point.

Signature: `round(a interface{}, p int, rOpt ...float64) float64`

Example:

```template
`{{ round 123.555555 3 }}` //output 123.556
```

We can also provide a `roundOn` number as third parameter

Example:

```template
`{{ round 123.88571428571 5 .2 }}` //output 123.88572
```

With default `roundOn` of `.5` the above value would be `123.88571`

## int

Convert value to an int.

Signature: `toInt(v interface{}) int`

Example:

```template
`{{ "3" | int }}` //output 3
```

## float64

Convert to a float64.

Signature: `toFloat64(v interface{}) float64`

Example:

```template
`{{ "3.5" | float64 }}` //output 3.5
```

## fromJson

Decodes a JSON document into a structure. If the input cannot be decoded as JSON the function will return an empty string.

Signature: `fromJson(v string) interface{}`

Example:

```template
`{{fromJson "{\"foo\": 55}"}}`
```

Example of a query to print a newline per queries stored as a json array in the log line:

```logql
{job="loki/querier"} |= "finish in prometheus" | logfmt | line_format `{{ range $q := fromJson .queries }} {{ $q.query }} {{ end }}`
```

## now

Returns the current time in the local timezone of the Loki server.

Signature: `Now() time.Time`

Example:

```template
`{{ now }}`
```

## toDate

Parses a formatted string and returns the time value it represents using the local timezone of the server running Loki.

For more consistency between Loki installations, it's recommended to use `toDateInZone`

The format string must use the exact date as defined in the [golang datetime layout](https://pkg.go.dev/time#pkg-constants)

Signature: `toDate(fmt, str string) time.Time`

Examples: 

```template
`{{ toDate "2006-01-02" "2021-11-02" }}`
`{{ .foo | toDate "2006-01-02T15:04:05.999999999Z" }}`
```

## toDateInZone

Parses a formatted string and returns the time value it represents in the provided timezone.

The format string must use the exact date as defined in the [golang datetime layout](https://pkg.go.dev/time#pkg-constants)

The timezone value can be `Local`, `UTC`, or any of the IANA Time Zone database values

Signature: `toDateInZone(fmt, zone, str string) time.Time`

Examples:

```template
`{{ toDateInZone "2006-01-02" "UTC" "2021-11-02" }}`
`{{ .foo | toDateInZone "2006-01-02T15:04:05.999999999Z" "UTC" }}`
```

## date

Returns a textual representation of the time value formatted according to the provided [golang datetime layout](https://pkg.go.dev/time#pkg-constants).

Signature: `date(fmt string, date interface{}) string`

Example:

```template
`{{ date "2006-01-02" now }}`
```

## unixEpoch

Returns the number of seconds elapsed since January 1, 1970 UTC.

Signature: `unixEpoch(date time.Time) string`

Examples:

```template
`{{ unixEpoch now }}`
`{{ .foo | toDateInZone "2006-01-02T15:04:05.999999999Z" "UTC" | unixEpoch }}`
```

Example of a query to filter Loki querier jobs which create time is 1 day before:
```logql
{job="loki/querier"} | label_format nowEpoch=`{{(unixEpoch now)}}`,createDateEpoch=`{{unixEpoch (toDate "2006-01-02" .createDate)}}` | label_format dateTimeDiff=`{{sub .nowEpoch .createDateEpoch}}` | dateTimeDiff > 86400
```

## unixEpochMillis

Returns the number of milliseconds elapsed since January 1, 1970 UTC.

Signature: `unixEpochMillis(date time.Time) string`

Examples:

```template
`{{ unixEpochMillis now }}`
`{{ .foo | toDateInZone "2006-01-02T15:04:05.999999999Z" "UTC" | unixEpochMillis }}`
```

## unixEpochNanos

Returns the number of nanoseconds elapsed since January 1, 1970 UTC.

Signature: `unixEpochNanos(date time.Time) string`

Examples:

```template
`{{ unixEpochNanos now }}`
`{{ .foo | toDateInZone "2006-01-02T15:04:05.999999999Z" "UTC" | unixEpochNanos }}`
```

## unixToTime

Converts the string epoch to the time value it represents. Epoch times in days, seconds, milliseconds, microseconds and nanoseconds are supported.

Signature: `unixToTime(epoch string) time.Time`

Examples:

Consider the following log line `{"from": "1679577215","to":"1679587215","message":"some message"}`. To print the `from` field as human readable add the following at the end of the LogQL query:
```logql
... | json | line_format `from="{{date "2006-01-02" (unixToTime .from)}}"`
```

## default

Checks whether the string(`src`) is set, and returns default(`d`) if not set.

Signature: `default(d string, src string) string`

Examples:

```template
`{{ default "-" "" }}` // output: -
`{{ default "-" "foo" }}` // output: foo
```

Example of a query to print a `-` if the `http_request_headers_x_forwarded_for` label is empty:
```logql
{job="access_log"} | json | line_format `{{.http_request_headers_x_forwarded_for | default "-"}}`
```

## count

Counts occurrences of the regex (`regex`) in (`src`).

Signature: `count(regex string, src string) int`

Examples:

```template
`{{ count "a|b" "abab" }}` // output: 4
`{{ count "o" "foo" }}`    // output: 2
```

Example of a query to print how many times XYZ occurs in a line:
```logql
{job="xyzlog"} | line_format `{{ __line__ | count "XYZ"}}`
```

## urlencode

Use this function to [urlencode](https://en.wikipedia.org/wiki/URL_encoding) a string.

Signature: `urlencode(string) string`

Examples:

```template
`{{ .request_url | urlencode }}`
`{{ urlencode  .request_url}}`
```

## urldecode

Use this function to [urldecode](https://en.wikipedia.org/wiki/URL_encoding) a string.

Signature: `urldecode(string) string`

Examples:

```template
`{{ .request_url | urldecode }}`
`{{ urldecode  .request_url}}`
```

## b64enc

Base64 encode a string.

Signature: `b64enc(string) string`

Examples:

```template
`{{ .foo | b64enc }}`
`{{ b64enc  .foo }}`
```

## b64dec

Base64 decode a string.

Signature: `b64dec(string) string`

Examples:

```template
`{{ .foo | b64dec }}`
`{{ b64dec  .foo }}`
```

## bytes

Convert a humanized byte string to bytes using [go-humanize](https://pkg.go.dev/github.com/dustin/go-humanize#ParseBytes)

Signature: `bytes(string) string`

Examples:

```template
`{{ .foo | bytes }}`
`{{ bytes .foo }}`
```

## duration

An alias for `duration_seconds`

Examples:

```template
`{{ .foo | duration }}`
`{{ duration .foo }}`
```

## duration_seconds

Convert a humanized time duration to seconds using [time.ParseDuration](https://pkg.go.dev/time#ParseDuration)

Signature: `duration_seconds(string) float64`

Examples:

```template
`{{ .foo | duration_seconds }}`
`{{ duration_seconds .foo }}`
```
