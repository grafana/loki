---
title: Install the monolithic Helm chart
menuTitle: Install monolithic Loki
description: Install Loki in monolithic, single binary mode.
aliases:
  - ./installation/helm/monolithic/
weight: 100
keywords: 
---

# Install the monolithic Helm chart

This Helm Chart installation runs the Grafana Loki *single binary* within a Kubernetes cluster.

If you set the `singleBinary.replicas` value to 1, this chart configures Loki to run the `all` target in a [monolithic mode]({{< relref "../../../../get-started/deployment-modes#monolithic-mode" >}}), designed to work with a filesystem storage. It will also configure meta-monitoring of metrics and logs.
If you set the `singleBinary.replicas` value to 2 or more, this chart configures Loki to run a *single binary* in a replicated, highly available mode.  When running replicas of a single binary, you must configure object storage.

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

1. Create the configuration file `values.yaml`:

    - If running a single replica of Loki, configure the `filesystem` storage:

      ```yaml
      loki:
        commonConfig:
          replication_factor: 1
        storage:
          type: 'filesystem'
      singleBinary:
        replicas: 1
      ```

    - If running Loki with a replication factor greater than 1, set the desired number replicas and provide object storage credentials:

      ```yaml
      loki:
        commonConfig:
          replication_factor: 3
        storage:
          type: 's3'
          s3:
            endpoint: foo.aws.com
            bucketnames: loki-chunks
            secret_access_key: supersecret
            access_key_id: secret
      singleBinary:
        replicas: 3
      ```

1. Deploy the Loki cluster using one of these commands.

    - Deploy with the defined configuration:

        ```bash
        helm install --values values.yaml loki grafana/loki
        ```

    - Deploy with the defined configuration in a custom Kubernetes cluster namespace:

        ```bash
        helm install --values values.yaml loki --namespace=loki grafana/loki
        ```
