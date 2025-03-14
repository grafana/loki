---
title: timestamp
menuTitle:  
description: The 'timestamp' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/timestamp/
weight:  
---

# timestamp

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The `timestamp` stage is an action stage that can change the timestamp of a log
line before it is sent to Loki. When a `timestamp` stage is not present, the
timestamp of a log line defaults to the time when the log entry is scraped.

## Schema

```yaml
timestamp:
  # Name from extracted data to use for the timestamp.
  source: <string>

  # Determines how to parse the time string. Can use
  # pre-defined formats by name: [ANSIC UnixDate RubyDate RFC822
  # RFC822Z RFC850 RFC1123 RFC1123Z RFC3339 RFC3339Nano Unix
  # UnixMs UnixUs UnixNs].
  format: <string>

  # Fallback formats to try if the format fails to parse the value
  # Can use pre-defined formats by name: [ANSIC UnixDate RubyDate RFC822
  # RFC822Z RFC850 RFC1123 RFC1123Z RFC3339 RFC3339Nano Unix
  # UnixMs UnixUs UnixNs].
  [fallback_formats: []<string>]

  # IANA Timezone Database string.
  [location: <string>]

  # Which action should be taken in case the timestamp can't
  # be extracted or parsed. Valid values are: [skip, fudge].
  # Defaults to "fudge".
  [action_on_failure: <string>]
```

### Reference Time

The `format` field can be how the reference time, defined to be `Mon Jan 2 15:04:05 -0700 MST 2006`, would be interpreted in the format or can alternatively be one of the following common forms:

- `ANSIC`: `Mon Jan _2 15:04:05 2006`
- `UnixDate`: `Mon Jan _2 15:04:05 MST 2006`
- `RubyDate`: `Mon Jan 02 15:04:05 -0700 2006`
- `RFC822`: `02 Jan 06 15:04 MST`
- `RFC822Z`: `02 Jan 06 15:04 -0700`
- `RFC850`: `Monday, 02-Jan-06 15:04:05 MST`
- `RFC1123`: `Mon, 02 Jan 2006 15:04:05 MST`
- `RFC1123Z`: `Mon, 02 Jan 2006 15:04:05 -0700`
- `RFC3339`: `2006-01-02T15:04:05-07:00`
- `RFC3339Nano`: `2006-01-02T15:04:05.999999999-07:00`

Additionally, support for common Unix timestamps is supported with the following
`format` values:

- `Unix`: `1562708916` or with fractions `1562708916.000000123`
- `UnixMs`: `1562708916414`
- `UnixUs`: `1562708916414123`
- `UnixNs`: `1562708916000000123`

Custom formats are passed directly to the layout parameter in Go's
[time.Parse](https://golang.org/pkg/time/#Parse) function. If the custom format
has no year component specified, Promtail will assume that the current year
according to the system's clock should be used.

The syntax used by the custom format defines the reference date and time using
specific values for each component of the timestamp (i.e., `Mon Jan 2 15:04:05
-0700 MST 2006`). The following table shows supported reference values which
should be used in the custom format.

| Timestamp component | Format value                                                                                                                         |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| Year                | `06`, `2006`                                                                                                                         |
| Month               | `1`, `01`, `Jan`, `January`                                                                                                          |
| Day                 | `2`, `02`, `_2` (two digits right justified)                                                                                         |
| Day of the week     | `Mon`, `Monday`                                                                                                                      |
| Hour                | `3` (12-hour), `03` (12-hour zero prefixed), `15` (24-hour)                                                                          |
| Minute              | `4`, `04`                                                                                                                            |
| Second              | `5`, `05`                                                                                                                            |
| Fraction of second  | `.000` (ms zero prefixed), `.000000` (μs), `.000000000` (ns), `.999` (ms without trailing zeroes), `.999999` (μs), `.999999999` (ns) |
| 12-hour period      | `pm`, `PM`                                                                                                                           |
| Timezone name       | `MST`                                                                                                                                |
| Timezone offset     | `-0700`, `-070000` (with seconds), `-07`, `07:00`, `-07:00:00` (with seconds)                                                        |
| Timezone ISO-8601   | `Z0700` (Z for UTC or time offset), `Z070000`, `Z07`, `Z07:00`, `Z07:00:00`                                                          |

In order to correctly format the time, for the timestamp `2006/01/02 03:04:05.000`:

- If you want a 24-hour format you should be using `15:04:0.000`.
- If you want a 12-hour format you should be using either `3:04:05.000 PM` or `03:04:05.000 PM`.

### Action on Failure

The `action_on_failure` setting defines which action should be taken by the
stage in case the `source` field doesn't exist in the extracted data or the
timestamp parsing fails. The supported actions are:

- `fudge` (default): change the timestamp to the last known timestamp, summing
  up 1 nanosecond (to guarantee log entries ordering)
- `skip`: do not change the timestamp and keep the time when the log entry has
  been scraped by Promtail

## Examples

```yaml
- timestamp:
    source: time
    format: RFC3339Nano
```

This stage looks for a `time` field in the extracted map and reads its value in
`RFC3339Nano` form (e.g., `2006-01-02T15:04:05.999999999-07:00`). The resulting
time value will be used as the timestamp sent with the log line to Loki.
