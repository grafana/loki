---
title: Short-Lived Token Authentication
authors:
  - "@periklis"
reviewers:
  - "@xperimental"
  - "@JoaoBraveCoding"
  - "@btaani"
creation-date: 2023-10-26
last-updated: 2023-10-26
draft: false
menu:
  docs:
    parent: "enhancements"
weight: 100
toc: true
---

## Summary

LokiStack object storage access on all public cloud providers supports currently only providing credentials for a static cloud service account (i.e. a pair of client id and secret). Provisioning static cloud service account represent a simple and automation-friendly approach to control access to each provider's resources (e.g. access to S3/GCS/etc.). However the administrator of such needs to consider manual handling of certain security aspects, i.e. secret rotation, account rotation on expiry, etc.

To enhance IAM automation across the entire lifetime of access credentials all public cloud providers offer specific services (e.g. named STS, Workload Identity Federation):
1. Automate creation and rotation of credentials per Kubernetes workload using one OIDC authorization server per Kubernetes cluster for it's service accounts.
2. Each workload on the managed Kubernetes cluster must be bound to a specific IAM role to access any provider service (e.g. S3/GCS/Azure Storage)
3. The OIDC-based workflow ensures issuing only short-lived tokens to each workload and in turn frequent credentials rotation.

Such an approach ensures that each Kubernetes workload requests access to the IAM controlled resources by it's Kubernetes service account only. Each valid request is further secured by providing only rotated short lived tokens. Thus any Kubernetes workload access can be controlled through the same IAM mechanisms as with static service accounts (e.g. disable on security breaches) plus using automatic rotation to minimize the impact vector on security incidents.

The following proposal discusses the implemented support of the above workflow in the Loki Operator for all three public cloud providers (AWS, GCP, Azure). Furthermore it provides instructions on how to create IAM resources upfront to control LokiStack object storage access using AWS STS or GCP Workload Identity or Azure Workload Identity Federation.

__Note:__ Short-lived Token authentication is picked in this proposal as a generic term that maps well enough to the following public cloud providers IAM offerings:
- Azure: [Workload Identity Federation](https://learn.microsoft.com/en-us/entra/workload-id/workload-identity-federation)
- AWS: [Secure Service Token](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_credentials_temp.html)
- Google Cloud Platform: [Workload Identity Federation](https://cloud.google.com/iam/docs/workload-identity-federation)

## Motivation

Building upon the fact that short-lived token authentication is supported by all three object storage clients in Loki (Azure, AWS, GAP), this proposal's main focus is to automate the administration effort to run LokiStack using such an authentication workflow for object storage access.

### Goals

- The LokiStack administrator provides only minimal but valid object storage secrets that enable short-lived token authentication for all three major cloud providers.
- The LokiStack administrator is not required to expose credentials/identity-tokens/etc. to the cloud provider when operating in that workflow.

### Non-Goals

- Provide auxiliary and comprehensive tooling to automate preparing the cloud provider IAM resources for short-lived token authentication.

## Proposal

The following proposal describes the required changes and validations for each object storage secret for each major cloud provider (Azure, AWS, GCP).

### API Extensions

The implementation of this proposal does not require any custom resource extension or such.

### Implementation Details/Notes/Constraints

The following sections describe the required changes in LokiStack's object storage secret format for each object storage client to enable short-lived token authentication

#### Azure Workload Identity Federation

##### Overview

The current Azure object storage secret requires the following mandatory set of fields to be available:

```yaml
data:
  environment:  # The Azure Storage account environment
  container:    # The Azure Storage account container
  account_name: # The Azure Storage account name
  account_key:  # The Azure Storage account key
```

In contrast, a minimal configuration set of fields for short-lived token authentication requires:

```yaml
data:
  environment:     # The Azure Storage account environment
  container:       # The Azure Storage account container
  account_name:    # The Azure Storage account name
  client_id:       # The Azure Workload Identity's Client ID
  tenant_id:       # The Azure Account's Tenant ID holding the workload identity for LokiStack
  subscription_id: # The Azure Account's Subscription ID holding the workload identity for LokiStack
  region:          # The Azure Location hosting the Kubernetes cluster and in turn LokiStack
```

##### Pre-requisites

The LokiStack administrator is required to create a custom Azure Managed Identity and associated federated credentials to trust LokiStack's Kubernetes ServiceAccount.

1. Create an Azure Managed Identity on the same resource group as the Kubernetes cluster hosting LokiStack:

```shell
az identity create \
  --name $IDENTITY_NAME \
  --resource-group $RESOURCE_GROUP_NAME \
  --location $LOCATION \
  --subscription $SUBSCRIPTION_ID
```

2. Create two Federated Credentials for scenario `Kubernetes accessing Azure resources`:

```shell
az identity federated-credential create \
  --name openshift-logging-lokistack \
  --identity-name $IDENTITY_NAME \
  --resource-group $RESOURCE_GROUP_NAME \
  --issuer $CLUSTER_ISSUER_URL \
  --subject system:serviceaccount:$LOKISTACK_NS:$LOKISTACK_NAME \
  --audiences $AUDIENCES

az identity federated-credential create \
  --name openshift-logging-lokistack-ruler \
  --identity-name $IDENTITY_NAME \
  --resource-group $RESOURCE_GROUP_NAME \
  --issuer $CLUSTER_ISSUER_URL \
  --subject system:serviceaccount:$LOKISTACK_NS:$LOKISTACK_NAME-ruler \
  --audiences $AUDIENCES
```

__Note:__ To enable the required federated credential scenario in the above command the subject needs to be of the form: `system:serviceaccount:<NAMESPACE>:<SA_NAME>`. The above creates two federated credentials because the operator reconciles two Kubernetes serviceaccounts one common for all Loki pods and one for the Loki Ruler pod. The issuer and audiences are related to the Kubernetes cluster hosting LokiStack. Audience can be set to the Azure default `api://AzureADTokenExchange`.

3. Assign the above managed identity to the Azure role `Storage Blob Data Contributor`:

```shell
az role assignment create \
  --assignee "$MANAGED_IDENTITY_ID" \
  --role "Storage Blob Data Contributor" \
  --scope "/subscriptions/$SUBSCRIPTION_ID"
```

__Note:__ To lookup the managed identity id you can use the following command:

```shell
az ad sp list --all --filter "servicePrincipalType eq 'ManagedIdentity'"
```

#### AWS Secure Token Service

##### Overview

The current S3 object storage secret requires the following mandatory set of fields to be available:

```yaml
data:
  bucketnames:       # A comma-separated list of bucket names
  access_key_id:     # The AWS static service account's key ID
  access_key_secret: # The AWS static service account's key secret
  endpoint:          # The AWS endpoint URL.
```

In contrast, a minimal configuration set of fields for short-lived token authentication requires:

```yaml
data:
  bucketnames: # A comma-separated list of bucket names
  region:      # A valid AWS region, e.g. us-east-1
  role_arn:    # The AWS IAM Role associated with a trust relationship to Lokistack's serviceaccount
```

##### Pre-requisites

The LokiStack administrator is required to create a custom AWS IAM Role associated with a trust relationship to the LokiStack's Kubernetes ServiceAccount

1. Trust relationship: Ensures that each Lokistack container authenticating to AWS STS is using as identity it's serviceaccount token.

```json
{
 "Version": "2012-10-17",
 "Statement": [
   {
     "Effect": "Allow",
     "Principal": {
       "Federated": "arn:aws:iam::${AWS_ACCOUNT_ID}:oidc-provider/${OIDC_PROVIDER}"
     },
     "Action": "sts:AssumeRoleWithWebIdentity",
     "Condition": {
       "StringEquals": {
         "${OIDC_PROVIDER}:sub": [
           "system:serviceaccount:${LOKISTACK_NS}:${LOKISTACK_NAME}"
           "system:serviceaccount:${LOKISTACK_NS}:${LOKISTACK_NAME}-ruler"
         ]
       }
     }
   }
 ]
}
```

__Note:__ To enable the required trust relationship scenario in the above command the subject needs to be of the form: `system:serviceaccount:<NAMESPACE>:<SA_NAME>`.

2. Create an AWS IAM role:

```shell
aws iam create-role \
  --role-name "my-lokistack-s3-access" \
  --assume-role-policy-document file:///tmp/trust.json \
  --query Role.Arn \
  --output text
```

3. Attach a specific policy that role:

```shell
aws iam attach-role-policy \
  --role-name "my-lokistack-s3-access" \
  --policy-arn "arn:aws:iam::aws:policy/AmazonS3FullAccess"
```

#### GCP Workload Identity Federation

There is no change required for GCP WIF as a provider service for short-lived token authentication. However specific validation of the provided `serviceaccount.json` field will ensure that WIF is used only with GCP external services accounts, i.e. the following format will be validated:

```json
{
   "type": "external_account",
   "audience": "//iam.googleapis.com/projects/123456789/locations/global/workloadIdentityPools/test-pool/providers/test-provider",
   "subject_token_type": "urn:ietf:params:oauth:token-type:jwt",
   "token_url": "https://sts.googleapis.com/v1/token",
   "service_account_impersonation_url": "https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/test-service-account-42ssv@test-project.iam.gserviceaccount.com:generateAccessToken",
   "credential_source": {
      "file": "/path/to/oidc/token",
      "format": {
         "type": "text"
      }
   }
}
```

__Note:__ As a credentials source file the Loki Operator will overwrite any user provider input with using the Kubernetes ServiceAccount token per container.

##### Pre-requisites

The LokiStack administrator is required to create a custom Google Managed Identity and an associated credentials configuration to trust the LokiStack's Kubernetes ServiceAccount.

1. Create a GCP service account to be used by the LokiStack to access GCP resources:

```shell
gcloud iam service-accounts create "$SERVICE_ACCOUNT_NAME" \
  --display-name="Loki Operator Account" \
  --project "$PROJECT_ID"
```

2. Bind the minimal set of GCP roles `roles/iam.workloadIdentityUser` and `roles/storage.objectAdmin` to the newly created GCP serviceaccount:

```shell
gcloud projects add-iam-policy-binding "$project_id" \
  --member="serviceAccount:$SERVICE_ACCOUNT_EMAIL" \
  --role="roles/iam.workloadIdentityUser"\
  --format=none

gcloud projects add-iam-policy-binding "$project_id" \
  --member="serviceAccount:$SERVICE_ACCOUNT_EMAIL" \
  --role="roles/storage.objectAdmin" \
  --format=none
```

and the LokiStack serviceaccounts:

```shell
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:$SERVICE_ACCOUNT_EMAIL" \
  --role="roles/iam.workloadIdentityUser" \
  --member="principal://iam.googleapis.com/projects/$PROJECT_NUMBER/locations/global/workloadIdentityPools/$POOL_ID/subject/system:serviceaccount:${LOKISTACK_NS}:${LOKISTACK_NAME}"

gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:$SERVICE_ACCOUNT_EMAIL" \
  --role="roles/storage.objectAdmin" \
  --member="principal://iam.googleapis.com/projects/$PROJECT_NUMBER/locations/global/workloadIdentityPools/$POOL_ID/subject/system:serviceaccount:${LOKISTACK_NS}:${LOKISTACK_NAME}"

gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:$SERVICE_ACCOUNT_EMAIL" \
  --role="roles/iam.workloadIdentityUser" \
  --member="principal://iam.googleapis.com/projects/$PROJECT_NUMBER/locations/global/workloadIdentityPools/$POOL_ID/subject/system:serviceaccount:${LOKISTACK_NS}:${LOKISTACK_NAME}-ruler"

gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:$SERVICE_ACCOUNT_EMAIL" \
  --role="roles/storage.objectAdmin" \
  --member="principal://iam.googleapis.com/projects/$PROJECT_NUMBER/locations/global/workloadIdentityPools/$POOL_ID/subject/system:serviceaccount:${LOKISTACK_NS}:${LOKISTACK_NAME}-ruler"
```

__Note:__ To enable the required membership scenario in the above commands the subject needs to be of the form: `system:serviceaccount:<NAMESPACE>:<SA_NAME>`. The workload identity pool needs to be the same that manages your other Kubernetes cluster's managed identities.

3. Create a credentials configuration file for the managed identity to be used by LokiStack:

```shell
gcloud iam workload-identity-pools create-cred-config \
    "projects/$PROJECT_NUMBER/locations/global/workloadIdentityPools/$POOL_ID/providers/$PROVIDER_ID" \
    --service-account="$SERVICE_ACCOUNT_EMAIL" \
    --credential-source-file=/var/run/secrets/serviceaccount/token \
    --credential-source-type=text \
    --output-file="/tmp/google-application-credentials.json"
```

__Note:__ The workload identity pool and associated OIDC provider needs to be the same that manages your other Kubernetes cluster's managed identities.

4. Use the contents from `/tmp/google-application-credentials.json` as `key.json` when creating the GCS object storage secret for LokiStack:

```yaml
data:
  bucketname: # A comma-separated list of bucket names
  key.json:   # The file contents of the newly created GCP credentials configuration
```

## Implementation History

- https://github.com/grafana/loki/pull/11481
