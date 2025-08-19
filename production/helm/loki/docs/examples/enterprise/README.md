## Introduction
This example gives you an example or getting started overrides value file for deploying Loki (Enterprise Licensed) using the Simple Scalable architecture in GKE and using GCS.

## Installation of Helm Chart
These instructions assume you already have access to a Kubernetes cluster, GCS Bucket and GCP Service Account which has read/write permissions to that GCS Bucket.

### Populate Secret Values
Populate the [enterprise-secrets.yaml](./enterprise-secrets.yaml) so that:
- The `gcp_service_account.json` secret has the contents of your GCP Service Account JSON key.
- The `license.jwt` secret has the contents of your Grafana Enterprise Logs license key given to your by Grafana Labs.

Deploy the secrets file to your k8s cluster with the command:

`kubectl apply -f enterprise-secrets.yaml`

### Configure the Helm Chart
Open [overrides-enterprise-gcs.yaml](./overrides-enterprise-gcs.yaml) and replace `{YOUR_GCS_BUCKET}` with the name of your GCS bucket. If there are other things you'd like to configure, view the core [Values.yaml file](https://github.com/grafana/loki/blob/main/production/helm/loki/values.yaml) and override anything else you need to within the overrides-enterprise-gcs.yaml file.

### Create Admin Token Secret (Required for Provisioner)

If you are using the enterprise provisioner to automatically create tenant tokens, you must first create an admin token secret:

1. Generate an admin token using the Loki CLI:
   ```bash
   docker run grafana/loki:latest -target=tokengen -tokengen.token-file=/tmp/token
   # Copy the generated token from the container
   docker cp <container-id>:/tmp/token ./admin-token
   ```

2. Create the admin token secret:
   ```bash
   kubectl create secret generic loki-admin-token \
     --from-file=token=./admin-token \
     --namespace {KUBERNETES_NAMESPACE}
   ```

3. Update your overrides file to reference this secret:
   ```yaml
   enterprise:
     adminToken:
       secret: loki-admin-token
   ```

### Install the Helm chart

`helm upgrade --install --values {PATH_TO_YOUR_OVERRIDES_YAML_FILE} {YOUR_RELEASE_NAME} grafana/loki-simple-scalable --namespace {KUBERNETES_NAMESPACE}`

### Get Tenant Tokens from Provisioner

If you enabled the provisioner, retrieve the generated tenant tokens from the provisioner logs:

```bash
# Get provisioner job logs
kubectl logs -l job-name=loki-provisioner --namespace {KUBERNETES_NAMESPACE}
```

The provisioner outputs tokens for each configured tenant. You must manually create Kubernetes secrets for each tenant using these tokens:

```bash
# Example for creating a tenant secret
kubectl create secret generic <tenant-name> \
  --from-literal=token-write=<write-token-from-logs> \
  --from-literal=token-read=<read-token-from-logs> \
  --namespace <tenant-namespace>
```

### Manual Token Generation (Without Provisioner)

If you're not using the provisioner, you can manually generate tokens:

1. Port-forward to the Loki service:
   ```bash
   kubectl port-forward svc/loki-gateway 3100:80 --namespace {KUBERNETES_NAMESPACE}
   ```

2. Use the admin token to create tenant tokens via the Admin API:
   ```bash
   # Example: Create a token for a tenant
   curl -X POST http://localhost:3100/admin/api/v1/tokens \
     -H "Authorization: Bearer <admin-token>" \
     -H "Content-Type: application/json" \
     -d '{"name": "my-tenant", "displayName": "My Tenant", "access_policy": "logs:write,logs:read"}'
   ```

Take note of these tokens, you will need them when connecting Grafana Enterprise Logs to Grafana.
