---
title: "Alert - LokiRequestLatency"
description: "The standard operational procedure for alert LokiRequestLatency"
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

A service(s) is affected by slow request responses.

## Summary

A service(s) is slower than expected at processing data.

## Severity

`Critical`

## Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging` (LokiStack)
    - `openshift-operators-redhat` (Loki Operator)

## Steps

- Check the logs of all the services
- Check to ensure that the Loki components can reach the storage
  - Particularly for queriers, examine metrics for a small query queue: `cortex_query_scheduler_inflight_requests`
