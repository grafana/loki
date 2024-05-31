---
title: Deploy the Helm chart localy with Kind
menuTitle: Deploy the Helm chart localy with Kind
description: Installing Loki in microservice (distributed) with Kind for local development and testing purposes.
aliases:
weight: 300
keywords: 
---


# Deploy the Helm chart localy with Kind

This guide walks you through testing the Grafana Loki Helm chart localy in microservice mode using [Kind](https://kind.sigs.k8s.io/), a tool for running local Kubernetes clusters using Docker container nodes.

## Prerequisites

To run through this tutorial, you need the following tools installed:
- [Docker](https://docs.docker.com/get-docker/)
- [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/)
- [Helm](https://helm.sh/docs/intro/install/)
  
## Steps

1. Create a Kind cluster config. Save the following configuration to a file named `kind-config.yaml`:

    ```yaml
    kind: Cluster
    apiVersion: kind.x-k8s.io/v1alpha4
    nodes:
    - role: control-plane
    - role: worker
    - role: worker
    ```

2. Create a Kind cluster using the configuration file:

    ```bash
    kind create cluster --config kind-config.yaml
    ```

3. Add the Grafana Helm chart repository to Helm:

    ```bash
    helm repo add grafana https://grafana.github.io/helm-charts
    ```

4. Update the chart repository:

    ```bash
    helm repo update
    ```

5. Create a `values.yaml` file to configure the Loki installation. The example below illustrates how to deploy Loki in test mode using MinIO as storage:

    ```yaml
        loki:
          schemaConfig:
            configs:
              - from: 2024-04-01
                store: tsdb
                object_store: s3
                schema: v13
                index:
                  prefix: loki_index_
                  period: 24h
          ingester:
            chunk_encoding: snappy
          tracing:
            enabled: true
          querier:
            # Default is 4, if you have enough memory and CPU you can increase, reduce if OOMing
            max_concurrent: 4

        #gateway:
        #  ingress:
        #    enabled: true
        #    hosts:
        #      - host: FIXME
        #        paths:
        #          - path: /
        #            pathType: Prefix

        deploymentMode: Distributed

        ingester:
          replicas: 3
        querier:
          replicas: 3
          maxUnavailable: 2
        queryFrontend:
          replicas: 2
          maxUnavailable: 1
        queryScheduler:
          replicas: 2
        distributor:
          replicas: 3
          maxUnavailable: 2
        compactor:
          replicas: 1
        indexGateway:
          replicas: 2
          maxUnavailable: 1

        bloomCompactor:
          replicas: 0
        bloomGateway:
          replicas: 0

        # Enable minio for storage
        minio:
          enabled: true

        # Zero out replica counts of other deployment modes
        backend:
          replicas: 0
        read:
          replicas: 0
        write:
          replicas: 0

        singleBinary:
          replicas: 0 
    ```

6. Install or upgrade the Loki deployment.
     - To install:
        ```bash
       helm install --values values.yaml loki grafana/loki
       ```
    - To upgrade:
       ```bash
       helm upgrade --values values.yaml loki grafana/loki
       ```

7. Verify that Loki is running:

    ```bash
    kubectl get pods
    ```
    The output should an output similar to the following:

    ```bash

      loki-canary-8thrx                      1/1     Running   0          167m
      loki-canary-h965l                      1/1     Running   0          167m
      loki-canary-th8kb                      1/1     Running   0          167m
      loki-chunks-cache-0                    0/2     Pending   0          167m
      loki-compactor-0                       1/1     Running   0          167m
      loki-compactor-1                       1/1     Running   0          167m
      loki-distributor-7c9bb8f4dd-bcwc5      1/1     Running   0          167m
      loki-distributor-7c9bb8f4dd-jh9h8      1/1     Running   0          167m
      loki-distributor-7c9bb8f4dd-np5dw      1/1     Running   0          167m
      loki-gateway-77bc447887-qgc56          1/1     Running   0          167m
      loki-index-gateway-0                   1/1     Running   0          167m
      loki-index-gateway-1                   1/1     Running   0          166m
      loki-ingester-zone-a-0                 1/1     Running   0          167m
      loki-ingester-zone-b-0                 1/1     Running   0          167m
      loki-ingester-zone-c-0                 1/1     Running   0          167m
      loki-minio-0                           1/1     Running   0          167m
      loki-querier-bb8695c6d-bv9x2           1/1     Running   0          167m
      loki-querier-bb8695c6d-bz2rw           1/1     Running   0          167m
      loki-querier-bb8695c6d-z9qf8           1/1     Running   0          167m
      loki-query-frontend-6659566b49-528j5   1/1     Running   0          167m
      loki-query-frontend-6659566b49-84jtx   1/1     Running   0          167m
      loki-query-frontend-6659566b49-9wfr7   1/1     Running   0          167m
      loki-query-scheduler-f6dc4b949-fknfk   1/1     Running   0          167m
      loki-query-scheduler-f6dc4b949-h4nwh   1/1     Running   0          167m
      loki-query-scheduler-f6dc4b949-scfwp   1/1     Running   0          167m
      loki-results-cache-0                   2/2     Running   0          167m

    ```

