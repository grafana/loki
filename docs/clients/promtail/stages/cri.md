# `cri` stage

The `cri` stage is a parsing stage that reads the log line as the way that cri generated.

## Format

Each log from cri is string format and covert following three parts:
1. `log`: the content of log
2. `stream`: stdout/stderr
3. `time`: the timestamp string of log

Each part is splited with blank with others and no blanks in each part.

So in the following logs, only the first is cri format which `cri` stage can format:
```
"2019-01-01T01:00:00.000000001Z stderr P test\ngood"
"2019-01-01 T01:00:00.000000001Z stderr testgood"
"2019-01-01T01:00:00.000000001Z testgood"
```

More case refer to [entry_parser_test](../../../../pkg/promtail/api/entry_parser_test.go).

## Examples

### Using log line

```yaml
cri: {}
```

Given the following log line:

```
"2019-04-30T02:12:41.8443515Z stdout xx message"
```

The following key-value pairs would be created in the set of extracted data:

- `output`: `message`
- `stream`: `stdout`
- `timestamp`: `2019-04-30T02:12:41.8443515`
