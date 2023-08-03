---
title: "Alert - LokiReadPathHighLoad"
description: "The standard operational procedure for alert LokiReadPathHighLoad"
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

The read path is under high load.

## Summary

The query queue is currently under high load.

## Severity

`Warning`

## Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging` (LokiStack)
    - `openshift-operators-redhat` (Loki Operator)

## Steps

- Increase the number of queriers
