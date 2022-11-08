---
title: Configure storage
menuTitle: Configure storage 
description: Configure Loki storage
aliases:
  - /docs/writers-toolkit/latest/templates/task-template
weight: 100
keywords:
  - object store
  - filesystem
  - minio
---

# Configure Loki's storage

The [scalable](../install-scalable/) installation requires a managed object store such as AWS S3 or Google Cloud Storage or a self-hosted store such as Minio. The [single binary](../install-monolithic/) installation can only use the filesystem for storage.

This guide assumes Loki has been installed in on of the modes above and that a `values.yaml ` has been created.

**To use a managed object store:**

1. Set the `type` of `storage` in `values.yaml` to `gcs` or `s3`.

2. Configure the storage client under `storage.gcs` or `storage.s3`.


**To install Minio alongside Loki:**

1. Change the configuration in `values.yaml`:

    - Enable Minio

    ```yaml
    minio:
      enabled: true
    ```
