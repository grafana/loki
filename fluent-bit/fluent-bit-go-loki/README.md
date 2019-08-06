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
$ make
```

### Configuration Options

| Key           | Description                                   | Default                             |
| --------------|-----------------------------------------------|-------------------------------------|
| Url           | Url of loki server API endpoint               | http://localhost:3100/api/prom/push |
| BatchWait     | Time to wait before send a log batch to Loki, full or not. (unit: sec) | 1 second                     |
| BatchSize     | Log batch size to send a log batch to Loki (unit: Bytes)    | 10 KiB (10 * 1024 Bytes)                            |
| Labels        | labels for API requests                       | job="fluent-bit" (describe below)   |

Example:

add this section to fluent-bit.conf

```properties
[Output]
    Name loki
    Match *
    Url http://localhost:3100/api/prom/push
    BatchWait 10 # (10msec)
    BatchSize 30 # (30KiB)
    Labels {test="fluent-bit-go", lang="Golang"}
```

## Useful links

* [fluent-bit-go](https://github.com/fluent/fluent-bit-go)
