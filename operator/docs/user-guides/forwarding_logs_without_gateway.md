---
title: "Forwarding Logs without the Gateway"
description: "Forwarding Logs to LokiStack without LokiStack Gateway"
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

This document describes how to send application, infrastructure, and audit logs to the Loki Distributor as different tenants using Fluentd or Vector.

__Please read the [hacking guide](../operator/hack_loki_operator.md) before proceeding with the following instructions.__

_Note:_ This document only applies to OpenShift-based deployments.

_Disclaimer:_ This document helps to connect the forwarder (fluentd or vector) to the LokiStack by going around the authentication gateway. This is not a normal configuration for an OpenShift-based deployments and should only be used for testing if going through the gateway is no option.

## OpenShift Logging

[OpenShift Logging](https://github.com/openshift/cluster-logging-operator) supports [forwarding logs to an external Loki instance](https://docs.openshift.com/container-platform/4.9/logging/cluster-logging-external.html#cluster-logging-collector-log-forward-loki_cluster-logging-external) using fluentd or vector as log forwarders.
The below step-by-step guide will help you to send application, infrastructure, and audit logs to the LokiStack through the Distributor endpoint.
The steps remain same for both fluentd and vector.

In order to enable communication between the log forwarder and the distributor, follow these steps:

* Deploy the Loki Operator and a `lokistack` instance for [OpenShift](./hack_loki_operator.md#hacking-on-loki-operator-on-openshift).

* Deploy the OpenShift Logging Operator from the Operator Hub or using the following command locally:

    ```console
    make deploy-image deploy-catalog install
    ```
  
* Create a Cluster Logging instance in the `openshift-logging` namespace with only `collection` defined.

    For fluentd:

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

    For vector:

    ```yaml
    apiVersion: logging.openshift.io/v1
    kind: ClusterLogging
    metadata:
      name: instance
      namespace: openshift-logging
    spec:
      collection:
        logs:
          type: vector
          fluentd: {}
    ```

* By default, TLS is enabled on all components deployed by loki-operator. Because the service certificates are signed by a cluster-internal CA you need to set up a secret that enables the collector to validate the certificate returned by the distributor. The secret must exist in the openshift-logging namespace, and must have a key `ca-bundle.crt`.

  The CA certificate is part of a ConfigMap that gets created by loki-operator as part of the LokiStack. Unfortunately this ConfigMap can not be used directly and has to be converted to a Secret readable by the collector.

    Fetch the `ca-bundle.crt` using:

    ```console
    kubectl -n openshift-logging get cm lokistack-dev-gateway-ca-bundle -o jsonpath="{.data.service-ca\.crt}" > <FILE_NAME>
    ```
  
    where `<FILE_NAME>` can be `ca_bundle.crt` and used directly to create secret in the next step.
  
* Once secret is fetched, create a new secret file:

    ```console
    kubectl -n openshift-logging create secret generic loki-distributor-ca \
    --from-file=ca-bundle.crt=<PATH/TO/CA_BUNDLE.CRT>
    ```
    
    where `<PATH/TO/CA_BUNDLE.CRT>` is the file path where the `ca_bundle.crt` was copied to.

* Now create a ClusterLogForwarder CR to forward logs to LokiStack:

  ```yaml
  apiVersion: logging.openshift.io/v1
  kind: ClusterLogForwarder
  metadata:
    name: instance
    namespace: openshift-logging
  spec:
    outputs:
     - name: loki-operator
       type: loki
       url: https://lokistack-dev-distributor-http.openshift-logging.svc:3100
       secret:
         name: loki-distributor-ca
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
