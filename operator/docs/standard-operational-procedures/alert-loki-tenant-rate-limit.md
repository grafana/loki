---
title: "Alert - LokiTenantRateLimit"
description: "The standard operational procedure for alert LokiTenantRateLimit"
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

A tenant is being rate limited, resulting in potential loss of data.

## Summary

A service(s) is rate limiting at least 10% of all incoming requests.

## Severity

`Warning`

## Access Required

- Console access to the cluster
- Edit access to the deployed operator and Loki namespace:
  - OpenShift
    - `openshift-logging` (LokiStack)
    - `openshift-operators-redhat` (Loki Operator)

## Steps

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
