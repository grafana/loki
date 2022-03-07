# Alerts

<!-- TOC depthTo:2 -->

- Loki Cluster
  - [Loki Request Errors](#Loki-Request-Errors)
  - [Loki Request Panics](#Loki-Request-Panics)

<!-- /TOC -->

## Loki Request Errors

### Impact

One or multiple components are unavailable due to some server error.

### Summary

One or multiple components are unable to process incoming requests and are serving 5XX responses as a result.

### Severity

`Critical`

### Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging`
    - `openshift-operators-redhat`

### Steps

- Identify which service is returning the 5XX error
  - Check the logs of the `distributor`, `query-frontend`, and `ingester`
- Ensure that the `ingester` and `index-gateway` can communicate with the backend storage
- Check the `ingester` metrics for signs of WAL complications
  - `loki_ingester_wal_disk_full_failures_total`
  - `loki_ingester_wal_corruptions_total`

## Loki Request Panics

### Impact

One or multiple components are unavailable due to panics.

### Summary

One or multiple components are unable to process incoming requests because the component(s) are panicking.

### Severity

`Critical`

### Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging`
    - `openshift-operators-redhat`

### Steps

- Identify which component is panicking
  - Examining pods for numerous restarts
  - Check the logs of the all components
