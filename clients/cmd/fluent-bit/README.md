# Fluent Bit output plugin

[Fluent Bit](https://fluentbit.io/) is a Fast and Lightweight Data Forwarder, it can be configured with the [Loki output plugin](https://fluentbit.io/documentation/0.12/output/) to ship logs to Loki. You can define which log files you want to collect using the [`Tail`](https://fluentbit.io/documentation/0.12/input/tail.html) or [`Stdin`](https://docs.fluentbit.io/manual/pipeline/inputs/standard-input) [input plugin](https://fluentbit.io/documentation/0.12/getting_started/input.html). Additionally Fluent Bit supports multiple `Filter` and `Parser` plugins (`Kubernetes`, `JSON`, etc..) to structure and alter log lines.

This plugin is implemented with [Fluent Bit's Go plugin](https://github.com/fluent/fluent-bit-go) interface. It pushes logs to Loki using a GRPC connection.

> **Warning**
> `syslog` and `systemd` input plugins have not been tested yet. Feedback appreciated, file [an issue](https://github.com/grafana/loki/issues/new?template=bug_report.md) if you encounter any misbehaviors.

## Building

**Prerequisites**

* Go 1.17+
* gcc (for cgo)

To [build](https://docs.fluentbit.io/manual/development/golang-output-plugins#build-a-go-plugin) the output plugin library file `out_grafana_loki.so`, in the root directory of Loki source code, you can use:

```bash
$ make fluent-bit-plugin
```

You can also build the Docker image with the plugin pre-installed using:

```bash
$ make fluent-bit-image
```

## Running

```bash
$ fluent-bit -e out_grafana_loki.so -c /etc/fluent-bit.conf
```

**Testing**

Issue the following command to send `/var/log` logs to your `http://localhost:3100/loki/api/` Loki instance for testing:

```bash
$ make fluent-bit-test
```

You can easily override the address by setting  the `$LOKI_URL` environment variable.