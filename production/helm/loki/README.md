
# Loki: like Prometheus, but for logs.

Loki is a horizontally-scalable, highly-available, multi-tenant log aggregation system inspired by Prometheus. It is designed to be very cost effective and easy to operate. It does not index the contents of the logs, but rather a set of labels for each log stream.

## Introduction

This chart bootstraps a [Loki](https://grafana.com/oss/loki) statefulset on a [Kubernetes](http://kubernetes.io) cluster using the [Helm](https://helm.sh) package manager.

## Prerequisites

Kubernetes >= 1.10.0

Make sure you have Helm [installed](https://helm.sh/docs/using_helm/#installing-helm) and [deployed](https://helm.sh/docs/using_helm/#installing-tiller) to your cluster. Then add [Loki's chart repository](https://github.com/grafana/loki/tree/master/production/helm/loki) to Helm.

## Installing the Chart

To install the chart with the release name `my-release`:

```console
helm repo add loki https://grafana.github.io/loki/charts
helm install --name my-release loki/loki
```

The command deploys loki on the Kubernetes cluster in the default configuration. The [configuration](#configuration) section lists the parameters that can be configured during installation.

The command removes all the Kubernetes components associated with the chart and deletes the release.

## Configuration

The available configuration parameters can be found in the values.yaml of the chart.

Specify each parameter using the `--set key=value[,key=value]` argument to `helm install`. For example,

```console
helm install --name my-release loki/loki --set persistence.size=10Gi
```

Alternatively, a YAML file that specifies the values for the above parameters can be provided while installing the chart. For example,

```console
helm install --name my-release loki/loki -f values.yaml
```

## Uninstalling the Chart

To uninstall/delete the `my-release` deployment:

```console
helm delete my-release
```