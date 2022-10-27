---
title: Install the Helm Chart
menuTitle: Install Loki
description: Install Loki in scalable mode.
aliases:
  - /docs/writers-toolkit/latest/templates/task-template
weight: 100
keywords:
  - keyword
  - key
  - word
---

# Install the Helm Chart
<!-- vale Grafana.Quotes = NO -->
<!-- vale Grafana.Quotes = YES -->

This Helm Chart installation runs the Grafana Loki cluster within a Kubernetes cluster.

If object storge is configured, this chart configures Loki to run `read` and `write` targets in a [scalable](../../fundamentals/architecture/deployment-modes/#simple-scalable-deployment-mode), highly available architecture (3 replicas of each) designed to work with AWS S3 object storage. It will also configure meta-monitoring of metrics and logs.

It is not possible to run the scalable mode with the `fulesystem` storage.

**Before you begin:**

- Helm 3 or above. See [Installing Helm](https://helm.sh/docs/intro/install/).
- A running Kubernetes cluster.
- A Prometheus operator installation in case meta-monitoring should be used.

**To deploy Loki in scalable mode:**

1. Add [Grafana's chart repository](https://github.com/grafana/helm-charts) to Helm:

    ```bash
    helm repo add grafana https://grafana.github.io/helm-charts
    ```

1. Update the chart repository:

    ```bash
    helm repo update
    ```

1. Configure the object storage:

    - Create the configuration file `values.yaml`:

      ```yaml
      storage:
        bucketNames:
          chunks: chunks
          ruler: ruler
          admin: admin
        type: s3
        s3:
          s3: <WHAT SHOULD GO HERE?>
          endpoint: <endpoint>
          region: <AWS region>
          secretAccessKey: <AWS secret access key>
          accessKeyId: <AWS access key ID>
          s3ForcePathStyle: false
          insecure: false
      ```

      Consult the [Reference](../reference) for configuring otehr storage providers.

    - Define the AWS S3 credentials in the file.

1. Upgrade the Loki deployment with this command.

   ```bash
   helm upgrade --values values.yaml loki grafana/loki
   ```
