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
$ export LOKI_ADDR=https://logs-us-west1.grafana.net
$ export LOKI_USERNAME=<username>
$ export LOKI_PASSWORD=<password>
```

Otherwise you can point LogCLI to a local instance directly
without needing a username and password:

```bash
$ export LOKI_ADDR=http://localhost:3100
```

> Note: If you are running Loki behind a proxy server and you have
> authentication configured, you will also have to pass in LOKI_USERNAME
> and LOKI_PASSWORD accordingly.

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
      --version          Show application version.
  -q, --quiet            suppress everything but log entries
      --stats            show query statistics
  -o, --output=default   specify output mode [default, raw, jsonl]
  -z, --timezone=Local   Specify the timezone to use when formatting output timestamps [Local, UTC]
      --addr="http://localhost:3100"
                         Server address. Can also be set using LOKI_ADDR env var.
      --username=""      Username for HTTP basic auth. Can also be set using LOKI_USERNAME env var.
      --password=""      Password for HTTP basic auth. Can also be set using LOKI_PASSWORD env var.
      --ca-cert=""       Path to the server Certificate Authority. Can also be set using LOKI_CA_CERT_PATH env var.
      --tls-skip-verify  Server certificate TLS skip verify.
      --cert=""          Path to the client certificate. Can also be set using LOKI_CLIENT_CERT_PATH env var.
      --key=""           Path to the client certificate key. Can also be set using LOKI_CLIENT_KEY_PATH env var.
      --org-id=ORG-ID    org ID header to be substituted for auth

Commands:
  help [<command>...]
    Show help.

  query [<flags>] <query>
    Run a LogQL query.

    The default output of this command are log entries (combination of
    timestamp, labels, and log line) along with metainformation about the query
    made to Loki. The metainformation can be filtered out using the --quiet
    flag. Raw log lines (i.e., no labels or timestamp) can be retrieved using
    -oraw.

    When running a metrics query, this command outputs multiple data points
    between the start and the end query time.  This produces values that are
    used to build graphs. If you just want a single data point (i.e., the
    Grafana explore "table"), then you should use instant-query instead.

  instant-query [<flags>] <query>
    Run an instant LogQL query.

    This query type can only be used for metrics queries, where the query is
    evaluated for a single point in time. This is equivalent to the Grafana
    explore "table" view; if you want data that is used to build the Grafana
    graph, you should use query instead.

  labels [<label>]
    Find values for a given label.

$ logcli help query
usage: logcli query [<flags>] <query>

Run a LogQL query.

The default output of this command are log entries (combination of timestamp,
labels, and log line) along with metainformation about the query made to Loki.
The metainformation can be filtered out using the --quiet flag. Raw log lines
(i.e., no labels or timestamp) can be retrieved using -oraw.

When running a metrics query, this command outputs multiple data points between
the start and the end query time. This produces values that are used to build
graphs. If you just want a single data point (i.e., the Grafana explore
"table"), then you should use instant-query instead.

Flags:
      --help             Show context-sensitive help (also try --help-long and --help-man).
      --version          Show application version.
  -q, --quiet            suppress everything but log entries
      --stats            show query statistics
  -o, --output=default   specify output mode [default, raw, jsonl]
  -z, --timezone=Local   Specify the timezone to use when formatting output timestamps [Local, UTC]
      --addr="http://localhost:3100"
                         Server address. Can also be set using LOKI_ADDR env var.
      --username=""      Username for HTTP basic auth. Can also be set using LOKI_USERNAME env var.
      --password=""      Password for HTTP basic auth. Can also be set using LOKI_PASSWORD env var.
      --ca-cert=""       Path to the server Certificate Authority. Can also be set using LOKI_CA_CERT_PATH env var.
      --tls-skip-verify  Server certificate TLS skip verify.
      --cert=""          Path to the client certificate. Can also be set using LOKI_CLIENT_CERT_PATH env var.
      --key=""           Path to the client certificate key. Can also be set using LOKI_CLIENT_KEY_PATH env var.
      --org-id=ORG-ID    org ID header to be substituted for auth
      --limit=30         Limit on number of entries to print.
      --since=1h         Lookback window.
      --from=FROM        Start looking for logs at this absolute time (inclusive)
      --to=TO            Stop looking for logs at this absolute time (exclusive)
      --step=STEP        Query resolution step width
      --forward          Scan forwards through logs.
      --local-config=""  Execute the current query using a configured storage from a given Loki configuration file.
      --no-labels        Do not print any labels
      --exclude-label=EXCLUDE-LABEL ...
                         Exclude labels given the provided key during output.
      --include-label=INCLUDE-LABEL ...
                         Include labels given the provided key during output.
      --labels-length=0  Set a fixed padding to labels
  -t, --tail             Tail the logs
      --delay-for=0      Delay in tailing by number of seconds to accumulate logs for re-ordering

Args:
  <query>  eg '{foo="bar",baz=~".*blip"} |~ ".*error.*"'

$ logcli help instant-query
usage: logcli instant-query [<flags>] <query>

Run an instant LogQL query.

This query type can only be used for metrics queries, where the query is evaluated for a single point in time. This is
equivalent to the Grafana explore "table" view; if you want data that is used to build the Grafana graph, you should
use query instead.

Flags:
      --help             Show context-sensitive help (also try --help-long and --help-man).
      --version          Show application version.
  -q, --quiet            suppress everything but log entries
      --stats            show query statistics
  -o, --output=default   specify output mode [default, raw, jsonl]
  -z, --timezone=Local   Specify the timezone to use when formatting output timestamps [Local, UTC]
      --addr="http://localhost:3100"
                         Server address. Can also be set using LOKI_ADDR env var.
      --username=""      Username for HTTP basic auth. Can also be set using LOKI_USERNAME env var.
      --password=""      Password for HTTP basic auth. Can also be set using LOKI_PASSWORD env var.
      --ca-cert=""       Path to the server Certificate Authority. Can also be set using LOKI_CA_CERT_PATH env var.
      --tls-skip-verify  Server certificate TLS skip verify.
      --cert=""          Path to the client certificate. Can also be set using LOKI_CLIENT_CERT_PATH env var.
      --key=""           Path to the client certificate key. Can also be set using LOKI_CLIENT_KEY_PATH env var.
      --org-id=ORG-ID    org ID header to be substituted for auth
      --limit=30         Limit on number of entries to print.
      --now=NOW          Time at which to execute the instant query.
      --forward          Scan forwards through logs.
      --no-labels        Do not print any labels
      --exclude-label=EXCLUDE-LABEL ...
                         Exclude labels given the provided key during output.
      --include-label=INCLUDE-LABEL ...
                         Include labels given the provided key during output.
      --labels-length=0  Set a fixed padding to labels

Args:
  <query>  eg '{foo="bar",baz=~".*blip"} |~ ".*error.*"'
```
