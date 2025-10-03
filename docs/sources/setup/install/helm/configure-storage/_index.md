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
         region: <INSERT-REGION-FOR-CLUSTER>
       bucketNames:
         chunks: <BUCKET-NAME>
         ruler: <BUCKET-NAME>
         admin: <BUCKET-NAME>
   ```

   Note that `endpoint`, `secretAccessKey` and `accessKeyId` have been omitted.

**Server-Side Encryption (SSE)**  


You can configure Server-Side Encryption (SSE) for your S3 storage. There are three main SSE types available:

- SSE-C: You provide and manage your own encryption key manually.
- SSE-KMS: Uses Key Management Service (KMS) keys, providing more control over key management.
- SSE-S3: Encryption keys are fully managed by the S3 service.

{{< admonition type="caution" >}}  
Use caution when opting for server-side encryption with customer-provided keys (SSE-C). Unlike SSE-S3 and SSE-KMS, where key rotation is handled automatically by AWS, SSE-C requires manual key rotation which can be complex and error-prone in production environments.
{{< /admonition >}}

SSE-S3 can be set as follows: 
```yaml
loki:
  storage:
    type: "s3"
    s3:
      region: <INSERT-REGION-FOR-CLUSTER>
      sse:
        type: SSE-S3
```

SSE-KMS requires setting `kms_key_id` and `kms_encryption_context`.  
```yaml
loki:
  storage:
    type: "s3"
    s3:
      region: <INSERT-REGION-FOR-CLUSTER>
      sse:
        type: SSE-KMS
        kms_key_id: <YOUR-KMS-KEY-ID>
        kms_encryption_context: <YOUR-KMS-ENCRYPTION-CONTEXT>
```

To use SSE-C, you must provide a 32-byte (256-bit) encryption key. The encryption algorithm is set to `AES256` by default. 

{{< admonition type="warning" >}}  
Treat the `encryption_key` as a sensitive value since it directly protects your data. Proper key management and security practices are essential, as losing this key means losing access to the encrypted objects.
{{< /admonition >}}

```yaml
loki:
  storage:
    type: "s3"
    s3:
      region: <INSERT-REGION-FOR-CLUSTER>
      sse:
        type: SSE-C
        encryption_key: <YOUR-CUSTOMER-PROVIDED-ENCRYPTION-KEY>
```
