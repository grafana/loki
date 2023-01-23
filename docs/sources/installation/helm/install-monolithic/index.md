---
title: Install the Single Binary Helm Chart
menuTitle: Install single binary Loki
description: Install Loki in single binary mode.
aliases:
  - /docs/installation/helm/monolithic
weight: 100
keywords: []
---

# Install the Single Binary Helm Chart

This Helm Chart installation runs the Grafana Loki *single binary* within a Kubernetes cluster.

If the storage type is set to `filesystem`, this chart configures Loki to run the `all` target in a [monolithic mode]({{<relref "../../../fundamentals/architecture/deployment-modes#monolithic-mode">}}), designed to work with a filesystem storage. It will also configure meta-monitoring of metrics and logs.

It is not possible to install the single binary with a different storage type.

**Before you begin: Software Requirements**

- Helm 3 or above. See [Installing Helm](https://helm.sh/docs/intro/install/).
- A running Kubernetes cluster

**To deploy Loki in monolithic mode:**

1. Add [Grafana's chart repository](https://github.com/grafana/helm-charts) to Helm:

    ```bash
    helm repo add grafana https://grafana.github.io/helm-charts
    ```

1. Update the chart repository:

    ```bash
    helm repo update
    ```

1. Configure the `filesystem` storage:

    - Create the configuration file `values.yaml`:

      ```yaml
      loki:
        commonConfig:
          replication_factor: 1
        storage:
          type: 'filesystem'
      ```

1. Deploy the Loki cluster using one of these commands.

    - Deploy with the defined configuration:

        ```bash
        helm install --values values.yaml loki grafana/loki
        ```

    - Deploy with the defined configuration in a custom Kubernetes cluster namespace:

        ```bash
        helm install --values values.yaml loki --namespace=loki grafana/loki-simple-scalable
        ```
