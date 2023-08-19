<p align="center"><img src="../docs/sources/logo.png" alt="Loki Logo"></p>

# Loki Operator

This is the Kubernetes Operator for [Loki](https://grafana.com/docs/loki/latest/)
provided by the Grafana Loki SIG operator.

### Hacking on Loki Operator on kind or OpenShift

* If you want to contribute to this repository, you might need a step-by-step guide on how to start [hacking on Loki-operator with kind](https://github.com/grafana/loki/blob/main/operator/docs/operator/hack_loki_operator.md#hacking-using-kind).
* Also, there is a step-by-step guide on how to test Loki-operator on [OpenShift](https://github.com/grafana/loki/blob/main/operator/docs/operator/hack_loki_operator.md#hacking-on-openshift).
* There is also a [basic troubleshooting guide](https://github.com/grafana/loki/blob/main/operator/docs/operator/hack_loki_operator.md#basic-troubleshooting) if you run into some common problems.
* There is also a [document](https://github.com/grafana/loki/blob/main/operator/docs/operator/hack_operator_make_run.md) which demonstrates how to use Loki Operator for development and testing locally without deploying the operator each time on Kind and OpenShift using the `make run` command.

### Sending Logs to Loki

#### Sending Logs Through the Gateway Component

* The [forwarding logs to LokiStack guide](https://github.com/grafana/loki/blob/main/operator/docs/user-guides/forwarding_logs_to_gateway.md) provides instructions for configuring forwarding clients to ship logs to Loki through the gateway component.
* This section details [how to connect to OpenShift Logging](https://github.com/grafana/loki/blob/main/operator/docs/user-guides/forwarding_logs_to_gateway.md#openshift-logging) installation to the gateway.
* This section details [how to connect a Promtail](https://github.com/grafana/loki/blob/main/operator/docs/user-guides/forwarding_logs_to_gateway.md#promtail) installation to the gateway.
* This section details [how to connect a Grafana Fluentd plugin](https://github.com/grafana/loki/blob/main/operator/docs/user-guides/forwarding_logs_to_gateway.md#fluentd) installation to the gateway.

#### Sending Logs Directly to the Distributor Component

* The [forwarding logs to LokiStack without LokiStack Gateway](https://github.com/grafana/loki/blob/main/operator/docs/user-guides/forwarding_logs_without_gateway.md) is used to send application, infrastructure, audit and network logs to the Loki Distributor as different tenants using Fluentd or Vector.
* The guide has a step-by-step guide to connect with OpenShift Logging or OpenShift Network for forwarding logs to LokiStack.

### Installation of Storage Size Calculator on OpenShift

* Storage size calculator works out of the box on OpenShift. For non-openshift distributions you will need to create services like prometheus, serviceMonitor, scrape configuration for log-file-metric exporter, promsecret to access the custom prometheus URL, token.
* The step-by-step guide on how to install [storage size calculator](https://github.com/grafana/loki/blob/main/operator/docs/operator/storage_size_calculator.md) on OpenShift is available.
* Also, there is a step-by-step guide on how to [contribute](https://github.com/grafana/loki/blob/main/operator/docs/operator/storage_size_calculator.md#contribution) to this along with local development and testing procedure.
* There is also a [basic troubleshooting guide](https://github.com/grafana/loki/blob/main/operator/docs/operator/storage_size_calculator.md#troubleshooting) if you run into some common problems.
