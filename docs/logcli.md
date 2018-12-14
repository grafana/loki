# Log CLI usage Instructions

Loki's main UI is Grafana; however, a basic CLI is provided as a proof of concept.

Once you have Loki running in a cluster, you can query logs from that cluster using the following commands:

```
$ go get github.com/grafana/loki/cmd/logcli
$ export GRAFANA_ADDR=https://logs-us-west1.grafana.net
$ export GRAFANA_USERNAME=<username>
$ export GRAFANA_PASSWORD=<password>
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

The URLs of the requests are printed to help with integration work.

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
  -t, --tail     Tail the logs

Args:
  <query>    eg '{foo="bar",baz="blip"}'
  [<regex>]
```
