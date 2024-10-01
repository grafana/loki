---
title: Deploy the Simple Scalable Helm chart on AWS
menuTitle: Deploy on AWS
description: Installing Loki in Simple Scalable mode using the Helm chart on AWS.
aliases:
  - ../../../installation/helm/scalable/
  - ../../../installation/helm/install-scalable/
weight: 300
keywords: 
---

# Deploy the Simple Scalable Helm chart on AWS

This guide shows how to deploy a minimally viable Loki in simple scalable mode on AWS using the Helm chart. To run through this guide, we expect you to have the necessary tools and permissions to deploy resources on AWS, such as:

- Full access to EKS (Amazon Elastic Kubernetes Service)
- Full access to S3 (Amazon Simple Storage Service)
- Sufficient permissions to create IAM roles and policies

There are two methods for authenticating and connecting Loki to AWS S3. We will guide you through the recommended method of granting access via an IAM role.

## Prerequisites

- Helm 3 or above. Refer to [Installing Helm](https://helm.sh/docs/intro/install/). This should be installed on your local machine.
- A running Kubernetes cluster on AWS. A simple way to get started is by using EKSctl. See [Getting started with EKSctl](https://eksctl.io/).
- Kubectl installed on your local machine. Refer to [Install and Set Up kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/).
- (Optional) AWS CLI installed on your local machine. Refer to [Installing the AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/install-cliv2.html). This is required if you plan to use EKSctl to create the EKS cluster and modify the IAM roles and policies locally.
  
### EKS Minimum Requirements

{{< admonition type="warning" >}}
These EKS requirements are the minimum specification needed to deploy Loki using this guide. You may wish to adjust plugins and instance types based on your AWS environment and workload. **If you choose to do so, this may invalidate the guide.**

In this guide, we deploy Loki using `m5.xlarge` instances. This is a middle-of-the-road instance type that should work for most scenarios. However, you can modify the instance types and count based on your specific needs.
{{< /admonition >}}

Since EKS is manged and maintained by AWS, we can't provide a definitive method to deploying a cluster on EKS. However, we can provide you with the minimum requirements for deploying Loki on EKS: 

- Kubernetes version `1.30` or above.
- `3` nodes for the EKS cluster.
- Instance type depends on your workload. A good starting point is `m5.xlarge`.

Here is the EKSctl cluster configuration file used in this guide:

```yaml
# A simple example of ClusterConfig object:
---
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig

metadata:
  name: loki
  region: eu-west-2
  version: "1.31"

iam:
  withOIDC: true

addons:
  - name: aws-ebs-csi-driver
  - name: eks-pod-identity-agent

managedNodeGroups:
  - name: loki-workers
    instanceType: m5.xlarge
    desiredCapacity: 3
    minSize: 2
    maxSize: 3
    amiFamily: AmazonLinux2
    iam:
      withAddonPolicies:
        ebs: true
    volumeSize: 80
    volumeType: gp2
    ebsOptimized: true

```


The following plugins are also required to be installed within the EKS cluster:
- Amazon EBS CSI driver
- Amazon EKS Pod Identity Agent
- CoreDNS
- kube-proxy

You must also install an OIDC provider on the EKS cluster. This is required for the IAM roles and policies to work correctly. If you are using EKSctl, you can install the OIDC provider using the following command:

```bash
eksctl utils associate-iam-oidc-provider --cluster loki --approve
```
**This may be installed by default when creating the EKS cluster using EKSctl.**

## Create an S3 bucket

Before deploying Loki, you need to create an S3 bucket to store the logs. You can create the bucket using the AWS Management Console or the AWS CLI. The bucket name must be globally unique. For this guide, we will use the bucket name `loki-aws-bucket`.

```bash
aws s3api create-bucket --bucket loki-aws-bucket --region eu-west-2 --create-bucket-configuration LocationConstraint=eu-west-2
```
Make sure to replace the region and bucket name with your desired values. We will revisit the bucket policy later in this guide.



## Defining IAM roles and policies

The recommended method for connecting Loki to AWS S3 is to use an IAM role. This method is more secure than using access keys and secret keys which are directly stored in the Loki configuration. The role and policy can be created using the AWS CLI or the AWS Management Console. The below steps show how to create the role and policy using the AWS CLI.

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
                    "arn:aws:s3:::< Name of the bucket >",
                    "arn:aws:s3:::< Name of the bucket >/*"
                ]
            }
        ]
    }
    ```

    Make sure to replace the placeholder with the name of the bucket you created earlier.

1. Create the IAM policy using the AWS CLI:

    ```bash
    aws iam create-policy --policy-name LokiS3AccessPolicy --policy-document file://loki-s3-policy.json
    ```

1. Create a trust policy document named `trust-policy.json` with the following content:

    ```json
    {
        "Version": "2012-10-17",
        "Statement": [
            {
                "Effect": "Allow",
                "Principal": {
                    "Federated": "arn:aws:iam::< ACCOUNT ID >:oidc-provider/oidc.eks.<INSERT REGION>.amazonaws.com/id/< ID >"
                },
                "Action": "sts:AssumeRoleWithWebIdentity",
                "Condition": {
                    "StringEquals": {
                        "oidc.eks.<INSERT REGION>.amazonaws.com/id/< ID >:sub": "system:serviceaccount:loki:loki"
                    }
                }
            }
        ]
    }
    ```
    **Note: that you need to replace the placeholders with your AWS account ID, region, and the OIDC ID (you can find this in the EKS cluster configuration).**

1. Create the IAM role using the AWS CLI:

    ```bash
    aws iam create-role --role-name LokiServiceAccountRole --assume-role-policy-document file://trust-policy.json
    ```

1. Attach the policy to the role:

    ```bash
    aws iam attach-role-policy --role-name LokiServiceAccountRole --policy-arn arn:aws:iam::<Account ID>:policy/LokiS3AccessPolicy
    ```
    Make sure to replace the placeholder with your AWS account ID.

### Adding the policy to our S3 bucket

To allow the IAM role to access the S3 bucket, you need to add the policy to the bucket. You can do this using the AWS Management Console or the AWS CLI. The below steps show how to add the policy using the AWS CLI.

1. Create a bucket policy file named `bucket-policy.json` with the following content:

    ```json
    {
        "Version": "2012-10-17",
        "Statement": [
            {
                "Sid": "Statement1",
                "Effect": "Allow",
                "Principal": {
                    "AWS": "arn:aws:iam::<account id>:role/LokiServiceAccountRole"
                },
                "Action": [
                    "s3:PutObject",
                    "s3:GetObject",
                    "s3:DeleteObject",
                    "s3:ListBucket"
                ],
                "Resource": [
                    "arn:aws:s3:::<bucket name>",
                    "arn:aws:s3:::<bucket name>/*"
                ]
            }
        ]
    }
    ```
    **Note: that you need to replace the placeholders with your AWS account ID and the bucket name.**

1. Add the policy to the bucket:

    ```bash
    aws s3api put-bucket-policy --bucket loki-aws-bucket --policy file://bucket-policy.json
    ```

## Deploying the Helm chart

1. Add the Grafana chart repository to Helm:

    ```bash
    helm repo add grafana https://grafana.github.io/helm-charts
    ```
1. Update the chart repository:

    ```bash
    helm repo update
    ```

1. Create the `loki` namespace:

    ```bash
    kubectl create namespace loki
    ```
    **This is important as our trust policy is set to allow the role to be used by the `loki` service account in the `loki` namespace.**

### Loki Helm chart configuration

Create a `values.yaml` file with the following content:

```yaml
 loki:
   schemaConfig:
     configs:
       - from: 2024-04-01
         store: tsdb
         object_store: s3
         schema: v13
         index:
           prefix: loki_index_
           period: 24h
   storage_config:
     aws:
       region: <Insert s3 bucket region> # eu-west-2
       bucketnames: <Insert s3 bucket name> # Your actual S3 bucket name (loki-aws-bucket)
       s3forcepathstyle: false
   pattern_ingester:
       enabled: true
   limits_config:
     allow_structured_metadata: true
     volume_enabled: true
     retention_period: 672h # 28 days retention
   compactor:
     retention_enabled: true 
     delete_request_store: s3
     
       
   querier:
     max_concurrent: 4

   storage:
     type: s3
     bucketNames:
       chunks: "<Insert s3 bucket name>" # Your actual S3 bucket name (loki-aws-bucket)
       ruler: "<Insert s3 bucket name>" # Your actual S3 bucket name (loki-aws-bucket)
       admin: "<Insert s3 bucket name>" # Your actual S3 bucket name (loki-aws-bucket)
     s3:
       region: <Insert s3 bucket region> # eu-west-2
       #insecure: false
     # s3forcepathstyle: false
       
       
 deploymentMode: SimpleScalable

 serviceAccount:
   create: true
   annotations:
     "eks.amazonaws.com/role-arn": "arn:aws:iam::<Account ID>:role/LokiServiceAccountRole" # The service role you created

 backend:
   replicas: 2
   persistence:
     storageClass: gp2
     accessModes:
       - ReadWriteOnce
     size: 10Gi
 read:
   replicas: 2
   persistence:
     storageClass: gp2
     accessModes:
       - ReadWriteOnce
     size: 10Gi
 write:
   replicas: 3
   persistence:
     storageClass: gp2
     accessModes:
       - ReadWriteOnce
     size: 10Gi

 # This exposes the Loki gateway so it can be written to and queried externaly
 gateway:
   service:
     type: LoadBalancer


 # Enable minio for storage
 minio:
   enabled: false
 
```

It is critical to define a valid `values.yaml` file for the Loki deployment. To remove the risk of misconfiguration, let's break down the configuration options to keep in mind when deploying to AWS:

- **Loki Config vs. Values Config:**
  - The `values.yaml` file contains a section called `loki`, which contains a direct representation of the Loki configuration file.
  - This section defines the Loki configuration, including the schema, storage, and querier configuration.
  - The key configuration to focus on is the `storage_config` section, where you define the S3 bucket region and name.

- **Storage:**
  - Defines where the Helm chart stores data.
  - Set the type to `s3` since we are using Amazon S3.
  - Configure the bucket names for the chunks, ruler, and admin to match the bucket name created earlier.
  - The `s3` section specifies the region of the bucket.

- **Service Account:**
  - The `serviceAccount` section is used to define the IAM role for the Loki service account.
  - This is where the IAM role created earlier is linked.

- **Gateway:**
  - Defines how the Loki gateway will be exposed.
  - We are using a `LoadBalancer` service type in this configuration.

**Important:** 
  - Make sure to replace the placeholders with your actual values.

### Deploy Loki

Now that you have created the `values.yaml` file, you can deploy Loki using the Helm chart.

1. Deploy using the newly created `values.yaml` file:

    ```bash
    helm install --values values.yaml loki grafana/loki
    ```
    **Note: this may take a few minutes to complete.**

1. Verify the deployment:

    ```bash
    kubectl get pods -n loki
    ```
    You should see the Loki pods running.
    ```console
    NAME                           READY   STATUS    RESTARTS   AGE
    loki-backend-0                 2/2     Running   0          43m
    loki-backend-1                 2/2     Running   0          43m
    loki-canary-c7hbw              1/1     Running   0          43m
    loki-canary-wtxb8              1/1     Running   0          43m
    loki-chunks-cache-0            2/2     Running   0          43m
    loki-gateway-844ff6d84-9zg5k   1/1     Running   0          43m
    loki-read-848c686c9d-kd9xt     1/1     Running   0          43m
    loki-read-848c686c9d-vx697     1/1     Running   0          43m
    loki-results-cache-0           2/2     Running   0          43m
    loki-write-0                   1/1     Running   0          43m
    loki-write-1                   1/1     Running   0          43m
    loki-write-2                   1/1     Running   0          43m
    ```
  
### Find the Loki Gateway Service

To find the Loki Gateway service, run the following command:

```bash
kubectl get svc -n loki
```
You should see the Loki Gateway service with an external IP address. This is the address you will use to write to and query Loki.

```console
  NAME                             TYPE           CLUSTER-IP       EXTERNAL-IP                                                               PORT(S)              AGE
loki-backend                     ClusterIP      10.100.97.223    <none>                                                                    3100/TCP,9095/TCP    46m
loki-backend-headless            ClusterIP      None             <none>                                                                    3100/TCP,9095/TCP    46m
loki-canary                      ClusterIP      10.100.121.159   <none>                                                                    3500/TCP             46m
loki-chunks-cache                ClusterIP      None             <none>                                                                    11211/TCP,9150/TCP   46m
loki-gateway                     LoadBalancer   10.100.201.74    12345678975675456-1433434453245433545656563.eu-west-2.elb.amazonaws.com   80:30707/TCP         46m
loki-memberlist                  ClusterIP      None             <none>                                                                    7946/TCP             46m
loki-query-scheduler-discovery   ClusterIP      None             <none>                                                                    3100/TCP,9095/TCP    46m
loki-read                        ClusterIP      10.100.187.114   <none>                                                                    3100/TCP,9095/TCP    46m
loki-read-headless               ClusterIP      None             <none>                                                                    3100/TCP,9095/TCP    46m
loki-results-cache               ClusterIP      None             <none>                                                                    11211/TCP,9150/TCP   46m
loki-write                       ClusterIP      10.100.217.163   <none>                                                                    3100/TCP,9095/TCP    46m
loki-write-headless              ClusterIP      None             <none>                                                                    3100/TCP,9095/TCP    46m
```

## Testing You're Loki Deployment

k6 is one of the fastest way to test your Loki deployment. This will allow you to both write and query logs to Loki. To get started with k6, follow the steps below:

1. Install k6 with the Loki extension on your local machine. Refer to [Installing k6 and the xk6-loki extension]({{< relref "../../../../send-data/k6" >}}).

2. Create a `aws-test.js` file with the following content:

   ```javascript
    import {sleep, check} from 'k6';
    import loki from 'k6/x/loki';

    /**
    * URL used for push and query requests
    * Path is automatically appended by the client
    * @constant {string}
    */
    const BASE_URL = `http://<EXTERNAL-IP>/loki/api/v1/push`;

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

    /**
    * "main" function for each VU iteration
    */
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

    Replace `<EXTERNAL-IP>` with the external IP address of the Loki Gateway service.

## Considerations

- **IAM Role:** The IAM role created in this guide is a basic role that allows Loki to read and write to the S3 bucket. You may wish to add more granular permissions based on your requirements.

- **Authentication:** Grafana Loki doesn't come with any included authentication layer. Within the simple scalable mode, the Loki gateway (NGINX) is exposed to the internet. It is advised to add a username and password via the `basicAuth` section in the `values.yaml` file. NGINX can also be replaced with other open-source reverse proxies. Refer to [Authentication]({{< relref "../../../../operations/authentication" >}}) for more information.

- **Retention:** The retention period is set to 28 days in the `values.yaml` file. You may wish to adjust this based on your requirements.

- **Costs:** Running Loki on AWS will incur costs. Make sure to monitor your usage and costs to avoid any unexpected bills. In this guide we have used a simple EKS cluster with 3 nodes and m5.xlarge instances. You may wish to adjust the instance types and number of nodes based on your workload.

- **Guide:** Note that this guide was created on the first `1st of October 2024`. Cloud providers are constantly updating their services and offerings. This may mean that some of the steps require updating.

## Next steps

If you have successfully deployed Loki in simple scalable mode on AWS, you may wish to explore the following:

- [Sanding data to Loki]({{< relref "../../../../send-data" >}})
- [Querying Loki]({{< relref "../../../../query" >}})
- [Operations]({{< relref "../../../../operations" >}})