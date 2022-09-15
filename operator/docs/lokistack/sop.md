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
