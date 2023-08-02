---
title: "Recovery Procedure for Loki Availability Zone Failures"
description: "Recovery Procedure for Loki Availability Zone Failures"
lead: ""
date: 2023-07-25T08:48:45+00:00
lastmod: 2023-07-25T08:48:45+00:00
draft: false
images: []
menu:
  docs:
    parent: "user-guides"
weight: 100
toc: true
---

**Disclaimer:** This document describes a recovery procedure by manually recreating the failed pods in another zone. Right now, we are doing this by deleting PVCs of the impacted pods from the failed zone, so they can be recreated in a different zone. This will cause data loss of the data in the PVC. To avoid actual data loss we always set the replication factor in loki to be 2 or higher so data is always replicated.

## Why

In a Kubernetes/OpenShift cluster, a "zone failure" refers to a situation where nodes or resources in a specific availability zone become unavailable. An availability zone is a distinct location within a cloud provider's data center or region, designed to be isolated from failures in other zones to provide better redundancy and fault tolerance. When a zone failure occurs, it can lead to a loss of services or data if the cluster is not configured properly to handle such scenarios.

This document outlines steps that can be taken to recover stateful Loki pods when there is a zone failure. Stateful Loki pods are deployed as a part of a Statefulset. The Statefulset also has PVCs associated with the pods which are dynamically provisioned through the use of a StorageClass. Each stateful Loki pod and its associated PVCs are deployed in the same zone.

## Checks

 1. **Ensure data replication enabled**

    As discussed in the disclaimer above. The following procedure will delete the PVCs in the failed zone and the data held there. To avoid complete data loss the replication factor in the `LokiStack` CR should always be set to a value greater than 1. This ensures that loki is replicating the data and even if a zone is lost there should be already be copies of the data in another zone.

    ```yaml
    apiVersion: loki.grafana.com/v1
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
      factor: 2
      zones:
      - topologyKey: topology.kubernetes.io/zone
        maxSkew: 1
    ```
## Steps

When a zone failure occurs in a cluster, the StatefulSet controller will automatically attempt to recover the affected pods in the failed zone. The following steps outline the additional manual intervention required to make sure that the stateful Loki pods are succesfully recreated in a new zone.

 1. **Detect Zone Failure** - The control plane and cloud provider integration should mark nodes in the failed zone.

 2. **Reschedule Pods** - The StatefulSet controller will automatically attempt to reschedule the pods that were running in the failed zone to nodes in another zone.
  
 3. **Recover Pods and PVCs** - Since the Statefulsets have PVCs which are also in the failed zone, automatic reschedule of the stateful Loki pods to a different zone will not work. See - [Storage access for zones](https://kubernetes.io/docs/setup/best-practices/multiple-zones/#storage-access-for-zones). Manual intervention is required at this point to delete the old PVCs in the failed zone to allow succesful recreation of the stateful Loki Pod & PVC in the 
  
    3.1 **List pending pods**
    
    Multiple stateful Loki pod that is in a pending state, after the statefulsets have unsuccesfully tried to reschedule them to a different zone.
    The stateful Loki pods is in `Pending` state because they cannot be scheduled in a new node in a different zone:

    ```console
    kubectl get pods --field-selector status.phase==Pending -n openshift-logging
    NAME                            READY   STATUS    RESTARTS   AGE
    lokistack-dev-index-gateway-1   0/1     Pending   0          17m
    lokistack-dev-ingester-1        0/1     Pending   0          16m
    ```

    3.2 **List pending PVCs**
    
    The above pods are in phase `Pending` because their corresponding PVCs are  in the old zone.

    ```console
    kubectl get pvc -o=json -n openshift-logging | jq '.items[] | select(.status.phase == "Pending") | .metadata.name' -r
    storage-lokistack-dev-index-gateway-1
    storage-lokistack-dev-ingester-1
    wal-lokistack-dev-ingester-1
    ```

    3.3 **Delete pending PVCs, followed by pending pods**
    
    After successful deletion the pods and new PVCs should now be recreated in an available zone because the statefulset has a set number of replicas.

    ```console
    kubectl delete pvc storage-lokistack-dev-ingester-1 -n openshift-logging
    kubectl delete pvc wal-lokistack-dev-ingester-1 -n openshift-logging
    kubectl delete pod lokistack-dev-ingester-1 -n openshift-logging
    
    kubectl delete pvc storage-lokistack-dev-index-gateway-1 -n openshift-logging
    kubectl delete pod lokistack-dev-index-gateway-1 -n openshift-logging
    ```

These steps should be followed for all stateful Loki pods that are in the failed zone.

## Troubleshooting

### PVCs are stuck in Terminating state

If the PVCs are stuck in a terminating state and are not getting deleted it could be because of the finalizer. The reason why its not terminating is because the PVC metadata finalizers are set to `kubernetes.io/pv-protection`

These steps could remove the finalizer and allow the PVC to be deleted

```console
kubectl patch pvc wal-lokistack-dev-ingester-1 -p '{"metadata":{"finalizers":null}}' -n openshift-logging
kubectl patch pvc storage-lokistack-dev-ingester-1 -p '{"metadata":{"finalizers":null}}' -n openshift-logging
```
