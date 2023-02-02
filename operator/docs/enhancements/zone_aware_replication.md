---
title: Zone Awareness Replication Support
authors:
  - "@shwetaap"
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

## Summary

By default, data is transparently replicated across the whole pool of service instances, regardless of whether these instances are all running within the same availability zone (or data center, or rack) or in different ones. Storing multiple replicas for a given data within the same availability zone poses a risk for data loss if there is an outage affecting various nodes within a zone or a full zone outage. There is support fpr Zone Aware Replication in Loki. When enabled, replicas for the given data are guaranteed to span across different availability zones.
A zone represents a logical failure domain. It is common for Kubernetes clusters to span multiple zones for increased availability. While the exact definition of a zone is left to infrastructure implementations, common properties of a zone include very low network latency within a zone, no-cost network traffic within a zone, and failure independence from other zones.
The following sections describe a set of APIs in form of custom resource definitions (CRD) that enable users of `LokiStack` resources to support:
- Enable Zone Aware Replication configuration in LokiStack CR so the components are deployed in different zones.
- Configure the loki-config.yaml to support zoneaware data replication.

## Motivation

Zone-aware replication is the replication of data across failure domains. Avoiding data loss during a domain outage is the motivation to introduce zone aware component deployment and data replication. 

### Goals

* The user can enable zone aware replication for the read and the write path
* List options on how to make a distinct list of availability zones for Loki components in the LokiComponentSpec
* Propose which TopologySpreadConstraint fields should and which should not be exposed in the LokiComponentSpec to simplify CR usage.


### Non-Goals

* 

## Proposal

The following enhancement proposal describes the required API additions and changes in the Loki Operator to add zone aware replication support

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
  zoneaware:
    enabled: true
    maxskew: 2
    topologykey: topology.kubernetes.io/zone
```
	
#### General constraints

### Risks and Mitigations

## Design Details
The loki componects can be divided in the Write Path(Distributor, Ingester) amd the Read Path(Query fronted, Querier, Ingester) 
The first step would be to make sure that each of these componemts are configured to be deployed over different zomes. This can be done making using of the PodTopologySpreadConstraint feature in Kubernetes.
Kubernetes makes a few assumptions about the structure of zones and regions:
* regions and zones are hierarchical: zones are strict subsets of regions and no zone can be in 2 regions
* zone names are unique across regions; for example region "africa-east-1" might be comprised of zones "africa-east-1a" and "africa-east-1b"
Commonly used node labels to identify domains are 
* topology.kubernetes.io/region
* topology.kubernetes.io/zone
* kubernetes.io/hostname
The user needs to be aware of the node labels set to identify the different topology domains. Node read operations are generall made available to admin/developer users in OCP.  This should be provided in the Lokistack CR Topologykey so that the TopologySpreadConstraint can use this schedule the pods accordingly 

* Check if the Topology key provided by the user matches a valid node topology domain label
* Verify that the replication factor(default value 3) defined is less than the number of available zones

If both these conditions are satisfied, deploy the loki components by being zone aware using the following construct in their PodSpec.
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

To enable the zone-aware replication for the write path and the read path:

* Configure the availability zone for each ingester via the -ingester.availability-zone CLI flag (or its respective YAML config option)
* Rollout ingesters to apply the configured zone
* Enable time-series zone-aware replication via the -distributor.zone-awareness-enabled CLI flag (or its respective YAML config option). Please be aware this configuration option should be set to distributors, queriers and rulers.

### Open Questions [optional]
* The lokiconfig.yanl should be updated to include the `zone_awareness_enabled=true`. The `instance_availability_zone` will be different in the pods of differnt zones. 
* Should we use the exiting rollout-operator, since this operator is able to handle pods to be deploed automatically in different zones - https://github.com/grafana/rollout-operator

## Implementation History

## Drawbacks
By just using the Topologykey to deploy the replicas in the different zones, we only ensure that 2 pods of different zones are notin the same node. But we cant control 2 replica pods in the same zone getting deployed on the same node. To prevent this we might want to let the user input zone details that can be used as a Nodeselector

## Alternatives
https://github.com/grafana/rollout-operator
