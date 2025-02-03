---
title: Deploy the Loki Helm chart on AWS
menuTitle: Deploy on AWS
description: Installing the Loki Helm chart on AWS.
keywords: 
---

# Deploy the Loki Helm chart on AWS

This guide shows how to deploy a minimally viable Loki in **microservice** mode on AWS using the Helm chart. To run through this guide, we expect you to have the necessary tools and permissions to deploy resources on AWS, such as:

- Full access to EKS (Amazon Elastic Kubernetes Service)
- Full access to S3 (Amazon Simple Storage Service)
- Sufficient permissions to create IAM (Identity Access Management) roles and policies

There are two methods for authenticating and connecting Loki to AWS S3. We will guide you through the recommended method of granting access via an IAM role.

{{< youtube id="5lXmWmofqwM" >}}

## Considerations

{{< admonition type="caution" >}}
This guide was accurate at the time it was last updated on **31st October, 2024**.  As cloud providers frequently update their services and offerings, as a best practice, you should refer to the [AWS S3 documentation](https://docs.aws.amazon.com/AmazonS3/latest/userguide/Welcome.html) before creating your buckets and assigning roles.
{{< /admonition >}}

- **IAM Role:** The IAM role created in this guide is a basic role that allows Loki to read and write to the S3 bucket. You may wish to add more granular permissions based on your requirements.

- **Authentication:** Grafana Loki comes with a basic authentication layer. The Loki gateway (NGINX) is exposed to the internet using basic authentication in this example. NGINX can also be replaced with other open-source reverse proxies. Refer to [Authentication](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/authentication/) for more information.

- **Retention:** The retention period is set to 28 days in the `values.yaml` file. You may wish to adjust this based on your requirements.

- **Costs:** Running Loki on AWS will incur costs. Make sure to monitor your usage and costs to avoid any unexpected bills. In this guide we have used a simple EKS cluster with 3 nodes and m5.xlarge instances. You may wish to adjust the instance types and number of nodes based on your workload.

## Prerequisites

- Helm 3 or above. Refer to [Installing Helm](https://helm.sh/docs/intro/install/). This should be installed on your local machine.
- A running Kubernetes cluster on AWS. A simple way to get started is by using EKSctl. Refer to [Getting started with EKSctl](https://eksctl.io/).
- Kubectl installed on your local machine. Refer to [Install and Set Up kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/).
- (Optional) AWS CLI installed on your local machine. Refer to [Installing the AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/install-cliv2.html). This is required if you plan to use EKSctl to create the EKS cluster and modify the IAM roles and policies locally.
  
### EKS Minimum Requirements

{{< admonition type="caution" >}}
These EKS requirements are the minimum specification needed to deploy Loki using this guide. You may wish to adjust plugins and instance types based on your AWS environment and workload. **If you choose to do so, we cannot guarantee that this sample configuration will still meet your needs.**

In this guide, we deploy Loki using `m5.xlarge` instances. This is a instance type that should work for most scenarios. However, you can modify the instance types and count based on your specific needs.
{{< /admonition >}}

The minimum requirements for deploying Loki on EKS are: 

- Kubernetes version `1.30` or above.
- `3` nodes for the EKS cluster.
- Instance type depends on your workload. A good starting point for a production cluster is `m7i.2xlarge`.

Here is the EKSctl cluster configuration file used in this guide:

```yaml
# A simple example of ClusterConfig object:
---
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig

metadata:
  name: <INSERT-CLUSTER-NAME>
  region: <INSERT-REGION-FOR-CLUSTER>
  version: "1.31"

iam:
  withOIDC: true

addons:
  - name: aws-ebs-csi-driver

managedNodeGroups:
  - name: loki-workers
    instanceType: m7i.2xlarge
    desiredCapacity: 3
    minSize: 2
    maxSize: 3
    amiFamily: AmazonLinux2023
    iam:
      withAddonPolicies:
        ebs: true
    volumeSize: 80
    volumeType: gp3
    ebsOptimized: true
```


The following plugins must also be installed within the EKS cluster:
- **Amazon EBS CSI Driver**: Enables Kubernetes to dynamically provision and manage EBS volumes as persistent storage for applications. We use this to provision the node volumes for Loki.
- **CoreDNS**: Provides internal DNS service for Kubernetes clusters, ensuring that services and pods can communicate with each other using DNS names. 
- **kube-proxy**: Maintains network rules on nodes, enabling communication between pods and services within the cluster.

You must also install an **OIDC (OpenID Connect) provider** on the EKS cluster. This is required for the IAM roles and policies to work correctly. If you are using EKSctl, you can install the OIDC provider using the following command:

{{< admonition type="tip" >}}
If you used the above EKSctl configuration file to create the cluster, you do not need to run this command. The OIDC provider is automatically installed.
{{< /admonition >}}

```bash
eksctl utils associate-iam-oidc-provider --cluster loki --approve
```

## Create S3 buckets

{{< admonition type="warning" >}}
 **DO NOT** use the default bucket names;  `chunk`, `ruler` and `admin`. Choose a **unique** name for each bucket. For more information see the following [security update](https://grafana.com/blog/2024/06/27/grafana-security-update-grafana-loki-and-unintended-data-write-attempts-to-amazon-s3-buckets/).
{{< /admonition >}}

Before deploying Loki, you need to create two S3 buckets; one to store logs (chunks), the second to store alert rules. You can create the bucket using the AWS Management Console or the AWS CLI. The bucket name must be globally unique.

{{< admonition type="note" >}}
GEL customers will require a third bucket to store the admin data. This bucket is not required for OSS users.
{{< /admonition >}}

```bash
aws s3api create-bucket --bucket  <YOUR CHUNK BUCKET NAME e.g. `loki-aws-dev-chunks`> --region <S3 region your account is on, e.g. `eu-west-2`> --create-bucket-configuration LocationConstraint=<S3 region your account is on, e.g. `eu-west-2`> \
aws s3api create-bucket --bucket  <YOUR RULER BUCKET NAME e.g. `loki-aws-dev-ruler`> --region <S3 REGION your account is on, e.g. `eu-west-2`> --create-bucket-configuration LocationConstraint=<S3 REGION your account is on, e.g. `eu-west-2`>
```
Make sure to replace the `region` and `bucket` name with your desired values. We will revisit the bucket policy later in this guide.

## Defining IAM roles and policies

The recommended method for connecting Loki to AWS S3 is to use an IAM role. This method is more secure than using access keys and secret keys which are directly stored in the Loki configuration. The role and policy can be created using the AWS CLI or the AWS Management Console. The below steps show how to create the role and policy using the AWS CLI.

1. Create a new directory and navigate to it. Make sure to create the files in this directory. All commands in this guide assume you are in this directory.

1. Create a `loki-s3-policy.json` file with the following content:

    ```json
    {
        "Version": "2012-10-17",
        "Statement": [
            {
                "Sid": "LokiStorage",
                "Effect": "Allow",
                "Action": [
                    "s3:ListBucket",
                    "s3:PutObject",
                    "s3:GetObject",
                    "s3:DeleteObject"
                ],
                "Resource": [
                    "arn:aws:s3:::< CHUNK BUCKET NAME >",
                    "arn:aws:s3:::< CHUNK BUCKET NAME >/*",
                    "arn:aws:s3:::< RULER BUCKET NAME >",
                    "arn:aws:s3:::< RULER BUCKET NAME >/*"
                ]
            }
        ]
    }
    ```

    **Make sure to replace the placeholders with the names of the buckets you created earlier.**

1. Create the IAM policy using the AWS CLI:

    ```bash
    aws iam create-policy --policy-name LokiS3AccessPolicy --policy-document file://loki-s3-policy.json
    ```

2. Create a trust policy document named `trust-policy.json` with the following content:

    ```json
    {
        "Version": "2012-10-17",
        "Statement": [
            {
                "Effect": "Allow",
                "Principal": {
                    "Federated": "arn:aws:iam::< ACCOUNT ID >:oidc-provider/oidc.eks.<INSERT REGION>.amazonaws.com/id/< OIDC ID >"
                },
                "Action": "sts:AssumeRoleWithWebIdentity",
                "Condition": {
                    "StringEquals": {
                        "oidc.eks.<INSERT REGION>.amazonaws.com/id/< OIDC ID >:sub": "system:serviceaccount:loki:loki",
                        "oidc.eks.<INSERT REGION>.amazonaws.com/id/< OIDC ID >:aud": "sts.amazonaws.com"
                    }
                }
            }
        ]
    }
    ```
   **Make sure to replace the placeholders with your AWS account ID, region, and the OIDC ID (you can find this in the EKS cluster configuration).**

3. Create the IAM role using the AWS CLI:

    ```bash
    aws iam create-role --role-name LokiServiceAccountRole --assume-role-policy-document file://trust-policy.json
    ```

4. Attach the policy to the role:

    ```bash
    aws iam attach-role-policy --role-name LokiServiceAccountRole --policy-arn arn:aws:iam::<Account ID>:policy/LokiS3AccessPolicy
    ```
    **Make sure to replace the placeholder with your AWS account ID.**

## Deploying the Helm chart

Before we can deploy the Loki Helm chart, we need to add the Grafana chart repository to Helm. This repository contains the Loki Helm chart.

1. Add the Grafana chart repository to Helm:

    ```bash
    helm repo add grafana https://grafana.github.io/helm-charts
    ```
1. Update the chart repository:

    ```bash
    helm repo update
    ```
1. Create a new namespace for Loki:

    ```bash
    kubectl create namespace loki
    ```
### Loki Basic Authentication

Loki by default does not come with any authentication. Since we will be deploying Loki to AWS and exposing the gateway to the internet, we recommend adding at least basic authentication. In this guide we will give Loki a username and password:

1. To start we will need create a `.htpasswd` file with the username and password. You can use the `htpasswd` command to create the file:

   {{< admonition type="tip" >}}
    If you don't have the `htpasswd` command installed, you can install it using `brew` or `apt-get` or `yum` depending on your OS.
   {{< /admonition >}}

    ```bash
    htpasswd -c .htpasswd <username>
    ```
    This will create a file called `auth` with the username `loki`. You will be prompted to enter a password.

 1. Create a Kubernetes secret with the `.htpasswd` file:

    ```bash
    kubectl create secret generic loki-basic-auth --from-file=.htpasswd -n loki
    ```

    This will create a secret called `loki-basic-auth` in the `loki` namespace. We will reference this secret in the Loki Helm chart configuration.
  
1. Create a `canary-basic-auth` secret for the canary:

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
         object_store: s3
         schema: v13
         index:
           prefix: loki_index_
           period: 24h
   storage_config:
     aws:
       region: <S3 BUCKET REGION> # for example, eu-west-2  
       bucketnames: <CHUNK BUCKET NAME> # Your actual S3 bucket name, for example, loki-aws-dev-chunks
       s3forcepathstyle: false
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
     delete_request_store: s3
   ruler:
    enable_api: true
    storage:
      type: s3
      s3:
        region: <S3 BUCKET REGION> # for example, eu-west-2
        bucketnames: <RULER BUCKET NAME> # Your actual S3 bucket name, for example, loki-aws-dev-ruler
        s3forcepathstyle: false
      alertmanager_url: http://prom:9093 # The URL of the Alertmanager to send alerts (Prometheus, Mimir, etc.)

   querier:
      max_concurrent: 4

   storage:
      type: s3
      bucketNames:
        chunks: "<CHUNK BUCKET NAME>" # Your actual S3 bucket name (loki-aws-dev-chunks)
        ruler: "<RULER BUCKET NAME>" # Your actual S3 bucket name (loki-aws-dev-ruler)
        # admin: "<Insert s3 bucket name>" # Your actual S3 bucket name (loki-aws-dev-admin) - GEL customers only
      s3:
        region: <S3 BUCKET REGION> # eu-west-2
        #insecure: false
      # s3forcepathstyle: false

serviceAccount:
 create: true
 annotations:
   "eks.amazonaws.com/role-arn": "arn:aws:iam::<Account ID>:role/LokiServiceAccountRole" # The service role you created

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

It is critical to define a valid `values.yaml` file for the Loki deployment. To remove the risk of misconfiguration, let's break down the configuration options to keep in mind when deploying to AWS:

- **Loki Config vs. Values Config:**
  - The `values.yaml` file contains a section called `loki`, which contains a direct representation of the Loki configuration file.
  - This section defines the Loki configuration, including the schema, storage, and querier configuration.
  - The key configuration to focus on for chunks is the `storage_config` section, where you define the S3 bucket region and name. This tells Loki where to store the chunks.
  - The `ruler` section defines the configuration for the ruler, including the S3 bucket region and name. This tells Loki where to store the alert and recording rules.
  - For the full Loki configuration, refer to the [Loki Configuration](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/) documentation.

- **Storage:**
  - Defines where the Helm chart stores data.
  - Set the type to `s3` since we are using Amazon S3.
  - Configure the bucket names for the chunks and ruler to match the buckets created earlier.
  - The `s3` section specifies the region of the bucket.

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

1. Verify the deployment:

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
loki-gateway                     LoadBalancer   10.100.201.74    12345678975675456-1433434453245433545656563.eu-west-2.elb.amazonaws.com   80:30707/TCP         46m
```

Congratulations! You have successfully deployed Loki on AWS using the Helm chart. Before we finish, let's test the deployment.

## Testing Your Loki Deployment

k6 is one of the fastest ways to test your Loki deployment. This will allow you to both write and query logs to Loki. To get started with k6, follow the steps below:

1. Install k6 with the Loki extension on your local machine. Refer to [Installing k6 and the xk6-loki extension](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/k6/).

2. Create a `aws-test.js` file with the following content:

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
    ./k6 run aws-test.js
    ```

    This will run the test and output the results. You should see the test writing logs to Loki and querying logs from Loki.

Now that you have successfully deployed Loki in microservices mode on AWS, you may wish to explore the following:

- [Sending data to Loki](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/)
- [Querying Loki](https://grafana.com/docs/loki/<LOKI_VERSION>/query/)
- [Manage](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/)
