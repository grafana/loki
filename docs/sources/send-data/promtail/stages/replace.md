---
title: replace
menuTitle:  
description: The 'replace' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/replace/
weight:  
---

# replace

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The `replace` stage is a parsing stage that parses a log line using a regular
expression and replaces the log line. Named capture groups in the regex support adding data into the
extracted map.

## Schema

```yaml
replace:
  # The RE2 regular expression. Each named capture group will be added to extracted.
  # Each capture group and named capture group will be replaced with the value given in `replace`
  expression: <string>

  # Name from extracted data to parse. If empty, uses the log message.
  # The replaced value will be assigned back to soure key
  [source: <string>]

  # Value to which the captured group will be replaced. The captured group or the named captured group will be
  # replaced with this value and the log line will be replaced with new replaced values. An empty value will
  # remove the captured group from the log line.
  [replace: <string>]
```

`expression` needs to be a [Go RE2 regex
string](https://github.com/google/re2/wiki/Syntax). Every named capture group `(?P<name>re)`
will be set into the `extracted` map. The name of the capture group will be used as the key in the
extracted map.

Because of how YAML treats backslashes in double-quoted strings, note that all
backslashes in a regex expression must be escaped when using double quotes. For
example, all of these are valid:

- `expression: \w*`
- `expression: '\w*'`
- `expression: "\\w*"`

But these are not:

- `expression: \\w*` (only escape backslashes when using double quotes)
- `expression: '\\w*'` (only escape backslashes when using double quotes)
- `expression: "\w*"` (backslash must be escaped)

## Example

### Without `source`

Given the pipeline:

```yaml
- replace:
    expression: "password (\\S+)"
    replace: "****"
```

And the log line:

```
2019-01-01T01:00:00.000000001Z stderr P i'm a log message who has sensitive information with password xyz!
```

The log line becomes

```
2019-01-01T01:00:00.000000001Z stderr P i'm a log message who has sensitive information with password ****!
```


### With `source`

Given the pipeline:

```yaml
- json:
    expressions:
     level:
     msg:
- replace:
    expression: "\\S+ - \"POST (\\S+) .*"
    source:     "msg"
    replace: "/loki/api/v1/push"
```

And the log line:

```
{"time":"2019-01-01T01:00:00.000000001Z", "level": "info", "msg":"11.11.11.11 - "POST /loki/api/push/ HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"}
```

The first stage would add the following key-value pairs into the `extracted`
map:

- `time`: `2019-01-01T01:00:00.000000001Z`
- `level`: `info`
- `msg`: `11.11.11.11 - "POST /loki/api/push/ HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"`

While the replace stage would then parse the value for `msg` in the extracted map
and replaces the `msg` value. `msg` in extracted will now become

- `msg`: `11.11.11.11 - "POST /loki/api/v1/push/ HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"`

### With `replace` value in `template` format

Given the pipeline:

```yaml
- replace:
    expression: "^(\\S+) (\\S+) (\\S+) \\[([\\w:/]+\\s[+\\-]\\d{4})\\] \"(\\S+)\\s?(\\S+)?\\s?(\\S+)?\" (\\d{3}|-) (\\d+|-)\\s?\"?([^\"]*)\"?\\s?\"?([^\"]*)?\"?$"
    replace: '{{ if eq .Value "200" }}{{ Replace .Value "200" "HttpStatusOk" -1 }}{{ else }}{{ .Value | ToUpper }}{{ end }}'
```

And the log line:

```
11.11.11.11 - frank [25/Jan/2000:14:00:01 -0500] "GET /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
```

The replace stage parses the log line and if the captured group has a value `200` it replaces the value to `HttpStatusOk`.

The log line would become

```
11.11.11.11 - frank [25/Jan/2000:14:00:01 -0500] "GET /1986.js HTTP/1.1" HttpStatusOk 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
```

### With `replace` value in `template` format with hashing for obfuscating data

To obfuscate sensitive data, you can combine the `replace` stage with the `Hash` template method.

```yaml
- replace:
    # SSN
    expression: '([0-9]{3}-[0-9]{2}-[0-9]{4})'
    replace: '*SSN*{{ .Value | Hash "salt" }}*'
- replace:
    # IP4
    expression: '(\d{1,3}[.]\d{1,3}[.]\d{1,3}[.]\d{1,3})'
    replace: '*IP4*{{ .Value | Hash "salt" }}*'    
- replace:
    # email
    expression: '([\w\.=-]+@[\w\.-]+\.[\w]{2,64})'
    replace: '*email*{{ .Value | Hash "salt" }}*'  
- replace:
    # creditcard
    expression: '((?:\d[ -]*?){13,16})'
    replace: '*creditcard*{{ .Value | Hash "salt" }}*'  
```

### `replace` with named captured group

Given the pipeline:

```yaml
- replace:
    expression: "^(?P<ip>\\S+) (?P<identd>\\S+) (?P<user>\\S+) \\[(?P<timestamp>[\\w:/]+\\s[+\\-]\\d{4})\\] \"(?P<action>\\S+)\\s?(?P<path>\\S+)?\\s?(?P<protocol>\\S+)?\" (?P<status>\\d{3}|-) (?P<size>\\d+|-)\\s?\"?(?P<referer>[^\"]*)\"?\\s?\"?(?P<useragent>[^\"]*)?\"?$"
    replace: '{{ .Value | ToUpper }}'
```

And the log line:

```
11.11.11.11 - frank [25/Jan/2000:14:00:01 -0500] "GET /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
```

The replace stage parses the log line and replaces the value of all named captured groups to upper case. The
named captured groups will be extracted to

- `ip`: `11.11.11.11`
- `identd`: `-`
- `user`: `FRANK`
- `timestamp`: `25/JAN/2000:14:00:01 -0500`
- `action`: `GET`
- `path`: `/1986.JS`
- `protocol`: `HTTP/1.1`
- `status`: `200`
- `size`: `932`
- `referer`:  `-`
- `useragent`: `MOZILLA/5.0 (WINDOWS; U; WINDOWS NT 5.1; DE; RV:1.9.1.7) GECKO/20091221 FIREFOX/3.5.7 GTB6"`

The log line would become

```
11.11.11.11 - FRANK [25/JAN/2000:14:00:01 -0500] "GET /1986.JS HTTP/1.1" 200 932 "-" "MOZILLA/5.0 (WINDOWS; U; WINDOWS NT 5.1; DE; RV:1.9.1.7) GECKO/20091221 FIREFOX/3.5.7 GTB6"
```

### `replace` with both named captured group and only captured group

Given the pipeline:

```yaml
- replace:
    expression: "^(?P<ip>\\S+) (?P<identd>\\S+) (\\S+) \\[(?P<timestamp>[\\w:/]+\\s[+\\-]\\d{4})\\] \"(?P<action>\\S+)\\s?(?P<path>\\S+)?\\s?(?P<protocol>\\S+)?\" (?P<status>\\d{3}|-) (?P<size>\\d+|-)\\s?\"?(?P<referer>[^\"]*)\"?\\s?\"?(?P<useragent>[^\"]*)?\"?$"
    replace: '{{ .Value | ToUpper }}'
```

And the log line:

```
11.11.11.11 - frank [25/Jan/2000:14:00:01 -0500] "GET /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
```

The replace stage parses the log line and replaces the value of all named captured groups to upper case. The
named captured groups will be extracted to. Observe here that `user` is not extracted since it was just `(\\S+)` and not a named captured group like this `(?P<user>\\S+)`

- `ip`: `11.11.11.11`
- `identd`: `-`
- `timestamp`: `25/JAN/2000:14:00:01 -0500`
- `action`: `GET`
- `path`: `/1986.JS`
- `protocol`: `HTTP/1.1`
- `status`: `200`
- `size`: `932`
- `referer`:  `-`
- `useragent`: `MOZILLA/5.0 (WINDOWS; U; WINDOWS NT 5.1; DE; RV:1.9.1.7) GECKO/20091221 FIREFOX/3.5.7 GTB6"`

The log line would become

```
11.11.11.11 - FRANK [25/JAN/2000:14:00:01 -0500] "GET /1986.JS HTTP/1.1" 200 932 "-" "MOZILLA/5.0 (WINDOWS; U; WINDOWS NT 5.1; DE; RV:1.9.1.7) GECKO/20091221 FIREFOX/3.5.7 GTB6"
```

### With empty `replace`

Given the pipeline:

```yaml
- replace:
    expression: "11.11.11.11 - (\\S+\\s)"
    replace: ""
```

And the log line:

```
11.11.11.11 - frank [25/Jan/2000:14:00:01 -0500] "GET /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
```

The log line becomes

```
11.11.11.11 - [25/Jan/2000:14:00:01 -0500] "GET /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
```
