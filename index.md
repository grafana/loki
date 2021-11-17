## Welcome to Loki Operator 

This is the Kubernetes Operator for Loki provided by the Red Hat OpenShift engineering team. This is currently a work in progress and is subject to large scale changes that will break any dependencies. Do not use this in any production environment.

### Installation of Storage Size Calculator on OpenShift

* Storage size calculator works out of the box on OpenShift. For non-openshift distributions you will need to create services like prometheus, serviceMonitor, scrape configuration for log-file-metric exporter, promsecret to access the custom prometheus URL, token.
* The step-by-step guide on how to install [storage size calculator](https://github.com/ViaQ/loki-operator/blob/master/docs/storage_size_calculator.md) on OpenShift is available.
* Also, there is a step-by-step guide on how to [contribute](https://github.com/ViaQ/loki-operator/blob/master/docs/storage_size_calculator.md#contribution) to this along with local development and testing procedure.
* There is also a [basic troubleshooting guide](https://github.com/ViaQ/loki-operator/blob/master/docs/storage_size_calculator.md#troubleshooting) if you run into some common problems.
