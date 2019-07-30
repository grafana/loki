# Log CLI usage Instructions

Loki's main query interface is Grafana; however, a basic CLI is provided as a proof of concept.

Once you have Loki running in a cluster, you can query logs from that cluster.

## Installation

### Get latest version

```
$ go get github.com/grafana/loki/cmd/logcli
```

### Build from source

```
$ go get github.com/grafana/loki
$ cd $GOPATH/src/github.com/grafana/loki
$ go build ./cmd/logcli
```

Now `logcli` is in your current directory.

## Usage

### Example

```
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

### Configuration

Configuration values are considered in the following order (lowest to highest):

- environment value
- command line

The URLs of the requests are printed to help with integration work.

### Details

```console
$ logcli help
usage: logcli [<flags>] <command> [<args> ...]

A command-line for loki.

Flags:
      --help             Show context-sensitive help (also try --help-long and --help-man).
  -q, --quiet            suppress everything but log lines
  -o, --output=default   specify output mode [default, raw, jsonl]
      --addr="https://logs-us-west1.grafana.net"  
                         Server address.
      --username=""      Username for HTTP basic auth.
      --password=""      Password for HTTP basic auth.
      --ca-cert=""       Path to the server Certificate Authority.
      --tls-skip-verify  Server certificate TLS skip verify.
      --cert=""          Path to the client certificate.
      --key=""           Path to the client certificate key.

Commands:
  help [<command>...]
    Show help.

  query [<flags>] <query> [<regex>]
    Run a LogQL query.

  labels [<label>]
    Find values for a given label.

$ logcli help query
usage: logcli query [<flags>] <query> [<regex>]

Run a LogQL query.

Flags:
      --help             Show context-sensitive help (also try --help-long and --help-man).
  -q, --quiet            suppress everything but log lines
  -o, --output=default   specify output mode [default, raw, jsonl]
      --addr="https://logs-us-west1.grafana.net"  
                         Server address.
      --username=""      Username for HTTP basic auth.
      --password=""      Password for HTTP basic auth.
      --ca-cert=""       Path to the server Certificate Authority.
      --tls-skip-verify  Server certificate TLS skip verify.
      --cert=""          Path to the client certificate.
      --key=""           Path to the client certificate key.
      --limit=30         Limit on number of entries to print.
      --since=1h         Lookback window.
      --from=FROM        Start looking for logs at this absolute time (inclusive)
      --to=TO            Stop looking for logs at this absolute time (exclusive)
      --forward          Scan forwards through logs.
  -t, --tail             Tail the logs
      --delay-for=0      Delay in tailing by number of seconds to accumulate logs for re-ordering
      --no-labels        Do not print any labels
      --exclude-label=EXCLUDE-LABEL ...  
                         Exclude labels given the provided key during output.
      --include-label=INCLUDE-LABEL ...  
                         Include labels given the provided key during output.
      --labels-length=0  Set a fixed padding to labels

Args:
  <query>    eg '{foo="bar",baz="blip"}'
  [<regex>]
```
