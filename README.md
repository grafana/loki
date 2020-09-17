<p align="center"><img src="docs/sources/logo_and_name.png" alt="Loki Logo"></p>

<a href="https://drone.grafana.net/grafana/loki"><img src="https://drone.grafana.net/api/badges/grafana/loki/status.svg" alt="Drone CI" /></a>
<a href="https://circleci.com/gh/grafana/loki/tree/master"><img src="https://circleci.com/gh/grafana/loki.svg?style=shield&circle-token=618193e5787b2951c1ea3352ad5f254f4f52313d" alt="CircleCI" /></a>
<a href="https://goreportcard.com/report/github.com/grafana/loki"><img src="https://goreportcard.com/badge/github.com/grafana/loki" alt="Go Report Card" /></a>
<a href="https://slack.grafana.com/"><img src="https://img.shields.io/badge/join%20slack-%23loki-brightgreen.svg" alt="Slack" /></a>

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

- `promtail` is the agent, responsible for gathering logs and sending them to Loki.
- `loki` is the main server, responsible for storing logs and processing queries.
- [Grafana](https://github.com/grafana/grafana) for querying and displaying the logs.

Loki is like Prometheus, but for logs: we prefer a multidimensional label-based approach to indexing, and want a single-binary, easy to operate system with no dependencies.
Loki differs from Prometheus by focusing on logs instead of metrics, and delivering logs via push, instead of pull.

## Getting started

* [Installing Loki](https://grafana.com/docs/loki/latest/installation/)
* [Installing Promtail](https://grafana.com/docs/loki/latest/clients/promtail/installation/)
* [Getting Started Guide](https://grafana.com/docs/loki/latest/getting-started/)

## Upgrading

* [Upgrading Loki](https://grafana.com/docs/loki/latest/operations/upgrade/)

### Documentation

* [master](https://grafana.com/docs/loki/latest/)
* [v1.5.0](https://github.com/grafana/loki/tree/v1.5.0/docs/README.md)
* [v1.4.1](https://github.com/grafana/loki/tree/v1.4.1/docs/README.md)
* [v1.4.0](https://github.com/grafana/loki/tree/v1.4.0/docs/README.md)
* [v1.3.0](https://github.com/grafana/loki/tree/v1.3.0/docs/README.md)
* [v1.2.0](https://github.com/grafana/loki/tree/v1.2.0/docs/README.md)
* [v1.1.0](https://github.com/grafana/loki/tree/v1.1.0/docs/README.md)
* [v1.0.0](https://github.com/grafana/loki/tree/v1.0.0/docs/README.md)

Commonly used sections:

- [API documentation](https://grafana.com/docs/loki/latest/api/) for alternative ways of getting logs into Loki.
- [Labels](https://grafana.com/docs/loki/latest/getting-started/labels/)
- [Operations](https://grafana.com/docs/loki/latest/operations/) for important aspects of running Loki.
- [Promtail](https://grafana.com/docs/loki/latest/clients/promtail/) is an agent which can tail your log files and push them to Loki.
- [Pipelines](https://grafana.com/docs/loki/latest/clients/promtail/pipelines/) for detailed log processing pipeline documentation
- [Docker Logging Driver](https://grafana.com/docs/loki/latest/clients/docker-driver/) is a docker plugin to send logs directly to Loki from Docker containers.
- [LogCLI](https://grafana.com/docs/loki/latest/getting-started/logcli/) on how to query your logs without Grafana.
- [Loki Canary](https://grafana.com/docs/loki/latest/operations/loki-canary/) for monitoring your Loki installation for missing logs.
- [Troubleshooting](https://grafana.com/docs/loki/latest/getting-started/troubleshooting/) for help around frequent error messages.
- [Loki in Grafana](https://grafana.com/docs/loki/latest/getting-started/grafana/) for how to set up a Loki datasource in Grafana and query your logs.

## Getting Help

If you have any questions or feedback regarding Loki:

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

[devopsdays19-talk]: https://grafana.com/blog/2019/05/06/how-loki-correlates-metrics-and-logs-and-saves-you-money/
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

You need `go`, we recommend using the version found in [our build Dockerfile](https://github.com/grafana/loki/blob/master/loki-build-image/Dockerfile)

```bash

$ go get github.com/grafana/loki
$ cd $GOPATH/src/github.com/grafana/loki # GOPATH is $HOME/go by default.

$ go build ./cmd/loki
$ ./loki -config.file=./cmd/loki/loki-local-config.yaml
...
```

To build Promtail on non-Linux platforms, use the following command:

```bash
$ go build ./cmd/promtail
```

On Linux, Promtail requires the systemd headers to be installed for
Journal support.

With Journal support on Ubuntu, run with the following commands:

```bash
$ sudo apt install -y libsystemd-dev
$ go build ./cmd/promtail
```

With Journal support on CentOS, run with the following commands:

```bash
$ sudo yum install -y systemd-devel
$ go build ./cmd/promtail
```

Otherwise, to build Promtail without Journal support, run `go build`
with CGO disabled:

```bash
$ CGO_ENABLED=0 go build ./cmd/promtail
```

## License

Apache License 2.0, see [LICENSE](LICENSE).
