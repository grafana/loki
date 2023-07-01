---
title: Configure storage
menuTitle: Configure storage 
description: Configure Loki storage
aliases:
  - /docs/installation/helm/storage
weight: 500
keywords:
  - object store
  - filesystem
  - minio
---

# Configure storage

The [scalable]({{< relref "../install-scalable" >}}) installation requires a managed object store such as AWS S3 or Google Cloud Storage or a self-hosted store such as Minio. The [single binary]({{< relref "../install-monolithic" >}}) installation can only use the filesystem for storage.

This guide assumes Loki will be installed in one of the modes above and that a `values.yaml ` has been created.

**To use a managed object store:**

1. Set the `type` of `storage` in `values.yaml` to `gcs` or `s3`.

2. Configure the storage client under `loki.storage.gcs` or `loki.storage.s3`.


**To install Minio alongside Loki:**

1. Change the configuration in `values.yaml`:

    - Enable Minio

    ```yaml
    minio:
      enabled: true
    ```

**To grant access to S3 via an IAM role without providing credentials:**

1. Provision an IAM role, policy and S3 bucket as described in [Storage]({{< relref "../../../storage#aws-deployment-s3-single-store" >}}).
   - If the Terraform module was used note the annotation emitted by `terraform output -raw annotation`.

2. Add the IAM role annotation to the service account in `values.yaml`:

   ```
   serviceAccount:
     annotations:
       "eks.amazonaws.com/role-arn": "arn:aws:iam::<account id>:role/<role name>:
   ```

3. Configure the storage:

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
