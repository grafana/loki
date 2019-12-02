# Querying Loki with LogCLI

If you prefer a command line interface, LogCLI also allows users to run LogQL
queries against a Loki server.

## Installation

### Binary (Recommended)

Every release includes binaries for `logcli` which can be found on the
[Releases page](https://github.com/grafana/loki/releases).

### From source

Use `go get` to install `logcli` to `$GOPATH/bin`:

```
$ go get github.com/grafana/loki/cmd/logcli
```

## Usage

### Example

If you are running on Grafana Cloud, use:

```bash
$ export GRAFANA_ADDR=https://logs-us-west1.grafana.net
$ export GRAFANA_USERNAME=<username>
$ export GRAFANA_PASSWORD=<password>
```

Otherwise you can point LogCLI to a local instance directly
without needing a username and password:

```bash
$ export GRAFANA_ADDR=http://localhost:3100
```

> Note: If you are running Loki behind a proxy server and you have
> authentication configured, you will also have to pass in GRAFANA_USERNAME
> and GRAFANA_PASSWORD accordingly.

```bash
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

- Environment variables
- Command line flags

### Details

```bash
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
