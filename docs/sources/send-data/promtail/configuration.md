---
title: Configure Promtail
menuTitle:  Configuration reference
description: Configuration parameters for the Promtail agent.
weight:  200
---

# Configure Promtail

Promtail is configured in a YAML file (usually referred to as `config.yaml`)
which contains information on the Promtail server, where positions are stored,
and how to scrape logs from files.

## Printing Promtail Config At Runtime

If you pass Promtail the flag `-print-config-stderr` or `-log-config-reverse-order`, (or `-print-config-stderr=true`)
Promtail will dump the entire config object it has created from the built in defaults combined first with
overrides from config file, and second by overrides from flags.

The result is the value for every config object in the Promtail config struct.

Some values may not be relevant to your install, this is expected as every option has a default value if it is being used or not.

This config is what Promtail will use to run, it can be invaluable for debugging issues related to configuration and
is especially useful in making sure your config files and flags are being read and loaded properly.

`-print-config-stderr` is nice when running Promtail directly e.g. `./promtail ` as you can get a quick output of the entire Promtail config.

`-log-config-reverse-order` is the flag we run Promtail with in all our environments, the config entries are reversed so
that the order of configs reads correctly top to bottom when viewed in Grafana's Explore.


## Configuration File Reference

To specify which configuration file to load, pass the `-config.file` flag at the
command line. The file is written in [YAML format](https://en.wikipedia.org/wiki/YAML),
defined by the schema below. Brackets indicate that a parameter is optional. For
non-list parameters the value is set to the specified default.

For more detailed information on configuring how to discover and scrape logs from
targets, see [Scraping]({{< relref "./scraping" >}}). For more information on transforming logs
from scraped targets, see [Pipelines]({{< relref "./pipelines" >}}).

## Reload at runtime

Promtail can reload its configuration at runtime. If the new configuration
is not well-formed, the changes will not be applied.
A configuration reload is triggered by sending a `SIGHUP` to the Promtail process or
sending a HTTP POST request to the `/reload` endpoint (when the `--server.enable-runtime-reload` flag is enabled).

### Use environment variables in the configuration

You can use environment variable references in the configuration file to set values that need to be configurable during deployment.
To do this, pass `-config.expand-env=true` and use:

```
${VAR}
```

Where VAR is the name of the environment variable.

Each variable reference is replaced at startup by the value of the environment variable.
The replacement is case-sensitive and occurs before the YAML file is parsed.
References to undefined variables are replaced by empty strings unless you specify a default value or custom error text.

To specify a default value, use:

```
${VAR:-default_value}
```

Where default_value is the value to use if the environment variable is undefined.

{{% admonition type="note" %}}
With `expand-env=true` the configuration will first run through
[envsubst](https://pkg.go.dev/github.com/drone/envsubst) which will replace double
backslashes with single backslashes. Because of this every use of a backslash `\` needs to
be replaced with a double backslash `\\`.
{{% /admonition %}}

### Generic placeholders

- `<boolean>`: a boolean that can take the values `true` or `false`
- `<int>`: any integer matching the regular expression `[1-9]+[0-9]*`
- `<duration>`: a duration matching the regular expression `[0-9]+(ms|[smhdwy])`
- `<labelname>`: a string matching the regular expression `[a-zA-Z_][a-zA-Z0-9_]*`
- `<labelvalue>`: a string of Unicode characters
- `<filename>`: a valid path relative to current working directory or an
    absolute path.
- `<host>`: a valid string consisting of a hostname or IP followed by an optional port number
- `<string>`: a string
- `<secret>`: a string that represents a secret, such as a password

### Supported contents and default values of `config.yaml`:

```yaml
# Configures global settings which impact all targets.
[global: <global_config>]

# Configures the server for Promtail.
[server: <server_config>]

# Describes how Promtail connects to multiple instances
# of Grafana Loki, sending logs to each.
# WARNING: If one of the remote Loki servers fails to respond or responds
# with any error which is retryable, this will impact sending logs to any
# other configured remote Loki servers.  Sending is done on a single thread!
# It is generally recommended to run multiple Promtail clients in parallel
# if you want to send to multiple remote Loki instances.
clients:
  - [<client_config>]

# Describes how to save read file offsets to disk
[positions: <position_config>]

scrape_configs:
  - [<scrape_config>]

# Configures global limits for this instance of Promtail
[limits_config: <limits_config>]

# Configures how tailed targets will be watched.
[target_config: <target_config>]

# Configures additional promtail configurations.
[options: <options_config>]

# Configures tracing support
[tracing: <tracing_config>]
```

## global

The `global` block configures global settings which impact all scrape targets:

```yaml
# Configure how frequently log files from disk get polled for changes.
[file_watch_config: <file_watch_config>]
```

## file_watch_config

The `file_watch_config` block configures how often to poll log files from disk
for changes:

```yaml
# Minimum frequency to poll for files. Any time file changes are detected, the
# poll frequency gets reset to this duration.
[min_poll_frequency: <duration> | default = "250ms"]

# Maximum frequency to poll for files. Any time no file changes are detected,
# the poll frequency doubles in value up to the maximum duration specified by
# this value.
#
# The default is set to the same as min_poll_frequency.
[max_poll_frequency: <duration> | default = "250ms"]
```

## server

The `server` block configures Promtail's behavior as an HTTP server:

```yaml
# Disable the HTTP and GRPC server.
[disable: <boolean> | default = false]

# Enable the /debug/fgprof and /debug/pprof endpoints for profiling.
[profiling_enabled: <boolean> | default = false]

# HTTP server listen host
[http_listen_address: <string>]

# HTTP server listen port (0 means random port)
[http_listen_port: <int> | default = 80]

# gRPC server listen host
[grpc_listen_address: <string>]

# gRPC server listen port (0 means random port)
[grpc_listen_port: <int> | default = 9095]

# Register instrumentation handlers (/metrics, etc.)
[register_instrumentation: <boolean> | default = true]

# Timeout for graceful shutdowns
[graceful_shutdown_timeout: <duration> | default = 30s]

# Read timeout for HTTP server
[http_server_read_timeout: <duration> | default = 30s]

# Write timeout for HTTP server
[http_server_write_timeout: <duration> | default = 30s]

# Idle timeout for HTTP server
[http_server_idle_timeout: <duration> | default = 120s]

# Max gRPC message size that can be received
[grpc_server_max_recv_msg_size: <int> | default = 4194304]

# Max gRPC message size that can be sent
[grpc_server_max_send_msg_size: <int> | default = 4194304]

# Limit on the number of concurrent streams for gRPC calls (0 = unlimited)
[grpc_server_max_concurrent_streams: <int> | default = 100]

# Log only messages with the given severity or above. Supported values [debug,
# info, warn, error]
[log_level: <string> | default = "info"]

# Base path to server all API routes from (e.g., /v1/).
[http_path_prefix: <string>]

# Target managers check flag for Promtail readiness, if set to false the check is ignored
[health_check_target: <bool> | default = true]

# Enable reload via HTTP request.
[enable_runtime_reload: <bool> | default = false]
```

## clients

The `clients` block configures how Promtail connects to instances of
Loki:

```yaml
# The URL where Loki is listening, denoted in Loki as http_listen_address and
# http_listen_port. If Loki is running in microservices mode, this is the HTTP
# URL for the Distributor. Path to the push API needs to be included.
# Example: http://example.com:3100/loki/api/v1/push
url: <string>

# Custom HTTP headers to be sent along with each push request.
# Be aware that headers that are set by Promtail itself (e.g. X-Scope-OrgID) can't be overwritten.
headers:
  # Example: CF-Access-Client-Id: xxx
  [ <labelname>: <labelvalue> ... ]

# The tenant ID used by default to push logs to Loki. If omitted or empty
# it assumes Loki is running in single-tenant mode and no X-Scope-OrgID header
# is sent.
[tenant_id: <string>]

# Maximum amount of time to wait before sending a batch, even if that
# batch isn't full.
[batchwait: <duration> | default = 1s]

# Maximum batch size (in bytes) of logs to accumulate before sending
# the batch to Loki.
[batchsize: <int> | default = 1048576]

# If using basic auth, configures the username and password
# sent.
basic_auth:
  # The username to use for basic auth
  [username: <string>]

  # The password to use for basic auth
  [password: <string>]

  # The file containing the password for basic auth
  [password_file: <filename>]

# Optional OAuth 2.0 configuration
# Cannot be used at the same time as basic_auth or authorization
oauth2:
  # Client id and secret for oauth2
  [client_id: <string>]
  [client_secret: <secret>]

  # Read the client secret from a file
  # It is mutually exclusive with `client_secret`
  [client_secret_file: <filename>]

  # Optional scopes for the token request
  scopes:
    [ - <string> ... ]

  # The URL to fetch the token from
  token_url: <string>

  # Optional parameters to append to the token URL
  endpoint_params:
    [ <string>: <string> ... ]

# Bearer token to send to the server.
[bearer_token: <secret>]

# File containing bearer token to send to the server.
[bearer_token_file: <filename>]

# HTTP proxy server to use to connect to the server.
[proxy_url: <string>]

# If connecting to a TLS server, configures how the TLS
# authentication handshake will operate.
tls_config:
  # The CA file to use to verify the server
  [ca_file: <string>]

  # The cert file to send to the server for client auth
  [cert_file: <filename>]

  # The key file to send to the server for client auth
  [key_file: <filename>]

  # Validates that the server name in the server's certificate
  # is this value.
  [server_name: <string>]

  # If true, ignores the server certificate being signed by an
  # unknown CA.
  [insecure_skip_verify: <boolean> | default = false]

# Configures how to retry requests to Loki when a request
# fails.
# Default backoff schedule:
# 0.5s, 1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(4.267m)
# For a total time of 511.5s(8.5m) before logs are lost
backoff_config:
  # Initial backoff time between retries
  [min_period: <duration> | default = 500ms]

  # Maximum backoff time between retries
  [max_period: <duration> | default = 5m]

  # Maximum number of retries to do
  [max_retries: <int> | default = 10]

# Disable retries of batches that Loki responds to with a 429 status code (TooManyRequests). This reduces
# impacts on batches from other tenants, which could end up being delayed or dropped due to exponential backoff.
[drop_rate_limited_batches: <boolean> | default = false]

# Static labels to add to all logs being sent to Loki.
# Use map like {"foo": "bar"} to add a label foo with
# value bar.
# These can also be specified from command line:
# -client.external-labels=k1=v1,k2=v2
# (or --client.external-labels depending on your OS)
# labels supplied by the command line are applied
# to all clients configured in the `clients` section.
# NOTE: values defined in the config file will replace values
# defined on the command line for a given client if the
# label keys are the same.
external_labels:
  [ <labelname>: <labelvalue> ... ]

# Maximum time to wait for a server to respond to a request
[timeout: <duration> | default = 10s]
```

## positions

The `positions` block configures where Promtail will save a file
indicating how far it has read into a file. It is needed for when Promtail
is restarted to allow it to continue from where it left off.

```yaml
# Location of positions file
[filename: <string> | default = "/var/log/positions.yaml"]

# How often to update the positions file
[sync_period: <duration> | default = 10s]

# Whether to ignore & later overwrite positions files that are corrupted
[ignore_invalid_yaml: <boolean> | default = false]
```

## scrape_configs

The `scrape_configs` block configures how Promtail can scrape logs from a series
of targets using a specified discovery method. Promtail uses the same [Prometheus scrape_configs](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#scrape_config). This means if you already own a Prometheus instance, the config will be very similar:

```yaml
# Name to identify this scrape config in the Promtail UI.
job_name: <string>

# Describes how to transform logs from targets.
[pipeline_stages: <pipeline_stages>]

# Defines decompression behavior for the given scrape target.
decompression:
  # Whether decompression should be tried or not.
  [enabled: <boolean> | default = false]

  # Initial delay to wait before starting the decompression.
  # Especially useful in scenarios where compressed files are found before the compression is finished.
  [initial_delay: <duration> | default = 0s]

  # Compression format. Supported formats are: 'gz', 'bz2' and 'z.
  [format: <string> | default = ""]

# Describes how to scrape logs from the journal.
[journal: <journal_config>]

# Describes from which encoding a scraped file should be converted.
[encoding: <iana_encoding_name>]

# Describes how to receive logs from syslog.
[syslog: <syslog_config>]

# Describes how to receive logs via the Loki push API, (e.g. from other Promtails or the Docker Logging Driver)
[loki_push_api: <loki_push_api_config>]

# Describes how to scrape logs from the Windows event logs.
[windows_events: <windows_events_config>]

# Configuration describing how to pull/receive Google Cloud Platform (GCP) logs.
[gcplog: <gcplog_config>]

# Configuration describing how to get Azure Event Hubs messages.
[azure_event_hub: <azure_event_hub_config>]

# Describes how to fetch logs from Kafka via a Consumer group.
[kafka: <kafka_config>]

# Describes how to receive logs from gelf client.
[gelf: <gelf_config>]

# Configuration describing how to pull logs from Cloudflare.
[cloudflare: <cloudflare>]

# Configuration describing how to pull logs from a Heroku LogPlex drain.
[heroku_drain: <heroku_drain>]

# Describes how to relabel targets to determine if they should
# be processed.
relabel_configs:
  - [<relabel_config>]

# Static targets to scrape.
static_configs:
  - [<static_config>]

# Files containing targets to scrape.
file_sd_configs:
  - [<file_sd_configs>]

# Describes how to discover Kubernetes services running on the
# same host.
kubernetes_sd_configs:
  - [<kubernetes_sd_config>]

# Describes how to use the Consul Catalog API to discover services registered with the
# consul cluster.
consul_sd_configs:
  [ - <consul_sd_config> ... ]

# Describes how to use the Consul Agent API to discover services registered with the consul agent
# running on the same host as Promtail.
consulagent_sd_configs:
  [ - <consulagent_sd_config> ... ]

# Describes how to use the Docker daemon API to discover containers running on
# the same host as Promtail.
docker_sd_configs:
  [ - <docker_sd_config> ... ]
```

### pipeline_stages

[Pipeline]({{< relref "./pipelines" >}}) stages are used to transform log entries and their labels. The pipeline is executed after the discovery process finishes. The `pipeline_stages` object consists of a list of stages which correspond to the items listed below.

In most cases, you extract data from logs with `regex` or `json` stages. The extracted data is transformed into a temporary map object. The data can then be used by Promtail e.g. as values for `labels` or as an `output`. Additionally any other stage aside from `docker` and `cri` can access the extracted data.

```yaml
- [
    <docker> |
    <cri> |
    <regex> |
    <json> |
    <template> |
    <match> |
    <timestamp> |
    <output> |
    <labels> |
    <metrics> |
    <tenant> |
    <replace>
  ]
```

#### docker

The Docker stage parses the contents of logs from Docker containers, and is defined by name with an empty object:

```yaml
docker: {}
```

The docker stage will match and parse log lines of this format:

```nohighlight
`{"log":"level=info ts=2019-04-30T02:12:41.844179Z caller=filetargetmanager.go:180 msg=\"Adding target\"\n","stream":"stderr","time":"2019-04-30T02:12:41.8443515Z"}`
```

Automatically extracting the `time` into the logs timestamp, `stream` into a label, and `log` field into the output, this can be very helpful as docker is wrapping your application log in this way and this will unwrap it for further pipeline processing of just the log content.

The Docker stage is just a convenience wrapper for this definition:

```yaml
- json:
    expressions:
      output: log
      stream: stream
      timestamp: time
- labels:
    stream:
- timestamp:
    source: timestamp
    format: RFC3339Nano
- output:
    source: output
```



#### cri

The CRI stage parses the contents of logs from CRI containers, and is defined by name with an empty object:

```yaml
cri: {}
```

The CRI  stage will match and parse log lines of this format:

```nohighlight
2019-01-01T01:00:00.000000001Z stderr P some log message
```
Automatically extracting the `time` into the logs timestamp, `stream` into a label, and the remaining message into the output, this can be very helpful as CRI is wrapping your application log in this way and this will unwrap it for further pipeline processing of just the log content.

The CRI stage is just a convenience wrapper for this definition:

```yaml
- regex:
    expression: "^(?s)(?P<time>\\S+?) (?P<stream>stdout|stderr) (?P<flags>\\S+?) (?P<content>.*)$"
- labels:
    stream:
- timestamp:
    source: time
    format: RFC3339Nano
- output:
    source: content
```

#### regex

The Regex stage takes a regular expression and extracts captured named groups to
be used in further stages.

```yaml
regex:
  # The RE2 regular expression. Each capture group must be named.
  expression: <string>

  # Name from extracted data to parse. If empty, uses the log message.
  [source: <string>]
```

#### json

The JSON stage parses a log line as JSON and takes
[JMESPath](http://jmespath.org/) expressions to extract data from the JSON to be
used in further stages.

```yaml
json:
  # Set of key/value pairs of JMESPath expressions. The key will be
  # the key in the extracted data while the expression will be the value,
  # evaluated as a JMESPath from the source data.
  expressions:
    [ <string>: <string> ... ]

  # Name from extracted data to parse. If empty, uses the log message.
  [source: <string>]
```

#### template

The template stage uses Go's
[`text/template`](https://golang.org/pkg/text/template) language to manipulate
values.

```yaml
template:
  # Name from extracted data to parse. If key in extract data doesn't exist, an
  # entry for it will be created.
  source: <string>

  # Go template string to use. In additional to normal template
  # functions, ToLower, ToUpper, Replace, Trim, TrimLeft, TrimRight,
  # TrimPrefix, TrimSuffix, and TrimSpace are available as functions.
  template: <string>
```

Example:

```yaml
template:
  source: level
  template: '{{ if eq .Value "WARN" }}{{ Replace .Value "WARN" "OK" -1 }}{{ else }}{{ .Value }}{{ end }}'
```

#### match

The match stage conditionally executes a set of stages when a log entry matches
a configurable [LogQL]({{< relref "../../query" >}}) stream selector.

```yaml
match:
  # LogQL stream selector.
  selector: <string>

  # Names the pipeline. When defined, creates an additional label in
  # the pipeline_duration_seconds histogram, where the value is
  # concatenated with job_name using an underscore.
  [pipeline_name: <string>]

  # Nested set of pipeline stages only if the selector
  # matches the labels of the log entries:
  stages:
    - [
        <docker> |
        <cri> |
        <regex>
        <json> |
        <template> |
        <match> |
        <timestamp> |
        <output> |
        <labels> |
        <metrics>
      ]
```

#### timestamp

The timestamp stage parses data from the extracted map and overrides the final
time value of the log that is stored by Loki. If this stage isn't present,
Promtail will associate the timestamp of the log entry with the time that
log entry was read.

```yaml
timestamp:
  # Name from extracted data to use for the timestamp.
  source: <string>

  # Determines how to parse the time string. Can use
  # pre-defined formats by name: [ANSIC UnixDate RubyDate RFC822
  # RFC822Z RFC850 RFC1123 RFC1123Z RFC3339 RFC3339Nano Unix
  # UnixMs UnixUs UnixNs].
  format: <string>

  # IANA Timezone Database string.
  [location: <string>]
```

#### output

The output stage takes data from the extracted map and sets the contents of the
log entry that will be stored by Loki.

```yaml
output:
  # Name from extracted data to use for the log entry.
  source: <string>
```

#### labels

The labels stage takes data from the extracted map and sets additional labels
on the log entry that will be sent to Loki.

```yaml
labels:
  # Key is REQUIRED and the name for the label that will be created.
  # Value is optional and will be the name from extracted data whose value
  # will be used for the value of the label. If empty, the value will be
  # inferred to be the same as the key.
  [ <string>: [<string>] ... ]
```

#### metrics

The metrics stage allows for defining metrics from the extracted data.

Created metrics are not pushed to Loki and are instead exposed via Promtail's
`/metrics` endpoint. Prometheus should be configured to scrape Promtail to be
able to retrieve the metrics configured by this stage. 
If Promtail's configuration is reloaded, all metrics will be reset.


```yaml
# A map where the key is the name of the metric and the value is a specific
# metric type.
metrics:
  [<string>: [ <counter> | <gauge> | <histogram> ] ...]
```

##### counter

Defines a counter metric whose value only goes up.

```yaml
# The metric type. Must be Counter.
type: Counter

# Describes the metric.
[description: <string>]

# Key from the extracted data map to use for the metric,
# defaulting to the metric's name if not present.
[source: <string>]

config:
  # Filters down source data and only changes the metric
  # if the targeted value exactly matches the provided string.
  # If not present, all data will match.
  [value: <string>]

  # Must be either "inc" or "add" (case insensitive). If
  # inc is chosen, the metric value will increase by 1 for each
  # log line received that passed the filter. If add is chosen,
  # the extracted value must be convertible to a positive float
  # and its value will be added to the metric.
  action: <string>
```

##### gauge

Defines a gauge metric whose value can go up or down.

```yaml
# The metric type. Must be Gauge.
type: Gauge

# Describes the metric.
[description: <string>]

# Key from the extracted data map to use for the metric,
# defaulting to the metric's name if not present.
[source: <string>]

config:
  # Filters down source data and only changes the metric
  # if the targeted value exactly matches the provided string.
  # If not present, all data will match.
  [value: <string>]

  # Must be either "set", "inc", "dec"," add", or "sub". If
  # add, set, or sub is chosen, the extracted value must be
  # convertible to a positive float. inc and dec will increment
  # or decrement the metric's value by 1 respectively.
  action: <string>
```

##### histogram

Defines a histogram metric whose values are bucketed.

```yaml
# The metric type. Must be Histogram.
type: Histogram

# Describes the metric.
[description: <string>]

# Key from the extracted data map to use for the metric,
# defaulting to the metric's name if not present.
[source: <string>]

config:
  # Filters down source data and only changes the metric
  # if the targeted value exactly matches the provided string.
  # If not present, all data will match.
  [value: <string>]

  # Must be either "inc" or "add" (case insensitive). If
  # inc is chosen, the metric value will increase by 1 for each
  # log line received that passed the filter. If add is chosen,
  # the extracted value must be convertible to a positive float
  # and its value will be added to the metric.
  action: <string>

  # Holds all the numbers in which to bucket the metric.
  buckets:
    - <int>
```

#### tenant

The tenant stage is an action stage that sets the tenant ID for the log entry
picking it from a field in the extracted data map.

```yaml
tenant:
  # Either label, source or value config option is required, but not all (they
  # are mutually exclusive).

  # Name from labels to whose value should be set as tenant ID.
  [ label: <string> ]

  # Name from extracted data to whose value should be set as tenant ID.
  [ source: <string> ]

  # Value to use to set the tenant ID when this stage is executed. Useful
  # when this stage is included within a conditional pipeline with "match".
  [ value: <string> ]
```

#### replace

The replace stage is a parsing stage that parses a log line using
a regular expression and replaces the log line.

```yaml
replace:
  # The RE2 regular expression. Each named capture group will be added to extracted.
  # Each capture group and named capture group will be replaced with the value given in
  # `replace`
  expression: <string>

  # Name from extracted data to parse. If empty, uses the log message.
  # The replaced value will be assigned back to soure key
  [source: <string>]

  # Value to which the captured group will be replaced. The captured group or the named
  # captured group will be replaced with this value and the log line will be replaced with
  # new replaced values. An empty value will remove the captured group from the log line.
  [replace: <string>]
```

### journal

The `journal` block configures reading from the systemd journal from
Promtail. Requires a build of Promtail that has journal support _enabled_. If
using the AMD64 Docker image, this is enabled by default.

```yaml
# When true, log messages from the journal are passed through the
# pipeline as a JSON message with all of the journal entries' original
# fields. When false, the log message is the text content of the MESSAGE
# field from the journal entry.
[json: <boolean> | default = false]

# The oldest relative time from process start that will be read
# and sent to Loki.
[max_age: <duration> | default = 7h]

# Label map to add to every log coming out of the journal
labels:
  [ <labelname>: <labelvalue> ... ]

# Get labels from journal, when it is not empty
relabel_configs:
- source_labels: ['__journal__hostname']
  target_label: host
- source_labels: ['__journal__systemd_unit']
  target_label: systemd_unit
  regex: '(.+)'
- source_labels: ['__journal__systemd_user_unit']
  target_label: systemd_user_unit
  regex: '(.+)'
- source_labels: ['__journal__transport']
  target_label: transport
  regex: '(.+)'
- source_labels: ['__journal_priority_keyword']
  target_label: severity
  regex: '(.+)'

# Path to a directory to read entries from. Defaults to system
# paths (/var/log/journal and /run/log/journal) when empty.
[path: <string>]
```
#### Available Labels

Labels are imported from systemd-journal fields. The label name is the field name set to lower case with **__journal_** prefix. See the man page [systemd.journal-fields](https://www.freedesktop.org/software/systemd/man/latest/systemd.journal-fields.html) for more information.

For example:

| journal field      | label                        |
|--------------------|------------------------------|
| _HOSTNAME          | __journal__hostname          |
| _SYSTEMD_UNIT      | __journal__systemd_unit      |
| _SYSTEMD_USER_UNIT | __journal__systemd_user_unit |
| ERRNO              | __journal_errno              |

In addition to `__journal_priority` (imported from `PRIORITY` journal field, where the value is an integer from 0 to 7), promtail adds `__journal_priority_keyword` label where the value is generated using the `makeJournalPriority` mapping function.

| journal priority | keyword |
|------------------|---------|
| 0                | emerg   |
| 1                | alert   |
| 2                | crit    |
| 3                | error   |
| 4                | warning |
| 5                | notice  |
| 6                | info    |
| 7                | debug   |

### syslog

The `syslog` block configures a syslog listener allowing users to push
logs to Promtail with the syslog protocol. Currently supported both
[BSD syslog Protocol](https://datatracker.ietf.org/doc/html/rfc3164) and
[IETF Syslog (RFC5424)](https://tools.ietf.org/html/rfc5424) with and
without octet counting.

The recommended deployment is to have a dedicated syslog forwarder like **syslog-ng** or **rsyslog**
in front of Promtail. The forwarder can take care of the various specifications
and transports that exist (UDP, BSD syslog, ...).

[Octet counting](https://tools.ietf.org/html/rfc6587#section-3.4.1) is recommended as the
message framing method. In a stream with [non-transparent framing](https://tools.ietf.org/html/rfc6587#section-3.4.2),
Promtail needs to wait for the next message to catch multi-line messages,
therefore delays between messages can occur.

See recommended output configurations for
[syslog-ng]({{< relref "./scraping#syslog-ng-output-configuration" >}}) and
[rsyslog]({{< relref "./scraping#rsyslog-output-configuration" >}}). Both configurations enable
IETF Syslog with octet-counting.

You may need to increase the open files limit for the Promtail process
if many clients are connected. (`ulimit -Sn`)

```yaml
# TCP address to listen on. Has the format of "host:port".
listen_address: <string>

# Configure the receiver to use TLS.
tls_config:
  # Certificate and key files sent by the server (required)
  cert_file: <string>
  key_file: <string>

  # CA certificate used to validate client certificate. Enables client certificate verification when specified.
  [ ca_file: <string> ]

# The idle timeout for tcp syslog connections, default is 120 seconds.
idle_timeout: <duration>

# Whether to convert syslog structured data to labels.
# A structured data entry of [example@99999 test="yes"] would become
# the label "__syslog_message_sd_example_99999_test" with the value "yes".
label_structured_data: <bool>

# Label map to add to every log message.
labels:
  [ <labelname>: <labelvalue> ... ]

# Whether Promtail should pass on the timestamp from the incoming syslog message.
# When false, or if no timestamp is present on the syslog message, Promtail will assign the current timestamp to the log when it was processed.
# Default is false
use_incoming_timestamp: <bool>

# Sets the maximum limit to the length of syslog messages
max_message_length: <int>

# Defines used Sylog format at the target. 
syslog_format:
 [type: <string> | default = "rfc5424"]

# Defines whether the full RFC5424 formatted syslog message should be pushed to Loki
use_rfc5424_message: <bool>
```

#### Available Labels

- `__syslog_connection_ip_address`: The remote IP address.
- `__syslog_connection_hostname`: The remote hostname.
- `__syslog_message_severity`: The [syslog severity](https://tools.ietf.org/html/rfc5424#section-6.2.1) parsed from the message. Symbolic name as per [syslog_message.go](https://github.com/influxdata/go-syslog/blob/v2.0.1/rfc5424/syslog_message.go#L184).
- `__syslog_message_facility`: The [syslog facility](https://tools.ietf.org/html/rfc5424#section-6.2.1) parsed from the message. Symbolic name as per [syslog_message.go](https://github.com/influxdata/go-syslog/blob/v2.0.1/rfc5424/syslog_message.go#L235) and `syslog(3)`.
- `__syslog_message_hostname`: The [hostname](https://tools.ietf.org/html/rfc5424#section-6.2.4) parsed from the message.
- `__syslog_message_app_name`: The [app-name field](https://tools.ietf.org/html/rfc5424#section-6.2.5) parsed from the message.
- `__syslog_message_proc_id`: The [procid field](https://tools.ietf.org/html/rfc5424#section-6.2.6) parsed from the message.
- `__syslog_message_msg_id`: The [msgid field](https://tools.ietf.org/html/rfc5424#section-6.2.7) parsed from the message.
- `__syslog_message_sd_<sd_id>[_<iana_enterprise_id>]_<sd_name>`: The [structured-data field](https://tools.ietf.org/html/rfc5424#section-6.3) parsed from the message. The data field `[custom@99770 example="1"]` becomes `__syslog_message_sd_custom_99770_example`.

### loki_push_api

The `loki_push_api` block configures Promtail to expose a [Loki push API](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-http-api#ingest-logs) server.

Each job configured with a `loki_push_api` will expose this API and will require a separate port.

Note the `server` configuration is the same as [server](#server).

Promtail also exposes a second endpoint on `/promtail/api/v1/raw` which expects newline-delimited log lines.
This can be used to send NDJSON or plaintext logs.

The readiness of the loki_push_api server can be checked using the endpoint `/ready`.

```yaml
# The push server configuration options
[server: <server_config>]

# Label map to add to every log line sent to the push API
labels:
  [ <labelname>: <labelvalue> ... ]

# If Promtail should pass on the timestamp from the incoming log or not.
# When false Promtail will assign the current timestamp to the log when it was processed.
# Does not apply to the plaintext endpoint on `/promtail/api/v1/raw`.
[use_incoming_timestamp: <bool> | default = false]
```

See [Example Push Config](#example-push-config)


### windows_events

The `windows_events` block configures Promtail to scrape windows event logs and send them to Loki.

To subscribe to a specific events stream you need to provide either an `eventlog_name` or an `xpath_query`.

Events are scraped periodically every 3 seconds by default but can be changed using `poll_interval`.

A bookmark path `bookmark_path` is mandatory and will be used as a position file where Promtail will
keep record of the last event processed. This file persists across Promtail restarts.

You can set `use_incoming_timestamp` if you want to keep incoming event timestamps. By default Promtail will use the timestamp when
the event was read from the event log.

Promtail will serialize JSON windows events, adding `channel` and `computer` labels from the event received.
You can add additional labels with the `labels` property.


```yaml
# LCID (Locale ID) for event rendering
# - 1033 to force English language
# -  0 to use default Windows locale
[locale: <int> | default = 0]

# Name of eventlog, used only if xpath_query is empty
# Example: "Application"
[eventlog_name: <string> | default = ""]

# xpath_query can be in defined short form like "Event/System[EventID=999]"
# or you can form a XML Query. Refer to the Consuming Events article:
# https://docs.microsoft.com/en-us/windows/win32/wes/consuming-events
# XML query is the recommended form, because it is most flexible
# You can create or debug XML Query by creating Custom View in Windows Event Viewer
# and then copying resulting XML here
[xpath_query: <string> | default = "*"]

# Sets the bookmark location on the filesystem.
# The bookmark contains the current position of the target in XML.
# When restarting or rolling out Promtail, the target will continue to scrape events where it left off based on the bookmark position.
# The position is updated after each entry processed.
[bookmark_path: <string> | default = ""]

# PollInterval is the interval at which we're looking if new events are available. By default the target will check every 3seconds.
[poll_interval: <duration> | default = 3s]

# Allows to exclude the xml event data.
[exclude_event_data: <bool> | default = false]

# Allows to exclude the human-friendly event message.
[exclude_event_message: <bool> | default = false]

# Allows to exclude the user data of each windows event.
[exclude_user_data: <bool> | default = false]

# Label map to add to every log line read from the windows event log
labels:
  [ <labelname>: <labelvalue> ... ]

# If Promtail should pass on the timestamp from the incoming log or not.
# When false Promtail will assign the current timestamp to the log when it was processed
[use_incoming_timestamp: <bool> | default = false]
```

### GCP Log

The `gcplog` block configures how Promtail receives GCP logs. There are two strategies, based on the configuration of `subscription_type`:
- **Pull**: Using GCP Pub/Sub [pull subscriptions](https://cloud.google.com/pubsub/docs/pull). Promtail will consume log messages directly from the configured GCP Pub/Sub topic.
- **Push**: Using GCP Pub/Sub [push subscriptions](https://cloud.google.com/pubsub/docs/push). Promtail will expose an HTTP server, and GCP will deliver logs to that server.

When using the `push` subscription type, keep in mind:
- The `server` configuration is the same as [server](#server), since Promtail exposes an HTTP server for target that requires so.
- An endpoint at `POST /gcp/api/v1/push`, which expects requests from GCP PubSub message delivery system.

```yaml
# Type of subscription used to fetch logs from GCP. Can be either `pull` (default) or `push`.
[subscription_type: <string> | default = "pull"]

# If the subscription_type is pull,  the GCP project ID
[project_id: <string>]

# If the subscription_type is pull, GCP PubSub subscription from where Promtail will pull logs from
[subscription: <string>]

# If the subscription_type is push, the server configuration options
[server: <server_config>]

# Whether Promtail should pass on the timestamp from the incoming GCP Log message.
# When false, or if no timestamp is present in the GCP Log message, Promtail will assign the current
# timestamp to the log when it was processed.
[use_incoming_timestamp: <boolean> | default = false]

# use_full_line to force Promtail to send the full line from Cloud Logging even if `textPayload` is available.
# By default, if `textPayload` is present in the line, then it's used as log line.
[use_full_line: <boolean> | default = false]

# If the subscription_type is push, configures an HTTP handler timeout. If processing the incoming GCP Logs request takes longer
# than the configured duration, that is processing and then sending the entry down the processing pipeline, the server will abort
# and respond with a 503 HTTP status code.
[push_timeout: <duration>|  default = 0 (no timeout)]

# Label map to add to every log message.
labels:
  [ <labelname>: <labelvalue> ... ]
```

#### Available Labels

When Promtail receives GCP logs, various internal labels are made available for [relabeling](#relabel_configs). This depends on the subscription type chosen.

**Internal labels available for pull**

- `__gcp_logname`
- `__gcp_severity`
- `__gcp_resource_type`
- `__gcp_resource_labels_<NAME>`
- `__gcp_labels_<NAME>`

**Internal labels available for push**

- `__gcp_message_id`
- `__gcp_subscription_name`
- `__gcp_attributes_<NAME>`: All attributes read from `.message.attributes` in the incoming push message. Each attribute key is conveniently renamed, since it might contain unsupported characters. For example, `logging.googleapis.com/timestamp` is converted to `__gcp_attributes_logging_googleapis_com_timestamp`.
- `__gcp_logname`
- `__gcp_severity`
- `__gcp_resource_type`
- `__gcp_resource_labels_<NAME>`
- `__gcp_labels_<NAME>`

### Azure Event Hubs

The `azure_event_hubs` block configures how Promtail receives Azure Event Hubs messages. Promtail uses an Apache Kafka endpoint on Event Hubs to receive messages. For more information, see the [Azure Event Hubs documentation](https://learn.microsoft.com/en-us/azure/event-hubs/azure-event-hubs-kafka-overview).

To learn more about streaming Azure logs to an Azure Event Hubs, you can see this [tutorial](https://learn.microsoft.com/en-us/azure/active-directory/reports-monitoring/tutorial-azure-monitor-stream-logs-to-event-hub).

Note that an Apache Kafka endpoint is not available within the `Basic` pricing plan. For more information, see the [Event Hubs pricing page](https://azure.microsoft.com/en-us/pricing/details/event-hubs/).

```yaml
# Event Hubs namespace host names (Required). Typically, it looks like <your-namespace>.servicebus.windows.net:9093.
fully_qualified_namespace: <string> | default = ""

# Event Hubs to consume (Required).
event_hubs:
    [ - <string> ... ]

# Event Hubs ConnectionString for authentication on Azure Cloud (Required).
connection_string: <string> | default = "range"

# The consumer group id.
[group_id: <string> | default = "promtail"]

# If Promtail should pass on the timestamp from the incoming message or not.
# When false Promtail will assign the current timestamp to the log when it was processed.
[use_incoming_timestamp: <bool> | default = false]

# If Promtail should ignore messages that don't match the schema for Azure resource logs.
# Schema is described here https://learn.microsoft.com/en-us/azure/azure-monitor/essentials/resource-logs-schema.
[disallow_custom_messages: <bool> | default = false]

# Labels optionally hold labels to associate with each log line.
[labels]:
  [ <labelname>: <labelvalue> ... ]
```

#### Available Labels

When Promtail receives Azure Event Hubs messages, various internal labels are made available for [relabeling](#relabel_configs).

- `__azure_event_hubs_category`: The log category of the message when a message is an application log.

The following list of labels is discovered using the Kafka endpoint in Event Hubs.

- `__meta_kafka_topic`: The current topic for where the message has been read.
- `__meta_kafka_partition`: The partition id where the message has been read.
- `__meta_kafka_member_id`: The consumer group member id.
- `__meta_kafka_group_id`: The consumer group id.
- `__meta_kafka_message_key`: The message key. If it is empty, this value will be 'none'.

### kafka

The `kafka` block configures Promtail to scrape logs from [Kafka](https://kafka.apache.org/) using a group consumer.

The `brokers` should list available brokers to communicate with the Kafka cluster. Use multiple brokers when you want to increase availability.

The `topics` is the list of topics Promtail will subscribe to. If a topic starts with `^` then a regular expression ([RE2](https://github.com/google/re2/wiki/Syntax)) is used to match topics.
For instance `^promtail-.*` will match the topic `promtail-dev` and `promtail-prod`. Topics are refreshed every 30 seconds, so if a new topic matches, it will be automatically added without requiring a Promtail restart.

The `group_id` defined the unique consumer group id to use for consuming logs. Each log record published to a topic is delivered to one consumer instance within each subscribing consumer group.

- If all promtail instances have the same consumer group, then the records will effectively be load balanced over the promtail instances.
- If all promtail instances have different consumer groups, then each record will be broadcast to all promtail instances.

The `group_id` is useful if you want to effectively send the data to multiple Loki instances and/or other sinks.

The `assignor` configuration allow you to select the rebalancing strategy to use for the consumer group.
Rebalancing is the process where a group of consumer instances (belonging to the same group) co-ordinate to own a mutually exclusive set of partitions of topics that the group is subscribed to.

- `range` the default, assigns partitions as ranges to consumer group members.
- `sticky` assigns partitions to members with an attempt to preserve earlier assignments
- `roundrobin` assigns partitions to members in alternating order.

The `version` allows to select the kafka version required to connect to the cluster.(default to `2.2.1`)

By default, timestamps are assigned by Promtail when the message is read, if you want to keep the actual message timestamp from Kafka you can set the `use_incoming_timestamp` to true.

```yaml
# The list of brokers to connect to kafka (Required).
[brokers: <strings> | default = [""]]

# The list of Kafka topics to consume (Required).
[topics: <strings> | default = [""]]

# The Kafka consumer group id.
[group_id: <string> | default = "promtail"]

# The consumer group rebalancing strategy to use. (e.g `sticky`, `roundrobin` or `range`)
[assignor: <string> | default = "range"]

# Kafka version to connect to.
[version: <string> | default = "2.2.1"]

# Optional authentication configuration with Kafka brokers
authentication:
  # Type is authentication type. Supported values [none, ssl, sasl]
  [type: <string> | default = "none"]

  # TLS configuration for authentication and encryption. It is used only when authentication type is ssl.
  tls_config:
    [ <tls_config> ]

  # SASL configuration for authentication. It is used only when authentication type is sasl.
  sasl_config:
    # SASL mechanism. Supported values [PLAIN, SCRAM-SHA-256, SCRAM-SHA-512]
    [mechanism: <string> | default = "PLAIN"]

    # The user name to use for SASL authentication
    [user: <string>]

    # The password to use for SASL authentication
    [password: <secret>]

    # If true, SASL authentication is executed over TLS
    [use_tls: <boolean> | default = false]

    # The CA file to use to verify the server
    [ca_file: <string>]

    # Validates that the server name in the server's certificate
    # is this value.
    [server_name: <string>]

    # If true, ignores the server certificate being signed by an
    # unknown CA.
    [insecure_skip_verify: <boolean> | default = false]


# Label map to add to every log line read from kafka
labels:
  [ <labelname>: <labelvalue> ... ]

# If Promtail should pass on the timestamp from the incoming log or not.
# When false Promtail will assign the current timestamp to the log when it was processed
[use_incoming_timestamp: <bool> | default = false]
```

#### Available Labels

The list of labels below are discovered when consuming kafka:

- `__meta_kafka_topic`: The current topic for where the message has been read.
- `__meta_kafka_partition`: The partition id where the message has been read.
- `__meta_kafka_member_id`: The consumer group member id.
- `__meta_kafka_group_id`: The consumer group id.
- `__meta_kafka_message_key`: The message key. If it is empty, this value will be 'none'.

To keep discovered labels to your logs use the [relabel_configs](#relabel_configs) section.

### GELF

The `gelf` block configures a GELF UDP listener allowing users to push
logs to Promtail with the [GELF](https://docs.graylog.org/docs/gelf) protocol.
Currently only UDP is supported, submit a feature request if you're interested into TCP support.

> GELF messages can be sent uncompressed or compressed with either GZIP or ZLIB.

Each GELF message received will be encoded in JSON as the log line. For example:

```json
{"version":"1.1","host":"example.org","short_message":"A short message","timestamp":1231231123,"level":5,"_some_extra":"extra"}
```

You can leverage [pipeline stages]({{< relref "./stages" >}}) with the GELF target,
if for example, you want to parse the log line and extract more labels or change the log line format.

```yaml
# UDP address to listen on. Has the format of "host:port". Default to 0.0.0.0:12201
listen_address: <string>

# Label map to add to every log message.
labels:
  [ <labelname>: <labelvalue> ... ]

# Whether Promtail should pass on the timestamp from the incoming gelf message.
# When false, or if no timestamp is present on the gelf message, Promtail will assign the current timestamp to the log when it was processed.
# Default is false
use_incoming_timestamp: <bool>

```

#### Available Labels

- `__gelf_message_level`: The GELF level as string.
- `__gelf_message_host`: The host sending the GELF message.
- `__gelf_message_version`: The GELF level message version set by the client.
- `__gelf_message_facility`: The GELF facility.

To keep discovered labels to your logs use the [relabel_configs](#relabel_configs) section.

### Cloudflare

The `cloudflare` block configures Promtail to pull logs from the Cloudflare
[Logpull API](https://developers.cloudflare.com/logs/logpull).

These logs contain data related to the connecting client, the request path through the Cloudflare network, and the response from the origin web server. This data is useful for enriching existing logs on an origin server.

```yaml
# The Cloudflare API token to use. (Required)
# You can create a new token by visiting your [Cloudflare profile](https://dash.cloudflare.com/profile/api-tokens).
api_token: <string>

# The Cloudflare zone id to pull logs for. (Required)
zone_id: <string>

# The time range to pull logs for.
[pull_range: <duration> | default = 1m]

# The quantity of workers that will pull logs.
[workers: <int> | default = 3]

# The type list of fields to fetch for logs.
# Supported values: default, minimal, extended, all.
[fields_type: <string> | default = default]

# The additional list of fields to supplement those provided via `fields_type`.
[additional_fields: <string> ... ]

# Label map to add to every log message.
labels:
  [ <labelname>: <labelvalue> ... ]

```

By default Promtail fetches logs with the default set of fields.
Here are the different set of fields type available and the fields they include :

- `default` includes `"ClientIP", "ClientRequestHost", "ClientRequestMethod", "ClientRequestURI", "EdgeEndTimestamp", "EdgeResponseBytes",
"EdgeRequestHost", "EdgeResponseStatus", "EdgeStartTimestamp", "RayID"`
plus any extra fields provided via `additional_fields` argument.

- `minimal` includes all `default` fields and adds `"ZoneID", "ClientSSLProtocol", "ClientRequestProtocol", "ClientRequestPath", "ClientRequestUserAgent", "ClientRequestReferer",
"EdgeColoCode", "ClientCountry", "CacheCacheStatus", "CacheResponseStatus", "EdgeResponseContentType"`
plus any extra fields provided via `additional_fields` argument.

- `extended` includes all `minimal`fields and adds `"ClientSSLCipher", "ClientASN", "ClientIPClass", "CacheResponseBytes", "EdgePathingOp", "EdgePathingSrc", "EdgePathingStatus", "ParentRayID",
"WorkerCPUTime", "WorkerStatus", "WorkerSubrequest", "WorkerSubrequestCount", "OriginIP", "OriginResponseStatus", "OriginSSLProtocol",
"OriginResponseHTTPExpires", "OriginResponseHTTPLastModified"`
plus any extra fields provided via `additional_fields` argument.

- `all` includes all `extended` fields and adds `"BotScore", "BotScoreSrc", "BotTags", "ClientRequestBytes", "ClientSrcPort", "ClientXRequestedWith", "CacheTieredFill", "EdgeResponseCompressionRatio", "EdgeServerIP", "FirewallMatchesSources",
"FirewallMatchesActions", "FirewallMatchesRuleIDs", "OriginResponseBytes", "OriginResponseTime", "ClientDeviceType", "WAFFlags", "WAFMatchedVar", "EdgeColoID", "RequestHeaders", "ResponseHeaders", "ClientRequestSource"`
plus any extra fields provided via `additional_fields` argument (this is still relevant when new fields are made available via Cloudflare API but are not yet included in `all`).

- `custom` includes only the fields defined in `additional_fields`.

To learn more about each field and its value, refer to the [Cloudflare documentation](https://developers.cloudflare.com/logs/reference/log-fields/zone/http_requests).

Promtail saves the last successfully-fetched timestamp in the position file.
If a position is found in the file for a given zone ID, Promtail will restart pulling logs
from that position. When no position is found, Promtail will start pulling logs from the current time.

Promtail fetches logs using multiple workers (configurable via `workers`) which request the last available pull range
(configured via `pull_range`) repeatedly. Verify the last timestamp fetched by Promtail using the `cloudflare_target_last_requested_end_timestamp` metric.
It is possible for Promtail to fall behind due to having too many log lines to process for each pull.
Adding more workers, decreasing the pull range, or decreasing the quantity of fields fetched can mitigate this performance issue.

All Cloudflare logs are in JSON. Here is an example:

```json
{
	"CacheCacheStatus": "miss",
	"CacheResponseBytes": 8377,
	"CacheResponseStatus": 200,
	"CacheTieredFill": false,
	"ClientASN": 786,
	"ClientCountry": "gb",
	"ClientDeviceType": "desktop",
	"ClientIP": "100.100.5.5",
	"ClientIPClass": "noRecord",
	"ClientRequestBytes": 2691,
	"ClientRequestHost": "www.foo.com",
	"ClientRequestMethod": "GET",
	"ClientRequestPath": "/comments/foo/",
	"ClientRequestProtocol": "HTTP/1.0",
	"ClientRequestReferer": "https://www.foo.com/foo/168855/?offset=8625",
	"ClientRequestURI": "/foo/15248108/",
	"ClientRequestUserAgent": "some bot",
        "ClientRequestSource": "1"
	"ClientSSLCipher": "ECDHE-ECDSA-AES128-GCM-SHA256",
	"ClientSSLProtocol": "TLSv1.2",
	"ClientSrcPort": 39816,
	"ClientXRequestedWith": "",
	"EdgeColoCode": "MAN",
	"EdgeColoID": 341,
	"EdgeEndTimestamp": 1637336610671000000,
	"EdgePathingOp": "wl",
	"EdgePathingSrc": "macro",
	"EdgePathingStatus": "nr",
	"EdgeRateLimitAction": "",
	"EdgeRateLimitID": 0,
	"EdgeRequestHost": "www.foo.com",
	"EdgeResponseBytes": 14878,
	"EdgeResponseCompressionRatio": 1,
	"EdgeResponseContentType": "text/html",
	"EdgeResponseStatus": 200,
	"EdgeServerIP": "8.8.8.8",
	"EdgeStartTimestamp": 1637336610517000000,
	"FirewallMatchesActions": [],
	"FirewallMatchesRuleIDs": [],
	"FirewallMatchesSources": [],
	"OriginIP": "8.8.8.8",
	"OriginResponseBytes": 0,
	"OriginResponseHTTPExpires": "",
	"OriginResponseHTTPLastModified": "",
	"OriginResponseStatus": 200,
	"OriginResponseTime": 123000000,
	"OriginSSLProtocol": "TLSv1.2",
	"ParentRayID": "00",
	"RayID": "6b0a...",
        "RequestHeaders": [],
        "ResponseHeaders": [
          "x-foo": "bar"
        ],
	"SecurityLevel": "med",
	"WAFAction": "unknown",
	"WAFFlags": "0",
	"WAFMatchedVar": "",
	"WAFProfile": "unknown",
	"WAFRuleID": "",
	"WAFRuleMessage": "",
	"WorkerCPUTime": 0,
	"WorkerStatus": "unknown",
	"WorkerSubrequest": false,
	"WorkerSubrequestCount": 0,
	"ZoneID": 1234
}
```

You can leverage [pipeline stages]({{< relref "./stages" >}}) if, for example, you want to parse the JSON log line and extract more labels or change the log line format.

### heroku_drain

The `heroku_drain` block configures Promtail to expose a [Heroku HTTPS Drain](https://devcenter.heroku.com/articles/log-drains#https-drains).

Each job configured with a Heroku Drain will expose a Drain and will require a separate port.

The `server` configuration is the same as [server](#server), since Promtail exposes an HTTP server for each new drain.

Promtail exposes an endpoint at `/heroku/api/v1/drain`, which expects requests from Heroku's log delivery.

```yaml
# The Heroku drain server configuration options
[server: <server_config>]

# Label map to add to every log message.
labels:
  [ <labelname>: <labelvalue> ... ]

# Whether Promtail should pass on the timestamp from the incoming Heroku drain message.
# When false, or if no timestamp is present in the syslog message, Promtail will assign the current
# timestamp to the log when it was processed.
[use_incoming_timestamp: <boolean> | default = false]

```

#### Available Labels

Heroku Log drains send logs in [Syslog-formatted messages](https://datatracker.ietf.org/doc/html/rfc5424#section-6) (with
some [minor tweaks](https://devcenter.heroku.com/articles/log-drains#https-drain-caveats); they are not RFC-compatible).

The Heroku Drain target exposes for each log entry the received syslog fields with the following labels:

- `__heroku_drain_host`: The [HOSTNAME](https://tools.ietf.org/html/rfc5424#section-6.2.4) field parsed from the message.
- `__heroku_drain_app`: The [APP-NAME](https://tools.ietf.org/html/rfc5424#section-6.2.5) field parsed from the message.
- `__heroku_drain_proc`: The [PROCID](https://tools.ietf.org/html/rfc5424#section-6.2.6) field parsed from the message.
- `__heroku_drain_log_id`: The [MSGID](https://tools.ietf.org/html/rfc5424#section-6.2.7) field parsed from the message.

Additionally, the Heroku drain target will read all url query parameters from the
configured drain target url and make them available as
`__heroku_drain_param_<name>` labels, multiple instances of the same parameter
will appear as comma separated strings

### relabel_configs

Relabeling is a powerful tool to dynamically rewrite the label set of a target
before it gets scraped. Multiple relabeling steps can be configured per scrape
configuration. They are applied to the label set of each target in order of
their appearance in the configuration file.

After relabeling, the `instance` label is set to the value of `__address__` by
default if it was not set during relabeling. The `__scheme__` and
`__metrics_path__` labels are set to the scheme and metrics path of the target
respectively. The `__param_<name>` label is set to the value of the first passed
URL parameter called `<name>`.

Additional labels prefixed with `__meta_` may be available during the relabeling
phase. They are set by the service discovery mechanism that provided the target
and vary between mechanisms.

Labels starting with `__` will be removed from the label set after target
relabeling is completed.

If a relabeling step needs to store a label value only temporarily (as the
input to a subsequent relabeling step), use the `__tmp` label name prefix. This
prefix is guaranteed to never be used by Prometheus itself.

```yaml
# The source labels select values from existing labels. Their content is concatenated
# using the configured separator and matched against the configured regular expression
# for the replace, keep, and drop actions.
[ source_labels: '[' <labelname> [, ...] ']' ]

# Separator placed between concatenated source label values.
[ separator: <string> | default = ; ]

# Label to which the resulting value is written in a replace action.
# It is mandatory for replace actions. Regex capture groups are available.
[ target_label: <labelname> ]

# Regular expression against which the extracted value is matched.
[ regex: <regex> | default = (.*) ]

# Modulus to take of the hash of the source label values.
[ modulus: <uint64> ]

# Replacement value against which a regex replace is performed if the
# regular expression matches. Regex capture groups are available.
[ replacement: <string> | default = $1 ]

# Action to perform based on regex matching.
[ action: <relabel_action> | default = replace ]
```

`<regex>` is any valid
[RE2 regular expression](https://github.com/google/re2/wiki/Syntax). It is
required for the `replace`, `keep`, `drop`, `labelmap`,`labeldrop` and
`labelkeep` actions. The regex is anchored on both ends. To un-anchor the regex,
use `.*<regex>.*`.

`<relabel_action>` determines the relabeling action to take:

- `replace`: Match `regex` against the concatenated `source_labels`. Then, set
  `target_label` to `replacement`, with match group references
  (`${1}`, `${2}`, ...) in `replacement` substituted by their value. If `regex`
  does not match, no replacement takes place.
- `keep`: Drop targets for which `regex` does not match the concatenated `source_labels`.
- `drop`: Drop targets for which `regex` matches the concatenated `source_labels`.
- `hashmod`: Set `target_label` to the `modulus` of a hash of the concatenated `source_labels`.
- `labelmap`: Match `regex` against all label names. Then copy the values of the matching labels
   to label names given by `replacement` with match group references
  (`${1}`, `${2}`, ...) in `replacement` substituted by their value.
- `labeldrop`: Match `regex` against all label names. Any label that matches will be
  removed from the set of labels.
- `labelkeep`: Match `regex` against all label names. Any label that does not match will be
  removed from the set of labels.

Care must be taken with `labeldrop` and `labelkeep` to ensure that logs are
still uniquely labeled once the labels are removed.

### static_configs

A `static_configs` allows specifying a list of targets and a common label set
for them.  It is the canonical way to specify static targets in a scrape
configuration.

```yaml
# Configures the discovery to look on the current machine.
# This is required by the prometheus service discovery code but doesn't
# really apply to Promtail which can ONLY look at files on the local machine
# As such it should only have the value of localhost, OR it can be excluded
# entirely and a default value of localhost will be applied by Promtail.
targets:
  - localhost

# Defines a file to scrape and an optional set of additional labels to apply to
# all streams defined by the files from __path__.
labels:
  # The path to load logs from. Can use glob patterns (e.g., /var/log/*.log).
  __path__: <string>

  # Used to exclude files from being loaded. Can also use glob patterns.
  __path_exclude__: <string>

  # Additional labels to assign to the logs
  [ <labelname>: <labelvalue> ... ]
```

### file_sd_config

File-based service discovery provides a more generic way to configure static
targets and serves as an interface to plug in custom service discovery
mechanisms.

It reads a set of files containing a list of zero or more
`<static_config>`s. Changes to all defined files are detected via disk watches
and applied immediately. Files may be provided in YAML or JSON format. Only
changes resulting in well-formed target groups are applied.

The JSON file must contain a list of static configs, using this format:

```yaml
[
  {
    "targets": [ "localhost" ],
    "labels": {
      "__path__": "<string>", ...
      "<labelname>": "<labelvalue>", ...
    }
  },
  ...
]
```

As a fallback, the file contents are also re-read periodically at the specified
refresh interval.

Each target has a meta label `__meta_filepath` during the
[relabeling phase](#relabel_configs). Its value is set to the
filepath from which the target was extracted.

```yaml
# Patterns for files from which target groups are extracted.
files:
  [ - <filename_pattern> ... ]

# Refresh interval to re-read the files.
[ refresh_interval: <duration> | default = 5m ]
```

Where `<filename_pattern>` may be a path ending in `.json`, `.yml` or `.yaml`.
The last path segment may contain a single `*` that matches any character
sequence, e.g. `my/path/tg_*.json`.

### kubernetes_sd_config

Kubernetes SD configurations allow retrieving scrape targets from
[Kubernetes'](https://kubernetes.io/) REST API and always staying synchronized
with the cluster state.

One of the following `role` types can be configured to discover targets:

#### `node`

The `node` role discovers one target per cluster node with the address
defaulting to the Kubelet's HTTP port.

The target address defaults to the first existing address of the Kubernetes
node object in the address type order of `NodeInternalIP`, `NodeExternalIP`,
`NodeLegacyHostIP`, and `NodeHostName`.

Available meta labels:

- `__meta_kubernetes_node_name`: The name of the node object.
- `__meta_kubernetes_node_label_<labelname>`: Each label from the node object.
- `__meta_kubernetes_node_labelpresent_<labelname>`: `true` for each label from the node object.
- `__meta_kubernetes_node_annotation_<annotationname>`: Each annotation from the node object.
- `__meta_kubernetes_node_annotationpresent_<annotationname>`: `true` for each annotation from the node object.
- `__meta_kubernetes_node_address_<address_type>`: The first address for each node address type, if it exists.

In addition, the `instance` label for the node will be set to the node name
as retrieved from the API server.

#### `service`

The `service` role discovers a target for each service port of each service.
This is generally useful for blackbox monitoring of a service.
The address will be set to the Kubernetes DNS name of the service and respective
service port.

Available meta labels:

- `__meta_kubernetes_namespace`: The namespace of the service object.
- `__meta_kubernetes_service_annotation_<annotationname>`: Each annotation from the service object.
- `__meta_kubernetes_service_annotationpresent_<annotationname>`: "true" for each annotation of the service object.
- `__meta_kubernetes_service_cluster_ip`: The cluster IP address of the service. (Does not apply to services of type ExternalName)
- `__meta_kubernetes_service_external_name`: The DNS name of the service. (Applies to services of type ExternalName)
- `__meta_kubernetes_service_label_<labelname>`: Each label from the service object.
- `__meta_kubernetes_service_labelpresent_<labelname>`: `true` for each label of the service object.
- `__meta_kubernetes_service_name`: The name of the service object.
- `__meta_kubernetes_service_port_name`: Name of the service port for the target.
- `__meta_kubernetes_service_port_protocol`: Protocol of the service port for the target.

#### `pod`

The `pod` role discovers all pods and exposes their containers as targets. For
each declared port of a container, a single target is generated. If a container
has no specified ports, a port-free target per container is created for manually
adding a port via relabeling.

Available meta labels:

- `__meta_kubernetes_namespace`: The namespace of the pod object.
- `__meta_kubernetes_pod_name`: The name of the pod object.
- `__meta_kubernetes_pod_ip`: The pod IP of the pod object.
- `__meta_kubernetes_pod_label_<labelname>`: Each label from the pod object.
- `__meta_kubernetes_pod_labelpresent_<labelname>`: `true`for each label from the pod object.
- `__meta_kubernetes_pod_annotation_<annotationname>`: Each annotation from the pod object.
- `__meta_kubernetes_pod_annotationpresent_<annotationname>`: `true` for each annotation from the pod object.
- `__meta_kubernetes_pod_container_init`: `true` if the container is an [InitContainer](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/)
- `__meta_kubernetes_pod_container_name`: Name of the container the target address points to.
- `__meta_kubernetes_pod_container_port_name`: Name of the container port.
- `__meta_kubernetes_pod_container_port_number`: Number of the container port.
- `__meta_kubernetes_pod_container_port_protocol`: Protocol of the container port.
- `__meta_kubernetes_pod_ready`: Set to `true` or `false` for the pod's ready state.
- `__meta_kubernetes_pod_phase`: Set to `Pending`, `Running`, `Succeeded`, `Failed` or `Unknown`
  in the [lifecycle](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-phase).
- `__meta_kubernetes_pod_node_name`: The name of the node the pod is scheduled onto.
- `__meta_kubernetes_pod_host_ip`: The current host IP of the pod object.
- `__meta_kubernetes_pod_uid`: The UID of the pod object.
- `__meta_kubernetes_pod_controller_kind`: Object kind of the pod controller.
- `__meta_kubernetes_pod_controller_name`: Name of the pod controller.

#### `endpoints`

The `endpoints` role discovers targets from listed endpoints of a service. For
each endpoint address one target is discovered per port. If the endpoint is
backed by a pod, all additional container ports of the pod, not bound to an
endpoint port, are discovered as targets as well.

Available meta labels:

- `__meta_kubernetes_namespace`: The namespace of the endpoints object.
- `__meta_kubernetes_endpoints_name`: The names of the endpoints object.
- For all targets discovered directly from the endpoints list (those not additionally inferred
  from underlying pods), the following labels are attached:
  - `__meta_kubernetes_endpoint_hostname`: Hostname of the endpoint.
  - `__meta_kubernetes_endpoint_node_name`: Name of the node hosting the endpoint.
  - `__meta_kubernetes_endpoint_ready`: Set to `true` or `false` for the endpoint's ready state.
  - `__meta_kubernetes_endpoint_port_name`: Name of the endpoint port.
  - `__meta_kubernetes_endpoint_port_protocol`: Protocol of the endpoint port.
  - `__meta_kubernetes_endpoint_address_target_kind`: Kind of the endpoint address target.
  - `__meta_kubernetes_endpoint_address_target_name`: Name of the endpoint address target.
- If the endpoints belong to a service, all labels of the `role: service` discovery are attached.
- For all targets backed by a pod, all labels of the `role: pod` discovery are attached.

#### `ingress`

The `ingress` role discovers a target for each path of each ingress.
This is generally useful for blackbox monitoring of an ingress.
The address will be set to the host specified in the ingress spec.

Available meta labels:

- `__meta_kubernetes_namespace`: The namespace of the ingress object.
- `__meta_kubernetes_ingress_name`: The name of the ingress object.
- `__meta_kubernetes_ingress_label_<labelname>`: Each label from the ingress object.
- `__meta_kubernetes_ingress_labelpresent_<labelname>`: `true` for each label from the ingress object.
- `__meta_kubernetes_ingress_annotation_<annotationname>`: Each annotation from the ingress object.
- `__meta_kubernetes_ingress_annotationpresent_<annotationname>`: `true` for each annotation from the ingress object.
- `__meta_kubernetes_ingress_scheme`: Protocol scheme of ingress, `https` if TLS
  config is set. Defaults to `http`.
- `__meta_kubernetes_ingress_path`: Path from ingress spec. Defaults to `/`.

See below for the configuration options for Kubernetes discovery:

```yaml
# The information to access the Kubernetes API.

# The API server addresses. If left empty, Prometheus is assumed to run inside
# of the cluster and will discover API servers automatically and use the pod's
# CA certificate and bearer token file at /var/run/secrets/kubernetes.io/serviceaccount/.
[ api_server: <host> ]

# The Kubernetes role of entities that should be discovered.
role: <role>

# Optional authentication information used to authenticate to the API server.
# Note that `basic_auth`, `bearer_token` and `bearer_token_file` options are
# mutually exclusive.
# password and password_file are mutually exclusive.

# Optional HTTP basic authentication information.
basic_auth:
  [ username: <string> ]
  [ password: <secret> ]
  [ password_file: <string> ]

# Optional bearer token authentication information.
[ bearer_token: <secret> ]

# Optional bearer token file authentication information.
[ bearer_token_file: <filename> ]

# Optional proxy URL.
[ proxy_url: <string> ]

# TLS configuration.
tls_config:
  [ <tls_config> ]

# Optional namespace discovery. If omitted, all namespaces are used.
namespaces:
  names:
    [ - <string> ]

# Optional label and field selectors to limit the discovery process to a subset of available
#  resources. See
# https://kubernetes.io/docs/concepts/overview/working-with-objects/field-selectors/
# and https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/ to learn
# more about the possible filters that can be used. The endpoints role supports pod,
# service, and endpoint selectors. Roles only support selectors matching the role itself;
# for example, the node role can only contain node selectors.
# Note: When making decisions about using field/label selectors, make sure that this
# is the best approach. It will prevent Promtail from reusing single list/watch
# for all scrape configurations. This might result in a bigger load on the Kubernetes API,
# because for each selector combination, there will be additional LIST/WATCH.
# On the other hand, if you want to monitor a small subset of pods of a large cluster,
# we recommend using selectors. The decision on the use of selectors or not depends
# on the particular situation.
[ selectors:
          [ - role: <string>
                  [ label: <string> ]
                  [ field: <string> ] ]]
```

Where `<role>` must be `endpoints`, `service`, `pod`, `node`, or
`ingress`.

See
[this example Prometheus configuration file](https://github.com/prometheus/prometheus/blob/master/documentation/examples/prometheus-kubernetes.yml)
for a detailed example of configuring Prometheus for Kubernetes.

You may wish to check out the 3rd party
[Prometheus Operator](https://github.com/coreos/prometheus-operator),
which automates the Prometheus setup on top of Kubernetes.

### consul_sd_config

Consul SD configurations allow retrieving scrape targets from the [Consul Catalog API](https://www.consul.io).
When using the Catalog API, each running Promtail will get
a list of all services known to the whole consul cluster when discovering
new targets.

The following meta labels are available on targets during [relabeling](#relabel_configs):

* `__meta_consul_address`: the address of the target
* `__meta_consul_dc`: the datacenter name for the target
* `__meta_consul_health`: the health status of the service
* `__meta_consul_metadata_<key>`: each node metadata key value of the target
* `__meta_consul_node`: the node name defined for the target
* `__meta_consul_service_address`: the service address of the target
* `__meta_consul_service_id`: the service ID of the target
* `__meta_consul_service_metadata_<key>`: each service metadata key value of the target
* `__meta_consul_service_port`: the service port of the target
* `__meta_consul_service`: the name of the service the target belongs to
* `__meta_consul_tagged_address_<key>`: each node tagged address key value of the target
* `__meta_consul_tags`: the list of tags of the target joined by the tag separator

```yaml
# The information to access the Consul Catalog API. It is to be defined
# as the Consul documentation requires.
[ server: <host> | default = "localhost:8500" ]
[ token: <secret> ]
[ datacenter: <string> ]
[ scheme: <string> | default = "http" ]
[ username: <string> ]
[ password: <secret> ]

tls_config:
  [ <tls_config> ]

# A list of services for which targets are retrieved. If omitted, all services
# are scraped.
services:
  [ - <string> ]

# See https://www.consul.io/api/catalog.html#list-nodes-for-service to know more
# about the possible filters that can be used.

# An optional list of tags used to filter nodes for a given service. Services must contain all tags in the list.
tags:
  [ - <string> ]

# Node metadata key/value pairs to filter nodes for a given service.
[ node_meta:
  [ <string>: <string> ... ] ]

# The string by which Consul tags are joined into the tag label.
[ tag_separator: <string> | default = , ]

# Allow stale Consul results (see https://www.consul.io/api/features/consistency.html). Will reduce load on Consul.
[ allow_stale: <boolean> | default = true ]

# The time after which the provided names are refreshed.
# On large setup it might be a good idea to increase this value because the catalog will change all the time.
[ refresh_interval: <duration> | default = 30s ]
```

Note that the IP number and port used to scrape the targets is assembled as
`<__meta_consul_address>:<__meta_consul_service_port>`. However, in some
Consul setups, the relevant address is in `__meta_consul_service_address`.
In those cases, you can use the [relabel](#relabel_configs)
feature to replace the special `__address__` label.

The [relabeling phase](#relabel_configs) is the preferred and more powerful
way to filter services or nodes for a service based on arbitrary labels. For
users with thousands of services it can be more efficient to use the Consul API
directly which has basic support for filtering nodes (currently by node
metadata and a single tag).

### consulagent_sd_config

Consul Agent SD configurations allow retrieving scrape targets from [Consul's](https://www.consul.io)
Agent API. When using the Agent API, each running Promtail will only get
services registered with the local agent running on the same host when discovering
new targets. This is suitable for very large Consul clusters for which using the
Catalog API would be too slow or resource intensive.

The following meta labels are available on targets during [relabeling](#relabel_configs):

* `__meta_consulagent_address`: the address of the target
* `__meta_consulagent_dc`: the datacenter name for the target
* `__meta_consulagent_health`: the health status of the service
* `__meta_consulagent_metadata_<key>`: each node metadata key value of the target
* `__meta_consulagent_node`: the node name defined for the target
* `__meta_consulagent_service_address`: the service address of the target
* `__meta_consulagent_service_id`: the service ID of the target
* `__meta_consulagent_service_metadata_<key>`: each service metadata key value of the target
* `__meta_consulagent_service_port`: the service port of the target
* `__meta_consulagent_service`: the name of the service the target belongs to
* `__meta_consulagent_tagged_address_<key>`: each node tagged address key value of the target
* `__meta_consulagent_tags`: the list of tags of the target joined by the tag separator

```yaml
# The information to access the Consul Agent API. It is to be defined
# as the Consul documentation requires.
[ server: <host> | default = "localhost:8500" ]
[ token: <secret> ]
[ datacenter: <string> ]
[ scheme: <string> | default = "http" ]
[ username: <string> ]
[ password: <secret> ]

tls_config:
  [ <tls_config> ]

# A list of services for which targets are retrieved. If omitted, all services
# are scraped.
services:
  [ - <string> ]

# See https://www.consul.io/api-docs/agent/service#filtering to know more
# about the possible filters that can be used.

# An optional list of tags used to filter nodes for a given service. Services must contain all tags in the list.
tags:
  [ - <string> ]

# Node metadata key/value pairs to filter nodes for a given service.
[ node_meta:
  [ <string>: <string> ... ] ]

# The string by which Consul tags are joined into the tag label.
[ tag_separator: <string> | default = , ]
```

Note that the IP address and port number used to scrape the targets is assembled as
`<__meta_consul_address>:<__meta_consul_service_port>`. However, in some
Consul setups, the relevant address is in `__meta_consul_service_address`.
In those cases, you can use the [relabel](#relabel_configs)
feature to replace the special `__address__` label.

The [relabeling phase](#relabel_configs) is the preferred and more powerful
way to filter services or nodes for a service based on arbitrary labels. For
users with thousands of services it can be more efficient to use the Consul API
directly which has basic support for filtering nodes (currently by node
metadata and a single tag).

### docker_sd_configs

Docker service discovery allows retrieving targets from a Docker daemon.
It will only watch containers of the Docker daemon referenced with the host parameter. Docker
service discovery should run on each node in a distributed setup. The containers must run with
either the [json-file](https://docs.docker.com/config/containers/logging/json-file/)
or [journald](https://docs.docker.com/config/containers/logging/journald/) logging driver.

Note that the discovery will not pick up finished containers. That means
Promtail will not scrape the remaining logs from finished containers after a restart.

The Docker target correctly joins log segments if a long line was split into different frames by Docker.
To avoid hypothetically unlimited line size and out-of-memory errors in Promtail, this target applies
a default soft line size limit of 256 kiB corresponding to the default max line size in Loki.
If the buffer increases above this size, then the line will be sent to output immediately, and the rest
of the line discarded. To change this behaviour, set `limits_config.max_line_size` to a non-zero value
to apply a hard limit.

The configuration is inherited from [Prometheus' Docker service discovery](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#docker_sd_config).

```yaml
# Address of the Docker daemon.  Use unix:///var/run/docker.sock for a local setup.
host: <string>

# Optional proxy URL.
[ proxy_url: <string> ]

# TLS configuration.
tls_config:
  [ <tls_config> ]

# The port to scrape metrics from, when `role` is nodes, and for discovered
# tasks and services that don't have published ports.
[ port: <int> | default = 80 ]

# The host to use if the container is in host networking mode.
[ host_networking_host: <string> | default = "localhost" ]

# Optional filters to limit the discovery process to a subset of available
# resources.
# The available filters are listed in the Docker documentation:
# Containers: https://docs.docker.com/engine/api/v1.41/#operation/ContainerList
[ filters:
  [ - name: <string>
      values: <string>, [...] ]
]

# The time after which the containers are refreshed.
[ refresh_interval: <duration> | default = 60s ]

# Authentication information used by Promtail to authenticate itself to the
# Docker daemon.
# Note that `basic_auth` and `authorization` options are mutually exclusive.
# `password` and `password_file` are mutually exclusive.

# Optional HTTP basic authentication information.
basic_auth:
  [ username: <string> ]
  [ password: <secret> ]
  [ password_file: <string> ]

# Optional `Authorization` header configuration.
authorization:
  # Sets the authentication type.
  [ type: <string> | default: Bearer ]
  # Sets the credentials. It is mutually exclusive with
  # `credentials_file`.
  [ credentials: <secret> ]
  # Sets the credentials to the credentials read from the configured file.
  # It is mutually exclusive with `credentials`.
  [ credentials_file: <filename> ]

# Optional OAuth 2.0 configuration.
# Cannot be used at the same time as basic_auth or authorization.
oauth2:
  [ <oauth2> ]

# Configure whether HTTP requests follow HTTP 3xx redirects.
[ follow_redirects: <bool> | default = true ]
```

Available meta labels:

  * `__meta_docker_container_id`: the ID of the container
  * `__meta_docker_container_name`: the name of the container
  * `__meta_docker_container_network_mode`: the network mode of the container
  * `__meta_docker_container_label_<labelname>`: each label of the container
  * `__meta_docker_container_log_stream`: the log stream type `stdout` or `stderr`
  * `__meta_docker_network_id`: the ID of the network
  * `__meta_docker_network_name`: the name of the network
  * `__meta_docker_network_ingress`: whether the network is ingress
  * `__meta_docker_network_internal`: whether the network is internal
  * `__meta_docker_network_label_<labelname>`: each label of the network
  * `__meta_docker_network_scope`: the scope of the network
  * `__meta_docker_network_ip`: the IP of the container in this network
  * `__meta_docker_port_private`: the port on the container
  * `__meta_docker_port_public`: the external port if a port-mapping exists
  * `__meta_docker_port_public_ip`: the public IP if a port-mapping exists

These labels can be used during relabeling. For instance, the following configuration scrapes the container named `flog` and removes the leading slash (`/`) from the container name.

```yaml
scrape_configs:
  - job_name: flog_scrape
    docker_sd_configs:
      - host: unix:///var/run/docker.sock
        refresh_interval: 5s
        filters:
          - name: name
            values: [flog]
    relabel_configs:
      - source_labels: ['__meta_docker_container_name']
        regex: '/(.*)'
        target_label: 'container'
```

## limits_config

The optional `limits_config` block configures global limits for this instance of Promtail.

```yaml
# When true, enforces rate limiting on this instance of Promtail.
[readline_rate_enabled: <bool> | default = false]

# The rate limit in log lines per second that this instance of Promtail may push to Loki.
[readline_rate: <int> | default = 10000]

# The cap in the quantity of burst lines that this instance of Promtail may push
# to Loki.
[readline_burst: <int> | default = 10000]

# When true, exceeding the rate limit causes this instance of Promtail to discard
# log lines, rather than sending them to Loki. When false, exceeding the rate limit
# causes this instance of Promtail to temporarily hold off on sending the log lines and retry later.
[readline_rate_drop: <bool> | default = true]

# Limits the max number of active streams.
# Limiting the number of streams is useful as a mechanism to limit memory usage by Promtail, which helps
# to avoid OOM scenarios.
# 0 means it is disabled.
[max_streams: <int> | default = 0]

# Maximum log line byte size allowed without dropping. Example: 256kb, 2M. 0 to disable.
# If disabled, targets may apply default buffer size safety limits. If a target implements
# a default limit, this will be documented under the `scrape_configs` entry.
[max_line_size: <int> | default = 0]
# Whether to truncate lines that exceed max_line_size. No effect if max_line_size is disabled
[max_line_size_truncate: <bool> | default = false]
```

## target_config

The `target_config` block controls the behavior of reading files from discovered
targets.

```yaml
# Period to resync directories being watched and files being tailed to discover
# new ones or stop watching removed ones.
sync_period: "10s"
```

## options_config

## tracing_config

The `tracing` block configures tracing for Jaeger. Currently, limited to configuration per [environment variables](https://www.jaegertracing.io/docs/1.16/client-features/) only.

```yaml
# When true,
[enabled: <boolean> | default = false]
```

## Example Docker Config

It's fairly difficult to tail Docker files on a standalone machine because they are in different locations for every OS.  We recommend the [Docker logging driver]({{< relref "../../send-data/docker-driver" >}}) for local Docker installs or Docker Compose.

If running in a Kubernetes environment, you should look at the defined configs which are in [helm](https://github.com/grafana/helm-charts/blob/main/charts/promtail/templates/configmap.yaml) and [jsonnet](https://github.com/grafana/loki/blob/main/production/ksonnet/promtail/scrape_config.libsonnet), these leverage the prometheus service discovery libraries (and give Promtail its name) for automatically finding and tailing pods.  The jsonnet config explains with comments what each section is for.


## Example Static Config

While Promtail may have been named for the prometheus service discovery code, that same code works very well for tailing logs without containers or container environments directly on virtual machines or bare metal.

```yaml
server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /var/log/positions.yaml # This location needs to be writeable by Promtail.

clients:
  - url: http://ip_or_hostname_where_Loki_run:3100/loki/api/v1/push

scrape_configs:
 - job_name: system
   pipeline_stages:
   static_configs:
   - targets:
      - localhost
     labels:
      job: varlogs  # A `job` label is fairly standard in prometheus and useful for linking metrics and logs.
      host: yourhost # A `host` label will help identify logs from this machine vs others
      __path__: /var/log/*.log  # The path matching uses a third party library: https://github.com/bmatcuk/doublestar
```

If you are rotating logs, be careful when using a wildcard pattern like `*.log`, and make sure it doesn't match the rotated log file. For example, if you move your logs from `server.log` to `server.01-01-1970.log` in the same directory every night, a static config with a wildcard search pattern like `*.log` will pick up that new file and read it, effectively causing the entire days logs to be re-ingested.

## Example Static Config without targets

While Promtail may have been named for the prometheus service discovery code, that same code works very well for tailing logs without containers or container environments directly on virtual machines or bare metal.

```yaml
server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /var/log/positions.yaml # This location needs to be writeable by Promtail.

clients:
  - url: http://ip_or_hostname_where_Loki_run:3100/loki/api/v1/push

scrape_configs:
 - job_name: system
   pipeline_stages:
   static_configs:
   - labels:
      job: varlogs  # A `job` label is fairly standard in prometheus and useful for linking metrics and logs.
      host: yourhost # A `host` label will help identify logs from this machine vs others
      __path__: /var/log/*.log  # The path matching uses a third party library: https://github.com/bmatcuk/doublestar
```

## Example Journal Config

This example reads entries from a systemd journal:

```yaml
server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml

clients:
  - url: http://ip_or_hostname_where_loki_runs:3100/loki/api/v1/push

scrape_configs:
  - job_name: journal
    journal:
      max_age: 12h
      labels:
        job: systemd-journal
    relabel_configs:
      - source_labels: ['__journal__systemd_unit']
        target_label: 'unit'
```

## Example Syslog Config

This example starts Promtail as a syslog receiver and can accept syslog entries in Promtail over TCP:

```yaml
server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml

clients:
  - url: http://loki_addr:3100/loki/api/v1/push

scrape_configs:
  - job_name: syslog
    syslog:
      listen_address: 0.0.0.0:1514
      labels:
        job: "syslog"
    relabel_configs:
      - source_labels: ['__syslog_message_hostname']
        target_label: 'host'
```

## Example Push Config

The example starts Promtail as a Push receiver and will accept logs from other Promtail instances or the Docker Logging Driver:

```yaml
server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml

clients:
  - url: http://ip_or_hostname_where_Loki_run:3100/loki/api/v1/push

scrape_configs:
- job_name: push1
  loki_push_api:
    server:
      http_listen_port: 3500
      grpc_listen_port: 3600
    labels:
      pushserver: push1
```

Note the `job_name` must be provided and must be unique between multiple `loki_push_api` scrape_configs, it will be used to register metrics.

A new server instance is created so the `http_listen_port` and `grpc_listen_port` must be different from the Promtail `server` config section (unless it's disabled)

You can set `grpc_listen_port` to `0` to have a random port assigned if not using httpgrpc.
