## Welcome to Loki Operator

This is the Kubernetes Operator for Loki provided by the Red Hat OpenShift engineering team. This is currently a work in progress and is subject to large scale changes that will break any dependencies. Do not use this in any production environment.

### Hacking on Loki Operator on kind or OpenShift

* If you want to contribute to this repository, you might need a step-by-step guide on how to start [hacking on Loki-operator with kind](https://github.com/ViaQ/loki-operator/blob/master/docs/hack_loki_operator.md#hacking-on-loki-operator-using-kind).
* Also, there is a step-by-step guide on how to test Loki-operator on [OpenShift](https://github.com/ViaQ/loki-operator/blob/master/docs/hack_loki_operator.md#hacking-on-loki-operator-on-openshift).
* There is also a [basic troubleshooting guide](https://github.com/ViaQ/loki-operator/blob/master/docs/hack_loki_operator.md#basic-troubleshooting-on-hacking-on-loki-operator) if you run into some common problems.
* There is also a [document](https://github.com/ViaQ/loki-operator/blob/master/docs/hack_operator_make_run.md) which demonstrates how to use Loki Operator for development and testing locally without deploying the operator each time on Kind and OpenShift using the `make run` command.

### Sending Logs to Loki through the Gateway Component

* The [forwarding logs to LokiStack guide](https://github.com/ViaQ/loki-operator/tree/master/docs/forwarding_logs_to_gateway.md) provides instructions for configuring forwarding clients to ship logs to Loki through the gateway component.
* This section details [how to connect a Promtail](https://github.com/ViaQ/loki-operator/tree/master/docs/forwarding_logs_to_gateway.md#promtail) installation to the gateway.
* This section details [how to connect a Grafana Fluentd plugin](https://github.com/ViaQ/loki-operator/tree/master/docs/forwarding_logs_to_gateway.md#fluentd) installation to the gateway.

### Installation of Storage Size Calculator on OpenShift

* Storage size calculator works out of the box on OpenShift. For non-openshift distributions you will need to create services like prometheus, serviceMonitor, scrape configuration for log-file-metric exporter, promsecret to access the custom prometheus URL, token.
* The step-by-step guide on how to install [storage size calculator](https://github.com/ViaQ/loki-operator/blob/master/docs/storage_size_calculator.md) on OpenShift is available.
* Also, there is a step-by-step guide on how to [contribute](https://github.com/ViaQ/loki-operator/blob/master/docs/storage_size_calculator.md#contribution) to this along with local development and testing procedure.
* There is also a [basic troubleshooting guide](https://github.com/ViaQ/loki-operator/blob/master/docs/storage_size_calculator.md#troubleshooting) if you run into some common problems.
