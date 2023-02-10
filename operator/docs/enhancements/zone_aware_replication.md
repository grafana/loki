---
title: Zone Awareness Replication Support
authors:
  - "@shwetaap"
  - "@btaani"
reviewers:
  - "@xperimental"
  - "@periklis"
draft: false
menu:
  docs:
    parent: "enhancements"
weight: 100
toc: true
---

# Table of Contents
1. [Summary](#summary)
2. [Motivation](#motivation)
3. [Proposal](#proposal)
    1. [API Extensions](#api-extensions)
    2. [Implementation Details/Notes/Constraints](#implementation-detailsnotesconstraints)
4. [Design Details](#design-details)
5. [Impact on LokiStack Sizing Configurations](#sizing)

## Summary

By default, data is transparently replicated across the whole pool of service instances, regardless of whether these instances are all running within the same availability zone (or data center, or rack) or in different ones. Storing multiple replicas for a given data within the same availability zone poses a risk for data loss if there is an outage affecting various nodes within a zone or a full zone outage. There is support for Zone Aware Replication in Loki. When enabled, replicas for the given data are guaranteed to span across different availability zones.
A zone represents a logical failure domain. It is common for Kubernetes clusters to span multiple zones for increased availability. While the exact definition of a zone is left to infrastructure implementations, common properties of a zone include very low network latency within a zone, no-cost network traffic within a zone, and failure independence from other zones.
The following sections describe a set of APIs in form of Custom Resource Definitions (CRD) that enable users of `LokiStack` resources to support:
- Enable Zone-Aware Replication configuration in LokiStack CR so the components are deployed in different zones.
- Configure `loki-config.yaml` to support zone-aware data replication.

## Motivation

Zone-aware replication is the replication of data across failure domains. Avoiding data loss during a domain outage is the motivation to introduce zone-aware component deployment and data replication. 

### Goals

* The user can enable zone aware replication for the read and the write path
* List options on how to make a distinct list of availability zones for Loki components in the LokiComponentSpec
* Propose which `TopologySpreadConstraint` fields should and which should not be exposed in the LokiComponentSpec to simplify CR usage.


### Non-Goals

* 

## Proposal

The following enhancement proposal describes the required API additions and changes in the Loki Operator to add zone-aware replication support

### API Extensions

#### LokiStack Changes: Support for configuring log retention


```go

import "github.com/prometheus/prometheus/model/labels"


//  LokiStackSpec defines the desired state of LokiStack
type LokiStackSpec struct {
...
    // ZoneAware defines the configuration to support zone aware component deployments
    //
    // +optional
    // +kubebuilder:validation:Optional
    ZoneAware *ZoneAwareSpec `json:"zoneaware,omitempty"`
...
}

// ZoneAwareSpec defines the spec to support zone aware component deployments.
type ZoneAwareSpec struct {

    // Enabled defines a flag to enable/disable the zone awareness flags in teh configuration
    //
    // +required
    // +kubebuilder:validation:Required
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch",displayName="Enable"
    Enabled bool `json:"enabled"`

    // MaxSkew describes the maximum degree to which Pods can be unevenly distributed
    //
    // +optional
    // +kubebuilder:validation:optional
    // +kubebuilder:default:="1"
    MaxSkew int `json:"maxSkew,omitempty"`

    // Topologykey is the key that defines a topology in the Nodes' labels
    //
    // +optional
    // +kubebuilder:validation:required

    Topologykey string `json:"topologyKey,omitempty"`

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
  storageClassName: gp2
  zoneAware:
    enabled: true
    maxskew: 2
    topologyKey: topology.kubernetes.io/zone
```
	
#### General constraints

### Risks and Mitigations

## Design Details
The loki components can be divided in the Write Path (Distributor, Ingester) and the Read Path (Query fronted, Querier, Ingester) .
The first step would be to make sure that each of these componemts are configured to be deployed over different zones. This can be done making using of the `PodTopologySpreadConstraint` feature in Kubernetes.
Kubernetes makes a few assumptions about the structure of zones and regions:
* regions and zones are hierarchical: zones are strict subsets of regions and no zone can be in 2 regions
* zone names are unique across regions; for example region "africa-east-1" might be comprised of zones "africa-east-1a" and "africa-east-1b"
Commonly used node labels to identify domains are 
* topology.kubernetes.io/region
* topology.kubernetes.io/zone
* kubernetes.io/hostname
The user needs to be aware of the node labels set to identify the different topology domains. Node read operations are generally made available to admin/developer users in OCP. This should be provided in the Lokistack CR Topologykey so that the `podTopologySpreadConstraint` can use this schedule the pods accordingly 

* Check if the Topology key provided by the user matches a valid node topology domain label
* Verify that the replication factor (default value 3) defined is less than the number of available zones

If both these conditions are satisfied, deploy the lokistack components by being zone-aware using the following construct in their PodSpec (or in the template field in a deployment or a statefulset).
```yaml
kind: Pod
apiVersion: v1
spec:
  topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: topology.kubernetes.io/zone
    whenUnsatisfiable: DoNotSchedule
    labelSelector:
      matchLabels:
        ingester: pod
```

To enable zone-aware replication for the write path and the read path:

* Configure the availability zone for each ingester via the `-ingester.availability-zone` CLI flag (or its respective YAML config option)
* Rollout ingesters to apply the configured zone
* Enable time-series zone-aware replication via the `-distributor.zone-awareness-enabled` CLI flag (or its respective YAML config option). Please be aware this configuration option should be set to `distributors`, `queriers` and `rulers`.

## Implementation History

## Drawbacks
By just using the Topologykey to deploy the replicas in the different zones, we only ensure that 2 pods of different zones are not in the same node. But we cant control 2 replica pods in the same zone getting deployed on the same node. To prevent this we might want to let the user input zone details that can be used as a NodeSelector

## Alternatives
https://github.com/grafana/rollout-operator

<div id="sizing"/>

## Impact on LokiStack Sizing Configurations
Without zone-aware replication, the LokiStack pods are scheduled on different nodes within the same or different availability zones. There are no hard requirements on the number of zones within the cluster. In case a zone fails, the pods are simply rescheduled to other nodes based on the current policies (taints/tolerations/affinity).

If there are more than `floor(replication_factor/2)` zones with failing instances, reads and writes would not be possible and there would be a high chance of data loss. According to [Cortex](https://cortexmetrics.io/docs/guides/zone-aware-replication/#minimum-number-of-zones), the minimum number of zones should be equal to the replication factor. Which means the following for our production t-shirt sizes:
* `1x.small` has a replication factor of 2 & all components have 2 replicas. In order to replicate a `1x.small` LokiStack across zones, there has to be at least 2 zones available in the cluster. Each replica of the LokiStack pods will be scheduled on a node in a different zone. For example: zone-a and zone-b will have 1 replica each of `distributor`, `ingester`, `querier`, `query-frontend`, `gateway`, `index-gateway`, and `ruler`.

  The issue arises when we only have 2 zones, when one of them fails, the write-path cannot hold on if data ingestion rate is high, in turn data loss is possible. This can be overcome by only enabling zone-aware replication if the number of existing zones are `replication_factor + 1` (3 in this case, which is also the default for Cortex implementations). This is a possible solutions since most cloud provider regions have 3-5 availability zones.

  Switching a `1x.small` LokiStack to being zone-aware: setting `.zoneAware.enabled` in the LokiStack CR to `true` should be sufficient to restart the pods and reschedule them on different zones (given that the requirements are met).

* `1x.medium` has a replication factor of 3, with the `ingester` and `querier` having 3 replicas, while the `distributor`, `query-frontend`, `gateway`, `index-gateway`, and `ruler` having only 2 replicas, which means 1 zone will have only 1 `querier` and 1 `ingester` that will communicate with components in other zones with higher delay. In this case, losing one zone cannot be tolerated, and relying on having a 4th zone isn't always possible. In this case it is preferable to suggest a new sizing configuration.

### `1x.medium.multizone` as a new flavour of `1x.medium`
To increase availability and reduce cross-zone bandwidth, `2x.medium.multizone` ensures having 2 instances of ingesters and queriers in each zone if and only if zone-aware replication is enabled.

Details like the CPU/memory/disk requests can vary from `1x.medium` since the load is distributed among more replicas. This is something that should be benchmarcked.

```go
type LokiStackSizeType string

const (
...
   // SizeOneXMediumMultiZone defines the size of a single Loki deployment
   // with small resources/limits requirements and zone-aware replication support for all
   // Loki components. This size is dedicated for setup **with** the
   // requirement for single replication factor and auto-compaction.
   //
   SizeOneXMediumMultiZone LokiStackSizeType = "1x.medium.multizone"
...
)
```
```go
var StackSizeTable = map[lokiv1.LokiStackSizeType]lokiv1.LokiStackSpec{
  lokiv1.SizeOneXMediumMultiZone: {
    Size:              lokiv1.SizeOneXMediumMultiZone,
    ReplicationFactor: 3,
    Limits: &lokiv1.LimitsSpec{
      Global: &lokiv1.LimitsTemplateSpec{
        IngestionLimits: &lokiv1.IngestionLimitSpec{
          // Custom for 1x.medium.multizone
          IngestionRate:             50,
          IngestionBurstSize:        20,
          MaxGlobalStreamsPerTenant: 25000,
          // Defaults from Loki docs
          ...
        },
        QueryLimits: &lokiv1.QueryLimitSpec{
          // Defaults from Loki docs
          ...
        },
      },
    },
    Template: &lokiv1.LokiTemplateSpec{
      Compactor: &lokiv1.LokiComponentSpec{
        Replicas: 1,
      },
      Distributor: &lokiv1.LokiComponentSpec{
        Replicas: 3,
      },
      Ingester: &lokiv1.LokiComponentSpec{
				Replicas: 6,
			},
			Querier: &lokiv1.LokiComponentSpec{
				Replicas: 6,
			},
      QueryFrontend: &lokiv1.LokiComponentSpec{
        Replicas: 3,
      },
      Gateway: &lokiv1.LokiComponentSpec{
        Replicas: 3,
      },
      IndexGateway: &lokiv1.LokiComponentSpec{
        Replicas: 3,
      },
      Ruler: &lokiv1.LokiComponentSpec{
        Replicas: 3,
      },
    },
  }
...
}
```

  

## Open Questions
* `lokiconfig.yaml` should be updated to include the `zone_awareness_enabled=true`. The `instance_availability_zone` will be different in the pods of different zones. 
* Should we use the existing rollout-operator? since this operator is able to handle pods to be deployed automatically in different zones - https://github.com/grafana/rollout-operator
* How can we minimize cross-zone bandwidth? (how to make queriers and ingesters zone-aware, so that each querier will only query ingesters in the same zone?)
* Persistent volumes are bound to the zone they are created in. How do we re-bind the restarted pod to the PV after a zone failure?
* How do we simulate zone failure in an OCP cluster?



## References
1. https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/
2. https://docs.openshift.com/container-platform/4.12/nodes/scheduling/nodes-scheduler-pod-topology-spread-constraints.html
3. https://cortexmetrics.io/docs/guides/zone-aware-replication/

  
