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
