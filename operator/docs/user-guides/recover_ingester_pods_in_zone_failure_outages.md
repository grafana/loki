---
title: "Playbook for recovering ingester pods in zone-failure outages"
description: "Playbook for recovering ingester pods in zone-failure outages"
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

This document describes how to recover the ingester pods in the event of a zone failure.

_Disclaimer:_ This document describes a recovery procedure by manually recreating the failed pods in another zone. Right now, we are doing this by deleting PVCs of the impacted pods from the failed zone, so they can be recreated in a different zone. This will cause data loss of the data in the PVC. To avoid actual data loss we always set the replication factor in loki to be 1 or higher so data is always replicated.

## Ingester recovery during a Zone Failure

In a Kubernetes/Openshift cluster, a "zone failure" refers to a situation where nodes or resources in a specific availability zone become unavailable. An availability zone is a distinct location within a cloud provider's data center or region, designed to be isolated from failures in other zones to provide better redundancy and fault tolerance. When a zone failure occurs, it can lead to a loss of services or data if the cluster is not configured properly to handle such scenarios.

This document outlines steps that can be taken to recover ingester pods when there is a zone failure. Ingester pods are deployed as a part of a Statefulset. The Ingester Statefulset also has PVCs associated with the pods which are dynamically provisioned through the use of StorageClass. Each Ingester Pod and its associated PVCs are deployed in the same zone.

When a zone failure occurs in a cluster, the StatefulSet controller will automatically attempt to recover the affected pods in the failed zone. This document outlines the additional manual intervention required to make sure that the ingester pods are succesfully recreated in a new zone.

* Detect Zone Failure - The control plane and cloud provider integration should mark nodes in the failed zone.

* Reschedule Pods: The Ingester StatefulSet controller will automatically attempt to reschedule the pods that were running in the failed zone to nodes in another zone.
  
* Since the Statefulset has PVCs which are also in the failed zone, automatic reschedule of the Ingester pods to a different zone will not work. See - [Storage access for zones](https://kubernetes.io/docs/setup/best-practices/multiple-zones/#storage-access-for-zones). Manual intervention is required at this point to delete the old pvcs in the failed zone to allow succesful recreation of the Ingester Pod & Pvc in the new zone.

    Example showing an ingester pod that is in a pending state, after the statefulset has unsuccesfully tried to reschedule it to a different zone.
    Ingester-1 is in Pending state because it cannot be scheduled in a new node in a different zone:

    ```console
    oc get pods -n openshift-logging
    NAME                                           READY   STATUS    RESTARTS   AGE
    lokistack-dev-ingester-0                       1/1     Running   0          4m21s
    lokistack-dev-ingester-1                       0/1     Pending   0          44s
    ```

    This is because the pvc for ingester-1 is in the old zone

    ```console
    oc get pvc -n openshift-logging
    NAME                                    STATUS    VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS   AGE
    storage-lokistack-dev-ingester-0        Bound     pvc-1be8a2c2-f594-4ce7-9139-d57de6e06ce9   10Gi       RWO            gp3-csi        12m
    storage-lokistack-dev-ingester-1        Pending                                                                        gp3-csi        8m24s
    wal-lokistack-dev-ingester-0            Bound     pvc-0239dca6-cf2c-4b67-94a8-e2e0fb94509d   150Gi      RWO            gp3-csi        12m
    wal-lokistack-dev-ingester-1            Pending                                                                        gp3-csi        8m24s
    ```

    Delete the PVCs, followed by the pod. The pod and new pvcs should now be recreated in an available zone because the statefulset has a set number of replicas

    ```console
    oc delete pvc storage-lokistack-dev-ingester-1 -n openshift-logging
    oc delete pvc wal-lokistack-dev-ingester-1 -n openshift-logging
    oc delete pod lokistack-dev-ingester-1 -n openshift-logging
    ```

These steps should be followed for all ingester pods that are in the failed zone.

As discussed in the disclaimer above. This process will delete the PVC in the failed zone and the data held there. To avoid complete data loss the replication factor in the lokistack CR should always be set to a value greater than 0. This ensures that loki is replicating the data and even if a zone is lost there should be already be copies of the data in another zone.

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

## Troubleshooting

### PVCs are stuck in Terminating state

If the PVCs are stuck in a terminating state and are not getting deleted it could be because of the finalizer. The reason why its not terminating is because the PVC metadata finalizers are set to `kubernetes.io/pv-protection`

These steps could remove the finalizer and allow the PVC to be deleted

```console
    oc patch pvc wal-lokistack-dev-ingester-1 -p '{"metadata":{"finalizers":null}}' -n openshift-logging
    oc patch pvc storage-lokistack-dev-ingester-1 -p '{"metadata":{"finalizers":null}}' -n openshift-logging
```
