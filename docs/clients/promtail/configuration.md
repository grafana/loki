# Configuring Promtail

Promtail is configured in a YAML file (usually referred to as `config.yaml`)
which contains information on the Promtail server, where positions are stored,
and how to scrape logs from files.

* [Configuration File Reference](#configuration-file-reference)
* [server_config](#server_config)
* [client_config](#client_config)
* [position_config](#position_config)
* [scrape_config](#scrape_config)
    * [pipeline_stages](#pipeline_stages)
        * [docker](#docker)
        * [cri](#cri)
        * [regex](#regex)
        * [json](#json)
        * [template](#template)
        * [match](#match)
        * [timestamp](#timestamp)
        * [output](#output)
        * [labels](#labels)
        * [metrics](#metrics)
            * [counter](#counter)
            * [gauge](#gauge)
            * [histogram](#histogram)
        * [tenant](#tenant)
    * [journal_config](#journal_config)
    * [syslog_config](#syslog_config)
    * [relabel_config](#relabel_config)
    * [static_config](#static_config)
    * [file_sd_config](#file_sd_config)
    * [kubernetes_sd_config](#kubernetes_sd_config)
* [target_config](#target_config)
* [Example Docker Config](#example-docker-config)
* [Example Static Config](#example-static-config)
* [Example Journal Config](#example-journal-config)
* [Example Syslog Config](#example-syslog-config)

## Configuration File Reference

To specify which configuration file to load, pass the `-config.file` flag at the
command line. The file is written in [YAML format](https://en.wikipedia.org/wiki/YAML),
defined by the schema below. Brackets indicate that a parameter is optional. For
non-list parameters the value is set to the specified default.

For more detailed information on configuring how to discover and scrape logs from
targets, see [Scraping](scraping.md). For more information on transforming logs
from scraped targets, see [Pipelines](pipelines.md).

Generic placeholders are defined as follows:

* `<boolean>`: a boolean that can take the values `true` or `false`
* `<int>`: any integer matching the regular expression `[1-9]+[0-9]*`
* `<duration>`: a duration matching the regular expression `[0-9]+(ms|[smhdwy])`
* `<labelname>`: a string matching the regular expression `[a-zA-Z_][a-zA-Z0-9_]*`
* `<labelvalue>`: a string of Unicode characters
* `<filename>`: a valid path relative to current working directory or an
    absolute path.
* `<host>`: a valid string consisting of a hostname or IP followed by an optional port number
* `<string>`: a regular string
* `<secret>`: a regular string that is a secret, such as a password

Supported contents and default values of `config.yaml`:

```yaml
# Configures the server for Promtail.
[server: <server_config>]

# Describes how Promtail connects to multiple instances
# of Loki, sending logs to each.
clients:
  - [<client_config>]

# Describes how to save read file offsets to disk
[positions: <position_config>]

scrape_configs:
  - [<scrape_config>]

# Configures how tailed targets will be watched.
[target_config: <target_config>]
```

## server_config

The `server_config` block configures Promtail's behavior as an HTTP server:

```yaml
# HTTP server listen host
[http_listen_address: <string>]

# HTTP server listen port
[http_listen_port: <int> | default = 80]

# gRPC server listen host
[grpc_listen_address: <string>]

# gRPC server listen port
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
```

## client_config

The `client_config` block configures how Promtail connects to an instance of
Loki:

```yaml
# The URL where Loki is listening, denoted in Loki as http_listen_address and
# http_listen_port. If Loki is running in microservices mode, this is the HTTP
# URL for the Distributor.
url: <string>

# The tenant ID used by default to push logs to Loki. If omitted or empty
# it assumes Loki is running in single-tenant mode and no X-Scope-OrgID header
# is sent.
[tenant_id: <string>]

# Maximum amount of time to wait before sending a batch, even if that
# batch isn't full.
[batchwait: <duration> | default = 1s]

# Maximum batch size (in bytes) of logs to accumulate before sending
# the batch to Loki.
[batchsize: <int> | default = 102400]

# If using basic auth, configures the username and password
# sent.
basic_auth:
  # The username to use for basic auth
  [username: <string>]

  # The password to use for basic auth
  [password: <string>]

  # The file containing the password for basic auth
  [password_file: <filename>]

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
backoff_config:
  # Initial backoff time between retries
  [minbackoff: <duration> | default = 100ms]

  # Maximum backoff time between retries
  [maxbackoff: <duration> | default = 10s]

  # Maximum number of retries to do
  [maxretries: <int> | default = 10]

# Static labels to add to all logs being sent to Loki.
# Use map like {"foo": "bar"} to add a label foo with
# value bar.
external_labels:
  [ <labelname>: <labelvalue> ... ]

# Maximum time to wait for a server to respond to a request
[timeout: <duration> | default = 10s]
```

## position_config

The `position_config` block configures where Promtail will save a file
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

## scrape_config

The `scrape_config` block configures how Promtail can scrape logs from a series
of targets using a specified discovery method:

```yaml
# Name to identify this scrape config in the Promtail UI.
job_name: <string>

# Describes how to parse log lines. Suported values [cri docker raw]
[entry_parser: <string> | default = "docker"]

# Describes how to transform logs from targets.
[pipeline_stages: <pipeline_stages>]

# Describes how to scrape logs from the journal.
[journal: <journal_config>]

# Describes how to receive logs from syslog.
[syslog: <syslog_config>]

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
```

### pipeline_stages

The [pipeline](./pipelines.md) stages (`pipeline_stages`) is used to transform
log entries and their labels after discovery and consists of a list of any of the items listed below.

Stages serve several purposes, more detail can be found [here](./pipelines.md), however generally you extract data with `regex` or `json` stages into a temporary map which can then be use as `labels` or `output` or any of the other stages aside from `docker` and `cri` which are explained in more detail below.

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
    <tenant>
  ]
```

#### docker

The Docker stage is a programmatic wrapper for 

```yaml
- json:
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

Which is built to match this type of log line:

```nohighlight
`{"log":"level=info ts=2019-04-30T02:12:41.844179Z caller=filetargetmanager.go:180 msg=\"Adding target\"\n","stream":"stderr","time":"2019-04-30T02:12:41.8443515Z"}`
```

Automatically extracting the `time` into the logs timestamp, `stream` into a label, and `log` field into the output, this can be very helpful as docker is wrapping your application log in this way and this will unwrap it for further pipeline processing of just the log content.

#### cri

Identical in function to the docker stage however using a regex to match CRI container logs using this equivalent config:

```yaml
- regex:
    expression: "^(?s)(?P<time>\\S+?) (?P<stream>stdout|stderr) (?P<flags>\\S+?) (?P<content>.*)$",
- labels:
    stream:
- timestamp:
    source: time
    format: RFC3339Nano
- output:
    source: content
```

built to match this style container log line:

```nohighlight
2019-01-01T01:00:00.000000001Z stderr P some log message
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
a configurable [LogQL](../../logql.md) stream selector.

```yaml
match:
  # LogQL stream selector.
  selector: <string>

  # Names the pipeline. When defined, creates an additional label in
  # the pipeline_duration_seconds histogram, where the value is
  # concatenated with job_name using an underscore.
  [pipieline_name: <string>]

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

##### output

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

# Key from the extracted data map to use for the mtric,
# defaulting to the metric's name if not present.
[source: <string>]

config:
  # Filters down source data and only changes the metric
  # if the targeted value exactly matches the provided string.
  # If not present, all data will match.
  [value: <string>]

  # Must be either "inc" or "add" (case insensitive). If
  # inc is chosen, the metric value will increase by 1 for each
  # log line receieved that passed the filter. If add is chosen,
  # the extracted value most be convertible to a positive float
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

# Key from the extracted data map to use for the mtric,
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

# Key from the extracted data map to use for the mtric,
# defaulting to the metric's name if not present.
[source: <string>]

config:
  # Filters down source data and only changes the metric
  # if the targeted value exactly matches the provided string.
  # If not present, all data will match.
  [value: <string>]

  # Must be either "inc" or "add" (case insensitive). If
  # inc is chosen, the metric value will increase by 1 for each
  # log line receieved that passed the filter. If add is chosen,
  # the extracted value most be convertible to a positive float
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
  # Name from extracted data to whose value should be set as tenant ID.
  # Either source or value config option is required, but not both (they
  # are mutually exclusive).
  [ source: <string> ]

  # Value to use to set the tenant ID when this stage is executed. Useful
  # when this stage is included within a conditional pipeline with "match".
  [ value: <string> ]
```

### journal_config

The `journal_config` block configures reading from the systemd journal from
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

# Path to a directory to read entries from. Defaults to system
# paths (/var/log/journal and /run/log/journal) when empty.
[path: <string>]
```

### syslog_config

The `syslog_config` block configures a syslog listener allowing users to push
logs to promtail with the syslog protocol.
Currently supported is [IETF Syslog (RFC5424)](https://tools.ietf.org/html/rfc5424)
with and without octet counting.

The recommended deployment is to have a dedicated syslog forwarder like **syslog-ng** or **rsyslog**
in front of promtail. The forwarder can take care of the various specifications
and transports that exist (UDP, BSD syslog, ...).

[Octet counting](https://tools.ietf.org/html/rfc6587#section-3.4.1) is recommended as the
message framing method. In a stream with [non-transparent framing](https://tools.ietf.org/html/rfc6587#section-3.4.2),
promtail needs to wait for the next message to catch multi-line messages,
therefore delays between messages can occur.

See recommended output configurations for
[syslog-ng](scraping.md#syslog-ng-output-configuration) and
[rsyslog](scraping.md#rsyslog-output-configuration). Both configurations enable
IETF Syslog with octet-counting.

You may need to increase the open files limit for the promtail process
if many clients are connected. (`ulimit -Sn`)

```yaml
# TCP address to listen on. Has the format of "host:port".
listen_address: <string>

# The idle timeout for tcp syslog connections, default is 120 seconds.
idle_timeout: <duration>

# Whether to convert syslog structured data to labels.
# A structured data entry of [example@99999 test="yes"] would become
# the label "__syslog_message_sd_example_99999_test" with the value "yes".
label_structured_data: <bool>

# Label map to add to every log message.
labels:
  [ <labelname>: <labelvalue> ... ]
```

#### Available Labels

* `__syslog_connection_ip_address`: The remote IP address.
* `__syslog_connection_hostname`: The remote hostname.
* `__syslog_message_severity`: The [syslog severity](https://tools.ietf.org/html/rfc5424#section-6.2.1) parsed from the message. Symbolic name as per [syslog_message.go](https://github.com/influxdata/go-syslog/blob/v2.0.1/rfc5424/syslog_message.go#L184).
* `__syslog_message_facility`: The [syslog facility](https://tools.ietf.org/html/rfc5424#section-6.2.1) parsed from the message. Symbolic name as per [syslog_message.go](https://github.com/influxdata/go-syslog/blob/v2.0.1/rfc5424/syslog_message.go#L235) and `syslog(3)`.
* `__syslog_message_hostname`: The [hostname](https://tools.ietf.org/html/rfc5424#section-6.2.4) parsed from the message.
* `__syslog_message_app_name`: The [app-name field](https://tools.ietf.org/html/rfc5424#section-6.2.5) parsed from the message.
* `__syslog_message_proc_id`: The [procid field](https://tools.ietf.org/html/rfc5424#section-6.2.6) parsed from the message.
* `__syslog_message_msg_id`: The [msgid field](https://tools.ietf.org/html/rfc5424#section-6.2.7) parsed from the message.
* `__syslog_message_sd_<sd_id>[_<iana_enterprise_id>]_<sd_name>`: The [structured-data field](https://tools.ietf.org/html/rfc5424#section-6.3) parsed from the message. The data field `[custom@99770 example="1"]` becomes `__syslog_message_sd_custom_99770_example`.

### relabel_config

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

* `replace`: Match `regex` against the concatenated `source_labels`. Then, set
  `target_label` to `replacement`, with match group references
  (`${1}`, `${2}`, ...) in `replacement` substituted by their value. If `regex`
  does not match, no replacement takes place.
* `keep`: Drop targets for which `regex` does not match the concatenated `source_labels`.
* `drop`: Drop targets for which `regex` matches the concatenated `source_labels`.
* `hashmod`: Set `target_label` to the `modulus` of a hash of the concatenated `source_labels`.
* `labelmap`: Match `regex` against all label names. Then copy the values of the matching labels
   to label names given by `replacement` with match group references
  (`${1}`, `${2}`, ...) in `replacement` substituted by their value.
* `labeldrop`: Match `regex` against all label names. Any label that matches will be
  removed from the set of labels.
* `labelkeep`: Match `regex` against all label names. Any label that does not match will be
  removed from the set of labels.

Care must be taken with `labeldrop` and `labelkeep` to ensure that logs are
still uniquely labeled once the labels are removed.

### static_config

A `static_config` allows specifying a list of targets and a common label set
for them.  It is the canonical way to specify static targets in a scrape
configuration.

```yaml
# Configures the discovery to look on the current machine. Must be either
# localhost or the hostname of the current computer.
targets:
  - localhost

# Defines a file to scrape and an optional set of additional labels to apply to
# all streams defined by the files from __path__.
labels:
  # The path to load logs from. Can use glob patterns (e.g., /var/log/*.log).
  __path__: <string>

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
[relabeling phase](#relabel_config). Its value is set to the
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

* `__meta_kubernetes_node_name`: The name of the node object.
* `__meta_kubernetes_node_label_<labelname>`: Each label from the node object.
* `__meta_kubernetes_node_labelpresent_<labelname>`: `true` for each label from the node object.
* `__meta_kubernetes_node_annotation_<annotationname>`: Each annotation from the node object.
* `__meta_kubernetes_node_annotationpresent_<annotationname>`: `true` for each annotation from the node object.
* `__meta_kubernetes_node_address_<address_type>`: The first address for each node address type, if it exists.

In addition, the `instance` label for the node will be set to the node name
as retrieved from the API server.

#### `service`

The `service` role discovers a target for each service port of each service.
This is generally useful for blackbox monitoring of a service.
The address will be set to the Kubernetes DNS name of the service and respective
service port.

Available meta labels:

* `__meta_kubernetes_namespace`: The namespace of the service object.
* `__meta_kubernetes_service_annotation_<annotationname>`: Each annotation from the service object.
* `__meta_kubernetes_service_annotationpresent_<annotationname>`: "true" for each annotation of the service object.
* `__meta_kubernetes_service_cluster_ip`: The cluster IP address of the service. (Does not apply to services of type ExternalName)
* `__meta_kubernetes_service_external_name`: The DNS name of the service. (Applies to services of type ExternalName)
* `__meta_kubernetes_service_label_<labelname>`: Each label from the service object.
* `__meta_kubernetes_service_labelpresent_<labelname>`: `true` for each label of the service object.
* `__meta_kubernetes_service_name`: The name of the service object.
* `__meta_kubernetes_service_port_name`: Name of the service port for the target.
* `__meta_kubernetes_service_port_protocol`: Protocol of the service port for the target.

#### `pod`

The `pod` role discovers all pods and exposes their containers as targets. For
each declared port of a container, a single target is generated. If a container
has no specified ports, a port-free target per container is created for manually
adding a port via relabeling.

Available meta labels:

* `__meta_kubernetes_namespace`: The namespace of the pod object.
* `__meta_kubernetes_pod_name`: The name of the pod object.
* `__meta_kubernetes_pod_ip`: The pod IP of the pod object.
* `__meta_kubernetes_pod_label_<labelname>`: Each label from the pod object.
* `__meta_kubernetes_pod_labelpresent_<labelname>`: `true`for each label from the pod object.
* `__meta_kubernetes_pod_annotation_<annotationname>`: Each annotation from the pod object.
* `__meta_kubernetes_pod_annotationpresent_<annotationname>`: `true` for each annotation from the pod object.
* `__meta_kubernetes_pod_container_init`: `true` if the container is an [InitContainer](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/)
* `__meta_kubernetes_pod_container_name`: Name of the container the target address points to.
* `__meta_kubernetes_pod_container_port_name`: Name of the container port.
* `__meta_kubernetes_pod_container_port_number`: Number of the container port.
* `__meta_kubernetes_pod_container_port_protocol`: Protocol of the container port.
* `__meta_kubernetes_pod_ready`: Set to `true` or `false` for the pod's ready state.
* `__meta_kubernetes_pod_phase`: Set to `Pending`, `Running`, `Succeeded`, `Failed` or `Unknown`
  in the [lifecycle](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-phase).
* `__meta_kubernetes_pod_node_name`: The name of the node the pod is scheduled onto.
* `__meta_kubernetes_pod_host_ip`: The current host IP of the pod object.
* `__meta_kubernetes_pod_uid`: The UID of the pod object.
* `__meta_kubernetes_pod_controller_kind`: Object kind of the pod controller.
* `__meta_kubernetes_pod_controller_name`: Name of the pod controller.

#### `endpoints`

The `endpoints` role discovers targets from listed endpoints of a service. For
each endpoint address one target is discovered per port. If the endpoint is
backed by a pod, all additional container ports of the pod, not bound to an
endpoint port, are discovered as targets as well.

Available meta labels:

* `__meta_kubernetes_namespace`: The namespace of the endpoints object.
* `__meta_kubernetes_endpoints_name`: The names of the endpoints object.
* For all targets discovered directly from the endpoints list (those not additionally inferred
  from underlying pods), the following labels are attached:
  * `__meta_kubernetes_endpoint_hostname`: Hostname of the endpoint.
  * `__meta_kubernetes_endpoint_node_name`: Name of the node hosting the endpoint.
  * `__meta_kubernetes_endpoint_ready`: Set to `true` or `false` for the endpoint's ready state.
  * `__meta_kubernetes_endpoint_port_name`: Name of the endpoint port.
  * `__meta_kubernetes_endpoint_port_protocol`: Protocol of the endpoint port.
  * `__meta_kubernetes_endpoint_address_target_kind`: Kind of the endpoint address target.
  * `__meta_kubernetes_endpoint_address_target_name`: Name of the endpoint address target.
* If the endpoints belong to a service, all labels of the `role: service` discovery are attached.
* For all targets backed by a pod, all labels of the `role: pod` discovery are attached.

#### `ingress`

The `ingress` role discovers a target for each path of each ingress.
This is generally useful for blackbox monitoring of an ingress.
The address will be set to the host specified in the ingress spec.

Available meta labels:

* `__meta_kubernetes_namespace`: The namespace of the ingress object.
* `__meta_kubernetes_ingress_name`: The name of the ingress object.
* `__meta_kubernetes_ingress_label_<labelname>`: Each label from the ingress object.
* `__meta_kubernetes_ingress_labelpresent_<labelname>`: `true` for each label from the ingress object.
* `__meta_kubernetes_ingress_annotation_<annotationname>`: Each annotation from the ingress object.
* `__meta_kubernetes_ingress_annotationpresent_<annotationname>`: `true` for each annotation from the ingress object.
* `__meta_kubernetes_ingress_scheme`: Protocol scheme of ingress, `https` if TLS
  config is set. Defaults to `http`.
* `__meta_kubernetes_ingress_path`: Path from ingress spec. Defaults to `/`.

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
```

Where `<role>` must be `endpoints`, `service`, `pod`, `node`, or
`ingress`.

See
[this example Prometheus configuration file](https://github.com/prometheus/prometheus/blob/master/documentation/examples/prometheus-kubernetes.yml)
for a detailed example of configuring Prometheus for Kubernetes.

You may wish to check out the 3rd party
[Prometheus Operator](https://github.com/coreos/prometheus-operator),
which automates the Prometheus setup on top of Kubernetes.

## target_config

The `target_config` block controls the behavior of reading files from discovered
targets.

```yaml
# Period to resync directories being watched and files being tailed to discover
# new ones or stop watching removed ones.
sync_period: "10s"
```

## Example Docker Config

It's fairly difficult to tail docker files on a standalone machine because they are in different locations for every OS.  We recommend the [docker logging driver](../../../cmd/docker-driver/README.md) for local docker installs or docker-compose.

If running in a kubernetes environment, you should look at the defined configs which are in [helm](../../../production/helm/promtail/templates/configmap.yaml) and [jsonnet](../../../production/ksonnet/promtail/scrape_config.libsonnet), these leverage the prometheus service discovery libraries (and give promtail it's name) for automatically finding and tailing pods.  The jsonnet config explains with comments what each section is for.


## Example Static Config

While promtail may have been named for the prometheus service discovery code, that same code works very well for tailing logs without containers or container environments directly on virtual machines or bare metal.

```yaml
server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /var/log/positions.yaml ①

client:
  url: http://ip_or_hostname_where_Loki_run:3100/loki/api/v1/push

scrape_configs:
 - job_name: system
   pipeline_stages:
   static_configs:
   - targets:
      - localhost
     labels:
      job: varlogs  ②
      host: yourhost ③  
      __path__: /var/log/*.log  ④
```
① This location needs to be writeable by promtail.  
② A `job` label is fairly standard in prometheus and useful for linking metrics and logs.   
③ A `host` label will help identify logs from this machine vs others.  
④ The path matching uses [@bmatcuk's glob library](https://github.com/bmatcuk/doublestar)  


## Example Journal Config

For reading a systemd journal.

```yaml
server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml

clients:
  - url: http://ip_or_hostname_where_loki_runns:3100/loki/api/v1/push

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

For receiving syslogs in promtail over tcp.

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
