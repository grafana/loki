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

_Note_: Upon setting up LokiStack for any object storage provider, you should configure a logging collector that references the LokiStack in order to view the logs.

## AWS S3

### Requirements

* Create a [bucket](https://docs.aws.amazon.com/AmazonS3/latest/userguide/create-bucket-overview.html) on AWS.

### Installation

* Deploy the Loki Operator to your cluster.

* Create an Object Storage secret with keys as follows:

    ```console
    kubectl create secret generic lokistack-dev-s3 \
      --from-literal=bucketnames="<BUCKET_NAME>" \
      --from-literal=endpoint="<AWS_BUCKET_ENDPOINT>" \
      --from-literal=access_key_id="<AWS_ACCESS_KEY_ID>" \
      --from-literal=access_key_secret="<AWS_ACCESS_KEY_SECRET>" \
      --from-literal=region="<AWS_REGION_YOUR_BUCKET_LIVES_IN>"
    ```

    where `lokistack-dev-s3` is the secret name.

* Create an instance of [LokiStack](../hack/lokistack_dev.yaml) by referencing the secret name and type as `s3`:

  ```yaml
  spec:
    storage:
      secret:
        name: lokistack-dev-s3
        type: s3
  ```

## Azure

### Requirements

* Create a [bucket](https://docs.microsoft.com/en-us/azure/storage/blobs/storage-blobs-introduction) on Azure.

### Installation

* Deploy the Loki Operator to your cluster.

* Create an Object Storage secret with keys as follows:

    ```console
    kubectl create secret generic lokistack-dev-azure \
      --from-literal=container="<AZURE_CONTAINER_NAME>" \
      --from-literal=environment="<AZURE_ENVIRONMENTs>" \
      --from-literal=account_name="<AZURE_ACCOUNT_NAME>" \
      --from-literal=account_key="<AZURE_ACCOUNT_KEY>" \
      --from-literal=endpoint_suffix="<OPTIONAL_AZURE_ENDPOINT_SUFFIX>"
    ```

    where `lokistack-dev-azure` is the secret name.

* Create an instance of [LokiStack](../hack/lokistack_dev.yaml) by referencing the secret name and type as `azure`:

  ```yaml
  spec:
    storage:
      secret:
        name: lokistack-dev-azure
        type: azure
  ```

## Google Cloud Storage

### Requirements

* Create a [project](https://cloud.google.com/resource-manager/docs/creating-managing-projects) on Google Cloud Platform.
* Create a [bucket](https://cloud.google.com/storage/docs/creating-buckets) under same project.
* Create a [service account](https://cloud.google.com/docs/authentication/getting-started#creating_a_service_account) under same project for GCP authentication.

### Installation

* Deploy the Loki Operator to your cluster.

* Copy the service account credentials received from GCP into a file name `key.json`.

* Create an Object Storage secret with keys `bucketname` and `key.json` as follows:

    ```console
    kubectl create secret generic lokistack-dev-gcs \
      --from-literal=bucketname="<BUCKET_NAME>" \
      --from-file=key.json="<PATH/TO/KEY.JSON>"
    ```
  
    where `lokistack-dev-gcs` is the secret name, `<BUCKET_NAME>` is the name of bucket created in requirements step and `<PATH/TO/KEY.JSON>` is the file path where the `key.json` was copied to.

* Create an instance of [LokiStack](../hack/lokistack_dev.yaml) by referencing the secret name and type as `gcs`:

  ```yaml
  spec:
    storage:
      secret:
        name: lokistack-dev-gcs
        type: gcs
  ```

## Minio

### Requirements

* Deploy Minio on your Cluster, e.g. using the [Minio Operator](https://operator.min.io/)

* Create a [bucket](https://docs.min.io/docs/minio-client-complete-guide.html) on Minio via CLI.

### Installation

* Deploy the Loki Operator to your cluster.

* Create an Object Storage secret with keys as follows:

    ```console
    kubectl create secret generic lokistack-dev-minio \
      --from-literal=bucketnames="<BUCKET_NAME>" \
      --from-literal=endpoint="<MINIO_BUCKET_ENDPOINT>" \
      --from-literal=access_key_id="<MINIO_ACCESS_KEY_ID>" \
      --from-literal=access_key_secret="<MINIO_ACCESS_KEY_SECRET>"
    ```

    where `lokistack-dev-minio` is the secret name.

* Create an instance of [LokiStack](../hack/lokistack_dev.yaml) by referencing the secret name and type as `s3`:

  ```yaml
  spec:
    storage:
      secret:
        name: lokistack-dev-minio
        type: s3
  ```

## OpenShift Data Foundation

### Requirements

* Deploy the [OpenShift Data Foundation](https://access.redhat.com/documentation/en-us/red_hat_openshift_data_foundation/4.12) on your cluster.


### Installation

* Deploy the Loki Operator to your cluster.
* Create an ObjectBucketClaim in `openshift-logging` namespace:

  ```yaml
    apiVersion: objectbucket.io/v1alpha1
    kind: ObjectBucketClaim
    metadata:
      name: loki-bucket-odf
      namespace: openshift-logging
    spec:
      generateBucketName: loki-bucket-odf
  ```

* Get bucket properties from the associated ConfigMap:
  ```console
  BUCKET_HOST=$(kubectl get -n openshift-logging configmap loki-bucket-odf -o jsonpath='{.data.BUCKET_HOST}')
  BUCKET_NAME=$(kubectl get -n openshift-logging configmap loki-bucket-odf -o jsonpath='{.data.BUCKET_NAME}')
  BUCKET_PORT=$(kubectl get -n openshift-logging configmap loki-bucket-odf -o jsonpath='{.data.BUCKET_PORT}')
  ```

* Get bucket access key from the associated Secret:
  ```console
  ACCESS_KEY_ID=$(kubectl get -n openshift-logging secret loki-bucket-odf -o jsonpath='{.data.AWS_ACCESS_KEY_ID}' | base64 -d)
  SECRET_ACCESS_KEY=$(kubectl get -n openshift-logging secret loki-bucket-odf -o jsonpath='{.data.AWS_SECRET_ACCESS_KEY}' | base64 -d)
  ```

  * Create an Object Storage secret with keys as follows:

  ```console
    kubectl create -n openshift-logging secret generic lokistack-dev-odf \
    --from-literal=access_key_id="${ACCESS_KEY_ID}" \
    --from-literal=access_key_secret="${SECRET_ACCESS_KEY}" \
    --from-literal=bucketnames="${BUCKET_NAME}" \
    --from-literal=endpoint="https://${BUCKET_HOST}:${BUCKET_PORT}"
  ```

  Where `lokistack-dev-odf` is the secret name. The values for `ACCESS_KEY_ID`, `SECRET_ACCESS_KEY`, `BUCKET_NAME`, `BUCKET_HOST` and `BUCKET_PORT` are taken from your ObjectBucketClaim's accompanied secret and ConfigMap.

* Create an instance of [LokiStack](../../hack/lokistack_gateway_ocp.yaml) by referencing the secret name and type as `s3`:

  ```yaml
  apiVersion: loki.grafana.com/v1
  kind: LokiStack
  metadata:
    name: logging-loki
    namespace: openshift-logging
  spec:
    storage:
      secret:
        name: lokistack-dev-odf
        type: s3
      tls:
        caName: openshift-service-ca.crt
    tenants:
      mode: openshift-logging
  ```

## Swift

### Requirements

* Create a [bucket](https://docs.openstack.org/newton/user-guide/cli-swift-create-containers.html) on Swift.

### Installation

* Deploy the Loki Operator to your cluster.

* Create an Object Storage secret with keys as follows:

    ```console
    kubectl create secret generic lokistack-dev-swift \
      --from-literal=auth_url="<SWIFT_AUTH_URL>" \
      --from-literal=username="<SWIFT_USERNAMEClaim>" \
      --from-literal=user_domain_name="<SWIFT_USER_DOMAIN_NAME>" \
      --from-literal=user_domain_id="<SWIFT_USER_DOMAIN_ID>" \
      --from-literal=user_id="<SWIFT_USER_ID>" \
      --from-literal=password="<SWIFT_PASSWORD>" \
      --from-literal=domain_id="<SWIFT_DOMAIN_ID>" \
      --from-literal=domain_name="<SWIFT_DOMAIN_NAME>" \
      --from-literal=container_name="<SWIFT_CONTAINER_NAME>" \
    ```

    where `lokistack-dev-swift` is the secret name.

*  Optionally you can provide project specific data and/or a region as follows:

    ```console
    kubectl create secret generic lokistack-dev-swift \
      --from-literal=auth_url="<SWIFT_AUTH_URL>" \
      --from-literal=username="<SWIFT_USERNAMEClaim>" \
      --from-literal=user_domain_name="<SWIFT_USER_DOMAIN_NAME>" \
      --from-literal=user_domain_id="<SWIFT_USER_DOMAIN_ID>" \
      --from-literal=user_id="<SWIFT_USER_ID>" \
      --from-literal=password="<SWIFT_PASSWORD>" \
      --from-literal=domain_id="<SWIFT_DOMAIN_ID>" \
      --from-literal=domain_name="<SWIFT_DOMAIN_NAME>" \
      --from-literal=container_name="<SWIFT_CONTAINER_NAME>" \
      --from-literal=project_id="<SWIFT_PROJECT_ID>" \
      --from-literal=project_name="<SWIFT_PROJECT_NAME>" \
      --from-literal=project_domain_id="<SWIFT_PROJECT_DOMAIN_ID>" \
      --from-literal=project_domain_name="<SWIFT_PROJECT_DOMAIN_name>" \
      --from-literal=region="<SWIFT_REGION>" \
    ```

* Create an instance of [LokiStack](../hack/lokistack_dev.yaml) by referencing the secret name and type as `swift`:

  ```yaml
  spec:
    storage:
      secret:
        name: lokistack-dev-swift
        type: swift
  ```

## AlibabaCloud OSS

### Requirements

* Create a [bucket](https://www.alibabacloud.com/help/en/object-storage-service/latest/create-buckets-2) on AlibabaCloud.

### Installation

* Deploy the Loki Operator to your cluster.

* Create an Object Storage secret with keys as follows:

    ```console
    kubectl create secret generic lokistack-dev-alibabacloud \
      --from-literal=bucket="<BUCKET_NAME>" \
      --from-literal=endpoint="<OSS_BUCKET_ENDPOINT>" \
      --from-literal=access_key_id="<OSS_ACCESS_KEY_ID>" \
      --from-literal=secret_access_key="<OSS_ACCESS_KEY_SECRET>"
    ```

    where `lokistack-dev-alibabacloud` is the secret name.

* Create an instance of [LokiStack](../hack/lokistack_dev.yaml) by referencing the secret name and type as `alibabacloud`:

  ```yaml
  spec:
    storage:
      secret:
        name: lokistack-dev-alibabacloud
        type: alibabacloud
  ```
