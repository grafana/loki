---
title: Zone-Aware Replication Support
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

## Summary

By default, data is transparently replicated across the whole pool of service instances, regardless of whether these instances are all running within the same availability zone (or data center, or rack) or in different ones. Storing multiple replicas for a given data within the same availability zone poses a risk for data loss if there is an outage affecting various nodes within a zone or a full zone outage. There is support for zone-aware data replication in Loki. When enabled, replicas for the given data are guaranteed to span across different availability zones.
A zone represents a logical failure domain. It is common for Kubernetes clusters to span multiple zones for increased availability. While the exact definition of a zone is left to infrastructure implementations, common properties of a zone include very low network latency within a zone, no-cost network traffic within a zone, and failure independence from other zones.
The following sections describe a set of APIs in form of Custom Resource Definitions (CRD) that enable users of `LokiStack` resources to support:

- Enable Zone-Aware Replication configuration in LokiStack CR so the components are deployed in different zones.
- Configure Loki to span data replication across multiple zones.

## Motivation

Zone-aware replication is the replication of data across failure domains. Avoiding data loss during a domain outage is the motivation to introduce a zone-aware component deployment and enable Loki's zone-aware data replication capabilities.

### Goals

- The user can enable zone-aware replication in the Loki operator
- The LokiStack administrator can enable zone-aware data replication for a managed Loki cluster.
- The LokiStack administrator can choose at least one topology label to enable the cluster spread across domains

### Non-Goals

- Minimize cross-zone traffic costs.
- Introduce data replication across Loki clusters in different regions.

## Proposal

The following enhancement proposal describes the required API additions and changes in the Loki Operator to add zone-aware data replication support.

### API Extensions

#### LokiStack Changes: Support for configuring zone-aware data replication

The following API changes introduce a new spec to enable configuration for Loki's data replication properties.

_Note:_ The new `replicationSpec` introduces a `factor` field that is a replacement for the old `replicationFactor` field. Moving forward the old field is officially deprecated and will be removed in future CRD versions.

```go
import "github.com/prometheus/prometheus/model/labels"


// LokiStackSpec defines the desired state of LokiStack
type LokiStackSpec struct {
...
    // ReplicationFactor defines the policy for log stream replication. (Deprecated: Please use replication.factor instead. This field will be removed in future versions of this CRD)
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Replication Factor"
    ReplicationFactor int32 `json:"replicationFactor,omitempty"`
 
    // Replication defines the configuration for Loki data replication
    //
    // +optional
    // +kubebuilder:validation:Optional
    Replication *ReplicationSpec `json:"replication,omitempty"`
}

type ReplicationSpec struct {
    // Factor defines the policy for log stream replication.
    //
    // +optional
    // +kubebuilder:validation:Optional
    Factor int32 `json:"factor,omitempty"`

    // Zone is the key that defines a topology in the Nodes' labels
    //
    // +required
    // +kubebuilder:validation:Required
    Zones []ZoneSpec
}

// ZoneSpec defines the spec to support zone-aware component deployments.
type ZoneSpec struct {
    // MaxSkew describes the maximum degree to which Pods can be unevenly distributed
    //
    // +required
    // +kubebuilder:default:="1"
    MaxSkew int `json:"maxSkew,omitempty"`

    // Topologykey is the key that defines a topology in the Nodes' labels
    //
    // +required
    // +kubebuilder:validation:Required
    Topologykey string `json:"topologyKey,omitempty"`
}

```

### Implementation Details/Notes/Constraints

The following manifest represents a full example of a LokiStack with zone-aware data replication turned on using the `topology.kubernetes.io/zone` node label as a key to spread pods across zones and a replication factor of three:

```yaml
apiVersion: loki.grafana.com/v1beta1
kind: LokiStack
metadata:
  name: lokistack-dev
spec:
  size: 1x.small
  storage:
    secret:
      name: test
      type: s3
  storageClassName: gp3-csi
  replication:
    factor: 3
    zones:
    - topologyKey: topology.kubernetes.io/zone
      maxSkew: 1
```

#### General constraints

### Risks and Mitigations

## Design Details

### 1. Spreading the pods across failure domains using the Kubernetes scheduler (using the PodTopologySpreadConstraint)

The Loki components can be divided into the Write Path (Distributor, Ingester) and the Read Path (Query frontend, Querier, Ingester). In the first pass of the feature implementation, we will not distinguish between the two paths. See the `Drawbacks` section for more details on this.
In this step, all components and their replicas are configured to spread over different zones. This can be done by making use of the [`PodTopologySpreadConstraint` feature in Kubernetes](https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/).

Commonly used node labels to identify failure-domains are

- topology.kubernetes.io/region
- topology.kubernetes.io/zone
- kubernetes.io/hostname

Kubernetes makes a few assumptions about the structure of zones and regions:

- regions and zones are hierarchical: zones are strict subsets of regions and no zone can be in 2 regions
- zone names are unique across regions; for example region "africa-east-1" might be comprised of zones "africa-east-1a" and "africa-east-1b"

The user needs to be aware of the node labels set to identify the different topology domains. Node read operations are generally made available to admin/developer users in OCP. This should be provided in the Lokistack CR topology key so that the `podTopologySpreadConstraint` can use this to schedule the pods accordingly.

These are some of the checks needed before the deployment happens:

- Check if the topology-key provided by the user matches a valid node topology domain label
- Verify that the replication factor defined is less than the number of available zones

If both these conditions are satisfied, deploy the lokistack components using the `PodTopologySpreadConstraint` definition in the deployments/statefulsets Pod template spec.

### 2. Identifying the failure-domain a pod is currently scheduled in (the two solutions discussed in length)

The second step is to identify the domain the pod is scheduled in and then add this value into the Loki configuration file (`loki-config.yaml`). The configurations required for adding zone-aware information in the Loki configuration is discussed here(see [Loki config](https://grafana.com/docs/loki/latest/configuration/#ring)). There is no easy way to implement this since the Kubernetes Downward-API does not support exposing node labels within containers. (see [Kubernetes issue](https://github.com/kubernetes/kubernetes/issues/40610))

A couple of solutions to implement passing the node labels to the Loki pods are listed below:

#### a. Manage through an operator

The loki-operator watches the pods when they are scheduled. If the `pod.spec.topologySpreadConstraints.topologyKey` is set, then the operator extracts the topology key and value from the node where the pod is scheduled, and sets it as an annotation for the pod by patching the pod. For reference consider:

- [elastic/cloud-on-k8s implementation](https://github.com/elastic/cloud-on-k8s/blob/85aa75b7a7765330475c0b92a0851939ac3d9578/pkg/controller/elasticsearch/driver/node_labels.go#L57)
- [elastic/cloud-on-k8s discussion](https://github.com/elastic/cloud-on-k8s/issues/3933#issuecomment-962897004)

#### b. Manage through a webhook

Here, the plan is to introduce an Admission Mutating Webhook, that would watch the `pods/binding` sub-resource for each Loki pods. This webhook can update the pod annotations to add the topology key-value pair(s) when it is being scheduled on a node.

Catch API calls to `pods/binding` sub-resource using a webhook:

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
 name: mutating-pod-webhook-configuration
webhooks:
 - clientConfig:
     service:
       name: webhook-service
       namespace: system
       path: /pod-binding-loki-grafana-com-v1
   name: podbinding.loki.grafana.com
   objectSelector:
     matchLabels:    
       "app.kubernetes.io/instance": "lokistack-dev"
   rules:
     - apiGroups:
         - ""
       apiVersions:
         - v1
       resources:
         - bindings
         - pods/binding
       operations:
         - CREATE
```

Decoding the binding request provides the target node to read the topology labels from (e.g. `aws-node-0`):

```yaml
{
 "name": "sample-request-0",
 "namespace": "default",
 "operation": "CREATE",
 "userInfo": {
   "username": "system:kube-scheduler",
   "uid": "uid:system:kube-scheduler",
   "groups": ["system:authenticated"]
 },
 "object": {
   "kind": "Binding",
   "apiVersion": "v1",
   "metadata": {
     "name": "sample-request-0",
     "namespace": "default",
   },
   "target": {
     "kind": "Node",
     "name": "aws-node-0"
   }
 }
}
```

Finally injecting the node topology labels into each pod as annotations:

```go
func (wh *mutatingWebhook) Handle(ctx context.Context, request admission.Request) admission.Response {
  binding := &v1.Binding{}

  // Decode the /bind request
  err := wh.decoder.DecodeRaw(request.Object, binding)
  if err != nil {...}


  if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
     // 1. Read the current Pod specification
     key := client.ObjectKey{Namespace: binding.ObjectMeta.Namespace, Name: binding.ObjectMeta.Name}
     pod := &v1.Pod{}
     if err := wh.client.Get(ctx, key, pod); err != nil {...}
     // get the topology keys from the pods where the pod.spec.topologySpreadConstraints.topologyKey is set
     kv, err := getTopologyKeyValue(ctx, wh.client, binding.Target.Name, pod.spec.topologySpreadConstraints.topologyKey)
     if err != nil {...}
     if pod.Annotations == nil {
        pod.Annotations = make(map[string]string)
     }
     // 2. Add topology keys to the Pod's annotations
     for k, v := range kv {
        if k == topologyKey {
          pod.Annotations[k] = v
        }
     }
     // 3. Update the Pod
     return wh.client.Update(ctx, pod)
  }); err != nil {...}
}
```

Some reference implementations on how to use the a `pods/binding` sub-resource mutating webhook can be found here:

- [elastic/cloud-on-k8s](https://github.com/elastic/cloud-on-k8s/issues/3933#issuecomment-950581345)
- [kubemod/kubemod](https://github.com/kubemod/kubemod/commit/95c43bc7d532db05051a83df5ad97e36a0f5d7e2#diff-d63889fb133b18cb690d365c25f43bf0580f7a7aaa91ffa03b121b9b6b5f2d60)

### 3. Relaying the failure-domain information to the Loki configuration (using the instance_availability_zone configuration option & dynamically set environment variables)

The pod annotation (having the zone information) obtained as a result of the previous step, can then be used in the container as an ENV variable and be used in the loki-config.yaml

#### a. Use DownwardAPI Volume mount in the application container

In this approach we introduce a script in the application container which runs a loop waiting for the pod annotation value to be non-empty. Using the downwardAPI volume approach lets us know the updated value of the pod annotation, without any pod restart. (See <https://kubernetes.io/docs/tasks/inject-data-application/downward-api-volume-expose-pod-information/#store-pod-fields>)

Once the domain value information is collected via the volume, it can be used to update a ENV variable that is used in the loki-config.yaml. The loki application is then started. In this way we can be sure that the loki application will have the domain information

This is how the expected individual pod spec will look after the topology key annotation is added to the pod

```yaml
kind: Pod
apiVersion: v1
metadata:
  name: ingester
  annotations: 
    topology.kubernetes.io/zone: zone-a 
spec:
  topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: topology.kubernetes.io/zone
    whenUnsatisfiable: DoNotSchedule
    labelSelector:
      matchLabels:
        ingester: pod
  containers:
  - name: ingester
    env:
    - name: ZONE
      value: ""
    command: ["sh", "-c"]
        args:
        - while true; do
            if [[ -e /etc/podinfo/annotations ]]; then
              if [[ -s /etc/podinfo/annotations ]]; then
                echo -en '\n\n'; cat /etc/podinfo/annotations;
                ZONE=$(cat /etc/podinfo/annotations); 
              else
                echo "Empty File"; fi; fi;
            sleep 5;
          done;
    volumeMounts:
      - name: podinfo
        mountPath: /etc/podinfo
  volumes:
    - name: podinfo
      downwardAPI:
        items:
          - path: "annotations"
            fieldRef:
              fieldPath: metadata.annotations['topology.kubernetes.io/zone']
```

##### Drawbacks of Solution a

- The status of the application pod during the waiting phase is unknown
- The liveness&readiness probes will fail since the loki application would not have started

#### b. Introduce an Init Container

In this approach we introduce a conditional init container which is used only if `zone-aware` is enabled. The Init container has the task of checking for the pod annotation to be set to the topologykey. Once this value is succesfully set, the main application container is started, and an ENV variable is set that is used in the loki-config.yaml. The loki application is then started. In this way we can be sure that the loki application will have the domain information

```yaml
kind: Pod
apiVersion: v1
metadata:
  name: ingester
  annotations: 
    topology.kubernetes.io/zone: zone-a 
spec:
  topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: topology.kubernetes.io/zone
    whenUnsatisfiable: DoNotSchedule
    labelSelector:
      matchLabels:
        ingester: pod
  initContainers:
  - name: init-envval
    image: busybox:1.28
    command: ['sh', '-c', "until cat /etc/podinfo/annotations; do echo waiting for toppologykey; sleep 2; done"]
    volumeMounts:
      - name: podinfo
        mountPath: /etc/podinfo
  containers:
  - name: ingester
    env:
    - name: ZONE
      valueFrom:
        fieldRef:
          fieldPath: metadata.annotations['topology.kubernetes.io/zone']
  volumes:
    - name: podinfo
      downwardAPI:
        items:
          - path: "annotations"
            fieldRef:
              fieldPath: metadata.annotations['topology.kubernetes.io/zone']
```

##### Drawbacks of Solution b

- Maintain an image for the new init-container. Since the init-container's work is very basic, ee could try to see if we can use an existing lightweight container image for our purpose. That way we can avoid the additional work of maintaining a new image.

The container can then be started by passing the configuration via the cli flag `instance_availability_zone: $ZONE`

### Cluster uses multiple topology keys

PodTopologyConstraint in Kubernetes is able to handle multiple topology keys and can schedule the pods as expected. But, there is no documentation on how Cortex/Loki handles multiple topology keys. Until further information is found, a simple proposal is to concatenate the values of the different topology keys and create the $ZONE variable for the loki configuration. For example:

```plain
# node labels
topology.kubernetes.io/zone: "zone-a"
kubernetes.io/hostname: "ip-172-20-114-199.ec2.internal"
# possible concatenation of the values
ZONE: "zone-a-ip-172-20-114-199.ec2.internal"
```

### Open Questions

- Both these methods first modify the pod to add the annotation[topology.kubernetes.io/zone: zone-a] in the pod and then modify the container to add a new env var or a volume which picks the zone value from the pod annotation via the Downward-Api. For the first step should we pick the webhook method or the operator method to set the pod annotation?
- For the second step should we continue to use just one application container or should we introduce an init container?
- Can a startupprobe be used instead of the init container?

## Implementation History

## Drawbacks

### 1. Read and Write Path

As suggested in the `Design` section there is no separate enabling of the Read and the Write path in the initial pass of the feature implementation. The benefits of enabling this separately is that, during live migrations we can first turn on write path zone awareness, leave it enabled for the max query lookback period, and then enable read path zone awareness. This means that at the time we turn on the read path zone awareness all ingesters that should have data for a stream will have all the data for that stream. If you enable the read path zone awareness earlier, then potentially data would be missing in queries. At this point the loki-operator deploys all the components in an all or nothing fashion. So the users should expect missing queries in the data for the max query lookback period. To avoid missing the queries, the operator needs to update the deployment process to two stages - first the components in the write path are deployed, wait the max query lookback period(currently 30s), followed by the deployment of the components in the read path.

### 2. Topology Key limitations

By just using the topology key to deploy the replicas in the different zones, we only ensure that 2 pods of different zones are not in the same node. But we cant control 2 replica pods in the same zone getting deployed on the same node. To prevent this we might want to let the user input zone details that can be used as a NodeSelector

## Alternatives

To enable zone-aware replication for the write path and the read path:

- Configure the availability zone for each ingester via the `-ingester.availability-zone` CLI flag (or its respective YAML config option)
- Rollout ingesters to apply the configured zone
- Enable time-series zone-aware replication via the `-distributor.zone-awareness-enabled` CLI flag (or its respective YAML config option). Please be aware this configuration option should be set to `distributors`, `queriers` and `rulers`.
<https://github.com/grafana/rollout-operator>

## Impact on LokiStack Sizing Configurations

Without zone-aware replication, the LokiStack pods are scheduled on different nodes within the same or different availability zones. There are no hard requirements on the number of zones within the cluster. In case a zone fails, the pods are expected to be rescheduled on a different zone, but they will not be successful because the PV cannot be moved automatically, and we cannot create new pods in a different zone which can use the old PVs. At this point a manual interventation is the only way to fix the issue, by deleting the old PVCs so that new PVs are created that can be used in the new zone.

If there are more than `floor(replication_factor/2)` zones with failing instances, reads and writes would not be possible and there would be a high chance of data loss. According to [Cortex](https://cortexmetrics.io/docs/guides/zone-aware-replication/#minimum-number-of-zones), the minimum number of zones should be equal to the replication factor. Which means the following for our production t-shirt sizes:
- `1x.small` has a replication factor of 2 & all components have 2 replicas. In order to replicate a `1x.small` LokiStack across zones, there has to be at least 2 zones available in the cluster. Each replica of the LokiStack pods will be scheduled on a node in a different zone. For example: zone-a and zone-b will have 1 replica each of `distributor`, `ingester`, `querier`, `query-frontend`, `gateway`, `index-gateway`, and `ruler`.

  The issue arises when we only have 2 zones, when one of them fails, the write-path cannot hold on if data ingestion rate is high, in turn data loss is possible. This can be overcome by only enabling zone-aware replication if the number of existing zones are `replication_factor + 1` (3 in this case, which is also the default for Cortex implementations). This solution is possible since most public cloud providers have 3 availability zones per region.

  Switching a `1x.small` LokiStack to being zone-aware: setting `replication.topology.enabled` in the LokiStack CR to `true` should be sufficient to restart the pods and reschedule them on different zones (given that the requirements are met).

- `1x.medium` has a replication factor of 3, with the `ingester` and `querier` having 3 replicas, while the `distributor`, `query-frontend`, `gateway`, `index-gateway`, and `ruler` having only 2 replicas. This means 1 zone will have only 1 `querier` and 1 `ingester` that will communicate with components (e.g. distributors) in other zones . In this case, losing one zone cannot be tolerated, and relying on having a 4th zone isn't always possible. In this case it is preferable to suggest using a replication factor of 2 instead of the default set to 3.

### Zone Failure

- This proposal addresses zone-aware data replication only. This means that we need some sort of playbook to execute fail-over activities during a zone outage.
- The other option is to expand the operator capabilities to activate fail-over from one zone to the other.
In summary we have consensus to err on the side of a simpler feature going forward with option no.1. The latter option demands to re-visit the capabilities of the Loki Operator managing StatefulSets (e.g. ingesters) via a custom controller to enable fail-over across zones. This was identified to be a prerequisite to the zone-aware data replication feature as described in the proposal.

## Open Questions

- `lokiconfig.yaml` should be updated to include the `zone_awareness_enabled=true`. The `instance_availability_zone` will be different in the pods of different zones.
- Should we use the existing rollout-operator?  [rollout-operator](https://github.com/grafana/rollout-operator)? since this operator is able to handle pods to be deployed automatically in different zones.
- How can we minimize cross-zone traffic? (how to make queriers and ingesters zone-aware, so that each querier will only query ingesters in the same zone?)
- How do we simulate zone failure in an OCP cluster?

## References

1. https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/
2. https://docs.openshift.com/container-platform/4.12/nodes/scheduling/nodes-scheduler-pod-topology-spread-constraints.html
3. https://cortexmetrics.io/docs/guides/zone-aware-replication/
