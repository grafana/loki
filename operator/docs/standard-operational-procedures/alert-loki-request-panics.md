---
title: "Alert - LokiRequestPanics"
description: "The standard operational procedure for alert LokiRequestPanics"
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

A service(s) is unavailable to unavailable, resulting in potential loss of data.

## Summary

A service(s) has crashed.

## Severity

`Critical`

## Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging` (LokiStack)
    - `openshift-operators-redhat` (Loki Operator)

## Steps

- Check the logs of the service that is panicking
- Examine metrics for signs of failure
