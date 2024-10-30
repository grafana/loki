---
title: "Forwarding Logs to LokiStack"
description: "Forwarding Logs to Loki-Operator managed LokiStack resources"
lead: ""
date: 2022-06-21T08:48:45+00:00
lastmod: 2022-06-21T08:48:45+00:00
draft: false
images: []
menu:
  docs:
    parent: "user-guides"
weight: 100
toc: true
---


This document will describe how to send application, infrastructure, audit and network logs to the LokiStack Gateway as different tenants using Promtail or Fluentd. The built-in gateway provides secure access to the distributor (and query-frontend) via consulting an OAuth/OIDC endpoint for the request subject.

__Please read the [hacking guide](../operator/hack_loki_operator.md) before proceeding with the following instructions.__

_Note: While this document will only give instructions for two methods of log forwarding into the gateway, the examples given in the Promtail and Fluentd sections can be extrapolated to other log forwarders._

## OpenShift Logging

[OpenShift Logging](https://github.com/openshift/cluster-logging-operator) supports [forwarding logs to an external Loki instance](https://docs.openshift.com/container-platform/4.9/logging/cluster-logging-external.html#cluster-logging-collector-log-forward-loki_cluster-logging-external). This can also be used to forward logs to LokiStack gateway.

* Deploy the Loki Operator and an `lokistack` instance with the [gateway flag enabled](./hack_loki_operator.md#hacking-on-loki-operator-on-openshift).

* Deploy the [OpenShift Logging Operator](https://github.com/openshift/cluster-logging-operator/blob/master/docs/HACKING.md) from the Operator Hub or using the following command locally:

    ```console
    make deploy-image deploy-catalog install
    ```

* Create a Cluster Logging instance in the `openshift-logging` namespace with only `collection` defined.

    ```yaml
    apiVersion: logging.openshift.io/v1
    kind: ClusterLogging
    metadata:
      name: instance
      namespace: openshift-logging
    spec:
      collection:
        logs:
          type: fluentd
          fluentd: {}
    ```

* The LokiStack Gateway requires a bearer token for communication with fluentd. Therefore, create a secret with `token` key and the path to the file.

    ```console
    kubectl -n openshift-logging create secret generic lokistack-gateway-bearer-token \
      --from-literal=token="$(kubectl -n openshift-logging get secret logcollector-token --template='{{.data.token | base64decode}}')"  \
      --from-literal=ca-bundle.crt="$(kubectl -n openshift-logging get configmap openshift-service-ca.crt --template='{{index .data "service-ca.crt"}}')"
    ```

* Create the following `ClusterRole` and `ClusterRoleBinding` which will allow the cluster to authenticate the user(s) submitting the logs:

    ```yaml
  ---
    apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRole
    metadata:
      name: lokistack-dev-tenant-logs
    rules:
    - apiGroups:
      - 'loki.grafana.com'
      resources:
      - application
      - infrastructure
      - audit
      resourceNames:
      - logs
      verbs:
      - 'create'
  ---
    apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRoleBinding
    metadata:
      name: lokistack-dev-tenant-logs
    roleRef:
      apiGroup: rbac.authorization.k8s.io
      kind: ClusterRole
      name: lokistack-dev-tenant-logs
    subjects:
    - kind: ServiceAccount
      name: logcollector
      namespace: openshift-logging
    ```

* Now create a ClusterLogForwarder CR to forward logs to LokiStack:

    ```yaml
    apiVersion: logging.openshift.io/v1
    kind: ClusterLogForwarder
    metadata:
      name: instance
      namespace: openshift-logging
    spec:
      outputs:
       - name: loki-app
         type: loki
         url: https://lokistack-dev-gateway-http.openshift-logging.svc:8080/api/logs/v1/application
         secret:
           name: lokistack-gateway-bearer-token
       - name: loki-infra
         type: loki
         url: https://lokistack-dev-gateway-http.openshift-logging.svc:8080/api/logs/v1/infrastructure
         secret:
           name: lokistack-gateway-bearer-token
       - name: loki-audit
         type: loki
         url: https://lokistack-dev-gateway-http.openshift-logging.svc:8080/api/logs/v1/audit
         secret:
           name: lokistack-gateway-bearer-token
      pipelines:
       - name: send-app-logs
         inputRefs:
         - application
         outputRefs:
         - loki-app
       - name: send-infra-logs
         inputRefs:
         - infrastructure
         outputRefs:
         - loki-infra
       - name: send-audit-logs
         inputRefs:
         - audit
         outputRefs:
         - loki-audit
    ```

    _Note:_ You can add/remove any pipeline from the ClusterLogForwarder spec in case if you want to limit the logs being sent.

## Network Observability

[Network Observability](https://github.com/netobserv/network-observability-operator) also require an external loki instance and is compatible with LokiStack Gateway. You must use a separate instance than `openshift-logging` one.

The Network Observability Operator can automatically install and configure dependent operators. However, if you need to configure these manually, follow the step below.

* Deploy the Loki Operator and a dedicated `lokistack` for Network Observability:
- open Openshift admin console 
- go to Operators -> OperatorHub and install Loki Operator from Red Hat catalog if not already installed
- ensure you are in `network-observability` namespace
- go to Operators -> Installed Operators -> Loki Operator -> LokiStack
- click on Create LokiStack
- set name to `lokistack-network`
- set `Object Storage` -> `Secret` [check object storage documentation](../lokistack/object_storage.md)
- set `Tenants Configuration` -> `Mode` to `openshift-network`

* Create the following `ClusterRole` and `ClusterRoleBinding` which allow `flowlogs-pipeline` and `network-observability-plugin` service accounts to read and write the network logs:

    ```yaml
  ---
    apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRole
    metadata:
      name: lokistack-network-tenant-logs
    rules:
    - apiGroups:
      - 'loki.grafana.com'
      resources:
      - network
      resourceNames:
      - logs
      verbs:
      - 'get'
      - 'create'
  ---
    apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRoleBinding
    metadata:
      name: lokistack-network-tenant-logs
    roleRef:
      apiGroup: rbac.authorization.k8s.io
      kind: ClusterRole
      name: lokistack-network-tenant-logs
    subjects:
    - kind: ServiceAccount
      name: flowlogs-pipeline
      namespace: network-observability
    - kind: ServiceAccount
      name: network-observability-plugin
      namespace: network-observability
    ```

* Deploy the [Network Observability Operator](https://github.com/netobserv/network-observability-operator) following the [Getting started documentation](https://github.com/netobserv/network-observability-operator/blob/main/README.md#getting-started) either from Operator Hub or commands available in the repository.

* Apply the following configuration in `FlowCollector` for `network` tenant of `lokistack-network`:
```yaml
  loki:
    tenantID: network
    sendAuthToken: true
    url: 'https://lokistack-network-gateway-http.network-observability.svc.cluster.local:8080/api/logs/v1/network/'
```
Check [config samples](https://github.com/netobserv/network-observability-operator/tree/main/config/samples) for all options.

## Forwarding Clients

In order to enable communication between the client(s) and the gateway, follow these steps:

1. Deploy the Loki Operator and an `lokistack` instance with the [gateway flag enabled](./hack_loki_operator.md#hacking-on-loki-operator-on-openshift).

2. Create a `ServiceAccount` to generate the `Secret` which will be used to authorize the forwarder.

```console
kubectl -n openshift-logging create serviceaccount <SERVICE_ACCOUNT_NAME>
```

3. Configure the forwarder and deploy it to the `openshift-logging` namespace.

4. Create the following `ClusterRole` and `ClusterRoleBinding` which will allow the cluster to authenticate the user(s) submitting the logs:

```yaml
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: lokistack-dev-tenant-logs-role
rules:
- apiGroups:
  - 'loki.grafana.com'
  resources:
  - application
  - infrastructure
  - audit
  resourceNames:
  - logs
  verbs:
  - 'get'
  - 'create'
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: lokistack-dev-tenant-logs-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: lokistack-dev-tenant-logs-role
subjects:
- kind: ServiceAccount
  name: "<SERVICE_ACCOUNT_NAME>"
  namespace: openshift-logging
```

### Promtail

[Promtail](https://grafana.com/docs/loki/latest/clients/promtail/) is an agent managed by Grafana which forwards logs to a Loki instance. The Grafana documentation can be consulted for [configuring](https://grafana.com/docs/loki/latest/clients/promtail/configuration/#configuration-file-reference) and [deploying](https://grafana.com/docs/loki/latest/clients/promtail/installation/#kubernetes) an instance of Promtail in a Kubernetes cluster.

To configure Promtail to send application, audit, and infrastructure logs, add the following clients to the Promtail configuration

```yaml
clients:
  - # ...
    bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
    tls_config:
      ca_file: /run/secrets/kubernetes.io/serviceaccount/service-ca.crt
    url: https://lokistack-dev-gateway-http.openshift-logging.svc:8080/api/logs/v1/audit/loki/api/v1/push
  - # ...
    bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
    tls_config:
      ca_file: /run/secrets/kubernetes.io/serviceaccount/service-ca.crt
    url: https://lokistack-dev-gateway-http.openshift-logging.svc:8080/api/logs/v1/application/loki/api/v1/push
  - # ...
    bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
    tls_config:
      ca_file: /run/secrets/kubernetes.io/serviceaccount/service-ca.crt
    url: https://lokistack-dev-gateway-http.openshift-logging.svc:8080/api/logs/v1/infrastructure/loki/api/v1/push
```

The rest of the configuration can be configured to the developer's desire.

### Fluentd

Loki can receive logs from Fluentd via the [Grafana plugin](https://grafana.com/docs/loki/latest/clients/fluentd/).

The Fluentd configuration can be overrided to target the `application` endpoint to send those log types.

```
<match **>
  @type loki
  # ...
  bearer_token_file /var/run/secrets/kubernetes.io/serviceaccount/token
  ca_cert /run/secrets/kubernetes.io/serviceaccount/service-ca.crt
  url https://lokistack-dev-gateway-http.openshift-logging.svc:8080/api/logs/v1/application
</match>
```

## Troubleshooting

### Log Entries Out of Order

If the forwarder is configured to send too much data in a short span of time, Loki will back-pressure the forwarder and respond to the POST requests with `429` errors. In order to alleviate this, a few changes could be made to the spec:

* Consider moving up a t-shirt size. This will bring in addition resources and have a higher ingestion rate.

```console
kubectl -n openshift-logging edit lokistack
```

```yaml
size: 1x.medium
```

* Manually change the ingestion rate (global or tenant) can be changed via configuration changes to `lokistack`:

```console
kubectl -n openshift-logging edit lokistack
```

```yaml
limits:
    tenants:
        <TENANT_NAME>:
            IngestionLimits:
                IngestionRate: 15
```

where `<TENANT_NAME>` can be application, audit or infrastructure.
