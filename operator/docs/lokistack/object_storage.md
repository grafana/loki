---
title: "Object Storage"
description: "Setup for storing logs to Object Storage"
lead: ""
date: 2022-06-21T08:48:45+00:00
lastmod: 2022-06-21T08:48:45+00:00
draft: false
images: []
menu:
  docs:
    parent: "lokistack"
weight: 100
toc: true
---

Loki Operator supports [AWS S3](https://aws.amazon.com/), [Azure](https://azure.microsoft.com), [GCS](https://cloud.google.com/), [Minio](https://min.io/), [OpenShift Data Foundation](https://www.redhat.com/en/technologies/cloud-computing/openshift-data-foundation) and  [Swift](https://docs.openstack.org/swift/latest/) for LokiStack object storage.

## AWS S3

Coming soon.

### Requirements

### Installation

## Azure

Coming soon.

### Requirements

### Installation

## Google Cloud Storage

Loki Operator supports [GCS](https://cloud.google.com/) for Loki storage.

### Requirements

* Create a [project](https://cloud.google.com/resource-manager/docs/creating-managing-projects) on Google Cloud Platform.
* Create a [bucket](https://cloud.google.com/storage/docs/creating-buckets) under same project.
* Create a [service account](https://cloud.google.com/docs/authentication/getting-started#creating_a_service_account) under same project for GCP authentication.

### Installation

* Deploy the loki operator to your cluster.

* Copy the service account credentials received from GCP into a file name `key.json`.

* Create an Object Storage secret with keys `bucketname` and `key.json` as follows:

    ```console
    kubectl create secret generic test \
      --from-literal=bucketname="<BUCKET_NAME>" \
      --from-file=key.json="<PATH/TO/KEY.JSON>"
    ```
  
    where `test` is the secret name, `<BUCKET_NAME>` is the name of bucket created in requirements step and `<PATH/TO/KEY.JSON>` is the file path where the `key.json` was copied to.

* Create an instance of [lokistack](../hack/lokistack_dev.yaml) by referencing the secret name and type as `gcs`:

  ```yaml
  spec:
    storage:
      secret:
        name: test
        type: gcs
  ```

## Minio

Coming soon.

### Requirements

### Installation

## OpenShift Data Foundation

Coming soon.

### Requirements

### Installation

## Swift

Coming soon.

### Requirements

### Installation

