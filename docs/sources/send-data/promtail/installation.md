---
title: Install Promtail
menuTitle:  Install Promtail
description: Installation instructions for the Promtail client.
aliases: 
- ../../clients/promtail/installation/
weight:  100
---

# Install Promtail

Promtail is distributed as a binary, in a Docker container,
or there is a Helm chart to install it in a Kubernetes cluster.

## Install the binary

Every Grafana Loki release includes binaries for Promtail which can be found on the
[Releases page](https://github.com/grafana/loki/releases) as part of the release assets.

## Install using APT or RPM package manager

See the instructions [here](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/local/#install-using-apt-or-rpm-package-manager).

## Install using Docker

```bash
# modify tag to most recent version
docker pull grafana/promtail:2.9.2
```

## Install using Helm

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

Create the configuration file `values.yaml`. The example below illustrates a connection to the locally deployed loki server:

```yaml
config:
  # publish data to loki
  clients:
    - url: http://loki-gateway/loki/api/v1/push
      tenant_id: 1
```

Finally, Promtail can be deployed with:

```bash
# The default helm configuration deploys promtail as a daemonSet (recommended)
helm upgrade --values values.yaml --install promtail grafana/promtail
```

## Install as Kubernetes daemonSet (recommended)

A `DaemonSet` will deploy Promtail on every node within a Kubernetes cluster.

The DaemonSet deployment works well at collecting the logs of all containers within a
cluster. It's the best solution for a single-tenant model. Replace `{YOUR_LOKI_ENDPOINT}` with your Loki endpoint.

```yaml
--- # Daemonset.yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: promtail-daemonset
spec:
  selector:
    matchLabels:
      name: promtail
  template:
    metadata:
      labels:
        name: promtail
    spec:
      serviceAccount: promtail-serviceaccount
      containers:
      - name: promtail-container
        image: grafana/promtail
        args:
        - -config.file=/etc/promtail/promtail.yaml
        env: 
        - name: 'HOSTNAME' # needed when using kubernetes_sd_configs
          valueFrom:
            fieldRef:
              fieldPath: 'spec.nodeName'
        volumeMounts:
        - name: logs
          mountPath: /var/log
        - name: promtail-config
          mountPath: /etc/promtail
        - mountPath: /var/lib/docker/containers
          name: varlibdockercontainers
          readOnly: true
      volumes:
      - name: logs
        hostPath:
          path: /var/log
      - name: varlibdockercontainers
        hostPath:
          path: /var/lib/docker/containers
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
      pipeline_stages:
        - docker: {}
      relabel_configs:
        - source_labels:
            - __meta_kubernetes_pod_node_name
          target_label: __host__
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
        - replacement: /var/log/pods/*$1/*.log
          separator: /
          source_labels:
            - __meta_kubernetes_pod_uid
            - __meta_kubernetes_pod_container_name
          target_label: __path__

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
