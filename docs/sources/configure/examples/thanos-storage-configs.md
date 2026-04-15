---
title: "Configuration examples for using Thanos-based storage clients"
menuTitle: Thanos storage examples
description: "Minimal examples for using Thanos-based S3, GCS, Azure, and filesystem clients in Grafana Loki."
weight: 100
---

# Configuration examples for using Thanos-based storage clients

Use these examples as a starting point for configuring [Thanos based object storage clients](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#thanos_object_store_config) in Grafana Loki.

## GCS example
```yaml
storage_config:
  use_thanos_objstore: true
  object_store:
    gcs:
      bucket_name: my-gcs-bucket

      # JSON either from a Google Developers Console client_credentials.json file,
      # or a Google Developers service account key. Needs to be valid JSON, not a
      # filesystem path. If empty, fallback to Google default logic:
      # 1. A JSON file whose path is specified by the GOOGLE_APPLICATION_CREDENTIALS
      # environment variable. For workload identity federation, refer to
      # https://cloud.google.com/iam/docs/how-to#using-workload-identity-federation
      # on how to generate the JSON configuration file for on-prem/non-Google cloud
      # platforms.
      # 2. A JSON file in a location known to the gcloud command-line tool:
      # $HOME/.config/gcloud/application_default_credentials.json.
      # 3. On Google Compute Engine it fetches credentials from the metadata server.
      service_account:  |-
        {
            "type": "service_account",
            "project_id": "project",
            "private_key_id": "abcdefghijklmnopqrstuvwxyz12345678906666",
            "private_key": "-----BEGIN PRIVATE KEY-----\...\n-----END PRIVATE KEY-----\n",
            "client_email": "project@example.iam.gserviceaccount.com",
            "client_id": "123456789012345678901",
            "auth_uri": "https://accounts.google.com/o/oauth2/auth",
            "token_uri": "https://oauth2.googleapis.com/token",
            "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
            "client_x509_cert_url": "https://www.googleapis.com/robot/v1/metadata/x509/project@example.iam.gserviceaccount.com"
        }
```

## S3 example
```yaml
storage_config:
  use_thanos_objstore: true
  object_store:
    s3:
      bucket_name: my-s3-bucket
      endpoint: s3.us-west-2.amazonaws.com
      region: us-west-2
      # You can either declare the access key and secret in the config or
      # use environment variables AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY which will be picked up by the AWS SDK.
      access_key_id: access-key-id
      secret_access_key: secret-access-key
```

## Azure example
```yaml
storage_config:
  use_thanos_objstore: true
  object_store:
    azure:
      account_name: myaccount
      account_key: ${SECRET_ACCESS_KEY} # loki expands environment variables
      container_name: example-container
```

## Filesystem example
```yaml
storage_config:
  use_thanos_objstore: true
  object_store:
    filesystem:
      dir: /var/loki/chunks
```
