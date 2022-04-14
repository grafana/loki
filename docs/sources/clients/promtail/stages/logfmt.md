---
title: logfmt
---
# `logfmt` stage

The `logfmt` stage is a parsing stage that reads the log line as logfmt.

## Schema

```yaml
logfmt:
  # Maps set of keys/values extracted from the data
  #
  mapping:
    [ <string>: <string> ... ]
```

This stage uses the go-kit/log structured logging parser to extract key/value pairs of data from log lines into the extracted map.

## Examples

### Using log line

For the given pipeline:

```yaml
- logfmt:
    mapping:
      level: level
      tag: tag
      module: module
```

Given the following log line:

```
level=info tag=stopping_fetchers id=ConsumerFetcherManager-1382721708341 module=kafka.consumer.ConsumerFetcherManager
```

The following key-value pairs would be created in the set of extracted data:

- `level`: `info`
- `tag`: `stopping_fetchers`
- `module`: `kafka.consumer.ConsumerFetcherManager`
