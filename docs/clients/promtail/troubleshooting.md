# Troubleshooting Promtail

This document describes known failure modes of `promtail` on edge cases and the
adopted trade-offs.

## A tailed file is truncated while `promtail` is not running

Given the following order of events:

1. `promtail` is tailing `/app.log`
2. `promtail` current position for `/app.log` is `100` (byte offset)
3. `promtail` is stopped
4. `/app.log` is truncated and new logs are appended to it
5. `promtail` is restarted

When `promtail` is restarted, it reads the previous position (`100`) from the
positions file. Two scenarios are then possible:

- `/app.log` size is less than the position before truncating
- `/app.log` size is greater than or equal to the position before truncating

If the `/app.log` file size is less than the previous position, then the file is
detected as truncated and logs will be tailed starting from position `0`.
Otherwise, if the `/app.log` file size is greater than or equal to the previous
position, `promtail` can't detect it was truncated while not running and will
continue tailing the file from position `100`.

Generally speaking, `promtail` uses only the path to the file as key in the
positions file. Whenever `promtail` is started, for each file path referenced in
the positions file, `promtail` will read the file from the beginning if the file
size is less than the offset stored in the position file, otherwise it will
continue from the offset, regardless the file has been truncated or rolled
multiple times while `promtail` was not running.

## Loki is unavailable

For each tailing file, `promtail` reads a line, process it through the
configured `pipeline_stages` and push the log entry to Loki. Log entries are
batched together before getting pushed to Loki, based on the max batch duration
`client.batch-wait` and size `client.batch-size-bytes`, whichever comes first.

In case of any error while sending a log entries batch, `promtail` adopts a
"retry then discard" strategy:

- `promtail` retries to send log entry to the ingester up to `maxretries` times
- If all retries fail, `promtail` discards the batch of log entries (_which will
  be lost_) and proceeds with the next one

You can configure the `maxretries` and the delay between two retries via the
`backoff_config` in the promtail config file:

```yaml
clients:
  - url: INGESTER-URL
    backoff_config:
      minbackoff: 100ms
      maxbackoff: 10s
      maxretries: 10
```

The following table shows an example of the total delay applied by the backoff algorithm
with `minbackoff: 100ms` and `maxbackoff: 10s`:

| Retry | Min delay | Max delay | Total min delay | Total max delay |
| ----- | --------- | --------- | --------------- | --------------- |
| 1     | 100ms     | 200ms     | 100ms           | 200ms           |
| 2     | 200ms     | 400ms     | 300ms           | 600ms           |
| 3     | 400ms     | 800ms     | 700ms           | 1.4s            |
| 4     | 800ms     | 1.6s      | 1.5s            | 3s              |
| 5     | 1.6s      | 3.2s      | 3.1s            | 6.2s            |
| 6     | 3.2s      | 6.4s      | 6.3s            | 12.6s           |
| 7     | 6.4s      | 10s       | 12.7s           | 22.6s           |
| 8     | 6.4s      | 10s       | 19.1s           | 32.6s           |
| 9     | 6.4s      | 10s       | 25.5s           | 42.6s           |
| 10    | 6.4s      | 10s       | 31.9s           | 52.6s           |
| 11    | 6.4s      | 10s       | 38.3s           | 62.6s           |
| 12    | 6.4s      | 10s       | 44.7s           | 72.6s           |
| 13    | 6.4s      | 10s       | 51.1s           | 82.6s           |
| 14    | 6.4s      | 10s       | 57.5s           | 92.6s           |
| 15    | 6.4s      | 10s       | 63.9s           | 102.6s          |
| 16    | 6.4s      | 10s       | 70.3s           | 112.6s          |
| 17    | 6.4s      | 10s       | 76.7s           | 122.6s          |
| 18    | 6.4s      | 10s       | 83.1s           | 132.6s          |
| 19    | 6.4s      | 10s       | 89.5s           | 142.6s          |
| 20    | 6.4s      | 10s       | 95.9s           | 152.6s          |


## Log entries pushed after a `promtail` crash / panic / abruptly termination

When `promtail` shuts down gracefully, it saves the last read offsets in the
positions file, so that on a subsequent restart it will continue tailing logs
without duplicates neither losses.

In the event of a crash or abruptly termination, `promtail` can't save the last
read offsets in the positions file. When restarted, `promtail` will read the
positions file saved at the last sync period and will continue tailing the files
from there. This means that if new log entries have been read and pushed to the
ingester between the last sync period and the crash, these log entries will be
sent again to the ingester on `promtail` restart.

However, for each log stream (set of unique labels) the Loki ingester skips all
log entries received out of timestamp order. For this reason, even if duplicated
logs may be sent from `promtail` to the ingester, entries whose timestamp is
older than the latest received will be discarded to avoid having duplicated
logs. To leverage this, it's important that your `pipeline_stages` include
the `timestamp` stage, parsing the log entry timestamp from the log line instead
of relying on the default behaviour of setting the timestamp as the point in
time when the line is read by `promtail`.
