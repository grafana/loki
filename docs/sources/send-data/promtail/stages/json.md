---
title: json
menuTitle:  
description: The 'json' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/json/
weight:  
---

# json

The `json` stage is a parsing stage that reads the log line as JSON and accepts
[JMESPath](http://jmespath.org/) expressions to extract data.

## Schema

```yaml
json:
  # Set of key/value pairs of JMESPath expressions. The key will be
  # the key in the extracted data while the expression will be the value,
  # evaluated as a JMESPath from the source data.
  #
  # Literal JMESPath expressions can be done by wrapping a key in
  # double quotes, which then must be wrapped in single quotes in
  # YAML so they get passed to the JMESPath parser.
  expressions:
    [ <string>: <string> ... ]

  # Name from extracted data to parse. If empty, uses the log message.
  [source: <string>]

  # When true, then any lines that cannot be successfully parsed as valid JSON objects
  # will be dropped instead of being sent to Loki.
  [drop_malformed: <bool> | default = false]
```

This stage uses the Go JSON unmarshaler, which means non-string types like
numbers or booleans will be unmarshaled into those types. The extracted data
can hold non-string values and this stage does not do any type conversions;
downstream stages will need to perform correct type conversion of these values
as necessary. Please refer to the [the `template` stage]({{< relref "./template" >}}) for how
to do this.

If the value extracted is a complex type, such as an array or a JSON object, it
will be converted back into a JSON string before being inserted into the
extracted data.

## Examples

### Using log line

For the given pipeline:

```yaml
- json:
    expressions:
      output: log
      stream: stream
      timestamp: time
```

Given the following log line:

```
{"log":"log message\n","stream":"stderr","time":"2019-04-30T02:12:41.8443515Z"}
```

The following key-value pairs would be created in the set of extracted data:

- `output`: `log message\n`
- `stream`: `stderr`
- `timestamp`: `2019-04-30T02:12:41.8443515`

### Using extracted data

For the given pipeline:

```yaml
- json:
    expressions:
      output:    log
      stream:    stream
      timestamp: time
      extra:
- json:
    expressions:
      user:
    source: extra
```

And the given log line:

```
{"log":"log message\n","stream":"stderr","time":"2019-04-30T02:12:41.8443515Z","extra":"{\"user\":\"marco\"}"}
```

The first stage would create the following key-value pairs in the set of
extracted data:

- `output`: `log message\n`
- `stream`: `stderr`
- `timestamp`: `2019-04-30T02:12:41.8443515`
- `extra`: `{"user": "marco"}`

The second stage will parse the value of `extra` from the extracted data as JSON
and append the following key-value pairs to the set of extracted data:

- `user`: `marco`

### Using a JMESPath Literal

This pipeline uses a literal JMESPath expression to parse JSON fields with
special characters in the name, like `@` or `.`

For the given pipeline:

```yaml
- json:
    expressions:
      output: log
      stream: '"grpc.stream"'
      timestamp: time
```

Given the following log line:

```
{"log":"log message\n","grpc.stream":"stderr","time":"2019-04-30T02:12:41.8443515Z"}
```

The following key-value pairs would be created in the set of extracted data:

- `output`: `log message\n`
- `stream`: `stderr`
- `timestamp`: `2019-04-30T02:12:41.8443515`

{{< admonition type="note" >}}
Referring to `grpc.stream` without the combination of double quotes
wrapped in single quotes will not work properly.
{{< /admonition >}}
