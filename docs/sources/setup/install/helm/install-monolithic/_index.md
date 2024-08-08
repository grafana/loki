---
title: Install the monolithic Helm chart
menuTitle: Install monolithic Loki
description: Installing Loki in monolithic, single binary mode using the Helm chart.
aliases:
 - ../../../installation/helm/monolithic
 - ../../../installation/helm/install-monolithic/
weight: 100
---

# Install the monolithic Helm chart

This Helm Chart installation runs the Grafana Loki *single binary* within a Kubernetes cluster.

If you set the `singleBinary.replicas` value to 1 and set the deployment mode to `SingleBinary`, this chart configures Loki to run the `all` target in a [monolithic mode](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#monolithic-mode), designed to work with a filesystem storage. It will also configure meta-monitoring of metrics and logs.
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
      deploymentMode: SingleBinary
      loki:
        commonConfig:
          replication_factor: 1
        storage:
          type: 'filesystem'
        schemaConfig:
          configs:
          - from: "2024-01-01"
            store: tsdb
            index:
              prefix: loki_index_
              period: 24h
            object_store: filesystem # we're storing on filesystem so there's no real persistence here.
            schema: v13
      singleBinary:
        replicas: 1
      read:
        replicas: 0
      backend:
        replicas: 0
      write:
        replicas: 0
      ```

    - If running Loki with a replication factor greater than 1, set the desired number replicas and provide object storage credentials:

      ```yaml
      loki:
        commonConfig:
          replication_factor: 3
        schemaConfig:
          configs:
          - from: "2024-01-01"
            store: tsdb
            index:
              prefix: loki_index_
              period: 24h
            object_store: s3
            schema: v13
        storage:
          type: 's3'
          bucketNames:
            chunks: loki-chunks
            ruler: loki-ruler
            admin: loki-admin
          s3:
            endpoint: foo.aws.com
            region: <AWS region>
            secretAccessKey: supersecret
            accessKeyId: secret
            s3ForcePathStyle: false
            insecure: false
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
