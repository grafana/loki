## Introduction
This example gives you an example or getting started overrides value file for deploying Loki (OSS) using the Simple Scalable architecture in GKE and using GCS

## Installation of Helm Chart
These instructions assume you have already have access to a Kubernetes cluster, GCS Bucket and GCP Service Account which has read/write permissions to that GCS Bucket.

### Populate Secret Values
Populate the examples/enterprise/enterprise-secrets.yaml so that:
- The gcp_service_account.json secret has the contents of your GCP Service Account JSON key

Deploy the secrets file to your k8s cluster.

`kubectl apply -f loki-secrets.yaml`

### Configure the Helm Chart
Open examples/enterprise/overides-oss-gcs.yaml and replace `{YOUR_GCS_BUCKET}` with the name of your GCS bucket. If there are other things you'd like to configure, view the core [Values.yaml file](https://github.com/grafana/loki/blob/main/production/helm/loki/values.yaml) and override anything else you need to within the overrides-enterprise-gcs.yaml file.

### Install the Helm chart

`helm upgrade --install --values {PATH_TO_YOUR_OVERRIDES_YAML_FILE} {YOUR_RELEASE_NAME} grafana/loki-simple-scalable --namespace {KUBERNETES_NAMESPACE}`
