---
title: Configure storage
menuTitle: Configure storage 
description: Configuring Loki storage with the Helm Chart.
aliases:
  - ../../../installation/helm/configure-storage/
  - ../../../installation/helm/storage
weight: 500
keywords:
  - object store
  - filesystem
  - minio
---

# Configure storage

The [scalable](../install-scalable/) installation requires a managed object store such as AWS S3 or Google Cloud Storage or a self-hosted store such as Minio. The [single binary](../install-monolithic/) installation can use the filesystem for storage, but we recommend configuring object storage via cloud provider or pointing Loki at a MinIO cluster for production deployments.

This guide assumes Loki will be installed in one of the modes above and that a `values.yaml ` has been created.

**To use a managed object store:**

1. In the `values.yaml` file, set the value for `storage.type` to `azure`, `gcs`, or `s3`.

1. Configure the storage client under `loki.storage.azure`, `loki.storage.gcs`, or `loki.storage.s3`.


**To install Minio alongside Loki:**

1. Change the configuration in `values.yaml`:

    - Enable Minio

    ```yaml
    minio:
      enabled: true
    ```

**To use Thanos object store clients (experimental):**

Loki supports using Thanos-compatible storage clients as an alternative to the built-in storage clients. This is configured via `loki.storage.use_thanos_objstore` and will become the default in a future release.

1. Enable Thanos object store in `values.yaml`:

   ```yaml
   loki:
     storage:
       use_thanos_objstore: true
       object_store:
         type: s3  # Valid options: s3, gcs, azure
         s3:
           endpoint: <YOUR_ENDPOINT>
           region: <YOUR_REGION>
   ```

1. Configure the storage client under `loki.storage.object_store.s3`, `loki.storage.object_store.gcs`, or `loki.storage.object_store.azure`.

**To grant access to S3 via an IAM role without providing credentials:**

1. Provision an IAM role, policy and S3 bucket as described in [Storage](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/storage/#aws-deployment-s3-single-store).
   - If the Terraform module was used note the annotation emitted by `terraform output -raw annotation`.

1. Add the IAM role annotation to the service account in `values.yaml`:

   ```
   serviceAccount:
     annotations:
       "eks.amazonaws.com/role-arn": "arn:aws:iam::<account id>:role/<role name>"
   ```

1. Configure the storage:

   ```
   loki:
     storage:
       type: "s3"
       s3:
         region: eu-central-1
       bucketNames:
         chunks: <bucket name>
         ruler: <bucket name>
         admin: <bucket name>
   ```

   Note that `endpoint`, `secretAccessKey` and `accessKeyId` have been omitted.
