---
title: "Standard Operational Procedures"
description: "The LokiStack Alerts and Standard Operational Procedures"
lead: ""
date: 2022-06-21T08:48:45+00:00
lastmod: 2022-06-21T08:48:45+00:00
draft: false
images: []
menu:
  docs:
    parent: "lokistack"
weight: 100
toc: true
---

The following page describes Standard Operational Procedures for alerts provided and managed by the Loki Operator for any LokiStack instance.

## Loki Request Errors

### Impact

A service(s) is unable to perform its duties for a number of requests, resulting in potential loss of data.

### Summary

A service(s) is failing to process at least 10% of all incoming requests.

### Severity

`Critical`

### Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging` (LokiStack)
    - `openshift-operators-redhat` (Loki Operator)

### Steps

- Check the logs of the service that is emitting the server error (5XX)
- Ensure that store services (`ingester`, `querier`, `index-gateway`, `compactor`) can communicate with backend storage
- Examine metrics for signs of failure
  - WAL Complications
    - `loki_ingester_wal_disk_full_failures_total`
    - `loki_ingester_wal_corruptions_total`

## LokiStack Write Request Errors

### Impact

The LokiStack Gateway component is unable to perform its duties for a number of write requests, resulting in potential loss of data.

### Summary

The LokiStack Gateway is failing to process at least 10% of all incoming write requests.

### Severity

`Critical`

### Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging` (LokiStack)
    - `openshift-operators-redhat` (Loki Operator)

### Steps

- Ensure that the LokiStack Gateway component is ready and available
- Ensure that the `distributor`, `ingester`, and `index-gateway` components are ready and available
- Ensure that store services (`ingester`, `querier`, `index-gateway`, `compactor`) can communicate with backend storage
- Examine metrics for signs of failure
  - WAL Complications
    - `loki_ingester_wal_disk_full_failures_total`
    - `loki_ingester_wal_corruptions_total`

## LokiStack Read Request Errors

### Impact

The LokiStack Gateway component is unable to perform its duties for a number of query requests, resulting in a potential disruption.

### Summary

The LokiStack Gateway is failing to process at least 10% of all incoming query requests.

### Severity

`Critical`

### Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging` (LokiStack)
    - `openshift-operators-redhat` (Loki Operator)

### Steps

- Ensure that the LokiStack Gateway component is ready and available
- Ensure that the `query-frontend`, `querier`, `ingester`, and `index-gateway` components are ready and available
- Ensure that store services (`ingester`, `querier`, `index-gateway`, `compactor`) can communicate with backend storage
- Examine metrics for signs of failure
  - WAL Complications
    - `loki_ingester_wal_disk_full_failures_total`
    - `loki_ingester_wal_corruptions_total`

## Loki Request Panics

### Impact

A service(s) is unavailable to unavailable, resulting in potential loss of data.

### Summary

A service(s) has crashed.

### Severity

`Critical`

### Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging` (LokiStack)
    - `openshift-operators-redhat` (Loki Operator)

### Steps

- Check the logs of the service that is panicking
- Examine metrics for signs of failure

## Loki Request Latency

### Impact

A service(s) is affected by slow request responses.

### Summary

A service(s) is slower than expected at processing data.

### Severity

`Critical`

### Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging` (LokiStack)
    - `openshift-operators-redhat` (Loki Operator)

### Steps

- Check the logs of all the services
- Check to ensure that the Loki components can reach the storage
  - Particularly for queriers, examine metrics for a small query queue: `cortex_query_scheduler_inflight_requests`

## Loki Tenant Rate Limit

### Impact

A tenant is being rate limited, resulting in potential loss of data.

### Summary

A service(s) is rate limiting at least 10% of all incoming requests.

### Severity

`Warning`

### Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging` (LokiStack)
    - `openshift-operators-redhat` (Loki Operator)

### Steps

- Examine the metrics for the reason and tenant that is being limited: `loki_discarded_samples_total{namespace="<namespace>"}`
- Increase the limits allocated to the tenant in the LokiStack CRD
  - For ingestion limits, please consult the table below
  - For query limits, the `MaxEntriesLimitPerQuery`, `MaxChunksPerQuery`, or `MaxQuerySeries` can be changed to raise the limit

| Reason | Corresponding Ingestion Limit Keys |
| --- | --- |
| `rate_limited` | `ingestionRate`, `ingestionBurstSize` |
| `stream_limit` | `maxGlobalStreamsPerTenant` |
| `label_name_too_long` | `maxLabelNameLength` |
| `label_value_too_long` | `maxLabelValueLength` |
| `line_too_long` | `maxLineSize` |
| `max_label_names_per_series` | `maxLabelNamesPerSeries` |
| `per_stream_rate_limit` | `perStreamRateLimit`, `perStreamRateLimitBurst` |


## Loki Storage Slow Write

### Impact

The cluster is unable to push logs to backend storage in a timely manner.

### Summary

The cluster is unable to push logs to backend storage in a timely manner.

### Severity

`Warning`

### Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging` (LokiStack)
    - `openshift-operators-redhat` (Loki Operator)

### Steps

- Ensure that the cluster can communicate with the backend storage

## Loki Storage Slow Read

### Impact

The cluster is unable to retrieve logs to backend storage in a timely manner.

### Summary

The cluster is unable to retrieve logs to backend storage in a timely manner.

### Severity

`Warning`

### Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging` (LokiStack)
    - `openshift-operators-redhat` (Loki Operator)

### Steps

- Ensure that the cluster can communicate with the backend storage

## Loki Write Path High Load

### Impact

The write path is under high pressure and requires a storage flush.

### Summary

The write path is flushing the storage in response to back-pressuring.

### Severity

`Warning`

### Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging` (LokiStack)
    - `openshift-operators-redhat` (Loki Operator)

### Steps

- Adjust the ingestion limits for the affected tenant or increase the number of ingesters

## Loki Read Path High Load

### Impact

The read path is under high load.

### Summary

The query queue is currently under high load.

### Severity

`Warning`

### Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging` (LokiStack)
    - `openshift-operators-redhat` (Loki Operator)

### Steps

- Increase the number of queriers

## Loki Discarded Samples Warning

### Impact

Loki is discarding samples (log entries) because they fail validation. This alert only fires for errors that are not retryable. This means that the discarded samples are lost.

### Summary

Loki can reject log entries (samples) during submission when they fail validation. This happens on a per-stream basis, so only the specific samples or streams failing validation are lost.

The possible validation errors are documented in the [Loki documentation](https://grafana.com/docs/loki/latest/operations/request-validation-rate-limits/#validation-errors). This alert only fires for the validation errors that are not retryable, which means that discarded samples are permanently lost.

The alerting can only show the affected Loki tenant. Since Loki 3.1.0 more detailed information about the affected streams is provided in an error message emitted by the distributor component.

This information can be used to pinpoint the application sending the offending logs. For some of the validations there are configuration parameters that can be tuned in LokiStack's `limits` structure, if the messages should be accepted. Usually it is recommended to fix the issue either on the emitting application (if possible) or by changing collector configuration to fix non-compliant messages before sending them to Loki.

### Severity

`Warning`

### Access Required

- Console access to the cluster
- View access in the namespace where the LokiStack is deployed
  - OpenShift
    - `openshift-logging` (LokiStack)

### Steps

- View detailed log output from the Loki distributors to identify affected streams
- Decide on further steps depending on log source and validation error

## Loki Ingester Flush Failure Rate Critical

### Impact

Loki ingesters are unable to flush chunks to backend storage at a critical rate (>20% failure rate), resulting in potential data loss and Write Ahead Log (WAL) disk pressure.

### Summary

One or more Loki ingesters are failing to flush at least 20% of their chunks to backend storage over a 5-minute period. This indicates issues with storage connectivity, authentication, or storage capacity that require immediate intervention.

### Severity

`Critical`

### Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging` (LokiStack)
    - `openshift-operators-redhat` (Loki Operator)

### Steps

- **Immediate Actions**:
  - Check ingester pod logs for flush failure error messages to better understand what is the error: 
    - `kubectl logs -n <namespace> <ingester-pod>`
  - Verify backend storage connectivity and credentials in the storage secret
  - Verify Lokistack conditions to understand if there was an error processing the storage secret:
    - `kubectl -n <namespace> describe Lokistack <lokistack-name>`
  - Monitor WAL disk usage: 
    - `sum(loki_ingester_wal_bytes_in_use) by (pod, namespace)`: current WAL disk usage
    - `sum(rate(loki_ingester_wal_disk_full_failures_total[5m])) by (pod, namespace)`: number of failures due to fill wall disk

- **Root Cause Analysis**:
  - **Storage Authentication**: Verify that storage credentials (Secret) are valid and accessible by ingester pods
  - **Storage Connectivity**: Ensure ingesters can reach the storage endpoint (check NetworkPolicies, firewall rules)
  - **Storage Capacity**: Check if the storage backend has sufficient space and IOPS capacity
  - **Storage Configuration**: Validate the LokiStack storage configuration matches the actual storage setup

- **Resolution Steps**:
  - **For Authentication Issues**: Update or recreate the storage Secret with correct credentials
  - **For Connectivity Issues**: Adjust NetworkPolicies, security groups, or firewall rules
  - **For Capacity Issues**: Increase storage size/IOPS or implement lifecycle policies
  - **For Configuration Issues**: Correct the LokiStack storage configuration and wait for a restart of the ingesters

## Lokistack Storage Schema Warning

### Impact

The LokiStack warns on a newer object storage schema being available for configuration.

### Summary

The schema configuration does not contain the most recent schema version and needs an update.

### Severity

`Warning`

### Access Required

- Console access to the cluster
- Edit access to the namespace where the LokiStack is deployed:
  - OpenShift
    - `openshift-logging` (LokiStack)

### Steps

- Add a new object storage schema V13 with a future EffectiveDate

## Lokistack Components Not Ready Warning

### Impact

One or more LokiStack components are not ready, which can disrupt ingestion or querying and lead to degraded service.

### Summary

The LokiStack reports that some components have not reached the `Ready` state. This might be related to Kubernetes resources (Pods/Deployments), configuration, or external dependencies.

### Severity

`Warning`

### Access Required

- Console access to the cluster
- Edit or view access in the namespace where the LokiStack is deployed:
  - OpenShift
    - `openshift-logging` (LokiStack)

### Steps

- Inspect the LokiStack conditions and events
  - Describe the LokiStack resource and review status conditions:
    - `kubectl -n <namespace> describe lokistack <name>`
  - Check for conditions that would lead to some pods not being in the `Ready` state
- Check operator and reconciliation status
  - Ensure the Loki Operator is running and not reporting errors:
    - `kubectl -n <operator-namespace> logs deploy/loki-operator-controller-manager`
  - Look for reconcile errors related to missing permissions, invalid fields, or failed rollouts.
- Verify component Pods and Deployments
  - Ensure all core components are running and Ready in the LokiStack namespace:
    - `distributor`, `ingester`, `querier`, `query-frontend`, `index-gateway`, `compactor`, `gateway`
  - Check Pod readiness and recent restarts:
    - `kubectl -n <namespace> get pods`
    - `kubectl -n <namespace> describe pod <pod>`
- Examine Kubernetes events for failures
  - `kubectl -n <namespace> get events --sort-by=.lastTimestamp`
  - Common causes: image pull backoffs, failed mounts, readiness probe failures, or insufficient resources
- Validate configuration and referenced resources
  - Confirm referenced `Secrets` and `ConfigMaps` exist and have correct keys
- Look into the Pod logs of the component that still not `Ready`:
  - `kubectl -n <namespace> logs <pod>`