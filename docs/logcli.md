# LogCLI

LogCLI is a handy tool to query logs from Loki without having to run a full Grafana instance.

## Installation

### Binary (Recommended)
Head over to the [Releases](https://github.com/grafana/loki/releases) and download the `logcli` binary for your OS:
```bash
# download a binary (adapt app, os and arch as needed)
# installs v0.2.0. For up to date URLs refer to the release's description
$ curl -fSL -o "/usr/local/bin/logcli.gz" "https://github.com/grafana/logcli/releases/download/v0.2.0/logcli-linux-amd64.gz"
$ gunzip "/usr/local/bin/logcli.gz"

# make sure it is executable
$ chmod a+x "/usr/local/bin/logcli"
```

### From source

```
$ go get github.com/grafana/loki/cmd/logcli
```

Now `logcli` is in your current directory.

## Usage

### Example

If you are running on Grafana cloud, use:
```
$ export GRAFANA_ADDR=https://logs-us-west1.grafana.net
$ export GRAFANA_USERNAME=<username>
$ export GRAFANA_PASSWORD=<password>
```
Otherwise, when running e.g. [locally](https://github.com/grafana/loki/tree/master/production#run-locally-using-docker), point it to your Loki instance:
```
$ export GRAFANA_ADDR=http://localhost:3100
```
> Note: If you are running loki behind a proxy server and have an authentication setup, you will have to pass URL, username and password accordingly. Please refer to [Authentication](loki/operations.md#authentication) for more info.

```bash
$ logcli labels job
https://logs-dev-ops-tools1.grafana.net/api/prom/label/job/values
cortex-ops/consul
cortex-ops/cortex-gw
...

$ logcli query '{job="cortex-ops/consul"}'
https://logs-dev-ops-tools1.grafana.net/api/v1/query_range?query=%7Bjob%3D%22cortex-ops%2Fconsul%22%7D&limit=30&start=1529928228&end=1529931828&direction=backward&regexp=
Common labels: {job="cortex-ops/consul", namespace="cortex-ops"}
2018-06-25T12:52:09Z {instance="consul-8576459955-pl75w"} 2018/06/25 12:52:09 [INFO] raft: Snapshot to 475409 complete
2018-06-25T12:52:09Z {instance="consul-8576459955-pl75w"} 2018/06/25 12:52:09 [INFO] raft: Compacting logs from 456973 to 465169
```

### Configuration

Configuration values are considered in the following order (lowest to highest):

- Environment variables
- Command line flags

The URLs of the requests are printed to help with integration work.

### Details

```bash
$ logcli help
usage: logcli [<flags>] <command> [<args> ...]

A command-line for loki.

Flags:
      --help             Show context-sensitive help (also try --help-long and --help-man).
  -q, --quiet            suppress everything but log lines
  -o, --output=default   specify output mode [default, raw, jsonl]
  -z, --timezone=Local   Specify the timezone to use when formatting output timestamps [Local, UTC]
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

  query [<flags>] <query>
    Run a LogQL query.

  instant-query [<flags>] <query>
    Run an instant LogQL query

  labels [<label>]
    Find values for a given label.

$ logcli help query
usage: logcli query [<flags>] <query>

Run a LogQL query.

Flags:
      --help             Show context-sensitive help (also try --help-long and --help-man).
  -q, --quiet            suppress everything but log lines
  -o, --output=default   specify output mode [default, raw, jsonl]
  -z, --timezone=Local   Specify the timezone to use when formatting output timestamps [Local, UTC]
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
      --no-labels        Do not print any labels
      --exclude-label=EXCLUDE-LABEL ...  
                         Exclude labels given the provided key during output.
      --include-label=INCLUDE-LABEL ...  
                         Include labels given the provided key during output.
      --labels-length=0  Set a fixed padding to labels
  -t, --tail             Tail the logs
      --delay-for=0      Delay in tailing by number of seconds to accumulate logs for re-ordering

Args:
  <query>  eg '{foo="bar",baz="blip"}'
```
