# `cri` stage

The `cri` stage is a parsing stage that reads the log line using the standard CRI logging format.

## Schema

```yaml
cri: {}
```

Unlike most stages, the `cri` stage provides no configuration options and only
supports the specific CRI log format. CRI specifies log lines log lines as
space-delimited values with the following components:

1. `time`: The timestamp string of the log
2. `stream`: Either stdout or stderr
3. `log`: The contents of the log line

No whitespace is permitted between the components. In the following exmaple,
only the first log line can be properly formatted using the `cri` stage:

```
"2019-01-01T01:00:00.000000001Z stderr P test\ngood"
"2019-01-01 T01:00:00.000000001Z stderr testgood"
"2019-01-01T01:00:00.000000001Z testgood"
```

## Examples

For the given pipeline:

```yaml
- cri: {}
```

Given the following log line:

```
"2019-04-30T02:12:41.8443515Z stdout xx message"
```

The following key-value pairs would be created in the set of extracted data:

- `output`: `message`
- `stream`: `stdout`
- `timestamp`: `2019-04-30T02:12:41.8443515`
