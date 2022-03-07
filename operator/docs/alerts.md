# Alerts

<!-- TOC depthTo:2 -->

- Loki Cluster
  - [Loki Request Errors](#Loki-Request-Errors)
  - [Loki Request Panics](#Loki-Request-Panics)

<!-- /TOC -->

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
    - `openshift-logging`
    - `openshift-operators-redhat`

### Steps

- Check the logs of the service that is emitting the server error (5XX)
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
    - `openshift-logging`
    - `openshift-operators-redhat`

### Steps

- Check the logs of the service that is panicking
- Examine metrics for signs of failure
