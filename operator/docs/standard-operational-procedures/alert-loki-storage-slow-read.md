---
title: "Alert - LokiStorageSlowRead"
description: "The standard operational procedure for alert LokiStorageSlowRead"
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

The cluster is unable to retrieve logs to backend storage in a timely manner.

## Summary

The cluster is unable to retrieve logs to backend storage in a timely manner.

## Severity

`Warning`

## Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging` (LokiStack)
    - `openshift-operators-redhat` (Loki Operator)

## Steps

- Ensure that the cluster can communicate with the backend storage
