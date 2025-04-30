---
title: Deploy Loki meta-monitoring
menuTitle: Deploy Loki Meta Monitoring
description: Describes how to deploy Meta Monitoring for Loki using the Kubernetes Monitoring Helm chart.
weight: 100
---

# Deploy Loki meta-monitoring

The primary method for collecting and monitoring a Loki cluster is to use the [Kubernetes Monitoring Helm](https://github.com/grafana/k8s-monitoring-helm/) chart. This chart provides a comprehensive monitoring solution for Kubernetes clusters and includes direct integrations for monitoring the full LGTM (Loki, Grafana, Tempo, and Mimir) stack. This procedure will walk you through deploying the Kubernetes Monitoring Helm chart to monitor your Loki cluster.

{{< admonition type="note" >}}
We recommend running a production cluster of Loki in distributed mode using Kubernetes. This procedure assumes you have a running Kubernetes cluster and a running Loki deployment. There are other methods for deploying Loki, such as using Docker or VM installations. meta-monitoring is still possible when using these deployment methods but not covered in this procedure.
{{< /admonition >}}

## Prerequisites

- [kubectl](https://kubernetes.io/docs/reference/kubectl/)
- Helm 3 or above. See [Installing Helm](https://helm.sh/docs/intro/install/).
- A running Kubernetes cluster with a running Loki deployment.
- A Grafana Cloud account or a separate LGTM stack for monitoring.

## Preparing your environment

Before deploying the Kubernetes Monitoring Helm chart, you need to set up several components in your environment.

1. Add the Grafana Helm repository:

   ```bash
   helm repo add grafana https://grafana.github.io/helm-charts
   ```

1. Update your Helm repositories:

   ```bash
   helm repo update
   ```

1. Create a namespace for the monitoring stack:

   ```bash
   kubectl create namespace meta
   ```

### Authentication

The Kubernetes Monitoring Helm chart requires a Grafana Cloud account or a separate LGTM stack for monitoring. You will need to provide the necessary credentials to the Helm chart to authenticate with your Grafana Cloud account or LGTM stack. In this procedure, we will use Grafana Cloud as an example.

1. Create a new Cloud Access Policy in Grafana Cloud.
    1. Sign into [Grafana Cloud](https://grafana.com/auth/sign-in/).
    1. In the main menu, select **Security > Access Policies**.
    1. Click **Create access policy**.
    1. Give the policy a **Name** and select the following permissions:
       - Metrics: Write
       - Logs: Write
    1. Click **Create**.
    1. Click **Add Token**. Give the token a name and click **Create**.
   Save the token for later use.
1. Collect `url` and `username` for Prometheus and Loki.
   1. Navigate to the Grafana Cloud Portal **Overview** page.
   1. Click the **Details** button for your Prometheus instance.
        1. From the **Sending metrics using Grafana Alloy** section, collect the instance **username** and **url**.
        1. Navigate back to the **Overview** page.
   1. Click the **Details** button for your Loki instance.
        1. From the **Sending Logs to Grafana Cloud using Grafana Alloy** section, collect the instance **username** and **url**.
        2. Navigate back to the **Overview** page.

1. Create the Kubernetes Secrets with the collected credentials from Grafana Cloud.

   ```bash
   kubectl create secret generic metrics -n meta \
    --from-literal=username=<PROMETHEUS-USER> \
    --from-literal=password=<CLOUD-TOKEn>

   kubectl create secret generic logs -n meta \
    --from-literal=username=<LOKI-USER> \
    --from-literal=password=<CLOUD-TOKEN>
   ```

Note that the Kubernetes Monitoring Helm supports many different authentication methods based upon your requirements including:

- Bearer Tokens
- OAuth2
- SigV4
- External Secrets
  
For further information on how to configure these methods, see the [Kubernetes Monitoring Helm examples](https://github.com/grafana/k8s-monitoring-helm/tree/main/charts/k8s-monitoring/docs/examples/auth).

## Deploy the Kubernetes Monitoring Helm chart

Now that you have prepared your environment and collected the necessary credentials, you can deploy the Kubernetes Monitoring Helm chart to monitor your Loki cluster. To do this we need to copy a `values.yaml` file to our local machine and modify it to include the necessary configuration.

1. Download the `values.yaml` file from the Kubernetes Monitoring Helm chart repository:

   ```bash
   curl -O https://raw.githubusercontent.com/grafana/loki/main/production/meta-monitoring/values.yaml
   ```

1. Open the `values.yaml` file in a text editor of your choosing and add the Prometheus and Loki endpoints.

   ```yaml
   destinations:
     - name: prometheus
       type: prometheus
       url: https://<PROMETHEUS-ENDPOINT>/api/prom/push
       auth:
         type: basic
         usernameKey: username
         passwordKey: password
       secret:
           create: false
           name: metrics
           namespace: meta

     - name: loki
       type: loki
       url: https://<LOKI-ENDPOINT>/loki/api/v1/push
       auth:
         type: basic
         usernameKey: username
         passwordKey: password
       secret:
           create: false
           name: logs
           namespace: meta

   ```

1. (Optional) Update the cluster name to a human-readable name to identify your cluster in Grafana Cloud.

   ```yaml
   # Global Label to be added to all telemetry data. Should reflect a recognizable name for the cluster.
    cluster:
        name: loki-meta-monitoring-cluster
   ```

1. The default values file assumes that you have deployed Loki in the `loki` namespace and will deploy the Kubernetes monitoring stack in the `meta` namespace. If you have deployed Loki in a different namespace, you will need to update `namespaces` in the `values.yaml` file to match the namespace where Loki is deployed. Here is an example:

    ```yaml
    namespaces:
        - loki
    ```

    Note if you would like to collect from all available namespaces, you can remove the `namespaces` key from the `values.yaml` file.

1. Deploy the Kubernetes Monitoring Helm chart using the modified `values.yaml` file:

   ```bash
   helm install meta-loki grafana/k8s-monitoring \
    --namespace meta \
    -f values.yaml
   ```

1. Verify that the Kubernetes Monitoring Helm chart has been deployed successfully:

   ```bash
    kubectl get pods -n meta
    ```

    You should see a list of pods running in the `meta` namespace.

    ```console
    NAME                                           READY   STATUS    RESTARTS ...        
    meta-loki-alloy-singleton-6d7f8d8b86-sg4wx     2/2     Running   0        ...       
    meta-loki-kube-state-metrics-64bdcfcbd-5snqz   1/1     Running   0        ...       
    meta-loki-node-exporter-855l5                  1/1     Running   0        ...       
    meta-loki-node-exporter-b976b                  1/1     Running   0        ...       
    meta-loki-node-exporter-vsm4s                  1/1     Running   0        ...
    ```

## Next Steps

You have successfully deployed the Kubernetes Monitoring Helm chart to monitor your Loki cluster. You can now move onto the next step of deploying the Loki mixin to visualize the metrics and logs from your Loki cluster. For more information, see [Install the Loki Mixin](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/mixins).
