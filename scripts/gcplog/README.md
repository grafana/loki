# Cloud provisioning for GCP logs

This document covers how to configure your GCP via Terraform to make cloud logs available for `promtail` to consume.

## Prerequisite
- Terraform >= 0.14.5
- GCP Service account credentials with following roles/permissions
  - "roles/pubsub.editor"
  - "roles/logging.configWriter"

## Usage

```bash
terraform init
```

```bash
terraform plan
```

```bash
terraform apply
```

Terraform will prompt for following variables.

1. credentials_file - ServiceAccount credentials file with permissions mentioned in the prerequisite.
2. zone - GCP zone (e.g: `us-central1-b`)
3. region - GCP region (e.g: `us-central1`)
4. project - GCP Project ID
5. logname - Logname is the name we use to create pubsub topics, log router and pubsub subscription.

you can pass these variables via CLI.

e.g:
```bash
terraform apply \
-var="credentials_file=./permissions.json" \
-var="zone=us-central1-b" \
-var="region=us-central1" \
-var="project=grafanalabs-dev" \
-var="logname=cloud-logs"
```

These variables can be passed in multiple ways. For complete reference refer terraform [doc](https://www.terraform.io/docs/configuration/variables.html#assigning-values-to-root-module-variables)
