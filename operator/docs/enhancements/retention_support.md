---
title: Stream-based Retention Support
authors:
  - "@shwetaap"
reviewers:
  - "@xperimental"
  - "@periklis"
lead: ""
date: 2022-05-21T08:48:45+00:00
lastmod: 2022-05-21T08:48:45+00:00
draft: false
menu:
  docs:
    parent: "enhancements"
weight: 100
toc: true
---

## Summary

Retention in Grafana Loki is achieved either through the Table Manager or the Compactor.
Retention through the Compactor is supported only with the boltdb-shipper store. The Compactor retention will become the default and have long term support. It supports more granular retention policies on per-tenant and per-stream use cases. So we are defining the Lokistack resources to support retention via Compactor only.  
The following sections describe a set of APIs in form of custom resource definitions (CRD) that enable users of `LokiStack` resources to support:
- Enable Retention configuration in LokiStack.
- Define the Global and Per-tenant retention period and stream configurations within the Lokistack custom resource.

## Motivation

The Loki Operator manages `LokiStack` resources that consists of a set of Loki components for ingestion/quering and optionally a gateway microservice that ensures authenticated and authorized access to logs stored by Loki. 
Retention in Loki has always been global for a cluster and deferred to the underlying object store. Since v2.3.0 Loki can handle retention through the Compactor component. Retention can be configured per tenant and per stream. These different retention configurations allow storage cost control and meet security and compliance requirements in a more granular way.
A common use case for custom policies is to delete high-frequency logs earlier than other (low-frequency) logs.

### Goals

* The user can enable the retention via the `LokiStack` custom resource.
* The user can declare per-tenant and global policies through the LokiStack custom resource. These are ordered by priority. 
* The policies support time-based deletion of older logs.


### Non-Goals

* Stress-testing the compactor on each T-shirt-size with an overwhelming amount of retention rules

## Proposal

The following enhancement proposal describes the required API additions and changes in the Loki Operator to add support for configuring custom log retention per tenant
https://grafana.com/docs/loki/latest/operations/storage/retention/
https://grafana.com/docs/loki/latest/configuration/#compactor_config

### API Extensions

#### LokiStack Changes: Support for configuring log retention


```go

import "github.com/prometheus/prometheus/model/labels"

// LokiDuration defines the type for Prometheus durations.
//
// +kubebuilder:validation:Pattern:="((([0-9]+)y)?(([0-9]+)w)?(([0-9]+)d)?(([0-9]+)h)?(([0-9]+)m)?(([0-9]+)s)?(([0-9]+)ms)?|0)"
type LokiDuration string

// LokiStackSpec defines the desired state of LokiStack
type LokiStackSpec struct {
...
    // Retention defines the spec for log retention
    //
    // +optional
    // +kubebuilder:validation:Optional
    Retention *RetentionSpec `json:"retention,omitempty"`
...
}

// RetentionSpec defines the spec for the enabling retention in the Compactor.
type RetentionSpec struct {

    // DeleteDelay defines Delay after which chunks will be fully deleted during retention
    //
    // +optional
    // +kubebuilder:validation:optional
    DeleteDelay LokiDuration `json:"deletedelay,omitempty"`
}


// LimitsTemplateSpec defines the limits  applied at ingestion or query path.
type LimitsTemplateSpec struct {
...
    // RetentionLimits defines the configuration of the retention period.
    //
    // +optional
    // +kubebuilder:validation:Optional
    RetentionLimits *RetentionLimitSpec `json:"retention,omitempty"`
}

// RetentionLimitSpec configures the retention period and retention stream
type RetentionLimitSpec struct {
    // PeriodDays defines the log retention period.
    //
    // +optional
    // +kubebuilder:validation:Optional
    PeriodDays int `json:"period,omitempty"`

    // Stream defines the log stream.
    //
    // +optional
    // +kubebuilder:validation:Optional
    Stream []*StreamSpec `json:"stream,omitempty"`
}

// StreamSpec defines the map of per pod status per LokiStack component.
// Each component is represented by a separate map of v1.Phase to a list of pods.
type StreamSpec struct {
    // PeriodDays defines the log retention period.
    //
    // +optional
    // +kubebuilder:validation:Optional
    PeriodDays int `json:"period,omitempty"`
    // Priority defines the retenton priority.
    //
    // +optional
    // +kubebuilder:validation:Optional
    Priority int32 `json:"priority,omitempty"`
    // Selector is a set of labels to identify the log stream.
    //
    // +optional
    // +kubebuilder:validation:Optional
    Selector *labels.Matcher `json:"selector,omitempty"`
}
```

### Implementation Details/Notes/Constraints

```yaml
apiVersion: loki.grafana.com/v1beta1
kind: LokiStack
metadata:
  name: lokistack-dev
spec:
  size: 1x.extra-small
  storage:
    secret:
      name: test
      type: s3
  storageClassName: gp3-csi
  retention: 
    deleteDelay: 
  limits:
    global:
      retentionLimits:
        periodDays: 31
        stream:
          - selector:
              name: namespace
              type: equal
              value: dev
            priority: 1
            periodDays: 1
    tenants:
      tenanta:
        retentionLimits:
          periodDays: 7
          stream:
            - selector:
                name: namespace
                type: equal
                value: prod
              priority: 2
              periodDays: 14
            - selector:
                name: container
                type: equal
                value: loki
              priority: 1
              periodDays: 3
      tenantb:
        retentionLimits:
          periodDays:
          stream:
            - selector:
                name: container
                type: equal
                value: nginx
              priority: 1
              periodDays: 1
```
	
#### General constraints

### Risks and Mitigations

## Design Details
Retention is enabled in the cluster when the `retention` block is added to the Lokstack custom resource. `deleteDelay` is the time after which the compactor will delete marked chunks. boltdb-shipper indexes are refreshed from the shared store on components using it (querier and ruler) at a specific interval. This means deleting chunks instantly could lead to components still having reference to old chunks and so they could fail to execute queries. Having a delay allows for components to refresh their store and so remove gracefully their reference of those chunks. It also provides a short window of time in which to cancel chunk deletion in the case of a configuration mistake.
`DeleteWorkerCount` specifies the maximum quantity of goroutine workers instantiated to delete chunks. - https://grafana.com/docs/loki/latest/operations/storage/retention/#retention-configuration. The operator instantiates loki cluster of different t-shirt sizes. A pre-determined default value of `DeleteWorkerCount` per t-shirt size cluster is set to avoid issues like large number of goroutine workers instantiated on small clusters. The user cannot input `DeleteWorkerCount`.

Retention period is configured within the limits_config configuration section.

There are two ways of setting retention policies:

retention_period which is applied globally.
retention_stream which is only applied to chunks matching the selector

This can be confiured at a global level(applied to all tenants) or on a per-tenant basis.

The API configures RetentionLimit in the same way as configuring IngestionLimit/QueryLimit. During The Lokistack resource reconciliation, the configuration from the `global` section is added into the `limits_config` sextion of the loki-config.yaml and the configuration from the multiple `tenants` is provided in the running_config file in the overrides section. 


Once the configuration is read, the following rules are applied to decide the retention period

A rule to apply is selected by choosing the first in this list that matches:

If a per-tenant retention_stream matches the current stream, the highest priority is picked.
If a global retention_stream matches the current stream, the highest priority is picked.
If a per-tenant retention_period is specified, it will be applied.
The global retention_period will be selected if nothing else matched.
If no global retention_period is specified, the default value of 744h (30days) retention is used.


### Open Questions [optional]

## Implementation History

## Drawbacks
User is not allowed to input the `DeleteWorkerCount` value

## Alternatives

