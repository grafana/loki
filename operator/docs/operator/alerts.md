---
title: "Alert Response Runbook"
description: "Alert Response Runbook"
lead: ""
date: 2022-06-21T08:48:45+00:00
lastmod: 2022-06-21T08:48:45+00:00
draft: false
images: []
menu:
  docs:
    parent: "operator"
weight: 100
toc: true
---

# Alerts

<!-- TOC depthTo:2 -->

- Loki Cluster
  - [Loki Request Errors](#Loki-Request-Errors)
  - [Loki Request Panics](#Loki-Request-Panics)
  - [Loki Request Latency](#Loki-Request-Latency)
  - [Loki Tenant Rate Limit](#Loki-Tenant-Rate-Limit)
  - [Loki Storage Write Read](#Loki-Storage-Slow-Write)
  - [Loki Storage Slow Read](#Loki-Storage-Slow-Read)
  - [Loki Write Path High Load](#Loki-Write-Path-High-Load)
  - [Loki Read Path High Load](#Loki-Read-Path-High-Load)

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
    - `openshift-logging`
    - `openshift-operators-redhat`

### Steps

- Check the logs of all the services
- Check to ensure that the Loki components can reach the storage

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
    - `openshift-logging`
    - `openshift-operators-redhat`

### Steps

- Examine the metrics for the reason and tenant that is being limited: `loki_discarded_samples_total{namespace="<namespace>"}`
- Change the ingestion limits for the affected tenant or decrease the rate of logs entering the system

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
    - `openshift-logging`
    - `openshift-operators-redhat`

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
    - `openshift-logging`
    - `openshift-operators-redhat`

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
    - `openshift-logging`
    - `openshift-operators-redhat`

### Steps

- Adjust the ingestion limits for the affected tenant or increase the number of ingesters

## Loki Write Path High Load

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
    - `openshift-logging`
    - `openshift-operators-redhat`

### Steps

- Increase the number of queriers
