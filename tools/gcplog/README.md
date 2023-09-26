# Cloud provisioning for GCP logs

This document covers how to configure your GCP via Terraform to make cloud logs available for `promtail` to consume.

To choose what logs need to exported from Google Cloud, we use log filters. Log filters are normal GCP logging queries except the goal is export logs from specific set Google cloud resources

e.g: Export Google APP Engine logs
```bash
resource.type="gae_app" AND
severity>=ERROR
```

e.g: Export Google HTTP Loadbalancer logs
```bash
resource.type="http_load_balancer" AND
httpRequest.status>=500
```
You can read more about these log filters in [GCP logging](https://cloud.google.com/logging/docs/view/query-library)

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
5. name - name we use to create pubsub topics, log router and pubsub subscription.
6. inclusion_filter - To include cloud resources to export.
7. exclusions - Ignore these logs while exporting.

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
