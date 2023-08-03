---
title: "Alert - LokiRequestErrors"
description: "The standard operational procedure for alert LokiRequestErrors"
lead: ""
date: 2022-06-21T08:48:45+00:00
lastmod: 2022-06-21T08:48:45+00:00
draft: false
images: []
menu:
  docs:
    parent: "standard-operational-procedures"
weight: 100
toc: true
---

## Impact

A service(s) is unable to perform its duties for a number of requests, resulting in potential loss of data.

## Summary

A service(s) is failing to process at least 10% of all incoming requests.

## Severity

`Critical`

## Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging` (LokiStack)
    - `openshift-operators-redhat` (Loki Operator)

## Steps

- Check the logs of the service that is emitting the server error (5XX)
- Ensure that store services (`ingester`, `querier`, `index-gateway`, `compactor`) can communicate with backend storage
- Examine metrics for signs of failure
  - WAL Complications
    - `loki_ingester_wal_disk_full_failures_total`
    - `loki_ingester_wal_corruptions_total`
