# `docker` stage

The `docker` stage is a parsing stage that reads log lines in the standard
format of Docker log files.

## Schema

```yaml
docker: {}
```

Unlike most stages, the `docker` stage provides no configuration options and
only supports the specific Docker log format. Each log line from Docker is
written as JSON with the following keys:

1. `log`: The content of log line
2. `stream`: Either `stdout` or `stderr`
3. `time`: The timestamp string of the log line

## Examples

For the given pipeline:

```yaml
- docker: {}
```

Given the following log line:

```
{"log":"log message\n","stream":"stderr","time":"2019-04-30T02:12:41.8443515Z"}
```

The following key-value pairs would be created in the set of extracted data:

- `output`: `log message\n`
- `stream`: `stderr`
- `timestamp`: `2019-04-30T02:12:41.8443515`
