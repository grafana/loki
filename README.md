<p align="center"><img src="docs/sources/logo_and_name.png" alt="Loki Logo"></p>

<a href="https://drone.grafana.net/grafana/loki"><img src="https://drone.grafana.net/api/badges/grafana/loki/status.svg" alt="Drone CI" /></a>
<a href="https://goreportcard.com/report/github.com/grafana/loki"><img src="https://goreportcard.com/badge/github.com/grafana/loki" alt="Go Report Card" /></a>
<a href="https://slack.grafana.com/"><img src="https://img.shields.io/badge/join%20slack-%23loki-brightgreen.svg" alt="Slack" /></a>
[![Fuzzing Status](https://oss-fuzz-build-logs.storage.googleapis.com/badges/loki.svg)](https://bugs.chromium.org/p/oss-fuzz/issues/list?sort=-opened&can=1&q=proj:loki)

# Loki: like Prometheus, but for logs.

Loki is a horizontally-scalable, highly-available, multi-tenant log aggregation system inspired by [Prometheus](https://prometheus.io/).
It is designed to be very cost effective and easy to operate.
It does not index the contents of the logs, but rather a set of labels for each log stream.

Compared to other log aggregation systems, Loki:

- does not do full text indexing on logs. By storing compressed, unstructured logs and only indexing metadata, Loki is simpler to operate and cheaper to run.
- indexes and groups log streams using the same labels you’re already using with Prometheus, enabling you to seamlessly switch between metrics and logs using the same labels that you’re already using with Prometheus.
- is an especially good fit for storing [Kubernetes](https://kubernetes.io/) Pod logs. Metadata such as Pod labels is automatically scraped and indexed.
- has native support in Grafana (needs Grafana v6.0).

A Loki-based logging stack consists of 3 components:

- [Alloy](https://github.com/grafana/alloy) is agent, responsible for gathering logs and sending them to Loki.
- [Loki](https://github.com/grafana/loki) is the main service, responsible for storing logs and processing queries.
- [Grafana](https://github.com/grafana/grafana) for querying and displaying the logs.

**Note that Alloy replaced Promtail in the stack, because Promtail is considered to be feature complete, and future development for logs collection will be in [Grafana Alloy](https://github.com/grafana/alloy).**

Loki is like Prometheus, but for logs: we prefer a multidimensional label-based approach to indexing, and want a single-binary, easy to operate system with no dependencies.
Loki differs from Prometheus by focusing on logs instead of metrics, and delivering logs via push, instead of pull.

## Getting started

* [Installing Loki](https://grafana.com/docs/loki/latest/installation/)
* [Installing Alloy](https://grafana.com/docs/loki/latest/send-data/alloy/)
* [Getting Started](https://grafana.com/docs/loki/latest/get-started/)

## Upgrading

* [Upgrading Loki](https://grafana.com/docs/loki/latest/upgrading/)

## Documentation

* [Latest release](https://grafana.com/docs/loki/latest/)
* [Upcoming release](https://grafana.com/docs/loki/next/), at the tip of the main branch

Commonly used sections:

- [API documentation](https://grafana.com/docs/loki/latest/api/) for getting logs into Loki.
- [Labels](https://grafana.com/docs/loki/latest/getting-started/labels/)
- [Operations](https://grafana.com/docs/loki/latest/operations/)
- [Promtail](https://grafana.com/docs/loki/latest/clients/promtail/) is an agent which tails log files and pushes them to Loki.
- [Pipelines](https://grafana.com/docs/loki/latest/clients/promtail/pipelines/) details the log processing pipeline.
- [Docker Driver Client](https://grafana.com/docs/loki/latest/clients/docker-driver/) is a Docker plugin to send logs directly to Loki from Docker containers.
- [LogCLI](https://grafana.com/docs/loki/latest/query/logcli/) provides a command-line interface for querying logs.
- [Loki Canary](https://grafana.com/docs/loki/latest/operations/loki-canary/) monitors your Loki installation for missing logs.
- [Troubleshooting](https://grafana.com/docs/loki/latest/operations/troubleshooting/) presents help dealing with error messages.
- [Loki in Grafana](https://grafana.com/docs/loki/latest/operations/grafana/) describes how to set up a Loki datasource in Grafana.

## Getting Help

If you have any questions or feedback regarding Loki:

- Search existing thread in the Grafana Labs community forum for Loki: [https://community.grafana.com](https://community.grafana.com/c/grafana-loki/)
- Ask a question on the Loki Slack channel. To invite yourself to the Grafana Slack, visit [https://slack.grafana.com/](https://slack.grafana.com/) and join the #loki channel.
- [File an issue](https://github.com/grafana/loki/issues/new) for bugs, issues and feature suggestions.
- Send an email to [lokiproject@googlegroups.com](mailto:lokiproject@googlegroups.com), or use the [web interface](https://groups.google.com/forum/#!forum/lokiproject).
- UI issues should be filed directly in [Grafana](https://github.com/grafana/grafana/issues/new).

Your feedback is always welcome.

## Further Reading

- The original [design doc](https://docs.google.com/document/d/11tjK_lvp1-SVsFZjgOTr1vV3-q6vBAsZYIQ5ZeYBkyM/view) for Loki is a good source for discussion of the motivation and design decisions.
- Callum Styan's March 2019 DevOpsDays Vancouver talk "[Grafana Loki: Log Aggregation for Incident Investigations][devopsdays19-talk]".
- Grafana Labs blog post "[How We Designed Loki to Work Easily Both as Microservices and as Monoliths][architecture-blog]".
- Tom Wilkie's early-2019 CNCF Paris/FOSDEM talk "[Grafana Loki: like Prometheus, but for logs][fosdem19-talk]" ([slides][fosdem19-slides], [video][fosdem19-video]).
- David Kaltschmidt's KubeCon 2018 talk "[On the OSS Path to Full Observability with Grafana][kccna18-event]" ([slides][kccna18-slides], [video][kccna18-video]) on how Loki fits into a cloud-native environment.
- Goutham Veeramachaneni's blog post "[Loki: Prometheus-inspired, open source logging for cloud natives](https://grafana.com/blog/2018/12/12/loki-prometheus-inspired-open-source-logging-for-cloud-natives/)" on details of the Loki architecture.
- David Kaltschmidt's blog post "[Closer look at Grafana's user interface for Loki](https://grafana.com/blog/2019/01/02/closer-look-at-grafanas-user-interface-for-loki/)" on the ideas that went into the logging user interface.

[devopsdays19-talk]: https://grafana.com/blog/2019/05/06/how-loki-correlates-metrics-and-logs--and-saves-you-money/
[architecture-blog]: https://grafana.com/blog/2019/04/15/how-we-designed-loki-to-work-easily-both-as-microservices-and-as-monoliths/
[fosdem19-talk]: https://fosdem.org/2019/schedule/event/loki_prometheus_for_logs/
[fosdem19-slides]: https://speakerdeck.com/grafana/grafana-loki-like-prometheus-but-for-logs
[fosdem19-video]: https://mirror.as35701.net/video.fosdem.org/2019/UB2.252A/loki_prometheus_for_logs.mp4
[kccna18-event]: https://kccna18.sched.com/event/GrXC/on-the-oss-path-to-full-observability-with-grafana-david-kaltschmidt-grafana-labs
[kccna18-slides]: https://speakerdeck.com/davkal/on-the-path-to-full-observability-with-oss-and-launch-of-loki
[kccna18-video]: https://www.youtube.com/watch?v=U7C5SpRtK74&list=PLj6h78yzYM2PZf9eA7bhWnIh_mK1vyOfU&index=346

## Contributing

Refer to [CONTRIBUTING.md](CONTRIBUTING.md)

### Building from source

Loki can be run in a single host, no-dependencies mode using the following commands.

You need an up-to-date version of [Go](https://go.dev/), we recommend using the version found in our [Makefile](https://github.com/grafana/loki/blob/main/Makefile)

```bash
# Checkout source code
$ git clone https://github.com/grafana/loki
$ cd loki

# Build binary
$ go build ./cmd/loki

# Run executable
$ ./loki -config.file=./cmd/loki/loki-local-config.yaml
```

Alternatively, on Unix systems you can use `make` to build the binary, which adds additional arguments to the `go build` command.

```bash
# Build binary
$ make loki

# Run executable
$ ./cmd/loki/loki -config.file=./cmd/loki/loki-local-config.yaml
```

To build Promtail on non-Linux platforms, use the following command:

```bash
$ go build ./clients/cmd/promtail
```

On Linux, Promtail requires the systemd headers to be installed if
Journal support is enabled.
To enable Journal support the go build tag flag `promtail_journal_enabled` should be passed

With Journal support on Ubuntu, run with the following commands:

```bash
$ sudo apt install -y libsystemd-dev
$ go build --tags=promtail_journal_enabled ./clients/cmd/promtail
```

With Journal support on CentOS, run with the following commands:

```bash
$ sudo yum install -y systemd-devel
$ go build --tags=promtail_journal_enabled ./clients/cmd/promtail
```

Otherwise, to build Promtail without Journal support, run `go build`
with CGO disabled:

```bash
$ CGO_ENABLED=0 go build ./clients/cmd/promtail
```

## Adopters

Please see [ADOPTERS.md](ADOPTERS.md) for some of the organizations using Loki today.
If you would like to add your organization to the list, please open a PR to add it to the list.

## License

Grafana Loki is distributed under [AGPL-3.0-only](LICENSE). For Apache-2.0 exceptions, see [LICENSING.md](LICENSING.md).
