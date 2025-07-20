---
title: template
menuTitle:  
description: The 'template' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/template/
weight:  
---

# template

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The `template` stage is a transform stage that lets use manipulate the values in
the extracted map using [Go's template
syntax](https://golang.org/pkg/text/template/).

The `template` stage is primarily useful for manipulating data from other stages
before setting them as labels, such as to replace spaces with underscores or
converting an uppercase string into a lowercase one. `template` can also be used
to construct messages with multiple keys.

The template stage can also create new keys in the extracted map.

## Schema

```yaml
template:
  # Name from extracted data to parse. If key in extract data doesn't exist, an
  # entry for it will be created.
  source: <string>

  # Go template string to use. In additional to normal template
  # functions, ToLower, ToUpper, Replace, Trim, TrimLeft, TrimRight,
  # TrimPrefix, TrimSuffix, and TrimSpace are available as functions.
  template: <string>
```

## Examples

```yaml
- template:
    source: new_key
    template: 'hello world!'
```

Assuming no data has been added to the extracted map yet, this stage will first
add `new_key` with a blank value into the extracted map. Then its value will be
set to `hello world!`.

```yaml
- template:
    source: app
    template: '{{ .Value }}_some_suffix'
```

This pipeline takes the value of the `app` key in the existing extracted map and
appends `_some_suffix` to its value. For example, if the extracted map had a
key of `app` and a value of `loki`, this stage would modify the value from
`loki` to `loki_some_suffix`.

```yaml
- template:
    source: app
    template: '{{ ToLower .Value }}'
```

This pipeline takes the current value of `app` from the extracted map and
converts its value to be all lowercase. For example, if the extracted map
contained `app` with a value of `LOKI`, this pipeline would change its value to
`loki`.

```yaml
- template:
    source: output_msg
    template: '{{ .level }} for app {{ ToUpper .app }}'
```

This pipeline takes the current value of `level` and `app` from the extracted map and
a new key `output_msg` will be added to extracted map with evaluated template.

For example, if the extracted map contained `app` with a value of `loki`, this pipeline would change its value to `LOKI`. Assuming value of `level` is `warn`. A new key `output_msg` will be added to extracted map with value `warn for app LOKI`.

Any previously extracted keys can be used in `template`. All extracted keys are available for `template` to expand.

```yaml
- template:
    source: app
    template: '{{ .level }} for app {{ ToUpper .Value }} in module {{.module}}'
```

This pipeline takes the current value of `level`, `app` and `module` from the extracted map and
converts value of `app` to the evaluated template.

For example, if the extracted map contained `app` with a value of `loki`, this pipeline would change its value to `LOKI`. Assuming value of `level` is `warn` and value of `module` is `test`. Pipeline will change the value of `app` to `warn for app LOKI in module test`.

Any previously extracted keys can be used in `template`. All extracted keys are available for `template` to expand. Also, if source is available it can be referred as `.Value` in `template`. Here, `app` is provided as `source`. So, it can be referred as `.Value` in `template`.

```yaml
- template:
    source: app
    template: '{{ Replace .Value "loki" "blokey" 1 }}'
```

The template here uses Go's [`string.Replace`
function](https://golang.org/pkg/strings/#Replace). When the template executes,
the entire contents of the `app` key from the extracted map will have at most
`1` instance of `loki` changed to `blokey`.

A special key named `Entry` can be used to reference the current line, this can be useful when you need to append/prepend the log line.

```yaml
- template:
    source: message
    template: '{{.app }}: {{ .Entry }}'
- output:
    source: message
```

The snippet above will for instance prepend the log line with the application name.

```yaml
- template:
    source: time
    template: "\
      {{ .date_local | substr 6 10 }}-{{ .date_local | substr 0 2 }}-{{ .date_local | substr 3 5 }}T\
      {{ if eq (.time_local | substr 0 2) \"12\" }}\
          {{ if eq .hour_period \"AM\" }}00{{ else }}12{{ end }}{{ .time_local | substr 2 10}}Z\
      {{ else }}\
          {{ if eq .hour_period \"AM\" }}\
              {{ if or (eq (.time_local | substr 0 2) \"11\") (eq (.time_local | substr 0 2) \"10\") }}{{ .time_local }}Z\
                  {{ else }}0{{ .time_local }}Z\
              {{ end }}\
          {{ else }}\
              {{ if eq (.time_local | substr 0 2) \"11\" }}23{{ .time_local | substr 2 10 }}Z{{ end }}\
              {{ if eq (.time_local | substr 0 2) \"10\" }}22{{ .time_local | substr 2 10 }}Z{{ end }}\
              {{ if eq (.time_local | substr 0 2) \"9:\" }}21{{ .time_local | substr 1 10 }}Z{{ end }}\
              {{ if eq (.time_local | substr 0 2) \"8:\" }}20{{ .time_local | substr 1 10 }}Z{{ end }}\
              {{ if and (le (.time_local | substr 0 1) \"7\") (eq (.time_local | substr 1 2) \":\") }}1{{ add (.time_local | substr 0 1) 2 }}{{ .time_local | substr 1 10 }}Z{{ end }}\
          {{ end }}\
      {{ end }}"
  - timestamp:
      source: time
      format: RFC3339      
```

The snippet above is an example of a multiline template without spaces. The extracted data from logs using regex will be used to convert `11/08/2022, 12:53:24 PM` to `RFC3339` time format. It also makes use of functions like `eq`, `substr` and shows how we can use `if` with `and`, `or` in go templates.

## Supported Functions

> All [sprig functions](http://masterminds.github.io/sprig/) have been added to the template stage in Loki 2.3(along with function described below).

### ToLower and ToUpper

ToLower and ToUpper convert the entire string respectively to lowercase and uppercase.

Examples:

```yaml
- template:
    source: out
    template: '{{ ToLower .app }}'
```

```yaml
- template:
    source: out
    template: '{{ .app | ToUpper }}'
```

### Replace

`Replace` returns a copy of the string s with the first n non-overlapping instances of old replaced by new. If old is empty, it matches at the beginning of the string and after each UTF-8 sequence, yielding up to k+1 replacements for a k-rune string. If n < 0, there is no limit on the number of replacements.

The example below will replace the first two words `loki` by `Loki`.

```yaml
- template:
    source: output
    template: '{{ Replace .Value "loki" "Loki" 2 }}'
```

### Trim

`Trim` returns a slice of the string s with all leading and
trailing Unicode code points contained in cutset removed.

`TrimLeft` and `TrimRight` are the same as `Trim` except that it respectively trim only leading and trailing characters.

```yaml
- template:
    source: output
    template: '{{ Trim .Value ",. " }}'
```

`TrimSpace` TrimSpace returns a slice of the string s, with all leading
and trailing white space removed, as defined by Unicode.

```yaml
- template:
    source: output
    template: '{{ TrimSpace .Value }}'
```

`TrimPrefix` and `TrimSuffix` will trim respectively the prefix or suffix supplied.

```yaml
- template:
    source: output
    template: '{{ TrimPrefix .Value "--" }}'
```

### Regex

`regexReplaceAll` returns a copy of the input string, replacing matches of the Regexp with the replacement string replacement. Inside string replacement, $ signs are interpreted as in Expand, so for instance $1 represents the text of the first submatch

```yaml
- template:
    source: output
    template: '{{ regexReplaceAll "(a*)bc" .Value "${1}a" }}'
```

`regexReplaceAllLiteral` returns a copy of the input string, replacing matches of the Regexp with the replacement string replacement The replacement string is substituted directly, without using Expand.

```yaml
- template:
    source: output
    template: '{{ regexReplaceAllLiteral "(ts=)" .Value "timestamp=" }}'
```

### Hash and Sha2Hash

`Hash` returns a Sha3_256 hash of the string, represented as a hexadecimal number of 64 digits. You can use it to obfuscate sensitive data / PII in the logs. It requires a (fixed) salt value, to add complexity to low input domains (e.g. all possible Social Security Numbers).

```yaml
- template:
    source: output
    template: '{{ Hash .Value "salt" }}'
```

Alternatively, you can use `Sha2Hash` for calculating the Sha2_256 of the string. Sha2_256 is faster and requires less CPU than Sha3_256, however it is less secure.

We recommend using `Hash` as it has a stronger hashing algorithm which we plan to keep strong over time without requiring client config changes.

```yaml
- template:
    source: output
    template: '{{ Sha2Hash .Value "salt" }}'
```
