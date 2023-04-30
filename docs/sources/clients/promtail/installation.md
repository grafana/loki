---
title: Installation
description: Install Promtail
---
# Installation

Promtail is distributed as a binary, in a Docker container,
or there is a Helm chart to install it in a Kubernetes cluster.

## Binary

Every Grafana Loki release includes binaries for Promtail which can be found on the
[Releases page](https://github.com/grafana/loki/releases) as part of the release assets. 

## Docker

```bash
# modify tag to most recent version
docker pull grafana/promtail:2.0.0
```

## Helm

Make sure that Helm is installed.
See [Installing Helm](https://helm.sh/docs/intro/install/).
Then you can add Grafana's chart repository to Helm:

```bash
helm repo add grafana https://grafana.github.io/helm-charts
```

And the chart repository can be updated by running:

```bash
helm repo update
```

Finally, Promtail can be deployed with:

```bash
helm upgrade --install promtail grafana/promtail
```

## Kubernetes

### Deployment (recommended)

A `Deployment` will deploy Promtail in such a away that it can scrape all pods.

The Deployment works well at collecting the logs of all containers within a
cluster. It's the best solution for a single-tenant model. Replace `{YOUR_LOKI_ENDPOINT}` with your Loki endpoint.

```yaml
--- # Deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: promtail-deployment
spec:
  selector:
    matchLabels:
      name: promtail
  template:
    metadata:
      labels:
        name: promtail
    spec:
      serviceAccountName: promtail-serviceaccount
      containers:
      - name: promtail-container
        image: grafana/promtail
        args:
        - -config.file=/etc/promtail/promtail.yaml
        volumeMounts:
        - name: promtail-config
          mountPath: /etc/promtail
      volumes:
      - name: promtail-config
        configMap:
          name: promtail-config
--- # configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: promtail-config
data:
  promtail.yaml: |
    server:
      http_listen_port: 9080
      grpc_listen_port: 0

    clients:
    - url: https://{YOUR_LOKI_ENDPOINT}/loki/api/v1/push

    positions:
      filename: /tmp/positions.yaml
    target_config:
      sync_period: 10s
    scrape_configs:
    - job_name: pod-logs
      kubernetes_sd_configs:
        - role: pod
      relabel_configs:
        - action: labelmap
          regex: __meta_kubernetes_pod_label_(.+)
        - action: replace
          replacement: $1
          separator: /
          source_labels:
            - __meta_kubernetes_namespace
            - __meta_kubernetes_pod_name
          target_label: job
        - action: replace
          source_labels:
            - __meta_kubernetes_namespace
          target_label: namespace
        - action: replace
          source_labels:
            - __meta_kubernetes_pod_name
          target_label: pod
        - action: replace
          source_labels:
            - __meta_kubernetes_pod_container_name
          target_label: container

--- # Clusterrole.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: promtail-clusterrole
rules:
  - apiGroups: [""]
    resources:
    - nodes
    - services
    - pods
    verbs:
    - get
    - watch
    - list

--- # ServiceAccount.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: promtail-serviceaccount

--- # Rolebinding.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: promtail-clusterrolebinding
subjects:
    - kind: ServiceAccount
      name: promtail-serviceaccount
      namespace: default
roleRef:
    kind: ClusterRole
    name: promtail-clusterrole
    apiGroup: rbac.authorization.k8s.io
```
