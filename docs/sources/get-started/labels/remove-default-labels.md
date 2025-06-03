---
title: Remove default OpenTelemetry labels
menuTitle: Remove default labels
description: Describes how to modify your Alloy or OpenTelemetry Collector configuration to demote default index labels to structured metadata.
weight: 400
---

# Remove default OpenTelemetry labels

When Grafana Loki first started supporting OpenTelemetry, we selected a set of [default resource attributes to promote to labels](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/#default-labels-for-opentelemetry). As we've continued to learn more about how our customers are using OpenTelemetry, we realized that some of our choices had higher [cardinality](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/cardinality/) than we'd anticipated. Because of their high cardinality, we no longer recommend the following as default labels:

- `k8s.pod.name`
- `service.instance.id`

Because removing these resource attributes from labels would be a breaking change for existing users, they have not yet been deprecated. If you are a new user of Grafana Loki, we recommend that you modify your Grafana Alloy or OpenTelemetry Collector configuration to convert these resource attributes from index labels to structured metadata.

## Alloy configuration file example

If you are using [Grafana Alloy](https://grafana.com/docs/alloy/latest/) to collect logs, the following example shows how to update your `alloy.config` file to demote `k8s.pod.name` from an index label to structured metadata.

```alloy
// discovery.kubernetes allows you to find scrape targets from Kubernetes resources.
// It watches cluster state and ensures targets are continually synced with what is currently running in your cluster.
discovery.kubernetes "pod" {
  role = "pod"
}

// discovery.relabel rewrites the label set of the input targets by applying one or more relabeling rules.
// If no rules are defined, then the input targets are exported as-is.
discovery.relabel "pod_logs" {
  targets = discovery.kubernetes.pod.targets

  // Label creation - "namespace" field from "__meta_kubernetes_namespace"
  rule {
    source_labels = ["__meta_kubernetes_namespace"]
    action = "replace"
    target_label = "namespace"
  }

  // Label creation - "pod" field from "__meta_kubernetes_pod_name"
  //rule {
  //  source_labels = ["__meta_kubernetes_pod_name"]
  //  action = "replace"
  //  target_label = "pod"
  //}


  // Label creation - "container" field from "__meta_kubernetes_pod_container_name"
  rule {
    source_labels = ["__meta_kubernetes_pod_container_name"]
    action = "replace"
    target_label = "container"
  }

  // Label creation -  "app" field from "__meta_kubernetes_pod_label_app_kubernetes_io_name"
  rule {
    source_labels = ["__meta_kubernetes_pod_label_app_kubernetes_io_name"]
    action = "replace"
    target_label = "app"
  }

  // Label creation -  "job" field from "__meta_kubernetes_namespace" and "__meta_kubernetes_pod_container_name"
  // Concatenate values __meta_kubernetes_namespace/__meta_kubernetes_pod_container_name
  rule {
    source_labels = ["__meta_kubernetes_namespace", "__meta_kubernetes_pod_container_name"]
    action = "replace"
    target_label = "job"
    separator = "/"
    replacement = "$1"
  }

  // Label creation - "container" field from "__meta_kubernetes_pod_uid" and "__meta_kubernetes_pod_container_name"
  // Concatenate values __meta_kubernetes_pod_uid/__meta_kubernetes_pod_container_name.log
  rule {
    source_labels = ["__meta_kubernetes_pod_uid", "__meta_kubernetes_pod_container_name"]
    action = "replace"
    target_label = "__path__"
    separator = "/"
    replacement = "/var/log/pods/*$1/*.log"
  }

  // Label creation -  "container_runtime" field from "__meta_kubernetes_pod_container_id"
  rule {
    source_labels = ["__meta_kubernetes_pod_container_id"]
    action = "replace"
    target_label = "container_runtime"
    regex = "^(\\S+):\\/\\/.+$"
    replacement = "$1"
  }
}

// loki.source.kubernetes tails logs from Kubernetes containers using the Kubernetes API.
loki.source.kubernetes "pod_logs" {
  targets    = discovery.relabel.pod_logs.output
  forward_to = [loki.process.pod_logs.receiver]
}

// loki.process receives log entries from other Loki components, applies one or more processing stages,
// and forwards the results to the list of receivers in the component's arguments.
loki.process "pod_logs" {
  stage.static_labels {
      values = {
        cluster = "<CLUSTER_NAME>",
      }
  }

  forward_to = [loki.write.<WRITE_COMPONENT_NAME>.receiver]
}
```

## Kubernetes Monitoring Helm chart configuration example

If you are using the [Kubernetes Monitoring Helm chart](https://grafana.com/docs/grafana-cloud/monitor-infrastructure/kubernetes-monitoring/) to collect Kubernetes logs to send to Loki, the following example shows how to update the `values.yaml` file for your Helm chart to demote `k8s.pod.name` and `service.instance.id` from index labels to structured metadata.

```yaml
# Enable pod log collection for the cluster. Will collect logs from all pods in both the meta and loki namespace. 

podLogs:
  enabled: true
  collector: alloy-singleton
  #List of attributes to use as index labels in oki
  labelsToKeep:
    - app
    - app_kubernetes_io_name
    - component
    - container
    - job
    - level
    - namespace
    - service_name
    - cluster
  gatherMethod: kubernetesApi
  namespaces:
    - meta
    - loki
  #This is the section that specifies to send k8s.pod.name and service.instance.id to structured metadata
  structuredMetadata:
    instance_id:
    pod:
  ```

## Loki configuration example

If you are running Loki in a Docker container or on-premises, here is an example of how to modify your Loki `values.yaml` file to demote `k8s.pod.name` and `service.instance.id` from index labels to structured metadata.

```yaml

distributor:
  otlp_config:
    # List of default otlp resource attributes to be picked as index labels - EDIT TO REMOVE k8s.pod.name AND service.instance.id FROM THE LIST
    # CLI flag: -distributor.otlp.default_resource_attributes_as_index_labels
      default_resource_attributes_as_index_labels: [service.name service.namespace deployment.environment deployment.environment.name cloud.region cloud.availability_zone k8s.cluster.name k8s.namespace.name k8s.container.name container.name k8s.replicaset.name k8s.deployment.name k8s.statefulset.name k8s.daemonset.name k8s.cronjob.name k8s.job.name]

```

Kubernetes example `values.yaml`:

```yaml
loki:
  distributor:
    otlp_config:
    # List of default otlp resource attributes to be picked as index labels - EDIT TO REMOVE k8s.pod.name AND service.instance.id FROM THE LIST
    # CLI flag: -distributor.otlp.default_resource_attributes_as_index_labels
      default_resource_attributes_as_index_labels: [service.name service.namespace deployment.environment deployment.environment.name cloud.region cloud.availability_zone k8s.cluster.name k8s.namespace.name k8s.container.name container.name k8s.replicaset.name k8s.deployment.name k8s.statefulset.name k8s.daemonset.name k8s.cronjob.name k8s.job.name]
```
