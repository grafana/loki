# Loki: Like Prometheus, but for logs.

[![CircleCI](https://circleci.com/gh/grafana/loki/tree/master.svg?style=svg&circle-token=618193e5787b2951c1ea3352ad5f254f4f52313d)](https://circleci.com/gh/grafana/loki/tree/master) [Design doc](https://docs.google.com/document/d/11tjK_lvp1-SVsFZjgOTr1vV3-q6vBAsZYIQ5ZeYBkyM/edit)

Loki is a horizontally-scalable, highly-available, multi-tenant, log aggregation
system inspired by Prometheus.  It is designed to be very cost effective, as it does
not index the contents of the logs, but rather a set of labels for each log stream.

## Run it locally

Loki can be run in a single host, no-dependencies mode using the following commands.

Loki consists of 3 components; `loki` is the main server, responsible for storing
logs and processing queries.  `promtail` is the agent, responsible for gather logs
and sending them to loki and `grafana` as the UI.

To run loki, use the following commands:

```
$ go build ./cmd/loki
$ ./loki -config.file=./docs/loki-local-config.yaml
...
```

To run promtail, use the following commands:

```
$ go build ./cmd/promtail
$ ./promtail -config.file=./docs/promtail-local-config.yaml
...
```

Grafana is Loki's UI, so you'll also want to run one of those:

```
$ docker run -ti -p 3000:3000 -e "GF_EXPLORE_ENABLED=true" grafana/grafana:master
```

In the Grafana UI (http://localhost:3000), log in with "admin"/"admin", add a new "Grafana Logging" datasource for `http://host.docker.internal:80`, then go to explore and enjoy!

## Usage Instructions

Loki is running in the ops-tools1 cluster.  You can query logs from that cluster
using the following commands:

```
$ go get github.com/grafana/loki/cmd/logcli
$ . $GOPATH/src/github.com/grafana/loki/env # env vars inc. URL, username etc
$ logcli labels job
https://logs-dev-ops-tools1.grafana.net/api/prom/label/job/values
cortex-ops/consul
cortex-ops/cortex-gw
...
$ logcli query '{job="cortex-ops/consul"}'
https://logs-dev-ops-tools1.grafana.net/api/prom/query?query=%7Bjob%3D%22cortex-ops%2Fconsul%22%7D&limit=30&start=1529928228&end=1529931828&direction=backward&regexp=
Common labels: {job="cortex-ops/consul", namespace="cortex-ops"}
2018-06-25T12:52:09Z {instance="consul-8576459955-pl75w"} 2018/06/25 12:52:09 [INFO] raft: Snapshot to 475409 complete
2018-06-25T12:52:09Z {instance="consul-8576459955-pl75w"} 2018/06/25 12:52:09 [INFO] raft: Compacting logs from 456973 to 465169
```

The `logcli` command is temporary until we have Grafana integration. The URLs of
the requests are printed to help with integration work.

```
$ logcli help
usage: logcli [<flags>] <command> [<args> ...]

A command-line for loki.

Flags:
  --help         Show context-sensitive help (also try --help-long and --help-man).
  --addr="https://log-us.grafana.net"
                 Server address.
  --username=""  Username for HTTP basic auth.
  --password=""  Password for HTTP basic auth.

Commands:
  help [<command>...]
    Show help.

  query [<flags>] <query> [<regex>]
    Run a LogQL query.

  labels <label>
    Find values for a given label.

$ logcli help query
usage: logcli query [<flags>] <query> [<regex>]

Run a LogQL query.

Flags:
  --help         Show context-sensitive help (also try --help-long and --help-man).
  --addr="https://log-us.grafana.net"
                 Server address.
  --username=""  Username for HTTP basic auth.
  --password=""  Password for HTTP basic auth.
  --limit=30     Limit on number of entries to print.
  --since=1h     Lookback window.
  --forward      Scan forwards through logs.

Args:
  <query>    eg '{foo="bar",baz="blip"}'
  [<regex>]
```
