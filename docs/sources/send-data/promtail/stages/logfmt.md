---
title: logfmt
menuTitle:  
description: The 'logfmt' Promtail pipeline stage. The logfmt parsing stage reads logfmt log lines and extracts the data into labels.
aliases: 
- ../../../clients/promtail/stages/logfmt/
weight:  
---

# logfmt

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The `logfmt` stage is a parsing stage that reads the log line as [logfmt](https://brandur.org/logfmt) and allows extraction of data into labels.

## Schema

```yaml
logfmt:
  # Set of key/value pairs for mapping of logfmt fields to extracted labels. The YAML key will be
  # the key in the extracted data, while the expression will be the YAML value. If the value
  # is empty, then the logfmt field with the same name is extracted.
  mapping:
    [ <string>: <string> ... ]

  # Name from extracted data to parse. If empty, uses the log message.
  [source: <string>]
```

This stage uses the [go-logfmt](https://github.com/go-logfmt/logfmt) unmarshaler, which means non-string types like
numbers or booleans will be unmarshaled into those types. The extracted data
can hold non-string values, and this stage does not do any type conversions;
downstream stages will need to perform correct type conversion of these values
as necessary. Please refer to the [`template` stage](../template/) for how
to do this.

If the value extracted is a complex type, its value is extracted as a string.

## Examples

### Using log line

For the given pipeline:

```yaml
- logfmt:
    mapping:
      timestamp: time
      app:
      duration:
      unknown:
```

Given the following log line:

```
time=2012-11-01T22:08:41+00:00 app=loki level=WARN duration=125 message="this is a log line" extra="user=foo""
```

The following key-value pairs would be created in the set of extracted data:

- `timestamp`: `2012-11-01T22:08:41+00:00`
- `app`: `loki`
- `duration`: `125`

### Using extracted data

For the given pipeline:

```yaml
- logfmt:
    mapping:
      extra:
- logfmt:
    mapping:
      user:
    source: extra
```

And the given log line:

```
time=2012-11-01T22:08:41+00:00 app=loki level=WARN duration=125 message="this is a log line" extra="user=foo"
```

The first stage would create the following key-value pairs in the set of
extracted data:

- `extra`: `user=foo`

The second stage will parse the value of `extra` from the extracted data as logfmt
and append the following key-value pairs to the set of extracted data:

- `user`: `foo`
