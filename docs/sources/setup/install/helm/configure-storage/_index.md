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

The [scalable](../install-scalable/) installation requires a managed object store such as AWS S3 or Google Cloud Storage or a self-hosted store such as Minio. The [single binary](../install-monolithic/) installation can only use the filesystem for storage.

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
