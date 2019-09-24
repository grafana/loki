# `regex` stage

The `regex` stage is a parsing stage that parses a log line using a regular
expression. Named capture groups in the regex support adding data into the
extracted map.

## Schema

```yaml
regex:
  # The RE2 regular expression. Each capture group must be named.
  expression: <string>

  # Name from extracted data to parse. If empty, uses the log message.
  [source: <string>]
```

`expression` needs to be a [Go RE2 regex
string](https://github.com/google/re2/wiki/Syntax). Every capture group `(re)`
will be set into the `extracted` map, every capture group **must be named:**
`(?P<name>re)`. The name of the capture group will be used as the key in the
extracted map.

## Example

### Without `source`

Given the pipeline:

```yaml
- regex:
    expression: "^(?s)(?P<time>\\S+?) (?P<stream>stdout|stderr) (?P<flags>\\S+?) (?P<content>.*)$"
```

And the log line:

```
2019-01-01T01:00:00.000000001Z stderr P i'm a log message!
```

The following key-value pairs would be added to the extracted map:

- `time`: `2019-01-01T01:00:00.000000001Z`,
- `stream`: `stderr`,
- `flags`: `P`,
- `content`: `i'm a log message`

### With `source`

Given the pipeline:

```yaml
- json:
    expressions:
      time:
- regex:
    expression: "^(?P<year>\\d+)"
    source:     "time"
```

And the log line:

```
{"time":"2019-01-01T01:00:00.000000001Z"}
```

The first stage would add the following key-value pairs into the `extracted`
map:

- `time`: `2019-01-01T01:00:00.000000001Z`

While the regex stage would then parse the value for `time` in the extracted map
and append the following key-value pairs back into the extracted map:

- `year`: `2019`

