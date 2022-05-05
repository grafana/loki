---
title: Template functions
weight: 30
---

# Template functions

The [text template](https://golang.org/pkg/text/template) format used in `| line_format` and `| label_format` support the usage of functions.

All labels are added as variables in the template engine. They can be referenced using they label name prefixed by a `.`(e.g `.label_name`). For example the following template will output the value of the path label:

```template
{{ .path }}
```

Additionally you can also access the log line using the [`__line__`](#__line__) function.

You can take advantage of [pipeline](https://golang.org/pkg/text/template/#hdr-Pipelines) to join together multiple functions.
In a chained pipeline, the result of each command is passed as the last argument of the following command.

Example:

```template
{{ .path | replace " " "_" | trunc 5 | upper }}
```

## __line__

This function returns the current log line.

Signature:

`line() string`

Examples:

```template
"{{ __line__ | lower }}"
`{{ __line__ }}`
```


## ToLower and ToUpper

This function converts the entire string to lowercase or uppercase.

Signatures:

- `ToLower(string) string`
- `ToUpper(string) string`

Examples:

```template
"{{.request_method | ToLower}}"
"{{.request_method | ToUpper}}"
`{{ToUpper "This is a string" | ToLower}}`
```

> **Note:** In Grafana Loki 2.1 you can also use respectively [`lower`](#lower) and [`upper`](#upper) shortcut, e.g `{{.request_method | lower }}`.

## Replace string

> **Note:** In Loki 2.1 [`replace`](#replace) (as opposed to `Replace`) is available with a different signature but easier to chain within pipeline.

Use this function to perform a simple string replacement.

Signature:

`Replace(s, old, new string, n int) string`

It takes four arguments:

- `s` source string
- `old` string to replace
- `new` string to replace with
- `n` the maximun amount of replacement (-1 for all)

Example:

```template
`{{ Replace "This is a string" " " "-" -1 }}`
```

The results in `This-is-a-string`.

## Trim, TrimLeft, TrimRight, and TrimSpace

> **Note:** In Loki 2.1 [trim](#trim), [trimAll](#trimall), [trimSuffix](#trimsuffix) and [trimPrefix](#trimprefix) have been added with a different signature for better pipeline chaining.

`Trim` returns a slice of the string s with all leading and
trailing Unicode code points contained in cutset removed.

Signature: `Trim(value, cutset string) string`

`TrimLeft` and `TrimRight` are the same as `Trim` except that it trims only leading and trailing characters respectively.

```template
`{{ Trim .query ",. " }}`
`{{ TrimLeft .uri ":" }}`
`{{ TrimRight .path "/" }}`
```

`TrimSpace` TrimSpace returns string s with all leading
and trailing white space removed, as defined by Unicode.

Signature: `TrimSpace(value string) string`

```template
{{ TrimSpace .latency }}
```

`TrimPrefix` and `TrimSuffix` will trim respectively the prefix or suffix supplied.

Signature:

- `TrimPrefix(value string, prefix string) string`
- `TrimSuffix(value string, suffix string) string`

```template
{{ TrimPrefix .path "/" }}
```

## regexReplaceAll and regexReplaceAllLiteral

`regexReplaceAll` returns a copy of the input string, replacing matches of the Regexp with the replacement string replacement. Inside string replacement, $ signs are interpreted as in Expand, so for instance $1 represents the text of the first sub-match. See the golang [Regexp.replaceAll documentation](https://golang.org/pkg/regexp/#Regexp.ReplaceAll) for more examples.

```template
`{{ regexReplaceAll "(a*)bc" .some_label "${1}a" }}`
```

`regexReplaceAllLiteral` function returns a copy of the input string and replaces matches of the Regexp with the replacement string replacement. The replacement string is substituted directly, without using Expand.

```template
`{{ regexReplaceAllLiteral "(ts=)" .timestamp "timestamp=" }}`
```

You can combine multiple functions using pipe. For example, to strip out spaces and make the request method in capital, you would write the following template: `{{ .request_method | TrimSpace | ToUpper }}`.

## lower

> Added in Loki 2.1

Use this function to convert to lower case.

Signature:

`lower(string) string`

Examples:

```template
"{{ .request_method | lower }}"
`{{ lower  "HELLO"}}`
```

The last example will return `hello`.

## upper

> Added in Loki 2.1

Use this function to convert to upper case.

Signature:

`upper(string) string`

Examples:

```template
"{{ .request_method | upper }}"
`{{ upper  "hello"}}`
```

This results in `HELLO`.

## title

> **Note:** Added in Loki 2.1.

Convert to title case.

Signature:

`title(string) string`

Examples:

```template
"{{.request_method | title}}"
`{{ title "hello world"}}`
```

The last example will return `Hello World`.

## trunc

> **Note:** Added in Loki 2.1.

Truncate a string and add no suffix.

Signature:

`trunc(count int,value string) string`

Examples:

```template
"{{ .path | trunc 2 }}"
`{{ trunc 5 "hello world"}}`   // output: hello
`{{ trunc -5 "hello world"}}`  // output: world
```

## substr

> **Note:** Added in Loki 2.1.

Get a substring from a string.

Signature:

`trunc(start int,end int,value string) string`

If start is < 0, this calls value[:end].
If start is >= 0 and end < 0 or end bigger than s length, this calls value[start:]
Otherwise, this calls value[start, end].

Examples:

```template
"{{ .path | substr 2 5 }}"
`{{ substr 0 5 "hello world"}}`  // output: hello
`{{ substr 6 11 "hello world"}}` // output: world
```

## replace

> **Note:** Added in Loki 2.1.

This function performs simple string replacement.

Signature: `replace(old string, new string, src string) string`

It takes three arguments:

- `old` string to replace
- `new` string to replace with
- `src` source string

Examples:

```template
{{ .cluster | replace "-cluster" "" }}
{{ replace "hello" "world" "hello world" }}
```

The last example will return `world world`.

## trim

> **Note:** Added in Loki 2.1.

The trim function removes space from either side of a string.

Signature: `trim(string) string`

Examples:

```template
{{ .ip | trim }}
{{ trim "   hello    " }} // output: hello
```

## trimAll

> **Note:** Added in Loki 2.1.

Use this function to remove given characters from the front or back of a string.

Signature: `trimAll(chars string,src string) string`

Examples:

```template
{{ .path | trimAll "/" }}
{{ trimAll "$" "$5.00" }} // output: 5.00
```

## trimSuffix

> **Note:** Added in Loki 2.1.

Use this function to trim just the suffix from a string.

Signature: `trimSuffix(suffix string, src string) string`

Examples:

```template
{{  .path | trimSuffix "/" }}
{{ trimSuffix "-" "hello-" }} // output: hello
```

## trimPrefix

> **Note:** Added in Loki 2.1.

Use this function to trim just the prefix from a string.

Signature: `trimPrefix(suffix string, src string) string`

Examples:

```template
{{  .path | trimPrefix "/" }}
{{ trimPrefix "-" "-hello" }} // output: hello
```

## indent

> **Note:** Added in Loki 2.1.

The indent function indents every line in a given string to the specified indent width. This is useful when aligning multi-line strings.

Signature: `indent(spaces int,src string) string`

```template
{{ indent 4 .query }}
```

This indents each line contained in the `.query` by four (4) spaces.

## nindent

> **Note:** Added in Loki 2.1.

The nindent function is the same as the indent function, but prepends a new line to the beginning of the string.

Signature: `nindent(spaces int,src string) string`

```template
{{ nindent 4 .query }}
```

This will indent every line of text by 4 space characters and add a new line to the beginning.

## repeat

> **Note:** Added in Loki 2.1.

Use this function to repeat a string multiple times.

Signature: `repeat(c int,value string) string`

```template
{{ repeat 3 "hello" }} // output: hellohellohello
```

## contains

> **Note:** Added in Loki 2.1.

Use this function to test to see if one string is contained inside of another.

Signature: `contains(s string, src string) bool`

Examples:

```template
{{ if .err contains "ErrTimeout" }} timeout {{end}}
{{ if contains "he" "hello" }} yes {{end}}
```

## hasPrefix and hasSuffix

> **Note:** Added in Loki 2.1.

The `hasPrefix` and `hasSuffix` functions test whether a string has a given prefix or suffix.

Signatures:

- `hasPrefix(prefix string, src string) bool`
- `hasSuffix(suffix string, src string) bool`

Examples:

```template
{{ if .err hasSuffix "Timeout" }} timeout {{end}}
{{ if hasPrefix "he" "hello" }} yes {{end}}
```

## add

> **Note:** Added in Loki 2.3.

Sum numbers. Supports multiple numbers

Signature: `func(i ...interface{}) int64`

```template
{{ add 3 2 5 }} // output: 10
```

## sub

> **Note:** Added in Loki 2.3.

Subtract numbers.

Signature: `func(a, b interface{}) int64`

```template
{{ sub 5 2 }} // output: 3
```

## mul

> **Note:** Added in Loki 2.3.

Mulitply numbers. Supports multiple numbers.

Signature: `func(a interface{}, v ...interface{}) int64`

```template
{{ mul 5 2 3}} // output: 30
```

## div

> **Note:** Added in Loki 2.3.

Integer divide numbers.

Signature: `func(a, b interface{}) int64`

```template
{{ div 10 2}} // output: 5
```

## addf

> **Note:** Added in Loki 2.3.

Sum numbers. Supports multiple numbers.

Signature: `func(i ...interface{}) float64`

```template
{{ addf 3.5 2 5 }} // output: 10.5
```

## subf

> **Note:** Added in Loki 2.3.

Subtract numbers. Supports multiple numbers.

Signature: `func(a interface{}, v ...interface{}) float64`

```template
{{ subf  5.5 2 1.5 }} // output: 2
```

## mulf

> **Note:** Added in Loki 2.3.

Mulitply numbers. Supports multiple numbers

Signature: `func(a interface{}, v ...interface{}) float64`

```template
{{ mulf 5.5 2 2.5 }} // output: 27.5
```

## divf

> **Note:** Added in Loki 2.3.

Divide numbers. Supports multiple numbers.

Signature: `func(a interface{}, v ...interface{}) float64`

```template
{{ divf 10 2 4}} // output: 1.25
```

## mod

> **Note:** Added in Loki 2.3.

Modulo wit mod.

Signature: `func(a, b interface{}) int64`

```template
{{ mod 10 3}} // output: 1
```

## max

> **Note:** Added in Loki 2.3.

Return the largest of a series of integers:

Signature: `max(a interface{}, i ...interface{}) int64`

```template
{{ max 1 2 3 }} //output 3
```

## min

> **Note:** Added in Loki 2.3.

Return the smallest of a series of integers.

Signature: `min(a interface{}, i ...interface{}) int64`

```template
{{ max 1 2 3 }} //output 1
```

## maxf

> **Note:** Added in Loki 2.3.

Return the largest of a series of floats:

Signature: `maxf(a interface{}, i ...interface{}) float64`

```template
{{ maxf 1 2.5 3 }} //output 3
```

## minf

> **Note:** Added in Loki 2.3.

Return the smallest of a series of floats.

Signature: `minf(a interface{}, i ...interface{}) float64`

```template
{{ minf 1 2.5 3 }} //output 1.5
```

## ceil

> **Note:** Added in Loki 2.3.

Returns the greatest float value greater than or equal to input value

Signature: `ceil(a interface{}) float64`

```template
{{ ceil 123.001 }} //output 124.0
```

## floor

> **Note:** Added in Loki 2.3.

Returns the greatest float value less than or equal to input value

Signature: `floor(a interface{}) float64`

```template
{{ floor 123.9999 }} //output 123.0
```

## round

> **Note:** Added in Loki 2.3.

Returns a float value with the remainder rounded to the given number of digits after the decimal point.

Signature: `round(a interface{}, p int, rOpt ...float64) float64`

```template
{{ round 123.555555 3 }} //output 123.556
```

We can also provide a `roundOn` number as third parameter

```template
{{ round 123.88571428571 5 .2 }} //output 123.88572
```

With default `roundOn` of `.5` the above value would be `123.88571`

## int

> **Note:** Added in Loki 2.3.

Convert value to an int.

Signature: `toInt(v interface{}) int`

```template
{{ "3" | int }} //output 3
```

## float64

> **Note:** Added in Loki 2.3.

Convert to a float64.

Signature: `toFloat64(v interface{}) float64`

```template
{{ "3.5" | float64 }} //output 3.5
```

## fromJson

> **Note:** Added in Loki 2.3.

fromJson decodes a JSON document into a structure. If the input cannot be decoded as JSON the function will return an empty string.

```template
fromJson "{\"foo\": 55}"
```

Example of a query to print a newline per queries stored as a json array in the log line:

```logql
{job="loki/querier"} |= "finish in prometheus" | logfmt | line_format "{{ range $q := fromJson .queries }} {{ $q.query }} {{ end }}"
```

## now

`now` returns the current local time.

```template
{{ now }}
```

## toDate

`toDate` parses a formatted string and returns the time value it represents.

```template
{{ toDate "2006-01-02" "2021-11-02" }}
```

## date

`date` returns a textual representation of the time value formatted according to the provided [golang datetime layout](https://pkg.go.dev/time#pkg-constants).

```template
{ date "2006-01-02" now }}
```

## unixEpoch

`unixEpoch` returns the number of seconds elapsed since January 1, 1970 UTC.

```template
{ unixEpoch now }}
```

Example of a query to filter Loki querier jobs which create time is 1 day before:
```logql
{job="loki/querier"} | label_format nowEpoch=`{{(unixEpoch now)}}`,createDateEpoch=`{{unixEpoch (toDate "2006-01-02" .createDate)}}` | label_format dateTimeDiff="{{sub .nowEpoch .createDateEpoch}}" | dateTimeDiff > 86400
```
