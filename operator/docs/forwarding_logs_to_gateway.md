# Forwarding Logs to LokiStack

This document will describe how to send application, infrastructure, and audit logs to the Lokistack Gateway as different tenants using Promtail or Fluentd. The built-in gateway provides secure access to the distributor (and query-frontend) via consulting an OAuth/OIDC endpoint for the request subject.

__Please read the [hacking guide](./hack_loki_operator.md) before proceeding with the following instructions.__

_Note: While this document will only give instructions for two methods of log forwarding into the gateway, the examples given in the Promtail and Fluentd sections can be extrapolated to other log forwarders._

## Openshift Logging

Although there is a way to [forward logs to an external Loki instance](https://docs.openshift.com/container-platform/4.9/logging/cluster-logging-external.html#cluster-logging-collector-log-forward-loki_cluster-logging-external), [Openshift Logging](https://github.com/openshift/cluster-logging-operator) does not currently have support to send logs through the Lokistack Gateway.

Support will be added in the near future.

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
  - 'loki.openshift.io'
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
    url: http://lokistack-gateway-http-lokistack-dev.openshift-logging.svc:8080/api/logs/v1/audit/loki/api/v1/push
  - # ...
    bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
    url: http://lokistack-gateway-http-lokistack-dev.openshift-logging.svc:8080/api/logs/v1/application/loki/api/v1/push
  - # ...
    bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
    url: http://lokistack-gateway-http-lokistack-dev.openshift-logging.svc:8080/api/logs/v1/infrastructure/loki/api/v1/push
```

The rest of the configuration can be configured to the developer's desire.

### Fluentd

Loki can receive logs from Fluentd via the [Grafana plugin](https://grafana.com/docs/loki/latest/clients/fluentd/).

The Fluentd configuration can be overrided to target the `application` endpoint to send those log types.

```
<match **>
  @type loki
  # ...
  bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
  url: http://lokistack-gateway-http-lokistack-dev.openshift-logging.svc:8080/api/logs/v1/application
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
        4a5bb098-7caf-42ec-9b1a-8e1d979bfb95:
            IngestionLimits:
                IngestionRate: 15
```
