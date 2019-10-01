# `docker` stage

The `docker` stage is a parsing stage that reads the log line as the way that docker generated.
It works similar with `json` stage but no need to set anything.

## Format

Each log from docker is json format and covert following three keys:
1. `log`: the content of log
2. `stream`: stdout/stderr
3. `time`: the timestamp string of log

## Examples

### Using log line

```yaml
docker: {}
```

Given the following log line:

```
{"log":"log message\n","stream":"stderr","time":"2019-04-30T02:12:41.8443515Z"}
```

The following key-value pairs would be created in the set of extracted data:

- `output`: `log message\n`
- `stream`: `stderr`
- `timestamp`: `2019-04-30T02:12:41.8443515`
