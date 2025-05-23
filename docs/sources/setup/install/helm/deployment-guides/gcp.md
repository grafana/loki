---
title: Deploy the Loki Helm chart on GCP
menuTitle: Deploy on GCP
description: Installing the Loki Helm chart on GCP.
keywords:
---

# Deploy the Loki Helm chart on GCP

This guide shows how to deploy a minimally viable Loki in **microservice** mode on Google Cloud Platform (GCP) using the Helm chart. To run through this guide, we expect you to have the necessary tools and permissions to deploy resources on GCP, such as:

- Full access to Google Kubernetes Engine (GKE)
- Full access to Google Cloud Storage (GCS)
- Sufficient permissions to create Identity Access Management (IAM) roles and policies

There are two methods for authenticating and connecting Loki to GCP GCS. We will guide you through the recommended method of granting access via an IAM role: using Workload Identity Federation.

## Considerations

{{< admonition type="caution" >}}
This guide was accurate at the time it was last updated on **29th of April, 2025**.  As cloud providers frequently update their services and offerings, as a best practice, you should refer to the [GCP GCS documentation](https://cloud.google.com/storage/docs/introduction) before creating your buckets and assigning roles.
{{< /admonition >}}

- **IAM Role:** The IAM role created in this guide is a basic role that allows Loki to read and write to the GCS bucket. You may wish to add more granular permissions based on your requirements.

- **Authentication:** Grafana Loki comes with a basic authentication layer. The Loki gateway (NGINX) is exposed to the internet using basic authentication in this example. NGINX can also be replaced with other open-source reverse proxies. Refer to [Authentication](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/authentication/) for more information.

- **Retention:** The retention period is set to 28 days in the `values.yaml` file. You may wish to adjust this based on your requirements.

- **Costs:** Running Loki on GCP will incur costs. Make sure to monitor your usage and costs to avoid any unexpected bills. In this guide we have used a simple GKE cluster with 3 nodes and m5.xlarge instances. You may wish to adjust the instance types and number of nodes based on your workload.

## Prerequisites

- Helm 3 or above. Refer to [Installing Helm](https://helm.sh/docs/intro/install/). This should be installed on your local machine.
- A running Kubernetes cluster on GCP. Refer to [Create a cluster and deploy a workload in the Google Cloud console](https://cloud.google.com/kubernetes-engine/docs/quickstarts/create-cluster).
- Kubectl installed on your local machine. Refer to [Install and Set Up kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/).
- (Optional, but recommended) gcloud CLI installed on your local machine. Refer to [Install the Google Cloud CLI](https://cloud.google.com/sdk/docs/install-sdk). This is required if you plan to use gcloud CLI to create the GKE cluster and modify the IAM roles and policies locally.
  
### GKE Minimum Requirements

{{< admonition type="caution" >}}
These GKE requirements are the minimum specification needed to deploy Loki using this guide. You may wish to adjust plugins and instance types based on your GCP environment and workload. **If you choose to do so, we cannot guarantee that this sample configuration will still meet your needs.**

In this guide, we deploy Loki using `n2-standard-8` instances. This is a instance type that should work for most scenarios. However, you can modify the instance types and count based on your specific needs.
{{< /admonition >}}

The minimum requirements for deploying Loki on GKE are: 

- Kubernetes version `1.30` or above.
- `3` nodes for the GKE cluster.
- Instance type depends on your workload. A good starting point for a production cluster is `n2-standard-8`.

Here is an example of a command you can run using gcloud CLI to create a new cluster:

```bash
gcloud container clusters create loki-gcp \
  --location=europe-west4 \
  --num-nodes=1 \
  --machine-type=n2-standard-8 \
  --release-channel=regular \
  --workload-pool=<PROJECT_ID>.svc.id.goog \
  --enable-ip-alias \
  --no-enable-basic-auth \
  --no-issue-client-certificate
```

Replace `<PROJECT_ID>` with the ID of the project you want to create the cluster in. This should be something like `my-project-123456`.

{{< admonition type="warning" >}}
Regional clusters in GKE are designed for resilience, and thus by default span three zones within the region. If you set `num_nodes=3`, you would get 9 nodes in total for the region: 3 in *each* zone. In the command above, `num_nodes` is set to `1` for this reason.
{{< /admonition >}}

## Create GCS buckets

{{< admonition type="warning" >}}
 **DO NOT** use the default bucket names;  `chunks`, `ruler` and `admin`. Choose a **unique** name for each bucket. For more information see the following [security update](https://grafana.com/blog/2024/06/27/grafana-security-update-grafana-loki-and-unintended-data-write-attempts-to-amazon-GCS-buckets/).
{{< /admonition >}}

Before deploying Loki, you need to create two GCS buckets: one to store logs (chunks) and another to store alert rules (ruler). You can create the bucket using the GCP Management Console or the GCP CLI. The bucket name must be globally unique.

{{< admonition type="note" >}}
GEL customers will require a third bucket to store the admin data. This bucket is not required for OSS users.
{{< /admonition >}}

```bash
gcloud storage buckets create gs://<CHUNKS_BUCKET_NAME> gs://<RULER_BUCKET_NAME> \
  --location=<REGION> \
  --default-storage-class=STANDARD \
  --public-access-prevention \
  --uniform-bucket-level-access \
  --soft-delete-duration=7d
```

Make sure to replace the `region` and `bucket` name with your desired values. We will revisit the bucket policy later in this guide.

Here's an example with all the variables filled in:

```bash
gcloud storage buckets create gs://loki-gcp-chunks gs://loki-gcp-ruler \
  --location=europe-west4 \
  --default-storage-class=STANDARD \
  --public-access-prevention \
  --uniform-bucket-level-access \
  --soft-delete-duration=7d
```

When you run this command, you should get something like this in response:

```bash
Creating gs://loki-gcp-chunks/...
Creating gs://loki-gcp-ruler/...
```


## Defining IAM roles and policies

IAM determines who can access which resources on GCP and can be configured in several ways. The recommended method for allowing Loki to access GCS is to use Workload Identity Federation. This method is more secure than creating and distributing a service account key. The below steps show how to create the role and policy using the gcloud CLI.

### Authenticating to the GKE cluster

You need to be able to run `kubectl` commands on the cluster, so make sure you have it installed (run `gcloud components install kubectl` if not) and then run this command:

```bash
gcloud container clusters get-credentials <CLUSTER_NAME> \
  --region=<REGION>
```

Here's an example of that command with the variables filled in:

```bash
gcloud container clusters get-credentials loki-gcp \
  --region=europe-west4
```

This will authenticate you via your GCP IAM identity, write the cluster's access info to your local kubeconfig (usually `~/.kube/config`), and then allow `kubectl` commands to talk to the right cluster from now on.

Then check that you're connected to the GKE cluster and that you're accessing it via `kubectl` by running:

```bash
kubectl config current-context
```

You should get something like this in return:

```bash
gke_my-project-123456_europe-west4_loki-gcp
```

### Create a Kubernetes Namespace

Create a Kubernetes namespace where you'll install your Loki workloads:

```bash
kubectl create namespace <NAMESPACE>
```

Replace `<NAMESPACE>` with the namespace where your Loki workloads will be located.

Example:

```bash
kubectl create namespace loki
```

You should get the output:

```bash
namespace/loki created
```
### Create Kubernetes Service Account (KSA)

A KSA is a cluster identity (service account, named `default` by default) assigned to pods that allows pods to interact with each other.

Create a KSA on your Kubernetes cluster:

```bash
kubectl create serviceaccount <KSA_NAME> \
  --namespace <NAMESPACE>
```

Replace `<KSA_NAME>` with the name of the KSA created above, and `<NAMESPACE>` with the namespace where your Loki/GEL workloads are located.

Example:

```bash
kubectl create serviceaccount loki-gcp-ksa \
  --namespace loki
```

You should get this in response:

```bash
serviceaccount/loki-gcp-ksa created
```

### Add IAM Policy to Bucket(s)

{{< admonition type="note" >}}
The [pre-defined `role/storage.objectUser` role](https://cloud.google.com/storage/docs/access-control/iam-roles) is sufficient for Loki / GEL to
 operate. See [IAM permissions for Cloud Storage](https://cloud.google.com/storage/docs/access-control/iam-permissions) for details about each individual
 permission. You can use this predefined role or create your own with matching permissions.
{{< /admonition >}}

Create an IAM policy binding on the bucket(s) using the KSA created previously and the role(s) of your choice. One command per bucket.

```txt
gcloud storage buckets add-iam-policy-binding gs://<BUCKET_NAME> \
  --role=roles/storage.objectViewer \
  --member=principal://iam.googleapis.com/projects/<PROJECT_NUMBER>/locations/global/workloadIdentityPools/<PROJECT_ID>.svc.id.goog/subject/ns/<NAMESPACE>/sa/<KSA_NAME> \
  --condition=None
```

Replace `<PROJECT_ID>` with the GCP project ID (ex. project-name), `<PROJECT_NUMBER>` with the project number (ex. 1234567890),
`<NAMESPACE>` with the namespace where Loki/GEL is installed, and `<KSA_NAME>` with the name of the KSA you created above.

Example:

```bash
gcloud storage buckets add-iam-policy-binding gs://loki-gcp-chunks \
  --role=roles/storage.objectViewer \
  --member=principal://iam.googleapis.com/projects/12345678901/locations/global/workloadIdentityPools/my-project-123456.svc.id.goog/subject/ns/loki/sa/loki-gcp-ksa \
  --condition=None
```

You should get something like this in response:

```bash
bindings:
- members:
  - projectEditor:my-project-123456
  - projectOwner:my-project-123456
  role: roles/storage.legacyBucketOwner
- members:
  - projectViewer:my-project-123456
  role: roles/storage.legacyBucketReader
- members:
  - projectEditor:my-project-123456
  - projectOwner:my-project-123456
  role: roles/storage.legacyObjectOwner
- members:
  - projectViewer:my-project-123456
  role: roles/storage.legacyObjectReader
- members:
  - principal://iam.googleapis.com/projects/12345678901/locations/global/workloadIdentityPools/my-project-123456.svc.id.goog/subject/ns/loki/sa/loki-gcp-ksa
  role: roles/storage.objectViewer
etag: CAI=
kind: storage#policy
resourceId: projects/_/buckets/loki-gcp-chunks
version: 1
```

## Deploying the Helm chart

Before we can deploy the Loki Helm chart, we need to add the Grafana chart repository to Helm. This repository contains the Loki Helm chart.

1. Add the Grafana chart repository to Helm:

    ```bash
    helm repo add grafana https://grafana.github.io/helm-charts
    ```
2. Update the chart repository:

    ```bash
    helm repo update
    ```
3. Create a new namespace for Loki:

    ```bash
    kubectl create namespace loki
    ```
### Loki Basic Authentication

Loki by default does not come with any authentication. Since we will be deploying Loki to GCP and exposing the gateway to the internet, we recommend adding at least basic authentication. In this guide we will give Loki a username and password:

1. To start we will need create a `.htpasswd` file with the username and password. You can use the `htpasswd` command to create the file:

   {{< admonition type="tip" >}}
    If you don't have the `htpasswd` command installed, you can install it using `brew` or `apt-get` or `yum` depending on your OS.
   {{< /admonition >}}

    ```bash
    htpasswd -c .htpasswd <username>
    ```
    This will create a file called `auth` with the username `loki`. You will be prompted to enter a password.

 2. Create a Kubernetes secret with the `.htpasswd` file:

    ```bash
    kubectl create secret generic loki-basic-auth --from-file=.htpasswd -n loki
    ```

    This will create a secret called `loki-basic-auth` in the `loki` namespace. We will reference this secret in the Loki Helm chart configuration.
  
3. Create a `canary-basic-auth` secret for the canary:

    ```bash
    kubectl create secret generic canary-basic-auth \
      --from-literal=username=<USERNAME> \
      --from-literal=password=<PASSWORD> \
      -n loki
    ```
    We create a literal secret with the username and password for Loki canary to authenticate with the Loki gateway.
    **Make sure to replace the placeholders with your desired username and password.** 

### Loki Helm chart configuration

Create a `values.yaml` file choosing the configuration options that best suit your requirements. Below there is an example of `values.yaml` files for the Loki Helm chart in [microservices](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#microservices-mode) mode.

```yaml
loki:
   schemaConfig:
     configs:
       - from: "2024-04-01"
         store: tsdb
         object_store: gcs
         schema: v13
         index:
           prefix: loki_index_
           period: 24h
   storage_config:
     gcs:
       bucket_name: <CHUNK_BUCKET_NAME> # Your actual gcs bucket name, for example, loki-gcp-chunks
   ingester:
       chunk_encoding: snappy
   pattern_ingester:
       enabled: true
   limits_config:
     allow_structured_metadata: true
     volume_enabled: true
     retention_period: 672h # 28 days retention
   compactor:
     retention_enabled: true 
     delete_request_store: gcs
   ruler:
    enable_api: true
    storage_config:
      type: gcs
      gcs_storage_config:
        region: <REGION> # The GCS region, for example europe-west4
        bucketnames: <RULER_BUCKET_NAME> # Your actual gcs bucket name, for example, loki-gcp-ruler
      alertmanager_url: http://prom:9093 # The URL of the Alertmanager to send alerts (Prometheus, Mimir, etc.)

   querier:
      max_concurrent: 4

   storage:
      type: gcs
      bucketNames:
        chunks: <CHUNK_BUCKET_NAME> # Your actual gcs bucket name, for example, loki-gcp-chunks
        ruler: <RULER_BUCKET_NAME> # Your actual gcs bucket name, for example, loki-gcp-ruler
      
serviceAccount:
 create: false
 name: loki-gcp-ksa

deploymentMode: Distributed

ingester:
 replicas: 3
 zoneAwareReplication:
  enabled: false

querier:
 replicas: 3
 maxUnavailable: 2

queryFrontend:
 replicas: 2
 maxUnavailable: 1

queryScheduler:
 replicas: 2

distributor:
 replicas: 3
 maxUnavailable: 2
compactor:
 replicas: 1

indexGateway:
 replicas: 2
 maxUnavailable: 1

ruler:
 replicas: 1
 maxUnavailable: 1


# This exposes the Loki gateway so it can be written to and queried externaly
gateway:
 service:
   type: LoadBalancer
 basicAuth: 
     enabled: true
     existingSecret: loki-basic-auth

# Since we are using basic auth, we need to pass the username and password to the canary
lokiCanary:
  extraArgs:
    - -pass=$(LOKI_PASS)
    - -user=$(LOKI_USER)
  extraEnv:
    - name: LOKI_PASS
      valueFrom:
        secretKeyRef:
          name: canary-basic-auth
          key: password
    - name: LOKI_USER
      valueFrom:
        secretKeyRef:
          name: canary-basic-auth
          key: username

# Enable minio for storage
minio:
 enabled: false

backend:
 replicas: 0
read:
 replicas: 0
write:
 replicas: 0

singleBinary:
 replicas: 0
```

{{< admonition type="caution" >}}
Make sure to replace the placeholders with your actual values.
{{< /admonition >}}

It is critical to define a valid `values.yaml` file for the Loki deployment. To remove the risk of misconfiguration, let's break down the configuration options to keep in mind when deploying to GCP:

- **Loki Config vs. Values Config:**
  - The `values.yaml` file contains a section called `loki`, which contains a direct representation of the Loki configuration file.
  - This section defines the Loki configuration, including the schema, storage, and querier configuration.
  - The key configuration to focus on for chunks is the `storage_config` section, where you define the GCS bucket region and name. This tells Loki where to store the chunks.
  - The `ruler` section defines the configuration for the ruler, including the GCS bucket region and name. This tells Loki where to store the alert and recording rules.
  - For the full Loki configuration, refer to the [Loki Configuration](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/) documentation.

- **Storage:**
  - Defines where the Helm chart stores data.
  - Set the type to `GCS` since we are using Amazon GCS.
  - Configure the bucket names for the chunks and ruler to match the buckets created earlier.
  - The `GCS` section specifies the region of the bucket.

- **Service Account:**
  - The `serviceAccount` section is used to define the IAM role for the Loki service account.
  - This is where the IAM role created earlier is linked.

- **Gateway:**
  - Defines how the Loki gateway will be exposed.
  - We are using a `LoadBalancer` service type in this configuration.


### Deploy Loki

Now that you have created the `values.yaml` file, you can deploy Loki using the Helm chart.

1. Deploy using the newly created `values.yaml` file:

    ```bash
    helm install --values values.yaml loki grafana/loki -n loki --create-namespace
    ```
    **It is important to create a namespace called `loki` as our trust policy is set to allow the IAM role to be used by the `loki` service account in the `loki` namespace. This is configurable but make sure to update your service account.**

2. Verify the deployment:

    ```bash
    kubectl get pods -n loki
    ```
    You should see the Loki pods running.

    ```console
    NAME                                    READY   STATUS    RESTARTS   AGE
    loki-canary-crqpg                       1/1     Running   0          10m
    loki-canary-hm26p                       1/1     Running   0          10m
    loki-canary-v9wv9                       1/1     Running   0          10m
    loki-chunks-cache-0                     2/2     Running   0          10m
    loki-compactor-0                        1/1     Running   0          10m
    loki-distributor-78ccdcc9b4-9wlhl       1/1     Running   0          10m
    loki-distributor-78ccdcc9b4-km6j2       1/1     Running   0          10m
    loki-distributor-78ccdcc9b4-ptwrb       1/1     Running   0          10m
    loki-gateway-5f97f78755-hm6mx           1/1     Running   0          10m
    loki-index-gateway-0                    1/1     Running   0          10m
    loki-index-gateway-1                    1/1     Running   0          10m
    loki-ingester-zone-a-0                  1/1     Running   0          10m
    loki-ingester-zone-b-0                  1/1     Running   0          10m
    loki-ingester-zone-c-0                  1/1     Running   0          10m
    loki-querier-89d4ff448-4vr9b            1/1     Running   0          10m
    loki-querier-89d4ff448-7nvrf            1/1     Running   0          10m
    loki-querier-89d4ff448-q89kh            1/1     Running   0          10m
    loki-query-frontend-678899db5-n5wc4     1/1     Running   0          10m
    loki-query-frontend-678899db5-tf69b     1/1     Running   0          10m
    loki-query-scheduler-7d666bf759-9xqb5   1/1     Running   0          10m
    loki-query-scheduler-7d666bf759-kpb5q   1/1     Running   0          10m
    loki-results-cache-0                    2/2     Running   0          10m
    loki-ruler-0                            1/1     Running   0          10m
    ```
  
### Find the Loki Gateway Service

The Loki Gateway service is a LoadBalancer service that exposes the Loki gateway to the internet. This is where you will write logs to and query logs from. By default NGINX is used as the gateway.

{{< admonition type="caution" >}}
The Loki Gateway service is exposed to the internet. We provide basic authentication using a username and password in this tutorial. Refer to the [Authentication](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/authentication/) documentation for more information.
{{< /admonition >}}

To find the Loki Gateway service, run the following command:

```bash
kubectl get svc -n loki
```
You should see the Loki Gateway service with an external IP address. This is the address you will use to write to and query Loki.

```console
  NAME                             TYPE           CLUSTER-IP       EXTERNAL-IP                                                               PORT(S)              AGE
loki-gateway                   LoadBalancer   34.118.239.140   34.91.203.240   80:30566/TCP                 25m
```

In this case, the external IP address is `34.91.203.240`.

Congratulations! You have successfully deployed Loki on GCP using the Helm chart. Before we finish, let's test the deployment.

## Testing Your Loki Deployment

k6 is one of the fastest ways to test your Loki deployment. This will allow you to both write and query logs to Loki. To get started with k6, follow the steps below:

1. Install k6 with the Loki extension on your local machine. Refer to [Installing k6 and the xk6-loki extension](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/k6/).

2. Create a `gcp-test.js` file with the following content:

   ```javascript
    import {sleep, check} from 'k6';
    import loki from 'k6/x/loki';

    /**
    * URL used for push and query requests
    * Path is automatically appended by the client
    * @constant {string}
    */

    const username = '<USERNAME>';
    const password = '<PASSWORD>';
    const external_ip = '<EXTERNAL-IP>';

    const credentials = `${username}:${password}`;

    const BASE_URL = `http://${credentials}@${external_ip}`;

    /**
    * Helper constant for byte values
    * @constant {number}
    */
    const KB = 1024;

    /**
    * Helper constant for byte values
    * @constant {number}
    */
    const MB = KB * KB;

    /**
    * Instantiate config and Loki client
    */

    const conf = new loki.Config(BASE_URL);
    const client = new loki.Client(conf);

    /**
    * Define test scenario
    */
    export const options = {
      vus: 10,
      iterations: 10,
    };

    export default () => {
      // Push request with 10 streams and uncompressed logs between 800KB and 2MB
      var res = client.pushParameterized(10, 800 * KB, 2 * MB);
      // Check for successful write
      check(res, { 'successful write': (res) => res.status == 204 });

      // Pick a random log format from label pool
      let format = randomChoice(conf.labels["format"]);

      // Execute instant query with limit 1
      res = client.instantQuery(`count_over_time({format="${format}"}[1m])`, 1)
      // Check for successful read
      check(res, { 'successful instant query': (res) => res.status == 200 });

      // Execute range query over last 5m and limit 1000
      res = client.rangeQuery(`{format="${format}"}`, "5m", 1000)
      // Check for successful read
      check(res, { 'successful range query': (res) => res.status == 200 });

      // Wait before next iteration
      sleep(1);
    }

    /**
    * Helper function to get random item from array
    */
    function randomChoice(items) {
      return items[Math.floor(Math.random() * items.length)];
    }
   ```

   **Replace `<EXTERNAL-IP>` with the external IP address of the Loki Gateway service.**

   This script will write logs to Loki and query logs from Loki. It will write logs in a random format between 800KB and 2MB and query logs in a random format over the last 5 minutes.
  
3. Run the test:

    ```bash
    ./k6 run gcp-test.js
    ```

    This will run the test and output the results. You should see the test writing logs to Loki and querying logs from Loki.

Now that you have successfully deployed Loki in microservices mode on GCP, you may wish to explore the following:

- [Sending data to Loki](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/)
- [Querying Loki](https://grafana.com/docs/loki/<LOKI_VERSION>/query/)
- [Manage](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/)
