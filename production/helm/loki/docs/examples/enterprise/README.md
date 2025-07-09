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

### Install the Helm chart

`helm upgrade --install --values {PATH_TO_YOUR_OVERRIDES_YAML_FILE} {YOUR_RELEASE_NAME} grafana/loki-simple-scalable --namespace {KUBERNETES_NAMESPACE}`

### Get the Token for Grafana to connect
`export POD_NAME=$(kubectl get pods --namespace {KUBERNETES_NAMESPACE} -l "job-name=enterprise-logs-tokengen" -o jsonpath="{.items[0].metadata.name}")`

`kubectl --namespace {KUBERNETES_NAMESPACE} logs $POD_NAME loki | grep Token`

Take note of this token, you will need it when connecting Grafana Enterprise Logs to Grafana.
