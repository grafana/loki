# fluent-bit loki output plugin

This plugin works with fluent-bit's go plugin interface.
It allows fluent-bit to ship logs into Loki, for use with e.g. Grafana.

The configuration typically looks like:

```graphviz
fluent-bit --> loki --> grafana <-- other grafana sources
```

# Usage

```bash
$ fluent-bit -e /path/to/built/out_loki.so -c fluent-bit.conf
```

# Prerequisites

* Go 1.11+
* gcc (for cgo)

## Building

```bash
$ make fluent-bit-plugin
```

### Configuration Options

| Key           | Description                                   | Default                             |
| --------------|-----------------------------------------------|-------------------------------------|
| Url           | Url of loki server API endpoint               | http://localhost:3100/loki/api/v1/push |
| BatchWait     | Time to wait before send a log batch to Loki, full or not. (unit: sec) | 1 second   |
| BatchSize     | Log batch size to send a log batch to Loki (unit: Bytes)    | 10 KiB (10 * 1024 Bytes) |
| Labels        | labels for API requests                       | job="fluent-bit"                    |
| LogLevel      | LogLevel for plugin logger                    | "info"                              |
| RemoveKeys    | Specify removing keys                         | none                                |
| LabelKeys     | Comma separated list of keys to use as stream labels. All other keys will be placed into the log line | none |
| LineFormat    | Format to use when flattening the record to a log line. Valid values are "json" or "key_value". If set to "json" the log line sent to Loki will be the fluentd record (excluding any keys extracted out as labels) dumped as json. If set to "key_value", the log line will be each item in the record concatenated together (separated by a single space) in the format <key>=<value>. | json |
| DropSingleKey | if set to true and after extracting label_keys a record only has a single key remaining, the log line sent to Loki will just be the value of the record key.| true |


Example:

add this section to fluent-bit.conf

```properties
[Output]
    Name loki
    Match *
    Url http://localhost:3100/loki/api/v1/push
    BatchWait 1 # (1sec)
    BatchSize 30720 # (30KiB)
    Labels {test="fluent-bit-go", lang="Golang"}
    RemoveKeys key1,key2
    LabelKeys key3,key4
    LineFormat key_value
```

## Useful links

* [fluent-bit-go](https://github.com/fluent/fluent-bit-go)
