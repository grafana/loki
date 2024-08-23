---
title: LogCLI
menuTItle:
description: Describes LogCLI, the Grafana Loki command-line interface.
aliases:
- ../getting-started/logcli/
- ../tools/logcli/
weight: 700
---

# LogCLI

LogCLI is the command-line interface to Grafana Loki.
It facilitates running [LogQL]({{< relref "../query/_index.md" >}})
queries against a Loki instance.

## Installation

### Binary (Recommended)

Download the `logcli` binary from the
[Loki releases page](https://github.com/grafana/loki/releases).

### Build LogCLI from source

Clone the Loki repository and build `logcli` from source:

```bash
git clone https://github.com/grafana/loki.git
cd loki
make logcli
```

Optionally, move the binary into a directory that is part of your `$PATH`.

```bash
cp cmd/logcli/logcli /usr/local/bin/logcli
```

## Set up command completion

You can set up tab-completion for `logcli` with one of the two options, depending on your shell:

- For bash, add this to your `~/.bashrc` file:
  ```
  eval "$(logcli --completion-script-bash)"
  ```

- For zsh, add this to your `~/.zshrc` file:
  ```
  eval "$(logcli --completion-script-zsh)"
  ```

## LogCLI usage

### Grafana Cloud example

If you are running on Grafana Cloud, use:

```bash
export LOKI_ADDR=https://logs-us-west1.grafana.net
export LOKI_USERNAME=<username>
export LOKI_PASSWORD=<password>
```

Otherwise you can point LogCLI to a local instance directly
without needing a username and password:

```bash
export LOKI_ADDR=http://localhost:3100
```

{{% admonition type="note" %}}
If you are running Loki behind a proxy server and you have
authentication configured, you will also have to pass in LOKI_USERNAME
and LOKI_PASSWORD, LOKI_BEARER_TOKEN or LOKI_BEARER_TOKEN_FILE accordingly.
{{% /admonition %}}

```bash
$ logcli labels job
https://logs-dev-ops-tools1.grafana.net/api/prom/label/job/values
loki-ops/consul
loki-ops/loki-gw
...

$ logcli query '{job="loki-ops/consul"}'
https://logs-dev-ops-tools1.grafana.net/api/prom/query?query=%7Bjob%3D%22loki-ops%2Fconsul%22%7D&limit=30&start=1529928228&end=1529931828&direction=backward&regexp=
Common labels: {job="loki-ops/consul", namespace="loki-ops"}
2018-06-25T12:52:09Z {instance="consul-8576459955-pl75w"} 2018/06/25 12:52:09 [INFO] raft: Snapshot to 475409 complete
2018-06-25T12:52:09Z {instance="consul-8576459955-pl75w"} 2018/06/25 12:52:09 [INFO] raft: Compacting logs from 456973 to 465169
...

$ logcli series -q --match='{namespace="loki",container_name="loki"}'
{app="loki", container_name="loki", controller_revision_hash="loki-57c9df47f4", filename="/var/log/pods/loki_loki-0_8ed03ded-bacb-4b13-a6fe-53a445a15887/loki/0.log", instance="loki-0", job="loki/loki", name="loki", namespace="loki", release="loki", statefulset_kubernetes_io_pod_name="loki-0", stream="stderr"}
```

### Batched queries

LogCLI sends queries to Loki such that query results arrive in batches.

The `--limit` option for a `logcli query` command caps the quantity of
log lines for a single query.
When not set, `--limit` defaults to 30.
The limit protects the user from overwhelming the system
for cases in which the specified query would have returned a large quantity
of log lines.
The limit also protects the user from unexpectedly large responses.

The quantity of log line results that arrive in each batch
is set by the `--batch` option in a `logcli query` command.
When not set, `--batch` defaults to 1000.

Setting a `--limit` value larger than the `--batch` value causes the
requests from LogCLI to Loki to be batched.
Loki has a server-side limit that defaults to 5000 for the maximum quantity
of lines returned for a single query.
The batching of requests allows you to query for a results set that
is larger than the server-side limit,
as long as the `--batch` value is less than the server limit.

Query metadata is output to `stderr` for each batch.
Set the `--quiet` option on the `logcli query` command line to suppress
the output of the query metadata.

### Configuration

Configuration values are considered in the following order (lowest to highest):

- Environment variables
- Command-line options

### LogCLI command reference

The output of `logcli help`:

```nohighlight
usage: logcli [<flags>] <command> [<args> ...]

A command-line for Loki.

Flags:
      --help             Show context-sensitive help (also try --help-long and
                         --help-man).
      --version          Show application version.
  -q, --quiet            Suppress query metadata
      --stats            Show query statistics
  -o, --output=default   Specify output mode [default, raw, jsonl]. raw
                         suppresses log labels and timestamp.
  -z, --timezone=Local   Specify the timezone to use when formatting output
                         timestamps [Local, UTC]
      --cpuprofile=""    Specify the location for writing a CPU profile.
      --memprofile=""    Specify the location for writing a memory profile.
      --stdin            Take input logs from stdin
      --addr="http://localhost:3100"
                         Server address. Can also be set using LOKI_ADDR env
                         var.
      --username=""      Username for HTTP basic auth. Can also be set using
                         LOKI_USERNAME env var.
      --password=""      Password for HTTP basic auth. Can also be set using
                         LOKI_PASSWORD env var.
      --ca-cert=""       Path to the server Certificate Authority. Can also be
                         set using LOKI_CA_CERT_PATH env var.
      --tls-skip-verify  Server certificate TLS skip verify.
      --cert=""          Path to the client certificate. Can also be set using
                         LOKI_CLIENT_CERT_PATH env var.
      --key=""           Path to the client certificate key. Can also be set
                         using LOKI_CLIENT_KEY_PATH env var.
      --org-id=""        adds X-Scope-OrgID to API requests for representing
                         tenant ID. Useful for requesting tenant data when
                         bypassing an auth gateway.

Commands:
  help [<command>...]
    Show help.

  query [<flags>] <query>
    Run a LogQL query.

    The "query" command is useful for querying for logs. Logs can be returned in
    a few output modes:

      raw: log line
      default: log timestamp + log labels + log line
      jsonl: JSON response from Loki API of log line

    The output of the log can be specified with the "-o" flag, for example, "-o
    raw" for the raw output format.

    The "query" command will output extra information about the query and its
    results, such as the API URL, set of common labels, and set of excluded
    labels. This extra information can be suppressed with the --quiet flag.

    By default we look over the last hour of data; use --since to modify or
    provide specific start and end times with --from and --to respectively.

    Notice that when using --from and --to then ensure to use RFC3339Nano time
    format, but without timezone at the end. The local timezone will be added
    automatically or if using --timezone flag.

    Example:

      logcli query
         --timezone=UTC
         --from="2021-01-19T10:00:00Z"
         --to="2021-01-19T20:00:00Z"
         --output=jsonl
         'my-query'

    The output is limited to 30 entries by default; use --limit to increase.

    While "query" does support metrics queries, its output contains multiple
    data points between the start and end query time. This output is used to
    build graphs, similar to what is seen in the Grafana Explore graph view. If
    you are querying metrics and just want the most recent data point (like what
    is seen in the Grafana Explore table view), then you should use the
    "instant-query" command instead.

  instant-query [<flags>] <query>
    Run an instant LogQL query.

    The "instant-query" command is useful for evaluating a metric query for a
    single point in time. This is equivalent to the Grafana Explore table view;
    if you want a metrics query that is used to build a Grafana graph, you
    should use the "query" command instead.

    This command does not produce useful output when querying for log lines; you
    should always use the "query" command when you are running log queries.

    For more information about log queries and metric queries, refer to the
    LogQL documentation:

    https://grafana.com/docs/loki/<LOKI_VERSION>/logql/

  labels [<flags>] [<label>]
    Find values for a given label.

  series [<flags>] <matcher>
    Run series query.

    The "series" command will take the provided label matcher and return all the
    log streams found in the time window.

    It is possible to send an empty label matcher '{}' to return all streams.

    Use the --analyze-labels flag to get a summary of the labels found in all
    streams. This is helpful to find high cardinality labels.
```

### `query` command reference

The output of `logcli help query`:

```
usage: logcli query [<flags>] <query>

Run a LogQL query.

The "query" command is useful for querying for logs. Logs can be returned in a few output modes:

  raw: log line
  default: log timestamp + log labels + log line
  jsonl: JSON response from Loki API of log line

The output of the log can be specified with the "-o" flag, for example, "-o raw" for the raw output format.

The "query" command will output extra information about the query and its results, such as the API URL, set of common labels, and set of
excluded labels. This extra information can be suppressed with the --quiet flag.

By default we look over the last hour of data; use --since to modify or provide specific start and end times with --from and --to
respectively.

Notice that when using --from and --to then ensure to use RFC3339Nano time format, but without timezone at the end. The local timezone will be
added automatically or if using --timezone flag.

Example:

  logcli query
     --timezone=UTC
     --from="2021-01-19T10:00:00Z"
     --to="2021-01-19T20:00:00Z"
     --output=jsonl
     'my-query'

The output is limited to 30 entries by default; use --limit to increase.

While "query" does support metrics queries, its output contains multiple data points between the start and end query time. This output is used
to build graphs, similar to what is seen in the Grafana Explore graph view. If you are querying metrics and just want the most recent data
point (like what is seen in the Grafana Explore table view), then you should use the "instant-query" command instead.

Parallelization:

You can download an unlimited number of logs in parallel, there are a few flags which control this behaviour:

  --parallel-duration
  --parallel-max-workers
  --part-path-prefix
  --overwrite-completed-parts
  --merge-parts
  --keep-parts

Refer to the help for each flag for details about what each of them do.

Example:

  logcli query
     --timezone=UTC
     --from="2021-01-19T10:00:00Z"
     --to="2021-01-19T20:00:00Z"
     --output=jsonl
     --parallel-duration="15m"
     --parallel-max-workers="4"
     --part-path-prefix="/tmp/my_query"
     --merge-parts
     'my-query'

This example will create a queue of jobs to execute, each being 15 minutes in duration. In this case, that means, for the 10-hour total
duration, there will be forty 15-minute jobs. The --limit flag is ignored.

It will start four workers, and they will each take a job to work on from the queue until all the jobs have been completed.

Each job will save a "part" file to the location specified by the --part-path-prefix. Different prefixes can be used to run multiple queries
at the same time. The timestamp of the start and end of the part is in the file name. While the part is being downloaded, the filename will
end in ".part", when it is complete, the file will be renamed to remove this ".part" extension. By default, if a completed part file is found,
that part will not be downloaded again. This can be overridden with the `--overwrite-completed-parts` flag.

Part file example using the previous command, adding --keep-parts so they are not deleted:

Since we don't have the --forward flag, the parts will be downloaded in reverse. Two of the workers have finished their jobs (last two files),
and have picked up the next jobs in the queue. Running ls, this is what we should expect to see.

$ ls -1 /tmp/my_query* /tmp/my_query_20210119T183000_20210119T184500.part.tmp /tmp/my_query_20210119T184500_20210119T190000.part.tmp
/tmp/my_query_20210119T190000_20210119T191500.part.tmp /tmp/my_query_20210119T191500_20210119T193000.part.tmp
/tmp/my_query_20210119T193000_20210119T194500.part /tmp/my_query_20210119T194500_20210119T200000.part

If you do not specify the `--merge-parts` flag, the part files will be downloaded, and logcli will exit, and you can process the files as you
wish. With the flag specified, the part files will be read in order, and the output printed to the terminal. The lines will be printed as
soon as the next part is complete, you don't have to wait for all the parts to download before getting output. The `--merge-parts` flag will
remove the part files when it is done reading each of them. To change this, you can use the `--keep-parts` flag, and the part files will not be
removed.

Flags:
      --help                    Show context-sensitive help (also try --help-long and --help-man).
      --version                 Show application version.
  -q, --quiet                   Suppress query metadata
      --stats                   Show query statistics
  -o, --output=default          Specify output mode [default, raw, jsonl]. raw suppresses log labels and timestamp.
  -z, --timezone=Local          Specify the timezone to use when formatting output timestamps [Local, UTC]
      --cpuprofile=""           Specify the location for writing a CPU profile.
      --memprofile=""           Specify the location for writing a memory profile.
      --stdin                   Take input logs from stdin
      --addr="http://localhost:3100"
                                Server address. Can also be set using LOKI_ADDR env var.
      --username=""             Username for HTTP basic auth. Can also be set using LOKI_USERNAME env var.
      --password=""             Password for HTTP basic auth. Can also be set using LOKI_PASSWORD env var.
      --ca-cert=""              Path to the server Certificate Authority. Can also be set using LOKI_CA_CERT_PATH env var.
      --tls-skip-verify         Server certificate TLS skip verify. Can also be set using LOKI_TLS_SKIP_VERIFY env var.
      --cert=""                 Path to the client certificate. Can also be set using LOKI_CLIENT_CERT_PATH env var.
      --key=""                  Path to the client certificate key. Can also be set using LOKI_CLIENT_KEY_PATH env var.
      --org-id=""               adds X-Scope-OrgID to API requests for representing tenant ID. Useful for requesting tenant data when
                                bypassing an auth gateway. Can also be set using LOKI_ORG_ID env var.
      --query-tags=""           adds X-Query-Tags http header to API requests. This header value will be part of `metrics.go` statistics.
                                Useful for tracking the query. Can also be set using LOKI_QUERY_TAGS env var.
      --bearer-token=""         adds the Authorization header to API requests for authentication purposes. Can also be set using
                                LOKI_BEARER_TOKEN env var.
      --bearer-token-file=""    adds the Authorization header to API requests for authentication purposes. Can also be set using
                                LOKI_BEARER_TOKEN_FILE env var.
      --retries=0               How many times to retry each query when getting an error response from Loki. Can also be set using
                                LOKI_CLIENT_RETRIES env var.
      --min-backoff=0           Minimum backoff time between retries. Can also be set using LOKI_CLIENT_MIN_BACKOFF env var.
      --max-backoff=0           Maximum backoff time between retries. Can also be set using LOKI_CLIENT_MAX_BACKOFF env var.
      --auth-header="Authorization"
                                The authorization header used. Can also be set using LOKI_AUTH_HEADER env var.
      --proxy-url=""            The http or https proxy to use when making requests. Can also be set using LOKI_HTTP_PROXY_URL env var.
      --limit=30                Limit on number of entries to print. Setting it to 0 will fetch all entries.
      --since=1h                Lookback window.
      --from=FROM               Start looking for logs at this absolute time (inclusive)
      --to=TO                   Stop looking for logs at this absolute time (exclusive)
      --step=STEP               Query resolution step width, for metric queries. Evaluate the query at the specified step over the time range.
      --interval=INTERVAL       Query interval, for log queries. Return entries at the specified interval, ignoring those between. **This
                                parameter is experimental, please see Issue 1779**
      --batch=1000              Query batch size to use until 'limit' is reached
      --parallel-duration=1h    Split the range into jobs of this length to download the logs in parallel. This will result in the logs being
                                out of order. Use --part-path-prefix to create a file per job to maintain ordering.
      --parallel-max-workers=1  Max number of workers to start up for parallel jobs. A value of 1 will not create any parallel workers.
                                When using parallel workers, limit is ignored.
      --part-path-prefix=PART-PATH-PREFIX
                                When set, each server response will be saved to a file with this prefix. Creates files in the format:
                                'prefix-utc_start-utc_end.part'. Intended to be used with the parallel-* flags so that you can combine the
                                files to maintain ordering based on the filename. Default is to write to stdout.
      --overwrite-completed-parts
                                Overwrites completed part files. This will download the range again, and replace the original completed part
                                file. Default will skip a range if it's part file is already downloaded.
      --merge-parts             Reads the part files in order and writes the output to stdout. Original part files will be deleted with this
                                option.
      --keep-parts              Overrides the default behaviour of --merge-parts which will delete the part files once all the files have been
                                read. This option will keep the part files.
      --forward                 Scan forwards through logs.
      --no-labels               Do not print any labels
      --exclude-label=EXCLUDE-LABEL ...
                                Exclude labels given the provided key during output.
      --include-label=INCLUDE-LABEL ...
                                Include labels given the provided key during output.
      --labels-length=0         Set a fixed padding to labels
      --store-config=""         Execute the current query using a configured storage from a given Loki configuration file.
      --remote-schema           Execute the current query using a remote schema retrieved from the configured -schema-store.
      --schema-store=""         Store used for retrieving remote schema.
      --colored-output          Show output with colored labels
  -t, --tail                    Tail the logs
  -f, --follow                  Alias for --tail
      --delay-for=0             Delay in tailing by number of seconds to accumulate logs for re-ordering

Args:
  <query>  eg '{foo="bar",baz=~".*blip"} |~ ".*error.*"'
```

### `instant-query` command reference

The output of `logcli help instant-query`:

```
usage: logcli instant-query [<flags>] <query>

Run an instant LogQL query.

The "instant-query" command is useful for evaluating a metric query for a single point in time. This is equivalent to the Grafana Explore
table view; if you want a metrics query that is used to build a Grafana graph, you should use the "query" command instead.

This command does not produce useful output when querying for log lines; you should always use the "query" command when you are running log
queries.

For more information about log queries and metric queries, refer to the LogQL documentation:

https://grafana.com/docs/loki/latest/logql/

Flags:
      --help                  Show context-sensitive help (also try --help-long and --help-man).
      --version               Show application version.
  -q, --quiet                 Suppress query metadata
      --stats                 Show query statistics
  -o, --output=default        Specify output mode [default, raw, jsonl]. raw suppresses log labels and timestamp.
  -z, --timezone=Local        Specify the timezone to use when formatting output timestamps [Local, UTC]
      --cpuprofile=""         Specify the location for writing a CPU profile.
      --memprofile=""         Specify the location for writing a memory profile.
      --stdin                 Take input logs from stdin
      --addr="http://localhost:3100"
                              Server address. Can also be set using LOKI_ADDR env var.
      --username=""           Username for HTTP basic auth. Can also be set using LOKI_USERNAME env var.
      --password=""           Password for HTTP basic auth. Can also be set using LOKI_PASSWORD env var.
      --ca-cert=""            Path to the server Certificate Authority. Can also be set using LOKI_CA_CERT_PATH env var.
      --tls-skip-verify       Server certificate TLS skip verify. Can also be set using LOKI_TLS_SKIP_VERIFY env var.
      --cert=""               Path to the client certificate. Can also be set using LOKI_CLIENT_CERT_PATH env var.
      --key=""                Path to the client certificate key. Can also be set using LOKI_CLIENT_KEY_PATH env var.
      --org-id=""             adds X-Scope-OrgID to API requests for representing tenant ID. Useful for requesting tenant data when bypassing
                              an auth gateway. Can also be set using LOKI_ORG_ID env var.
      --query-tags=""         adds X-Query-Tags http header to API requests. This header value will be part of `metrics.go` statistics.
                              Useful for tracking the query. Can also be set using LOKI_QUERY_TAGS env var.
      --bearer-token=""       adds the Authorization header to API requests for authentication purposes. Can also be set using
                              LOKI_BEARER_TOKEN env var.
      --bearer-token-file=""  adds the Authorization header to API requests for authentication purposes. Can also be set using
                              LOKI_BEARER_TOKEN_FILE env var.
      --retries=0             How many times to retry each query when getting an error response from Loki. Can also be set using
                              LOKI_CLIENT_RETRIES env var.
      --min-backoff=0         Minimum backoff time between retries. Can also be set using LOKI_CLIENT_MIN_BACKOFF env var.
      --max-backoff=0         Maximum backoff time between retries. Can also be set using LOKI_CLIENT_MAX_BACKOFF env var.
      --auth-header="Authorization"
                              The authorization header used. Can also be set using LOKI_AUTH_HEADER env var.
      --proxy-url=""          The http or https proxy to use when making requests. Can also be set using LOKI_HTTP_PROXY_URL env var.
      --limit=30              Limit on number of entries to print. Setting it to 0 will fetch all entries.
      --now=NOW               Time at which to execute the instant query.
      --forward               Scan forwards through logs.
      --no-labels             Do not print any labels
      --exclude-label=EXCLUDE-LABEL ...
                              Exclude labels given the provided key during output.
      --include-label=INCLUDE-LABEL ...
                              Include labels given the provided key during output.
      --labels-length=0       Set a fixed padding to labels
      --store-config=""       Execute the current query using a configured storage from a given Loki configuration file.
      --remote-schema         Execute the current query using a remote schema retrieved from the configured -schema-store.
      --schema-store=""       Store used for retrieving remote schema.
      --colored-output        Show output with colored labels

Args:
  <query>  eg 'rate({foo="bar"} |~ ".*error.*" [5m])'
```

### `labels` command reference

The output of `logcli help labels`:

```
usage: logcli labels [<flags>] [<label>]

Find values for a given label.

Flags:
      --help                  Show context-sensitive help (also try --help-long and --help-man).
      --version               Show application version.
  -q, --quiet                 Suppress query metadata
      --stats                 Show query statistics
  -o, --output=default        Specify output mode [default, raw, jsonl]. raw suppresses log labels and timestamp.
  -z, --timezone=Local        Specify the timezone to use when formatting output timestamps [Local, UTC]
      --cpuprofile=""         Specify the location for writing a CPU profile.
      --memprofile=""         Specify the location for writing a memory profile.
      --stdin                 Take input logs from stdin
      --addr="http://localhost:3100"
                              Server address. Can also be set using LOKI_ADDR env var.
      --username=""           Username for HTTP basic auth. Can also be set using LOKI_USERNAME env var.
      --password=""           Password for HTTP basic auth. Can also be set using LOKI_PASSWORD env var.
      --ca-cert=""            Path to the server Certificate Authority. Can also be set using LOKI_CA_CERT_PATH env var.
      --tls-skip-verify       Server certificate TLS skip verify. Can also be set using LOKI_TLS_SKIP_VERIFY env var.
      --cert=""               Path to the client certificate. Can also be set using LOKI_CLIENT_CERT_PATH env var.
      --key=""                Path to the client certificate key. Can also be set using LOKI_CLIENT_KEY_PATH env var.
      --org-id=""             adds X-Scope-OrgID to API requests for representing tenant ID. Useful for requesting tenant data when bypassing
                              an auth gateway. Can also be set using LOKI_ORG_ID env var.
      --query-tags=""         adds X-Query-Tags http header to API requests. This header value will be part of `metrics.go` statistics.
                              Useful for tracking the query. Can also be set using LOKI_QUERY_TAGS env var.
      --bearer-token=""       adds the Authorization header to API requests for authentication purposes. Can also be set using
                              LOKI_BEARER_TOKEN env var.
      --bearer-token-file=""  adds the Authorization header to API requests for authentication purposes. Can also be set using
                              LOKI_BEARER_TOKEN_FILE env var.
      --retries=0             How many times to retry each query when getting an error response from Loki. Can also be set using
                              LOKI_CLIENT_RETRIES env var.
      --min-backoff=0         Minimum backoff time between retries. Can also be set using LOKI_CLIENT_MIN_BACKOFF env var.
      --max-backoff=0         Maximum backoff time between retries. Can also be set using LOKI_CLIENT_MAX_BACKOFF env var.
      --auth-header="Authorization"
                              The authorization header used. Can also be set using LOKI_AUTH_HEADER env var.
      --proxy-url=""          The http or https proxy to use when making requests. Can also be set using LOKI_HTTP_PROXY_URL env var.
      --since=1h              Lookback window.
      --from=FROM             Start looking for labels at this absolute time (inclusive)
      --to=TO                 Stop looking for labels at this absolute time (exclusive)

Args:
  [<label>]  The name of the label.
```

### `series` command reference

The output of `logcli help series`:

```
usage: logcli series [<flags>] <matcher>

Run series query.

The "series" command will take the provided label matcher and return all the log streams found in the time window.

It is possible to send an empty label matcher '{}' to return all streams.

Use the --analyze-labels flag to get a summary of the labels found in all streams. This is helpful to find high cardinality labels.

Flags:
      --help                  Show context-sensitive help (also try --help-long and --help-man).
      --version               Show application version.
  -q, --quiet                 Suppress query metadata
      --stats                 Show query statistics
  -o, --output=default        Specify output mode [default, raw, jsonl]. raw suppresses log labels and timestamp.
  -z, --timezone=Local        Specify the timezone to use when formatting output timestamps [Local, UTC]
      --cpuprofile=""         Specify the location for writing a CPU profile.
      --memprofile=""         Specify the location for writing a memory profile.
      --stdin                 Take input logs from stdin
      --addr="http://localhost:3100"
                              Server address. Can also be set using LOKI_ADDR env var.
      --username=""           Username for HTTP basic auth. Can also be set using LOKI_USERNAME env var.
      --password=""           Password for HTTP basic auth. Can also be set using LOKI_PASSWORD env var.
      --ca-cert=""            Path to the server Certificate Authority. Can also be set using LOKI_CA_CERT_PATH env var.
      --tls-skip-verify       Server certificate TLS skip verify. Can also be set using LOKI_TLS_SKIP_VERIFY env var.
      --cert=""               Path to the client certificate. Can also be set using LOKI_CLIENT_CERT_PATH env var.
      --key=""                Path to the client certificate key. Can also be set using LOKI_CLIENT_KEY_PATH env var.
      --org-id=""             adds X-Scope-OrgID to API requests for representing tenant ID. Useful for requesting tenant data when bypassing
                              an auth gateway. Can also be set using LOKI_ORG_ID env var.
      --query-tags=""         adds X-Query-Tags http header to API requests. This header value will be part of `metrics.go` statistics.
                              Useful for tracking the query. Can also be set using LOKI_QUERY_TAGS env var.
      --bearer-token=""       adds the Authorization header to API requests for authentication purposes. Can also be set using
                              LOKI_BEARER_TOKEN env var.
      --bearer-token-file=""  adds the Authorization header to API requests for authentication purposes. Can also be set using
                              LOKI_BEARER_TOKEN_FILE env var.
      --retries=0             How many times to retry each query when getting an error response from Loki. Can also be set using
                              LOKI_CLIENT_RETRIES env var.
      --min-backoff=0         Minimum backoff time between retries. Can also be set using LOKI_CLIENT_MIN_BACKOFF env var.
      --max-backoff=0         Maximum backoff time between retries. Can also be set using LOKI_CLIENT_MAX_BACKOFF env var.
      --auth-header="Authorization"
                              The authorization header used. Can also be set using LOKI_AUTH_HEADER env var.
      --proxy-url=""          The http or https proxy to use when making requests. Can also be set using LOKI_HTTP_PROXY_URL env var.
      --since=1h              Lookback window.
      --from=FROM             Start looking for logs at this absolute time (inclusive)
      --to=TO                 Stop looking for logs at this absolute time (exclusive)
      --analyze-labels        Printout a summary of labels including count of label value combinations, useful for debugging high cardinality
                              series

Args:
  <matcher>  eg '{foo="bar",baz=~".*blip"}'
```

### `fmt` command reference

The output of `logcli help fmt`:

```
usage: logcli fmt

Formats a LogQL query.

Flags:
      --help                  Show context-sensitive help (also try --help-long and --help-man).
      --version               Show application version.
  -q, --quiet                 Suppress query metadata
      --stats                 Show query statistics
  -o, --output=default        Specify output mode [default, raw, jsonl]. raw suppresses log labels and timestamp.
  -z, --timezone=Local        Specify the timezone to use when formatting output timestamps [Local, UTC]
      --cpuprofile=""         Specify the location for writing a CPU profile.
      --memprofile=""         Specify the location for writing a memory profile.
      --stdin                 Take input logs from stdin
      --addr="http://localhost:3100"
                              Server address. Can also be set using LOKI_ADDR env var.
      --username=""           Username for HTTP basic auth. Can also be set using LOKI_USERNAME env var.
      --password=""           Password for HTTP basic auth. Can also be set using LOKI_PASSWORD env var.
      --ca-cert=""            Path to the server Certificate Authority. Can also be set using LOKI_CA_CERT_PATH env var.
      --tls-skip-verify       Server certificate TLS skip verify. Can also be set using LOKI_TLS_SKIP_VERIFY env var.
      --cert=""               Path to the client certificate. Can also be set using LOKI_CLIENT_CERT_PATH env var.
      --key=""                Path to the client certificate key. Can also be set using LOKI_CLIENT_KEY_PATH env var.
      --org-id=""             adds X-Scope-OrgID to API requests for representing tenant ID. Useful for requesting tenant data when bypassing
                              an auth gateway. Can also be set using LOKI_ORG_ID env var.
      --query-tags=""         adds X-Query-Tags http header to API requests. This header value will be part of `metrics.go` statistics.
                              Useful for tracking the query. Can also be set using LOKI_QUERY_TAGS env var.
      --bearer-token=""       adds the Authorization header to API requests for authentication purposes. Can also be set using
                              LOKI_BEARER_TOKEN env var.
      --bearer-token-file=""  adds the Authorization header to API requests for authentication purposes. Can also be set using
                              LOKI_BEARER_TOKEN_FILE env var.
      --retries=0             How many times to retry each query when getting an error response from Loki. Can also be set using
                              LOKI_CLIENT_RETRIES env var.
      --min-backoff=0         Minimum backoff time between retries. Can also be set using LOKI_CLIENT_MIN_BACKOFF env var.
      --max-backoff=0         Maximum backoff time between retries. Can also be set using LOKI_CLIENT_MAX_BACKOFF env var.
      --auth-header="Authorization"
                              The authorization header used. Can also be set using LOKI_AUTH_HEADER env var.
      --proxy-url=""          The http or https proxy to use when making requests. Can also be set using LOKI_HTTP_PROXY_URL env var.
```

### `stats` command reference

The output of `logcli help stats`:

```
usage: logcli stats [<flags>] <query>

Run a stats query.

The "stats" command will take the provided query and return statistics from the index on how much data is contained in the matching stream(s).
This only works against Loki instances using the TSDB index format.

By default we look over the last hour of data; use --since to modify or provide specific start and end times with --from and --to
respectively.

Notice that when using --from and --to then ensure to use RFC3339Nano time format, but without timezone at the end. The local timezone will be
added automatically or if using --timezone flag.

Example:

  logcli stats
     --timezone=UTC
     --from="2021-01-19T10:00:00Z"
     --to="2021-01-19T20:00:00Z"
     'my-query'

Flags:
      --help                  Show context-sensitive help (also try --help-long and --help-man).
      --version               Show application version.
  -q, --quiet                 Suppress query metadata
      --stats                 Show query statistics
  -o, --output=default        Specify output mode [default, raw, jsonl]. raw suppresses log labels and timestamp.
  -z, --timezone=Local        Specify the timezone to use when formatting output timestamps [Local, UTC]
      --cpuprofile=""         Specify the location for writing a CPU profile.
      --memprofile=""         Specify the location for writing a memory profile.
      --stdin                 Take input logs from stdin
      --addr="http://localhost:3100"
                              Server address. Can also be set using LOKI_ADDR env var.
      --username=""           Username for HTTP basic auth. Can also be set using LOKI_USERNAME env var.
      --password=""           Password for HTTP basic auth. Can also be set using LOKI_PASSWORD env var.
      --ca-cert=""            Path to the server Certificate Authority. Can also be set using LOKI_CA_CERT_PATH env var.
      --tls-skip-verify       Server certificate TLS skip verify. Can also be set using LOKI_TLS_SKIP_VERIFY env var.
      --cert=""               Path to the client certificate. Can also be set using LOKI_CLIENT_CERT_PATH env var.
      --key=""                Path to the client certificate key. Can also be set using LOKI_CLIENT_KEY_PATH env var.
      --org-id=""             adds X-Scope-OrgID to API requests for representing tenant ID. Useful for requesting tenant data when bypassing
                              an auth gateway. Can also be set using LOKI_ORG_ID env var.
      --query-tags=""         adds X-Query-Tags http header to API requests. This header value will be part of `metrics.go` statistics.
                              Useful for tracking the query. Can also be set using LOKI_QUERY_TAGS env var.
      --bearer-token=""       adds the Authorization header to API requests for authentication purposes. Can also be set using
                              LOKI_BEARER_TOKEN env var.
      --bearer-token-file=""  adds the Authorization header to API requests for authentication purposes. Can also be set using
                              LOKI_BEARER_TOKEN_FILE env var.
      --retries=0             How many times to retry each query when getting an error response from Loki. Can also be set using
                              LOKI_CLIENT_RETRIES env var.
      --min-backoff=0         Minimum backoff time between retries. Can also be set using LOKI_CLIENT_MIN_BACKOFF env var.
      --max-backoff=0         Maximum backoff time between retries. Can also be set using LOKI_CLIENT_MAX_BACKOFF env var.
      --auth-header="Authorization"
                              The authorization header used. Can also be set using LOKI_AUTH_HEADER env var.
      --proxy-url=""          The http or https proxy to use when making requests. Can also be set using LOKI_HTTP_PROXY_URL env var.
      --since=1h              Lookback window.
      --from=FROM             Start looking for logs at this absolute time (inclusive)
      --to=TO                 Stop looking for logs at this absolute time (exclusive)

Args:
  <query>  eg '{foo="bar",baz=~".*blip"} |~ ".*error.*"'
```

### `volume` command reference

The output of `logcli help volume`:

```
usage: logcli volume [<flags>] <query>

Run a volume query.

The "volume" command will take the provided label selector(s) and return aggregate volumes for series matching those volumes. This only works
against Loki instances using the TSDB index format.

By default we look over the last hour of data; use --since to modify or provide specific start and end times with --from and --to
respectively.

Notice that when using --from and --to then ensure to use RFC3339Nano time format, but without timezone at the end. The local timezone will be
added automatically or if using --timezone flag.

Example:

  logcli volume
     --timezone=UTC
     --from="2021-01-19T10:00:00Z"
     --to="2021-01-19T20:00:00Z"
     'my-query'

Flags:
      --help                  Show context-sensitive help (also try --help-long and --help-man).
      --version               Show application version.
  -q, --quiet                 Suppress query metadata
      --stats                 Show query statistics
  -o, --output=default        Specify output mode [default, raw, jsonl]. raw suppresses log labels and timestamp.
  -z, --timezone=Local        Specify the timezone to use when formatting output timestamps [Local, UTC]
      --cpuprofile=""         Specify the location for writing a CPU profile.
      --memprofile=""         Specify the location for writing a memory profile.
      --stdin                 Take input logs from stdin
      --addr="http://localhost:3100"
                              Server address. Can also be set using LOKI_ADDR env var.
      --username=""           Username for HTTP basic auth. Can also be set using LOKI_USERNAME env var.
      --password=""           Password for HTTP basic auth. Can also be set using LOKI_PASSWORD env var.
      --ca-cert=""            Path to the server Certificate Authority. Can also be set using LOKI_CA_CERT_PATH env var.
      --tls-skip-verify       Server certificate TLS skip verify. Can also be set using LOKI_TLS_SKIP_VERIFY env var.
      --cert=""               Path to the client certificate. Can also be set using LOKI_CLIENT_CERT_PATH env var.
      --key=""                Path to the client certificate key. Can also be set using LOKI_CLIENT_KEY_PATH env var.
      --org-id=""             adds X-Scope-OrgID to API requests for representing tenant ID. Useful for requesting tenant data when bypassing
                              an auth gateway. Can also be set using LOKI_ORG_ID env var.
      --query-tags=""         adds X-Query-Tags http header to API requests. This header value will be part of `metrics.go` statistics.
                              Useful for tracking the query. Can also be set using LOKI_QUERY_TAGS env var.
      --bearer-token=""       adds the Authorization header to API requests for authentication purposes. Can also be set using
                              LOKI_BEARER_TOKEN env var.
      --bearer-token-file=""  adds the Authorization header to API requests for authentication purposes. Can also be set using
                              LOKI_BEARER_TOKEN_FILE env var.
      --retries=0             How many times to retry each query when getting an error response from Loki. Can also be set using
                              LOKI_CLIENT_RETRIES env var.
      --min-backoff=0         Minimum backoff time between retries. Can also be set using LOKI_CLIENT_MIN_BACKOFF env var.
      --max-backoff=0         Maximum backoff time between retries. Can also be set using LOKI_CLIENT_MAX_BACKOFF env var.
      --auth-header="Authorization"
                              The authorization header used. Can also be set using LOKI_AUTH_HEADER env var.
      --proxy-url=""          The http or https proxy to use when making requests. Can also be set using LOKI_HTTP_PROXY_URL env var.
      --since=1h              Lookback window.
      --from=FROM             Start looking for logs at this absolute time (inclusive)
      --to=TO                 Stop looking for logs at this absolute time (exclusive)
      --limit=30              Limit on number of series to return volumes for.
      --targetLabels=TARGETLABELS ...
                              List of labels to aggregate results into.
      --aggregateByLabels     Whether to aggregate results by label name only.

Args:
  <query>  eg '{foo="bar",baz=~".*blip"}
```

### `volume_range` command reference

The output of `logcli help volume_range`:

```
usage: logcli volume_range [<flags>] <query>

Run a volume query and return timeseries data.

The "volume_range" command will take the provided label selector(s) and return aggregate volumes for series matching those volumes, aggregated
into buckets according to the step value. This only works against Loki instances using the TSDB index format.

By default we look over the last hour of data; use --since to modify or provide specific start and end times with --from and --to
respectively.

Notice that when using --from and --to then ensure to use RFC3339Nano time format, but without timezone at the end. The local timezone will be
added automatically or if using --timezone flag.

Example:

        logcli volume_range
           --timezone=UTC
           --from="2021-01-19T10:00:00Z"
           --to="2021-01-19T20:00:00Z"
       --step=1h
           'my-query'

Flags:
      --help                  Show context-sensitive help (also try --help-long and --help-man).
      --version               Show application version.
  -q, --quiet                 Suppress query metadata
      --stats                 Show query statistics
  -o, --output=default        Specify output mode [default, raw, jsonl]. raw suppresses log labels and timestamp.
  -z, --timezone=Local        Specify the timezone to use when formatting output timestamps [Local, UTC]
      --cpuprofile=""         Specify the location for writing a CPU profile.
      --memprofile=""         Specify the location for writing a memory profile.
      --stdin                 Take input logs from stdin
      --addr="http://localhost:3100"
                              Server address. Can also be set using LOKI_ADDR env var.
      --username=""           Username for HTTP basic auth. Can also be set using LOKI_USERNAME env var.
      --password=""           Password for HTTP basic auth. Can also be set using LOKI_PASSWORD env var.
      --ca-cert=""            Path to the server Certificate Authority. Can also be set using LOKI_CA_CERT_PATH env var.
      --tls-skip-verify       Server certificate TLS skip verify. Can also be set using LOKI_TLS_SKIP_VERIFY env var.
      --cert=""               Path to the client certificate. Can also be set using LOKI_CLIENT_CERT_PATH env var.
      --key=""                Path to the client certificate key. Can also be set using LOKI_CLIENT_KEY_PATH env var.
      --org-id=""             adds X-Scope-OrgID to API requests for representing tenant ID. Useful for requesting tenant data when bypassing
                              an auth gateway. Can also be set using LOKI_ORG_ID env var.
      --query-tags=""         adds X-Query-Tags http header to API requests. This header value will be part of `metrics.go` statistics.
                              Useful for tracking the query. Can also be set using LOKI_QUERY_TAGS env var.
      --bearer-token=""       adds the Authorization header to API requests for authentication purposes. Can also be set using
                              LOKI_BEARER_TOKEN env var.
      --bearer-token-file=""  adds the Authorization header to API requests for authentication purposes. Can also be set using
                              LOKI_BEARER_TOKEN_FILE env var.
      --retries=0             How many times to retry each query when getting an error response from Loki. Can also be set using
                              LOKI_CLIENT_RETRIES env var.
      --min-backoff=0         Minimum backoff time between retries. Can also be set using LOKI_CLIENT_MIN_BACKOFF env var.
      --max-backoff=0         Maximum backoff time between retries. Can also be set using LOKI_CLIENT_MAX_BACKOFF env var.
      --auth-header="Authorization"
                              The authorization header used. Can also be set using LOKI_AUTH_HEADER env var.
      --proxy-url=""          The http or https proxy to use when making requests. Can also be set using LOKI_HTTP_PROXY_URL env var.
      --since=1h              Lookback window.
      --from=FROM             Start looking for logs at this absolute time (inclusive)
      --to=TO                 Stop looking for logs at this absolute time (exclusive)
      --limit=30              Limit on number of series to return volumes for.
      --targetLabels=TARGETLABELS ...
                              List of labels to aggregate results into.
      --aggregateByLabels     Whether to aggregate results by label name only.
      --step=1h               Query resolution step width, roll up volumes into buckets cover step time each.

Args:
  <query>  eg '{foo="bar",baz=~".*blip"}
```

### `--stdin` usage

You can consume log lines from your `stdin` instead of Loki servers.

Say you have log files in your local, and just want to do run some LogQL queries for that, `--stdin` flag can help.

{{% admonition type="note" %}}
Currently it doesn't support any type of metric queries.
{{% /admonition %}}

You may have to use `stdin` flag for several reasons
1. Quick way to check and validate a LogQL expressions.
2. Learn basics of LogQL with just Log files and `LogCLI`tool ( without needing set up Loki servers, Grafana etc.)
3. Easy discussion on public forums. Like Q&A, Share the LogQL expressions.

**NOTES on Usage**
1. `--limits` flag doesn't have any meaning when using `--stdin` (use pager like `less` for that)
1. Be aware there are no **labels** when using `--stdin`
   - So stream selector in the query is optional e.g just `|="timeout"|logfmt|level="error"` is same as `{foo="bar"}|="timeout|logfmt|level="error"`

**Examples**
1. Line filter - `cat mylog.log | logcli --stdin query '|="too many open connections"'`
2. Label matcher - `echo 'msg="timeout happened" level="warning"' | logcli --stdin query '|logfmt|level="warning"'`
3. Different parsers (logfmt, json, pattern, regexp) - `cat mylog.log | logcli --stdin query '|pattern <ip> - - <_> "<method> <uri> <_>" <status> <size> <_> "<agent>" <_>'`
4. Line formatters - `cat mylog.log | logcli --stdin query '|logfmt|line_format "{{.query}} {{.duration}}"'`
