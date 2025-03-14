---
title: LogQL template functions
menuTItle:  Template functions
description: Describes query functions that are supported by the Go text template.
aliases:
- ../logql/template_functions/
weight: 500
---

# LogQL template functions

The Go templating language is embedded in the Loki query language, LogQL.
The [text template](https://golang.org/pkg/text/template) format used in `| line_format` and `| label_format` support the usage of functions.

{{< admonition type="note" >}}
In the examples below we use backticks to quote the template strings. This is because some template strings contain double quotes, and using backticks lets us avoid escaping the double quotes.
If you are using a different quoting style, you may need to escape the double quotes.
{{< /admonition >}}

For more information, refer to the [Go template documentation](https://pkg.go.dev/text/template).

Additionally you can also access the log line using the `__line__` function and the timestamp using the `__timestamp__` function.

## Template pipeline syntax

A pipeline is a possibly chained sequence of "commands". A command is a simple value (argument) or a function or method call, possibly with multiple arguments. A pipeline may be "chained" by separating a sequence of commands with pipeline characters '|'. In a chained pipeline, the result of each command is passed as the last argument of the following command. The output of the final command in the pipeline is the value of the pipeline.

You can take advantage of [pipeline](https://golang.org/pkg/text/template/#hdr-Pipelines) to join together multiple functions.

Example:

```template
`{{ .path | replace " " "_" | trunc 5 | upper }}`
```

For function that returns a `bool` such as `contains`, `eq`, `hasPrefix` and `hasSuffix`, you can apply `AND` / `OR` and nested `if` logic.

Example:

```template
`{{ if and (contains "he" "hello") (contains "llo" "hello") }} yes {{end}}`
`{{ if or (contains "he" "hello") (contains("llo" "hello") }} yes {{end}}`
`{{ if contains .err "ErrTimeout" }} timeout {{else if contains "he" "hello"}} yes {{else}} no {{end}}`
```

## Built-in variables for log line properties

These variables provide a way of referencing something from the log line when writing a template expression.

### .label_name

All labels from the Log line are added as variables in the template engine. They can be referenced using the label name prefixed by a `.`(for example,`.label_name`). For example the following template will output the value of the `path` label:

```template
`{{ .path }}`
```

### __line__

The `__line__` function returns the original log line without any modifications.

Signature: `line() string`

Examples:

```template
`{{ __line__ | lower }}`
`{{ __line__ }}`
```

### __timestamp__

The `__timestamp__` function returns the current log line's timestamp.

Signature: `timestamp() time.Time`

```template
`{{ __timestamp__ }}`
`{{ __timestamp__ | date "2006-01-02T15:04:05.00Z-07:00" }}`
`{{ __timestamp__ | unixEpoch }}`
```

For more information, refer to the blog [Parsing and formatting date/time in Go](https://www.pauladamsmith.com/blog/2011/05/go_time.html).

## Date and time

You can use the following functions to manipulate dates and times when building LogQL queries.

### date

Returns a textual representation of the time value formatted according to the provided [golang datetime layout](https://pkg.go.dev/time#pkg-constants).

Signature: `date(fmt string, date interface{}) string`

Example:

```template
`{{ date "2006-01-02" now }}`
```

### duration

An alias for `duration_seconds`

Examples:

```template
`{{ .foo | duration }}`
`{{ duration .foo }}`
```

### duration_seconds

Convert a humanized time duration to seconds using [time.ParseDuration](https://pkg.go.dev/time#ParseDuration)

Signature: `duration_seconds(string) float64`

Examples:

```template
`{{ .foo | duration_seconds }}`
`{{ duration_seconds .foo }}`
```

### now

Returns the current time in the local timezone of the Loki server.

Signature: `Now() time.Time`

Example:

```template
`{{ now }}`
```

### toDate

Parses a formatted string and returns the time value it represents using the local timezone of the server running Loki.

For more consistency between Loki installations, it's recommended to use `toDateInZone`

The format string must use the exact date as defined in the [golang datetime layout](https://pkg.go.dev/time#pkg-constants)

Signature: `toDate(fmt, str string) time.Time`

Examples:

```template
`{{ toDate "2006-01-02" "2021-11-02" }}`
`{{ .foo | toDate "2006-01-02T15:04:05.999999999Z" }}`
```

### toDateInZone

Parses a formatted string and returns the time value it represents in the provided timezone.

The format string must use the exact date as defined in the [golang datetime layout](https://pkg.go.dev/time#pkg-constants)

The timezone value can be `Local`, `UTC`, or any of the IANA Time Zone database values

Signature: `toDateInZone(fmt, zone, str string) time.Time`

Examples:

```template
`{{ toDateInZone "2006-01-02" "UTC" "2021-11-02" }}`
`{{ .foo | toDateInZone "2006-01-02T15:04:05.999999999Z" "UTC" }}`
```

### unixEpoch

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

### unixEpochMillis

Returns the number of milliseconds elapsed since January 1, 1970 UTC.

Signature: `unixEpochMillis(date time.Time) string`

Examples:

```template
`{{ unixEpochMillis now }}`
`{{ .foo | toDateInZone "2006-01-02T15:04:05.999999999Z" "UTC" | unixEpochMillis }}`
```

### unixEpochNanos

Returns the number of nanoseconds elapsed since January 1, 1970 UTC.

Signature: `unixEpochNanos(date time.Time) string`

Examples:

```template
`{{ unixEpochNanos now }}`
`{{ .foo | toDateInZone "2006-01-02T15:04:05.999999999Z" "UTC" | unixEpochNanos }}`
```

### unixToTime

Converts the string epoch to the time value it represents. Epoch times in days, seconds, milliseconds, microseconds and nanoseconds are supported.

Signature: `unixToTime(epoch string) time.Time`

Examples:

Consider the following log line `{"from": "1679577215","to":"1679587215","message":"some message"}`. To print the `from` field as human readable add the following at the end of the LogQL query:

```logql
... | json | line_format `from="{{date "2006-01-02" (unixToTime .from)}}"`
```

## String manipulation

You can use the following templates to manipulate strings when building LogQL Queries.

### alignLeft

Use this function to format a string to a fixed with, aligning the content to the left.

Signature: `alignLeft(count int, src string) string`

Examples:

```template
`{{ alignLeft 5 "hello world"}}` // output: "hello"
`{{ alignLeft 5 "hi"}}`          // output: "hi   "
```

### alignRight

Use this function to format a string to a fixed with, aligning the content to the right.

Signature: `alignRight(count int, src string) string`

Examples:

```template
`{{ alignRight 5 "hello world"}}` // output: "world"
`{{ alignRight 5 "hi"}}`          // output: "   hi"
```

### b64enc

Base64 encode a string.

Signature: `b64enc(string) string`

Examples:

```template
`{{ .foo | b64enc }}`
`{{ b64enc  .foo }}`
```

### b64dec

Base64 decode a string.

Signature: `b64dec(string) string`

Examples:

```template
`{{ .foo | b64dec }}`
`{{ b64dec  .foo }}`
```

### bytes

Convert a humanized byte string to bytes using go-humanize. Durations can be turned into strings such as "3 days ago", numbers
representing sizes like 82854982 into useful strings like, "83 MB" or
"79 MiB"

Signature: `bytes(string) string`

Examples:

```template
`{{ .foo | bytes }}`
`{{ bytes .foo }}`
```

### default

Enables outputting a default value if the source string is otherwise empty. If the 'src' parameter is not empty, this function returns the value of 'src'. Useful for JSON fields that can be missing, like HTTP headers in a log line that aren't required, as in the following example:

```logql
{job="access_log"} | json | line_format `{{.http_request_headers_x_forwarded_for | default "-"}}`
```

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

### fromJson

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

### lower

Use this function to convert to lower case.

Signature: `lower(string) string`

Examples:

```template
`{{ .request_method | lower }}`
`{{ lower  "HELLO"}}`
```

The last example will return `hello`.

### indent

The indent function indents every line in a given string to the specified indent width. This is useful when aligning multi-line strings.

Signature: `indent(spaces int,src string) string`

Example:

```template
`{{ indent 4 .query }}`
```

This indents each line contained in the `.query` by four (4) spaces.

### nindent

The nindent function is the same as the indent function, but prepends a new line to the beginning of the string.

Signature: `nindent(spaces int,src string) string`

Example:

```template
`{{ nindent 4 .query }}`
```

This will indent every line of text by 4 space characters and add a new line to the beginning.

### repeat

Use this function to repeat a string multiple times.

Signature: `repeat(c int,value string) string`

Example:

```template
`{{ repeat 3 "hello" }}` // output: hellohellohello
```

### printf

Use this function to format a string in a custom way. For more information about the syntax, refer to the [Go documentation](https://pkg.go.dev/fmt).

Signature: `printf(format string, a ...interface{})`

Examples:

```template
`{{ printf "The IP address was %s" .remote_addr }}` // output: The IP address was 129.168.1.1

`{{ printf "%-40.40s" .request_uri}} {{printf "%-5.5s" .request_method}}`
// output: 
// /a/509965767/alternative-to-my-mtg.html  GET
// /id/609259548/hpr.html                   GET
```

```template
line_format "\"|\" {{printf \"%15.15s\" .ClientHost}} \"|\""
```

### replace

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

### substr

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

### title

Convert to title case.

Signature: `title(string) string`

Examples:

```template
`{{.request_method | title}}`
`{{ title "hello world"}}`
```

The last example will return `Hello World`.

### trim

The trim function removes space from either side of a string.

Signature: `trim(string) string`

Examples:

```template
`{{ .ip | trim }}`
`{{ trim "   hello    " }}` // output: hello
```

### trimAll

Use this function to remove given characters from the front or back of a string.

Signature: `trimAll(chars string,src string) string`

Examples:

```template
`{{ .path | trimAll "/" }}`
`{{ trimAll "$" "$5.00" }}` // output: 5.00
```

### trimPrefix

Use this function to trim just the prefix from a string.

Signature: `trimPrefix(prefix string, src string) string`

Examples:

```template
`{{  .path | trimPrefix "/" }}`
`{{ trimPrefix "-" "-hello" }}` // output: hello
```

### trimSuffix

Use this function to trim just the suffix from a string.

Signature: `trimSuffix(suffix string, src string) string`

Examples:

```template
`{{  .path | trimSuffix "/" }}`
`{{ trimSuffix "-" "hello-" }}` // output: hello
```

### trunc

Truncate a string and add no suffix.

Signature: `trunc(count int,value string) string`

Examples:

```template
`{{ .path | trunc 2 }}`
`{{ trunc 5 "hello world"}}`   // output: hello
`{{ trunc -5 "hello world"}}`  // output: world
```

### upper

Use this function to convert to upper case.

Signature: `upper(string) string`

Examples:

```template
`{ .request_method | upper }}`
`{{ upper  "hello"}}`
```

This results in `HELLO`.

### urlencode

Use this function to [urlencode](https://en.wikipedia.org/wiki/URL_encoding) a string.

Signature: `urlencode(string) string`

Examples:

```template
`{{ .request_url | urlencode }}`
`{{ urlencode  .request_url}}`
```

### urldecode

Use this function to [urldecode](https://en.wikipedia.org/wiki/URL_encoding) a string.

Signature: `urldecode(string) string`

Examples:

```template
`{{ .request_url | urldecode }}`
`{{ urldecode  .request_url}}`
```

## Logical functions

You can use the following logical functions to compare strings when building a template expression.

### contains

Use this function to test to see if one string is contained inside of another.

Signature: `contains(s string, src string,) bool`

Examples:

```template
`{{ if contains "ErrTimeout" .err }} timeout {{end}}`
`{{ if contains "he" "hello" }} yes {{end}}`
```

### eq

Use this function to test to see if one string has exact matching inside of another.

Signature: `eq(s string, src string) bool`

Examples:

```template
`{{ if eq "ErrTimeout" .err }} timeout {{end}}`
`{{ if eq "hello" "hello" }} yes {{end}}`
```

### hasPrefix and hasSuffix

The `hasPrefix` and `hasSuffix` functions test whether a string has a given prefix or suffix.

Signatures:

- `hasPrefix(prefix string, src string) bool`
- `hasSuffix(suffix string, src string) bool`

Examples:

```template
`{{ if hasSuffix .err "Timeout" }} timeout {{end}}`
`{{ if hasPrefix "he" "hello" }} yes {{end}}`
```

## Mathematical functions

You can use the following mathematical functions when writing template expressions.

### add

Sum numbers. Supports multiple numbers

Signature: `func(i ...interface{}) int64`

Example:

```template
`{{ add 3 2 5 }}` // output: 10
```

### addf

Sum floating point numbers. Supports multiple numbers.

Signature: `func(i ...interface{}) float64`

Example:

```template
`{{ addf 3.5 2 5 }}` // output: 10.5
```

### ceil

Returns the greatest float value greater than or equal to input value

Signature: `ceil(a interface{}) float64`

Example:

```template
`{{ ceil 123.001 }}` //output 124.0
```

### div

Divide two integers.

Signature: `func(a, b interface{}) int64`

Example:

```template
`{{ div 10 2}}` // output: 5
```

### divf

Divide floating point numbers. Supports multiple numbers.

Signature: `func(a interface{}, v ...interface{}) float64`

Example:

```template
`{{ divf 10 2 4}}` // output: 1.25
```

### float64

Convert a string to a float64.

Signature: `toFloat64(v interface{}) float64`

Example:

```template
`{{ "3.5" | float64 }}` //output 3.5
```

### floor

Returns the greatest float value less than or equal to input value.

Signature: `floor(a interface{}) float64`

Example:

```template
`{{ floor 123.9999 }}` //output 123.0
```

### int

Convert value to an integer.

Signature: `toInt(v interface{}) int`

Example:

```template
`{{ "3" | int }}` //output 3
```

### max

Return the largest of a series of integers:

Signature: `max(a interface{}, i ...interface{}) int64`

Example:

```template
`{{ max 1 2 3 }}` //output 3
```

### maxf

Return the largest of a series of floats:

Signature: `maxf(a interface{}, i ...interface{}) float64`

Example:

```template
`{{ maxf 1 2.5 3 }}` //output 3
```

### min

Return the smallest of a series of integers.

Signature: `min(a interface{}, i ...interface{}) int64`

Example:

```template
`{{ min 1 2 3 }}`//output 1
```

### minf

Return the smallest of a series of floats.

Signature: `minf(a interface{}, i ...interface{}) float64`

Example:

```template
`{{ minf 1 2.5 3 }}` //output 1.5
```

### mul

Multiply numbers. Supports multiple numbers.

Signature: `mul(a interface{}, v ...interface{}) int64`

Example:

```template
`{{ mul 5 2 3}}` // output: 30
```

### mulf

Multiply floating numbers. Supports multiple numbers

Signature: `mulf(a interface{}, v ...interface{}) float64`

Example:

```template
`{{ mulf 5.5 2 2.5 }}` // output: 27.5
```

### mod

Returns the remainder when number 'a' is divided by number 'b'.

Signature: `mod(a, b interface{}) int64`

Example:

```template
`{{ mod 10 3}}` // output: 1
```

### round

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

### sub

Subtract one number from another.

Signature: `func(a, b interface{}) int64`

Example:

```template
`{{ sub 5 2 }}` // output: 3
```

### subf

Subtract floating numbers. Supports multiple numbers.

Signature: `func(a interface{}, v ...interface{}) float64`

Example:

```template
`{{ subf  5.5 2 1.5 }}` // output: 2
```

## Regular expression functions

You can use the following functions to perform regular expressions in a template expression.

### count

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

### regexReplaceAll and regexReplaceAllLiteral

`regexReplaceAll` returns a copy of the input string, replacing matches of the Regexp with the replacement string replacement. Inside string replacement, $ signs are interpreted as in Expand, so for instance $1 represents the text of the first sub-match. See the golang [Regexp.replaceAll documentation](https://golang.org/pkg/regexp/#Regexp.ReplaceAll) for more examples.

Signature: regexReplaceAll(regex string, src string, replacement string)
(source)

Example:

```template
`{{ regexReplaceAll "(a*)bc" .some_label "${1}a" }}`
```

`regexReplaceAllLiteral` function returns a copy of the input string and replaces matches of the Regexp with the replacement string replacement. The replacement string is substituted directly, without using Expand.

Signature: regexReplaceAllLiteral(regex string, src string, replacement string)

Example:

```template
`{{ regexReplaceAllLiteral "(ts=)" .timestamp "timestamp=" }}`
```
