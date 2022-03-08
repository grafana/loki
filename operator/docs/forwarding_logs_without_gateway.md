# Forwarding Logs to LokiStack without LokiStack Gateway

This document will describe how to send application, infrastructure, and audit logs to the Loki Distributor as different tenants using Fluentd or Vector.

__Please read the [hacking guide](./hack_loki_operator.md) before proceeding with the following instructions.__

_Note:_ This document is meant to be used with OpenShift only.

_Disclaimer:_ This document helps in connecting fluentd or vector to LokiStack without authentication/authorization.

## Openshift Logging

[Openshift Logging](https://github.com/openshift/cluster-logging-operator) supports [forwarding logs to an external Loki instance](https://docs.openshift.com/container-platform/4.9/logging/cluster-logging-external.html#cluster-logging-collector-log-forward-loki_cluster-logging-external) using fluentd and vector as log forwarders.
The below step-by-step guide will help you to send application, infrastructure, and audit logs to the LokiStack through the Distributor endpoint.
The steps remain same for both fluentd and vector.

In order to enable communication between the client(s) and the gateway, follow these steps:

* Deploy the Loki Operator and a `lokistack` instance for [OpenShift](./hack_loki_operator.md#hacking-on-loki-operator-on-openshift).

* Deploy the OpenShift Logging Operator from the Operator Hub or using the following command locally:

    ```console
    make deploy-image deploy-catalog install
    ```
  
* Create a Cluster Logging instance in the `openshift-logging` namespace with only `collection` defined.

    For fluentd:

    ```yaml
    apiVersion: "logging.openshift.io/v1"
    kind: "ClusterLogging"
    metadata:
      name: "instance"
      namespace: openshift-logging
    spec:
      collection:
        logs:
          type: "fluentd"
          fluentd: {}
    ```

    For vector:

    ```yaml
    apiVersion: "logging.openshift.io/v1"
    kind: "ClusterLogging"
    metadata:
      name: "instance"
      namespace: openshift-logging
    spec:
      collection:
        logs:
          type: "vector"
          fluentd: {}
    ```

* By default, the TLS communication is on for communication with the distributor. Therefore, you need to create a secret. The secret must exist in the openshift-logging namespace, and must have a key `ca-bundle.crt`.

    This key already exists when LokiStack instance got created but as a Config Map. Hence, you need to fetch them and put into a secret file.

    Fetch the `ca-bundle.crt` using:

    ```console
    kubectl -n openshift-logging get cm lokistack-gateway-lokistack-dev-ca-bundle -o jsonpath="{.data.service-ca\.crt}" | base64 -w 0
    ```
  
* Once all the secrets are fetched, create a new secret file:

    ```yaml
    apiVersion: v1
    kind: Secret
    metadata:
      name: loki-distributor-metrics
      namespace: openshift-logging
    type: kubernetes.io/tls
    data:
      ca-bundle.crt: <CA_BUNDLE>
    ```
    
    where `<CA_BUNDLE>` is the copied value from previous step.

* Create the above secret file in the cluster:

    ```console
    kubectl create -f <SECRET_FILE_NAME>
    ```

* Now create a ClusterLogForwarder CR to forward logs to LokiStack:

  ```yaml
  apiVersion: "logging.openshift.io/v1"
  kind: "ClusterLogForwarder"
  metadata:
    name: "instance"
    namespace: "openshift-logging"
  spec:
    outputs:
     - name: loki-operator
       type: "loki"
       url: https://loki-distributor-http-lokistack-dev.openshift-logging.svc:3100
       secret:
         name: loki-distributor-metrics
       loki:
         tenantKey: log_type
    pipelines:
     - name: send-logs
       inputRefs:
       - application
       - audit
       - infrastructure
       outputRefs:
       - loki-operator
  ```

  _Note:_ The `tenantKey: log_type` gets resolved as `application`, `audit` or `infrastructure` by fluentd and vector based on the type of logs being collected. This is later used as different tenants for storing logs in Loki.

## Troubleshooting

### Log Entries Out of Order

If the forwarder is configured to send too much data in a short span of time, Loki will back-pressure the forwarder and respond to the POST requests with `429` errors.
In order to alleviate this, follow this [documentation](./forwarding_logs_to_gateway.md#log-entries-out-of-order).
